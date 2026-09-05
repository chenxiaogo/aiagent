package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
)

// ---------- 对外交付：按版本快照组装的单智能体 MCP 服务器 ----------
//
// 与 server.go 的「平台全局 MCP」不同，AgentServer 面向单个已发布的智能体：
//   - 工具集来自该版本快照（agent_release.snapshot），不是运行时实时查库；
//   - 同一智能体的不同客户可能落在不同版本上（订阅钉版本），行为因此天然隔离；
//   - 快照不可变，管理员后续改配置不会影响已发布版本的对外行为。

// builtinToolDef 内置工具定义。
type builtinToolDef struct {
	Name        string
	Description string
	// Categories 适用分类，空表示所有分类都可选；实际是否暴露取决于快照 ExposedTools。
	Categories []string
	Schema     map[string]interface{}
	Call       func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error)
}

// builtinTools 平台内置、可被智能体对外暴露的工具集。
var builtinTools = []builtinToolDef{
	{
		Name:        "agent_info",
		Description: "获取该智能体的基本信息、能力说明与预设问题，用于判断它能做什么",
		Schema:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolAgentInfo(ctx)
		},
	},
	{
		Name:        "video_search",
		Description: "在视频库中按语义描述检索相关场景片段，返回时间区间、画面描述与字幕",
		Categories:  []string{model.AgentCategoryVideo},
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "检索描述，如：穿红色外套的男性走过门前"},
				"top_k": map[string]interface{}{"type": "integer", "description": "返回条数，默认 5", "default": 5},
			},
			"required": []string{"query"},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolVideoSearch(ctx, args)
		},
	},
	{
		Name:        "video_summary",
		Description: "获取指定视频的摘要与完整转写文字",
		Categories:  []string{model.AgentCategoryVideo},
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"video_id": map[string]interface{}{"type": "integer", "description": "视频 ID"},
			},
			"required": []string{"video_id"},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolVideoSummary(ctx, args)
		},
	},
	{
		Name:        "list_videos",
		Description: "列出该智能体下已处理完成的视频数据源",
		Categories:  []string{model.AgentCategoryVideo},
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"limit": map[string]interface{}{"type": "integer", "description": "返回条数，默认 20", "default": 20},
			},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolListVideos(ctx, args)
		},
	},
	{
		Name:        "camera_search",
		Description: "按自然语言与结构化条件检索摄像头事件（人物/车辆/动物/包裹/动作/区域）",
		Categories:  []string{model.AgentCategoryCamera},
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query":        map[string]interface{}{"type": "string", "description": "事件描述，如：昨天下午有人在门口取包裹"},
				"camera_id":    map[string]interface{}{"type": "integer", "description": "摄像头 ID，可选"},
				"has_person":   map[string]interface{}{"type": "boolean", "description": "是否有人"},
				"has_vehicle":  map[string]interface{}{"type": "boolean", "description": "是否有车"},
				"has_pet":      map[string]interface{}{"type": "boolean", "description": "是否有宠物"},
				"has_package":  map[string]interface{}{"type": "boolean", "description": "是否有包裹"},
				"action":       map[string]interface{}{"type": "string", "description": "动作：walking/running/stopped/picking_up/delivering"},
				"zone":         map[string]interface{}{"type": "string", "description": "区域：entrance/yard/gate/front_door"},
				"top_k":        map[string]interface{}{"type": "integer", "description": "返回条数，默认 5", "default": 5},
			},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolCameraSearch(ctx, args)
		},
	},
	{
		Name:        "doc_search",
		Description: "在知识库文档中做全文检索，返回命中片段与来源文件",
		Categories:  []string{model.AgentCategoryDoc},
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{"type": "string", "description": "检索关键词或问题"},
				"top_k": map[string]interface{}{"type": "integer", "description": "返回条数，默认 5", "default": 5},
			},
			"required": []string{"query"},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolDocSearch(ctx, args)
		},
	},
	{
		Name:        "list_knowledge_bases",
		Description: "列出可用的知识库（文档检索型智能体）",
		Categories:  []string{model.AgentCategoryDoc},
		Schema:      map[string]interface{}{"type": "object", "properties": map[string]interface{}{}},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolListKnowledgeBases(ctx)
		},
	},
	{
		Name:        "list_reports",
		Description: "列出该智能体已生成的报告（标题、类型、状态、创建时间）",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"report_type": map[string]interface{}{
					"type":        "string",
					"description": "报告类型过滤：analysis/summary/custom，留空返回全部",
				},
				"limit": map[string]interface{}{"type": "integer", "description": "返回条数，默认 10", "default": 10},
			},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolListReports(ctx, args)
		},
	},
	{
		Name:        "generate_report",
		Description: "基于该智能体的数据生成分析报告（异步任务，返回报告 ID）",
		Schema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title":       map[string]interface{}{"type": "string", "description": "报告标题"},
				"video_ids":   map[string]interface{}{"type": "string", "description": "关联视频 ID，逗号分隔"},
				"report_type": map[string]interface{}{"type": "string", "description": "analysis/summary/custom", "default": "analysis"},
			},
			"required": []string{"title"},
		},
		Call: func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error) {
			return s.toolGenerateReport(ctx, args)
		},
	},
}

