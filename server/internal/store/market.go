package store

import (
	"context"
	"time"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// ---------- MCP 注册表 ----------

func (s *Store) ListMCPRegistry(ctx context.Context, keyword, category string) ([]*model.MCPRegistry, error) {
	q := s.db.WithContext(ctx).Model(&model.MCPRegistry{})
	if keyword != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	var list []*model.MCPRegistry
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	// TODO: AgentMCPServer 目前没有 registry_id 字段（Agent MCP 是独立配置，不引用平台注册表），
	// 待实现注册表引用机制后再恢复引用计数统计。
	for _, r := range list {
		r.RefCount = 0
	}
	return list, nil
}

func (s *Store) attachMCPRegistryRefCount(ctx context.Context, list []*model.MCPRegistry) {
	if len(list) == 0 {
		return
	}
	ids := make([]int64, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	type cnt struct {
		RegistryID int64
		N          int64
	}
	var rows []cnt
	s.db.WithContext(ctx).Model(&model.AgentMCPServer{}).
		Select("registry_id, COUNT(*) as n").
		Where("registry_id IN ? AND registry_id > 0", ids).
		Group("registry_id").Scan(&rows)
	m := map[int64]int64{}
	for _, r := range rows {
		m[r.RegistryID] = r.N
	}
	for _, r := range list {
		r.RefCount = int(m[r.ID])
	}
}

func (s *Store) GetMCPRegistry(ctx context.Context, id int64) (*model.MCPRegistry, error) {
	var item model.MCPRegistry
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateMCPRegistry(ctx context.Context, item *model.MCPRegistry) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *Store) UpdateMCPRegistry(ctx context.Context, item *model.MCPRegistry) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteMCPRegistry(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.MCPRegistry{}, id).Error
}

// ---------- 技能库 ----------

func (s *Store) ListSkillLibrary(ctx context.Context, keyword, category string) ([]*model.SkillLibrary, error) {
	q := s.db.WithContext(ctx).Model(&model.SkillLibrary{})
	if keyword != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	var list []*model.SkillLibrary
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	s.attachSkillLibRefCount(ctx, list)
	return list, nil
}

func (s *Store) attachSkillLibRefCount(ctx context.Context, list []*model.SkillLibrary) {
	if len(list) == 0 {
		return
	}
	ids := make([]int64, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	type cnt struct {
		SkillLibID int64
		N          int64
	}
	var rows []cnt
	s.db.WithContext(ctx).Model(&model.AgentSkill{}).
		Select("skill_lib_id, COUNT(*) as n").
		Where("skill_lib_id IN ? AND skill_lib_id > 0", ids).
		Group("skill_lib_id").Scan(&rows)
	m := map[int64]int64{}
	for _, r := range rows {
		m[r.SkillLibID] = r.N
	}
	for _, r := range list {
		r.RefCount = int(m[r.ID])
	}
}

func (s *Store) GetSkillLibrary(ctx context.Context, id int64) (*model.SkillLibrary, error) {
	var item model.SkillLibrary
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateSkillLibrary(ctx context.Context, item *model.SkillLibrary) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *Store) UpdateSkillLibrary(ctx context.Context, item *model.SkillLibrary) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteSkillLibrary(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.SkillLibrary{}, id).Error
}

// ---------- 工具库 ----------

func (s *Store) ListToolLibrary(ctx context.Context, keyword, category string) ([]*model.ToolLibrary, error) {
	q := s.db.WithContext(ctx).Model(&model.ToolLibrary{})
	if keyword != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	var list []*model.ToolLibrary
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	s.attachToolLibRefCount(ctx, list)
	return list, nil
}

// ListToolLibraryByIDs 按 ID 批量获取工具库（用于 Agent 挂载时加载定义）。
func (s *Store) ListToolLibraryByIDs(ctx context.Context, ids []int64) ([]*model.ToolLibrary, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var list []*model.ToolLibrary
	err := s.db.WithContext(ctx).Model(&model.ToolLibrary{}).
		Where("id IN ? AND status = 1", ids).
		Order("id ASC").Find(&list).Error
	return list, err
}

// ListAllEnabledBuiltinTools 获取所有启用的内置类型工具（Agent 未配置 tool_lib_ids 时的默认值）。
func (s *Store) ListAllEnabledBuiltinTools(ctx context.Context) ([]*model.ToolLibrary, error) {
	var list []*model.ToolLibrary
	err := s.db.WithContext(ctx).Model(&model.ToolLibrary{}).
		Where("tool_type = ? AND status = 1", "builtin").
		Order("id ASC").Find(&list).Error
	return list, err
}

func (s *Store) attachToolLibRefCount(ctx context.Context, list []*model.ToolLibrary) {
	if len(list) == 0 {
		return
	}
	ids := make([]int64, 0, len(list))
	for _, r := range list {
		ids = append(ids, r.ID)
	}
	// TODO: Agent.ToolLibIDs 是 JSON 数组，无法直接用 SQL 聚合引用数。
	// 当前先返回 0，后续如需精确计数可新建 agent_tool_libs 关联表。
	for _, r := range list {
		r.RefCount = 0
	}
}

func (s *Store) GetToolLibrary(ctx context.Context, id int64) (*model.ToolLibrary, error) {
	var item model.ToolLibrary
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateToolLibrary(ctx context.Context, item *model.ToolLibrary) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *Store) UpdateToolLibrary(ctx context.Context, item *model.ToolLibrary) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteToolLibrary(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.ToolLibrary{}, id).Error
}

