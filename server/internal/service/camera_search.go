package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// CameraSearchService 摄像头事件混合搜索服务。
// SQL 结构化过滤 + pgvector 向量搜索。
type CameraSearchService struct {
	store *store.Store
}

// NewCameraSearchService 创建混合搜索服务。
func NewCameraSearchService(s *store.Store) *CameraSearchService {
	return &CameraSearchService{store: s}
}

// SearchCondition 混合搜索条件
type SearchCondition struct {
	// EventIDs 由服务端资源授权注入，不接受模型输入，用于强制限定可访问事件。
	EventIDs     []int64   `json:"-"`
	AgentID      int64     `json:"-"`      // 服务端注入：按智能体直接归属隔离摄像头事件（agent_id 列）
	KnowledgeID  int64     `json:"-"`      // 服务端注入：按单知识库隔离摄像头事件（knowledge_id 列）
	KnowledgeIDs []int64   `json:"-"`      // 服务端注入：按多个知识库隔离摄像头事件（智能体绑定多个知识库时）
	CameraIDs    []int64   `json:"cameraIds"`
	StartTime   time.Time `json:"startTime"`
	EndTime     time.Time `json:"endTime"`
	HasPerson   *bool     `json:"hasPerson"`
	HasVehicle  *bool     `json:"hasVehicle"`
	HasPet      *bool     `json:"hasPet"`
	HasPackage  *bool     `json:"hasPackage"`
	VehicleType string    `json:"vehicleType"`
	PetType     string    `json:"petType"`
	Action      string    `json:"action"`
	Colors      []string  `json:"colors"`
	Zone        string    `json:"zone"`
}

// HybridSearch 混合搜索：SQL 结构化过滤 + 向量相似度排序。
func (s *CameraSearchService) HybridSearch(ctx context.Context, embedding []float64, condition *SearchCondition, topK int, threshold float64) ([]model.CameraEventSearchResult, error) {
	db := s.store.DB().WithContext(ctx)

	// 构建 SQL 条件
	whereClauses := []string{}
	args := []interface{}{}

	if len(condition.EventIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("id IN (%s)", placeholders(len(condition.EventIDs))))
		for _, id := range condition.EventIDs {
			args = append(args, id)
		}
	}
	if len(condition.CameraIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("camera_id IN (%s)", placeholders(len(condition.CameraIDs))))
		for _, id := range condition.CameraIDs {
			args = append(args, id)
		}
	}
	if condition.AgentID > 0 {
		whereClauses = append(whereClauses, "agent_id = ?")
		args = append(args, condition.AgentID)
	}
	if len(condition.KnowledgeIDs) > 0 {
		whereClauses = append(whereClauses, fmt.Sprintf("knowledge_id IN (%s)", placeholders(len(condition.KnowledgeIDs))))
		for _, id := range condition.KnowledgeIDs {
			args = append(args, id)
		}
	} else if condition.KnowledgeID > 0 {
		whereClauses = append(whereClauses, "knowledge_id = ?")
		args = append(args, condition.KnowledgeID)
	}
	if !condition.StartTime.IsZero() {
		whereClauses = append(whereClauses, "event_time >= ?")
		args = append(args, condition.StartTime)
	}
	if !condition.EndTime.IsZero() {
		whereClauses = append(whereClauses, "event_time <= ?")
		args = append(args, condition.EndTime)
	}
	if condition.HasPerson != nil {
		whereClauses = append(whereClauses, "has_person = ?")
		args = append(args, *condition.HasPerson)
	}
	if condition.HasVehicle != nil {
		whereClauses = append(whereClauses, "has_vehicle = ?")
		args = append(args, *condition.HasVehicle)
	}
	if condition.HasPet != nil {
		whereClauses = append(whereClauses, "has_pet = ?")
		args = append(args, *condition.HasPet)
	}
	if condition.HasPackage != nil {
		whereClauses = append(whereClauses, "has_package = ?")
		args = append(args, *condition.HasPackage)
	}
	if condition.VehicleType != "" {
		whereClauses = append(whereClauses, "vehicle_type = ?")
		args = append(args, condition.VehicleType)
	}
	if condition.PetType != "" {
		whereClauses = append(whereClauses, "pet_type = ?")
		args = append(args, condition.PetType)
	}
	if condition.Action != "" {
		whereClauses = append(whereClauses, "action = ?")
		args = append(args, condition.Action)
	}
	if len(condition.Colors) > 0 {
		for _, c := range condition.Colors {
			whereClauses = append(whereClauses, "dominant_colors LIKE ?")
			args = append(args, "%"+c+"%")
		}
	}
	if condition.Zone != "" {
		whereClauses = append(whereClauses, "zone = ?")
		args = append(args, condition.Zone)
	}

	// 向量部分
	vecStr := vectorToString(embedding)
	whereClauses = append(whereClauses, fmt.Sprintf("1 - (embedding <=> '%s'::vector) >= ?", vecStr))
	args = append(args, threshold)

	whereSQL := ""
	if len(whereClauses) > 0 {
		whereSQL = "WHERE " + strings.Join(whereClauses, " AND ")
	}

	query := fmt.Sprintf(`
		SELECT *,
			1 - (embedding <=> '%s'::vector) AS score
		FROM camera_events
		%s
		ORDER BY score DESC
		LIMIT ?
	`, vecStr, whereSQL)
	args = append(args, topK)

	var results []model.CameraEventSearchResult
	if err := db.Raw(query, args...).Scan(&results).Error; err != nil {
		return nil, fmt.Errorf("hybrid search: %w", err)
	}

	return results, nil
}