// BuiltinToolCatalog 返回可被勾选对外暴露的内置工具清单（供「MCP 工具」Tab 与发布校验使用）。
func BuiltinToolCatalog() []Tool {
	out := make([]Tool, 0, len(builtinTools))
	for _, d := range builtinTools {
		out = append(out, Tool{Name: d.Name, Description: d.Description, InputSchema: d.Schema})
	}
	return out
}

// AgentServer 单个已发布智能体的 MCP 服务器。
type AgentServer struct {
	store   *store.Store
	agent   *model.Agent
	release *model.AgentRelease
	snap    *model.AgentReleaseSnapshot
	tools   []Tool
	calls   map[string]func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error)
}

// NewAgentServer 按发布版本构建智能体 MCP 服务器。
func NewAgentServer(s *store.Store, agent *model.Agent, release *model.AgentRelease) (*AgentServer, error) {
	snap, err := model.DecodeAgentReleaseSnapshot(release.Snapshot)
	if err != nil {
		return nil, fmt.Errorf("decode release snapshot: %w", err)
	}
	srv := &AgentServer{
		store:   s,
		agent:   agent,
		release: release,
		snap:    snap,
		calls:   make(map[string]func(ctx context.Context, s *AgentServer, args map[string]interface{}) (CallToolResult, error)),
	}

	// 只暴露快照里登记的工具，且必须是当前版本存在实现的内置工具
	exposed := make(map[string]bool, len(snap.ExposedTools))
	for _, name := range snap.ExposedTools {
		exposed[name] = true
	}
	for _, def := range builtinTools {
		if !exposed[def.Name] {
			continue
		}
		if len(def.Categories) > 0 && !containsString(def.Categories, snap.Category) {
			continue
		}
		srv.tools = append(srv.tools, Tool{
			Name: def.Name, Description: def.Description, InputSchema: def.Schema,
		})
		srv.calls[def.Name] = def.Call
	}
	sort.Slice(srv.tools, func(i, j int) bool { return srv.tools[i].Name < srv.tools[j].Name })
	return srv, nil
}

// Tools 返回该版本对外暴露的工具。
func (s *AgentServer) Tools() []Tool { return s.tools }

// Snapshot 返回生效快照。
func (s *AgentServer) Snapshot() *model.AgentReleaseSnapshot { return s.snap }

// Release 返回生效版本。
func (s *AgentServer) Release() *model.AgentRelease { return s.release }