// ---------- 智能体模板 ----------

func (s *Store) ListAgentTemplates(ctx context.Context, keyword, category string) ([]*model.AgentTemplate, error) {
	q := s.db.WithContext(ctx).Model(&model.AgentTemplate{})
	if keyword != "" {
		q = q.Where("name ILIKE ? OR description ILIKE ?", "%"+keyword+"%", "%"+keyword+"%")
	}
	if category != "" {
		q = q.Where("category = ?", category)
	}
	var list []*model.AgentTemplate
	if err := q.Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) GetAgentTemplate(ctx context.Context, id int64) (*model.AgentTemplate, error) {
	var item model.AgentTemplate
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateAgentTemplate(ctx context.Context, item *model.AgentTemplate) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *Store) UpdateAgentTemplate(ctx context.Context, item *model.AgentTemplate) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteAgentTemplate(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.AgentTemplate{}, id).Error
}

// ---------- 模型路由规则 ----------

func (s *Store) ListModelRoutingRules(ctx context.Context) ([]*model.ModelRoutingRule, error) {
	var list []*model.ModelRoutingRule
	if err := s.db.WithContext(ctx).Order("priority ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func (s *Store) GetModelRoutingRule(ctx context.Context, id int64) (*model.ModelRoutingRule, error) {
	var item model.ModelRoutingRule
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) SaveModelRoutingRule(ctx context.Context, item *model.ModelRoutingRule) error {
	now := time.Now()
	if item.ID == 0 {
		item.CreatedAt, item.UpdatedAt = now, now
		return s.db.WithContext(ctx).Create(item).Error
	}
	item.UpdatedAt = now
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteModelRoutingRule(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.ModelRoutingRule{}, id).Error
}

// ---------- 调用观测 CallLog ----------

// RecordCallLog 写入一条调用明细（观测/成本下钻用）。
func (s *Store) RecordCallLog(ctx context.Context, log *model.CallLog) error {
	if log.CreatedAt.IsZero() {
		log.CreatedAt = time.Now()
	}
	return s.db.WithContext(ctx).Create(log).Error
}

// CallLogQuery 调用日志查询条件。
type CallLogQuery struct {
	AgentID   int64
	ClientID  int64
	TenantID  int64
	CallType  string
	// ExcludeCallType 排除某类调用：观测页默认排除 llm_aux（辅助调用），
	// 否则标题生成、记忆摘要这类高频后台记录会把主链路刷掉。
	ExcludeCallType string
	Status    int // 0=查全部,1=成功,2=失败(取反)
	DayFrom   string
	DayTo     string
	Page      int
	PageSize  int
}

// ListCallLogs 分页查询调用日志（新在前）。
func (s *Store) ListCallLogs(ctx context.Context, q CallLogQuery) ([]*model.CallLog, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.CallLog{})
	if q.AgentID > 0 {
		db = db.Where("agent_id = ?", q.AgentID)
	}
	if q.ClientID > 0 {
		db = db.Where("client_id = ?", q.ClientID)
	}
	if q.TenantID > 0 {
		db = db.Where("tenant_id = ?", q.TenantID)
	}
	if q.CallType != "" {
		db = db.Where("call_type = ?", q.CallType)
	}
	if q.ExcludeCallType != "" {
		db = db.Where("call_type <> ?", q.ExcludeCallType)
	}
	if q.Status == 1 {
		db = db.Where("status = ?", 1)
	} else if q.Status == 2 {
		db = db.Where("status = ?", 0)
	}
	if q.DayFrom != "" {
		db = db.Where("created_at >= ?", q.DayFrom+" 00:00:00")
	}
	if q.DayTo != "" {
		db = db.Where("created_at <= ?", q.DayTo+" 23:59:59")
	}

	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page, size := q.Page, q.PageSize
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 20
	}
	var list []*model.CallLog
	if err := db.Order("id DESC").Offset((page - 1) * size).Limit(size).Find(&list).Error; err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

// CallLogSummary 调用日志汇总（成本/错误率下钻）。
type CallLogSummary struct {
	TotalCalls    int64   `json:"totalCalls"`
	ErrorCalls    int64   `json:"errorCalls"`
	TotalTokens   int64   `json:"totalTokens"`
	TotalCostCents int64  `json:"totalCostCents"`
	AvgLatencyMs  int64   `json:"avgLatencyMs"`
}