// ParseNaturalLanguage 用 LLM 解析自然语言为搜索条件。
func (s *CameraSearchService) ParseNaturalLanguage(ctx context.Context, query string, chatService *ChatService, mcfg *ModelConfig) (*SearchCondition, string, error) {
	prompt := fmt.Sprintf(`你是一个搜索条件解析助手。将用户的自然语言查询转换为 JSON 搜索条件。

用户查询："%s"

当前时间：%s

请输出以下 JSON（只输出 JSON，不要 Markdown 或解释）：
{
  "search_query": "用于向量搜索的简短描述（提取核心搜索意图，去除时间/设备等过滤条件）",
  "camera_ids": [],
  "start_time": "RFC3339格式，如 2026-08-26T00:00:00Z",
  "end_time": "RFC3339格式",
  "has_person": true/false/null,
  "has_vehicle": true/false/null,
  "has_pet": true/false/null,
  "has_package": true/false/null,
  "vehicle_type": "car/bike/truck/motorcycle 或空字符串",
  "pet_type": "cat/dog/bird 或空字符串",
  "action": "walking/running/stopped/picking_up/delivering/entering/leaving 或空字符串",
  "colors": ["red", "blue"] 或 [],
  "zone": "entrance/yard/gate/front_door/driveway/indoor 或空字符串"
}

规则：
1. 从查询中提取时间信息（昨天、上周、具体日期等），转换为 RFC3339 格式
2. 提取颜色信息（红色衣服 → colors: ["red"]）
3. 提取动作信息（拿包裹 → action: "picking_up", has_package: true）
4. 提取对象信息（有人 → has_person: true, 有车 → has_vehicle: true）
5. search_query 只保留核心搜索意图，去除时间/设备/颜色等结构化条件
6. 不确定的字段设为 null 或空`, query, time.Now().Format(time.RFC3339))

	messages := []ChatMessage{
		{Role: "system", Content: "你是一个搜索条件解析助手。只输出 JSON，不要输出其他内容。"},
		{Role: "user", Content: prompt},
	}

	response, err := chatService.Chat(ctx, messages, mcfg)
	if err != nil {
		return nil, query, fmt.Errorf("parse query: %w", err)
	}

	// 解析 LLM 返回的 JSON
	response = extractJSON(response)

	var parsed struct {
		SearchQuery string   `json:"search_query"`
		CameraIDs   []int64  `json:"camera_ids"`
		StartTime   string   `json:"start_time"`
		EndTime     string   `json:"end_time"`
		HasPerson   *bool    `json:"has_person"`
		HasVehicle  *bool    `json:"has_vehicle"`
		HasPet      *bool    `json:"has_pet"`
		HasPackage  *bool    `json:"has_package"`
		VehicleType string   `json:"vehicle_type"`
		PetType     string   `json:"pet_type"`
		Action      string   `json:"action"`
		Colors      []string `json:"colors"`
		Zone        string   `json:"zone"`
	}

	if err := json.Unmarshal([]byte(response), &parsed); err != nil {
		ilog.Warnf("parse search condition: %v, raw: %s", err, response)
		return &SearchCondition{}, query, nil
	}

	condition := &SearchCondition{
		CameraIDs:   parsed.CameraIDs,
		HasPerson:   parsed.HasPerson,
		HasVehicle:  parsed.HasVehicle,
		HasPet:      parsed.HasPet,
		HasPackage:  parsed.HasPackage,
		VehicleType: parsed.VehicleType,
		PetType:     parsed.PetType,
		Action:      parsed.Action,
		Colors:      parsed.Colors,
		Zone:        parsed.Zone,
	}

	if parsed.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, parsed.StartTime); err == nil {
			condition.StartTime = t
		}
	}
	if parsed.EndTime != "" {
		if t, err := time.Parse(time.RFC3339, parsed.EndTime); err == nil {
			condition.EndTime = t
		}
	}

	searchQuery := parsed.SearchQuery
	if searchQuery == "" {
		searchQuery = query
	}

	return condition, searchQuery, nil
}

func placeholders(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = "?"
	}
	return strings.Join(parts, ",")
}

// extractJSON 从可能包含 Markdown 代码块的文本中提取 JSON。
func extractJSON(text string) string {
	text = strings.TrimSpace(text)
	if strings.HasPrefix(text, "```") {
		text = strings.TrimPrefix(text, "```json")
		text = strings.TrimPrefix(text, "```")
		text = strings.TrimSuffix(text, "```")
		text = strings.TrimSpace(text)
	}
	return text
}

// vectorToString 将 float64 向量转为 PostgreSQL 向量字符串。
func vectorToString(vec []float64) string {
	s := "["
	for i, v := range vec {
		if i > 0 {
			s += ","
		}
		s += fmt.Sprintf("%.6f", v)
	}
	s += "]"
	return s
}