// HandleRequest 处理 MCP JSON-RPC 请求。
func (s *AgentServer) HandleRequest(ctx context.Context, req *Request) *Response {
	resp := &Response{JSONRPC: "2.0", ID: req.ID}

	var result interface{}
	var err error
	switch req.Method {
	case MethodInitialize:
		result, err = s.handleInitialize(ctx, req.Params)
	case MethodPing:
		result = map[string]interface{}{}
	case MethodNotifications, "notifications/cancelled":
		result = nil
	case MethodListTools:
		result = map[string]interface{}{"tools": s.tools}
	case MethodCallTool:
		result, err = s.handleCallTool(ctx, req.Params)
	case MethodListPrompts:
		result = s.handleListPrompts()
	case MethodGetPrompt:
		result, err = s.handleGetPrompt(req.Params)
	case MethodListResources:
		result = map[string]interface{}{"resources": []interface{}{}}
	default:
		resp.Error = &RPCError{Code: -32601, Message: fmt.Sprintf("method not found: %s", req.Method)}
		return resp
	}
	if err != nil {
		resp.Error = &RPCError{Code: -32603, Message: err.Error()}
		return resp
	}
	resp.Result = result
	return resp
}

func (s *AgentServer) handleInitialize(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    "aiagent-" + s.agent.Slug,
			"version": s.release.Version,
		},
		"capabilities": map[string]interface{}{
			"tools":     map[string]interface{}{"listChanged": false},
			"prompts":   map[string]interface{}{"listChanged": false},
			"resources": map[string]interface{}{"listChanged": false},
		},
		"instructions": firstNonEmpty(s.snap.AgentDesc, s.agent.Description),
	}, nil
}

func (s *AgentServer) handleCallTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var p struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	call, ok := s.calls[p.Name]
	if !ok {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: fmt.Sprintf("unknown tool: %s", p.Name)}},
		}, nil
	}
	return call(ctx, s, p.Arguments)
}

// ---------- 工具实现 ----------