// SumCallLogs 按条件汇总调用日志。
func (s *Store) SumCallLogs(ctx context.Context, q CallLogQuery) (*CallLogSummary, error) {
	db := s.db.WithContext(ctx).Model(&model.CallLog{})
	if q.AgentID > 0 {
		db = db.Where("agent_id = ?", q.AgentID)
	}
	if q.ClientID > 0 {
		db = db.Where("client_id = ?", q.ClientID)
	}
	if q.TenantID > 0 {
		db = db.Where("tenant_id = ?", q.TenantID)
	}
	if q.CallType != "" {
		db = db.Where("call_type = ?", q.CallType)
	}
	if q.ExcludeCallType != "" {
		db = db.Where("call_type <> ?", q.ExcludeCallType)
	}
	if q.DayFrom != "" {
		db = db.Where("created_at >= ?", q.DayFrom+" 00:00:00")
	}
	if q.DayTo != "" {
		db = db.Where("created_at <= ?", q.DayTo+" 23:59:59")
	}
	var row struct {
		Total    int64
		Errors   int64
		Tokens   int64
		Cost     int64
		Latency  int64
	}
	if err := db.Select("COUNT(*) as total, COALESCE(SUM(1-status),0) as errors, " +
		"COALESCE(SUM(total_tokens),0) as tokens, COALESCE(SUM(cost_cents),0) as cost, " +
		"COALESCE(SUM(latency_ms),0) as latency").Scan(&row).Error; err != nil {
		return nil, err
	}
	avg := int64(0)
	if row.Total > 0 {
		avg = row.Latency / row.Total
	}
	return &CallLogSummary{
		TotalCalls:     row.Total,
		ErrorCalls:     row.Errors,
		TotalTokens:    row.Tokens,
		TotalCostCents: row.Cost,
		AvgLatencyMs:   avg,
	}, nil
}

// ---------- 成本估算 ----------

// EstimateCostCents 按模型单价估算一次调用的成本（分）。
// 单价来自 ModelConfig（PriceInPer1K / PriceOutPer1K，单位：分/1K token）。
func (s *Store) EstimateCostCents(ctx context.Context, modelID, promptTokens, outputTokens int64) int64 {
	if modelID <= 0 {
		return 0
	}
	var cfg model.ModelConfig
	if err := s.db.WithContext(ctx).First(&cfg, modelID).Error; err != nil {
		return 0
	}
	in := cfg.PriceInPer1K / 1000.0 * float64(promptTokens)
	out := cfg.PriceOutPer1K / 1000.0 * float64(outputTokens)
	return int64(in + out + 0.5)
}

// EnsureModelPriceBaseline 幂等：给已有模型配置补默认单价（仅当价格全为 0 时），便于成本核算展示。
func (s *Store) EnsureModelPriceBaseline(ctx context.Context) {
	type def struct {
		provider string
		in, out  float64
	}
	// 常见模型的默认参考单价（分/1K token），仅作占位，管理员可在界面调整。
	defaults := map[string]def{
		"qwen-plus":          {"qwen", 0.4, 0.8},
		"qwen-max":           {"qwen", 2.0, 6.0},
		"qwen-long":          {"qwen", 0.5, 1.0},
		"text-embedding-v3":  {"qwen", 0.07, 0},
		"gpt-4o":             {"openai", 2.5, 10.0},
		"gpt-4o-mini":        {"openai", 0.15, 0.6},
		"text-embedding-3-small": {"openai", 0.01, 0},
	}
	if err := s.db.WithContext(ctx).Model(&model.ModelConfig{}).
		Where("price_in_per1k = 0 AND price_out_per1k = 0").
		Find(&[]model.ModelConfig{}).Error; err != nil {
		return
	}
	var cfgs []model.ModelConfig
	if err := s.db.WithContext(ctx).Find(&cfgs).Error; err != nil {
		return
	}
	for i := range cfgs {
		c := cfgs[i]
		if c.PriceInPer1K != 0 || c.PriceOutPer1K != 0 {
			continue
		}
		if d, ok := defaults[c.ModelName]; ok {
			c.PriceInPer1K = d.in
			c.PriceOutPer1K = d.out
			c.BillingType = model.ModelBillingTypeToken
			c.Currency = "CNY"
			if err := s.db.WithContext(ctx).Model(&model.ModelConfig{}).
				Where("id = ?", c.ID).
				Updates(map[string]interface{}{
					"price_in_per1k":  d.in,
					"price_out_per1k": d.out,
					"billing_type":    model.ModelBillingTypeToken,
					"currency":        "CNY",
				}).Error; err != nil {
				ilog.Warnf("ensure model price baseline: model %d: %v", c.ID, err)
			}
		}
	}
}