func (s *AgentServer) toolAgentInfo(ctx context.Context) (CallToolResult, error) {
	text := fmt.Sprintf("智能体：%s（版本 %s）\n", s.agent.Name, s.release.Version)
	if s.snap.AgentDesc != "" {
		text += "简介：" + s.snap.AgentDesc + "\n"
	}
	text += fmt.Sprintf("类型：%s\n", s.snap.Category)
	if len(s.tools) > 0 {
		names := make([]string, 0, len(s.tools))
		for _, t := range s.tools {
			names = append(names, t.Name)
		}
		text += "可用工具：" + strings.Join(names, ", ") + "\n"
	}
	if len(s.snap.Skills) > 0 {
		names := make([]string, 0, len(s.snap.Skills))
		for _, sk := range s.snap.Skills {
			names = append(names, sk.Name)
		}
		text += "已装载技能：" + strings.Join(names, ", ") + "\n"
	}
	if len(s.snap.PresetQuestions) > 0 {
		text += "\n推荐提问：\n"
		for i, q := range s.snap.PresetQuestions {
			text += fmt.Sprintf("%d. %s\n", i+1, q)
		}
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolVideoSearch(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	query := getStringArg(args, "query", "")
	if query == "" {
		return toolError("query is required"), nil
	}
	topK := int(getFloatArg(args, "top_k", 5))
	if topK <= 0 {
		topK = 5
	}

	db := s.store.DB().WithContext(ctx).
		Where("(description ILIKE ? OR transcript ILIKE ?)", "%"+query+"%", "%"+query+"%")
	// 按快照绑定的视频源隔离：仅检索该版本冻结的视频数据源，避免跨 Agent 泄漏
	if ids := s.snap.Resources.VideoSourceIDs; len(ids) > 0 {
		db = db.Where("video_id IN ?", ids)
	} else {
		// 未绑定任何视频源：直接短路为空结果（严格隔离，宁可搜不到也不泄漏）
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体尚未绑定任何视频数据源"}}}, nil
	}

	var scenes []model.VideoScene
	if err := db.Order("id DESC").Limit(topK).Find(&scenes).Error; err != nil {
		return toolError("search failed: " + err.Error()), nil
	}
	if len(scenes) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "未找到相关视频场景"}}}, nil
	}

	videoIDs := make([]int64, 0, len(scenes))
	seen := map[int64]bool{}
	for _, sc := range scenes {
		if !seen[sc.VideoID] {
			seen[sc.VideoID] = true
			videoIDs = append(videoIDs, sc.VideoID)
		}
	}
	titles := map[int64]string{}
	var videos []model.VideoDatasource
	if err := s.store.DB().WithContext(ctx).Where("id IN ?", videoIDs).Find(&videos).Error; err == nil {
		for _, v := range videos {
			titles[v.ID] = v.Title
		}
	}

	text := fmt.Sprintf("命中 %d 个场景：\n\n", len(scenes))
	for i, sc := range scenes {
		title := titles[sc.VideoID]
		if title == "" {
			title = fmt.Sprintf("视频 #%d", sc.VideoID)
		}
		text += fmt.Sprintf("%d. 【%s】视频ID %d  时间 %.1fs-%.1fs\n", i+1, title, sc.VideoID, sc.StartTime, sc.EndTime)
		if sc.Description != "" {
			text += "   画面：" + sc.Description + "\n"
		}
		if sc.Transcript != "" {
			text += "   字幕：" + sc.Transcript + "\n"
		}
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolVideoSummary(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	videoID := int64(getFloatArg(args, "video_id", 0))
	if videoID <= 0 {
		return toolError("video_id is required"), nil
	}
	video, err := s.store.GetVideo(ctx, videoID)
	if err != nil {
		return toolError("video not found"), nil
	}
	text := fmt.Sprintf("视频：%s（时长 %.0fs）\n\n", video.Title, video.Duration)
	if video.Summary != "" {
		text += "【摘要】\n" + video.Summary + "\n\n"
	}
	if video.Transcript != "" {
		text += "【完整转写】\n" + video.Transcript
	}
	if video.Summary == "" && video.Transcript == "" {
		text += "暂无摘要与转写内容"
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolListVideos(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	limit := int(getFloatArg(args, "limit", 20))
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	videos, _, err := s.store.ListVideos(ctx, s.agent.ID, 0, model.VideoStatusReady, "", 1, limit)
	if err != nil {
		return toolError("list failed: " + err.Error()), nil
	}
	// 按快照绑定的视频源隔离
	if ids := s.snap.Resources.VideoSourceIDs; len(ids) > 0 {
		allowed := make(map[int64]bool, len(ids))
		for _, id := range ids {
			allowed[id] = true
		}
		filtered := videos[:0]
		for _, v := range videos {
			if allowed[v.ID] {
				filtered = append(filtered, v)
			}
		}
		videos = filtered
	} else {
		videos = nil
	}
	if len(videos) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体暂无已就绪的视频"}}}, nil
	}
	text := fmt.Sprintf("共 %d 个视频：\n\n", len(videos))
	for _, v := range videos {
		text += fmt.Sprintf("- ID %d｜%s｜时长 %.0fs｜场景 %d\n", v.ID, v.Title, v.Duration, v.SceneCount)
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolCameraSearch(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	query := getStringArg(args, "query", "")
	topK := int(getFloatArg(args, "top_k", 5))
	if topK <= 0 {
		topK = 5
	}

	db := s.store.DB().WithContext(ctx).Model(&model.CameraEvent{}).Where("processed = ?", true)
	// 按快照绑定的摄像头事件隔离：仅检索该版本冻结的事件 ID，避免跨 Agent 泄漏
	if ids := s.snap.Resources.CameraEventIDs; len(ids) > 0 {
		db = db.Where("id IN ?", ids)
	} else {
		// 未绑定任何摄像头事件：直接短路为空结果（严格隔离）
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体尚未绑定任何摄像头事件"}}}, nil
	}
	if query != "" {
		db = db.Where("summary ILIKE ? OR action_desc ILIKE ? OR person_desc ILIKE ?",
			"%"+query+"%", "%"+query+"%", "%"+query+"%")
	}
	if v := int64(getFloatArg(args, "camera_id", 0)); v > 0 {
		db = db.Where("camera_id = ?", v)
	}
	if v, ok := getBoolArg(args, "has_person"); ok {
		db = db.Where("has_person = ?", v)
	}
	if v, ok := getBoolArg(args, "has_vehicle"); ok {
		db = db.Where("has_vehicle = ?", v)
	}
	if v, ok := getBoolArg(args, "has_pet"); ok {
		db = db.Where("has_pet = ?", v)
	}
	if v, ok := getBoolArg(args, "has_package"); ok {
		db = db.Where("has_package = ?", v)
	}
	if v := getStringArg(args, "action", ""); v != "" {
		db = db.Where("action = ?", v)
	}
	if v := getStringArg(args, "zone", ""); v != "" {
		db = db.Where("zone = ?", v)
	}

	var events []model.CameraEvent
	if err := db.Order("event_time DESC").Limit(topK).Find(&events).Error; err != nil {
		return toolError("search failed: " + err.Error()), nil
	}
	if len(events) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "未找到匹配的摄像头事件"}}}, nil
	}

	text := fmt.Sprintf("命中 %d 个事件：\n\n", len(events))
	for i, e := range events {
		text += fmt.Sprintf("%d. 【%s】%s\n", i+1, e.CameraName, e.EventTime.Format("2006-01-02 15:04:05"))
		if e.Summary != "" {
			text += "   " + e.Summary + "\n"
		}
		tags := []string{}
		if e.HasPerson {
			tags = append(tags, fmt.Sprintf("人物×%d", e.PersonCount))
		}
		if e.HasVehicle {
			tags = append(tags, "车辆:"+e.VehicleType)
		}
		if e.HasPet {
			tags = append(tags, "宠物:"+e.PetType)
		}
		if e.HasPackage {
			tags = append(tags, "包裹")
		}
		if e.Action != "" {
			tags = append(tags, "动作:"+e.Action)
		}
		if e.Zone != "" {
			tags = append(tags, "区域:"+e.Zone)
		}
		if len(tags) > 0 {
			text += "   标签：" + strings.Join(tags, "，") + "\n"
		}
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolDocSearch(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	query := getStringArg(args, "query", "")
	if query == "" {
		return toolError("query is required"), nil
	}
	topK := int(getFloatArg(args, "top_k", 5))
	if topK <= 0 {
		topK = 5
	}
	// 按快照绑定的知识库隔离：仅检索该版本冻结的知识库，避免跨 Agent 泄漏
	kbIDs := s.snap.Resources.KnowledgeBaseIDs
	if len(kbIDs) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体尚未绑定任何知识库"}}}, nil
	}
	results, err := s.store.FullTextSearchInKBs(ctx, query, kbIDs, topK)
	if err != nil {
		return toolError("search failed: " + err.Error()), nil
	}
	if len(results) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "未检索到相关文档内容"}}}, nil
	}
	text := fmt.Sprintf("命中 %d 个片段：\n\n", len(results))
	for i, r := range results {
		text += fmt.Sprintf("%d. 【%s】(相关度 %.3f)\n%s\n\n", i+1, r.FileName, r.Score, r.Content)
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolListKnowledgeBases(ctx context.Context) (CallToolResult, error) {
	kbIDs := s.snap.Resources.KnowledgeBaseIDs
	if len(kbIDs) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体尚未绑定任何知识库"}}}, nil
	}
	var list []*model.KnowledgeBase
	if err := s.store.DB().WithContext(ctx).Where("id IN ?", kbIDs).Find(&list).Error; err != nil {
		return toolError("list failed: " + err.Error()), nil
	}
	if len(list) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "暂无知识库"}}}, nil
	}
	text := fmt.Sprintf("共 %d 个知识库：\n\n", len(list))
	for _, kb := range list {
		text += fmt.Sprintf("- ID %d｜%s｜文件 %d｜分块 %d\n", kb.ID, kb.Name, kb.FileCount, kb.ChunkCount)
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolListReports(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	reportType := getStringArg(args, "report_type", "")
	limit := int(getFloatArg(args, "limit", 10))
	if limit <= 0 || limit > 50 {
		limit = 10
	}
	reports, _, err := s.store.ListReports(ctx, s.agent.ID, reportType, 1, limit)
	if err != nil {
		return toolError("list failed: " + err.Error()), nil
	}
	if len(reports) == 0 {
		return CallToolResult{Content: []ToolContent{{Type: "text", Text: "该智能体暂无报告"}}}, nil
	}
	text := fmt.Sprintf("该智能体共 %d 份报告：\n\n", len(reports))
	for _, r := range reports {
		text += fmt.Sprintf("- ID %d｜%s｜类型:%s｜状态:%s｜创建于 %s\n",
			r.ID, r.Title, r.ReportType, r.Status, r.CreatedAt.Format("2006-01-02 15:04"))
	}
	return CallToolResult{Content: []ToolContent{{Type: "text", Text: text}}}, nil
}

func (s *AgentServer) toolGenerateReport(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	title := getStringArg(args, "title", "")
	if title == "" {
		return toolError("title is required"), nil
	}
	reportType := getStringArg(args, "report_type", model.ReportTypeAnalysis)
	report := &model.Report{
		AgentID:      s.agent.ID,
		Title:        title,
		ReportType:   reportType,
		Status:       model.ReportStatusGenerating,
		VideoIDs:     getStringArg(args, "video_ids", ""),
		CreatorID:    0,
		CreatorName:  "mcp:" + s.agent.Slug,
		ErrorMessage: "",
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := s.store.CreateReport(ctx, report); err != nil {
		return toolError("create report failed: " + err.Error()), nil
	}
	return CallToolResult{Content: []ToolContent{{
		Type: "text",
		Text: fmt.Sprintf("报告生成任务已提交，报告 ID %d，当前状态：生成中", report.ID),
	}}}, nil
}

// ---------- Prompts（预设问题） ----------

func (s *AgentServer) handleListPrompts() interface{} {
	prompts := make([]map[string]interface{}, 0, len(s.snap.PresetQuestions))
	for i, q := range s.snap.PresetQuestions {
		prompts = append(prompts, map[string]interface{}{
			"name":        fmt.Sprintf("preset-%d", i+1),
			"description": q,
		})
	}
	return map[string]interface{}{"prompts": prompts}
}

func (s *AgentServer) handleGetPrompt(params json.RawMessage) (interface{}, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	for i, q := range s.snap.PresetQuestions {
		if fmt.Sprintf("preset-%d", i+1) != req.Name {
			continue
		}
		messages := []map[string]interface{}{}
		if s.snap.Prompt != "" {
			messages = append(messages, map[string]interface{}{
				"role":    "system",
				"content": map[string]interface{}{"type": "text", "text": s.snap.Prompt},
			})
		}
		messages = append(messages, map[string]interface{}{
			"role":    "user",
			"content": map[string]interface{}{"type": "text", "text": q},
		})
		return map[string]interface{}{"messages": messages}, nil
	}
	return nil, fmt.Errorf("prompt not found: %s", req.Name)
}

// ---------- 工具函数 ----------

func toolError(msg string) CallToolResult {
	return CallToolResult{IsError: true, Content: []ToolContent{{Type: "text", Text: msg}}}
}

func getBoolArg(args map[string]interface{}, key string) (bool, bool) {
	if v, ok := args[key]; ok {
		if b, ok := v.(bool); ok {
			return b, true
		}
	}
	return false, false
}

func containsString(list []string, target string) bool {
	for _, v := range list {
		if v == target {
			return true
		}
	}
	return false
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// ToolNameFromRequest 从 tools/call 请求里解析被调用的工具名（用于调用观测日志）。
func ToolNameFromRequest(req *Request) string {
	if req == nil {
		return ""
	}
	var p struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(req.Params, &p); err == nil {
		return p.Name
	}
	return ""
}
