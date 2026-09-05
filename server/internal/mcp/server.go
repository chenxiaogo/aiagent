package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// MCP 协议方法名
const (
	MethodInitialize      = "initialize"
	MethodPing            = "ping"
	MethodListTools       = "tools/list"
	MethodCallTool        = "tools/call"
	MethodListResources   = "resources/list"
	MethodListPrompts     = "prompts/list"
	MethodGetPrompt       = "prompts/get"
	MethodNotifications   = "notifications/initialized"
)

// Request JSON-RPC 请求
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response JSON-RPC 响应
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  interface{}     `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError JSON-RPC 错误
type RPCError struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// Tool MCP 工具定义
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// CallToolResult 工具调用结果
type CallToolResult struct {
	Content []ToolContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

// ToolContent 工具返回内容
type ToolContent struct {
	Type string `json:"type"` // text / image / resource
	Text string `json:"text,omitempty"`
}

// Server MCP 服务器
type Server struct {
	store   *store.Store
	tools   []Tool
	handler map[string]func(ctx context.Context, params json.RawMessage) (interface{}, error)
	mu      sync.RWMutex
}

// NewServer 创建 MCP 服务器
func NewServer(s *store.Store) *Server {
	srv := &Server{
		store:   s,
		handler: make(map[string]func(ctx context.Context, params json.RawMessage) (interface{}, error)),
	}
	srv.registerTools()
	srv.registerHandlers()
	return srv
}

// registerTools 注册可用工具
func (s *Server) registerTools() {
	s.tools = []Tool{
		{
			Name:        "video_search",
			Description: "在视频知识库中搜索相关场景和内容，基于视频的语音转文字和场景描述进行语义检索",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_id": map[string]interface{}{
						"type":        "integer",
						"description": "智能体 ID",
					},
					"query": map[string]interface{}{
						"type":        "string",
						"description": "搜索查询内容",
					},
					"top_k": map[string]interface{}{
						"type":        "integer",
						"description": "返回结果数量，默认5",
						"default":     5,
					},
				},
				"required": []string{"agent_id", "query"},
			},
		},
		{
			Name:        "video_summary",
			Description: "获取指定视频的完整摘要和文字内容",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"video_id": map[string]interface{}{
						"type":        "integer",
						"description": "视频 ID",
					},
				},
				"required": []string{"video_id"},
			},
		},
		{
			Name:        "list_agents",
			Description: "列出所有可用的智能体",
			InputSchema: map[string]interface{}{
				"type":       "object",
				"properties": map[string]interface{}{},
			},
		},
		{
			Name:        "list_videos",
			Description: "列出指定智能体下的所有视频数据源",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_id": map[string]interface{}{
						"type":        "integer",
						"description": "智能体 ID",
					},
				},
				"required": []string{"agent_id"},
			},
		},
		{
			Name:        "generate_report",
			Description: "基于视频内容生成智能分析报告",
			InputSchema: map[string]interface{}{
				"type": "object",
				"properties": map[string]interface{}{
					"agent_id": map[string]interface{}{
						"type":        "integer",
						"description": "智能体 ID",
					},
					"title": map[string]interface{}{
						"type":        "string",
						"description": "报告标题",
					},
					"video_ids": map[string]interface{}{
						"type":        "string",
						"description": "视频ID列表，逗号分隔",
					},
					"report_type": map[string]interface{}{
						"type":        "string",
						"description": "报告类型：analysis/summary/custom",
						"default":     "analysis",
					},
				},
				"required": []string{"agent_id", "title"},
			},
		},
	}
}

// registerHandlers 注册方法处理器
func (s *Server) registerHandlers() {
	s.handler[MethodInitialize] = s.handleInitialize
	s.handler[MethodPing] = s.handlePing
	s.handler[MethodListTools] = s.handleListTools
	s.handler[MethodCallTool] = s.handleCallTool
	s.handler[MethodListPrompts] = s.handleListPrompts
	s.handler[MethodGetPrompt] = s.handleGetPrompt
	s.handler[MethodListResources] = s.handleListResources
	s.handler[MethodNotifications] = s.handleInitialized
}

// HandleRequest 处理 MCP 请求
func (s *Server) HandleRequest(ctx context.Context, req *Request) *Response {
	resp := &Response{
		JSONRPC: "2.0",
		ID:      req.ID,
	}

	s.mu.RLock()
	handler, ok := s.handler[req.Method]
	s.mu.RUnlock()

	if !ok {
		resp.Error = &RPCError{
			Code:    -32601,
			Message: fmt.Sprintf("method not found: %s", req.Method),
		}
		return resp
	}

	result, err := handler(ctx, req.Params)
	if err != nil {
		resp.Error = &RPCError{
			Code:    -32603,
			Message: err.Error(),
		}
		return resp
	}
	resp.Result = result
	return resp
}

// ---------- Handler 实现 ----------

func (s *Server) handleInitialize(ctx context.Context, params json.RawMessage) (interface{}, error) {
	ilog.Info("MCP client initialized")
	return map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]interface{}{
			"name":    "aiagent-mcp-server",
			"version": "1.0.0",
		},
		"capabilities": map[string]interface{}{
			"tools": map[string]interface{}{
				"listChanged": false,
			},
			"prompts": map[string]interface{}{
				"listChanged": false,
			},
			"resources": map[string]interface{}{
				"listChanged": false,
			},
		},
	}, nil
}

func (s *Server) handleInitialized(ctx context.Context, params json.RawMessage) (interface{}, error) {
	ilog.Info("MCP client initialization confirmed")
	return nil, nil
}

func (s *Server) handlePing(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{}, nil
}

func (s *Server) handleListTools(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"tools": s.tools,
	}, nil
}

func (s *Server) handleCallTool(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var callParams struct {
		Name      string                 `json:"name"`
		Arguments map[string]interface{} `json:"arguments"`
	}
	if err := json.Unmarshal(params, &callParams); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	switch callParams.Name {
	case "video_search":
		return s.toolVideoSearch(ctx, callParams.Arguments)
	case "video_summary":
		return s.toolVideoSummary(ctx, callParams.Arguments)
	case "list_agents":
		return s.toolListAgents(ctx, callParams.Arguments)
	case "list_videos":
		return s.toolListVideos(ctx, callParams.Arguments)
	case "generate_report":
		return s.toolGenerateReport(ctx, callParams.Arguments)
	default:
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{
				{Type: "text", Text: fmt.Sprintf("unknown tool: %s", callParams.Name)},
			},
		}, nil
	}
}

// toolVideoSearch 视频搜索工具
func (s *Server) toolVideoSearch(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	agentID := int64(getFloatArg(args, "agent_id", 0))
	query := getStringArg(args, "query", "")
	topK := int(getFloatArg(args, "top_k", 5))

	if agentID <= 0 {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "agent_id is required"}},
		}, nil
	}
	if query == "" {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "query is required"}},
		}, nil
	}

	// TODO: 接入真实的向量搜索，这里先返回场景描述的文本搜索结果
	var results []model.VideoScene
	db := s.store.DB().WithContext(ctx).
		Where("agent_id = ? AND description LIKE ?", agentID, "%"+query+"%").
		Limit(topK).Order("id DESC")
	if err := db.Find(&results).Error; err != nil {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "search failed: " + err.Error()}},
		}, nil
	}

	if len(results) == 0 {
		return CallToolResult{
			Content: []ToolContent{{Type: "text", Text: "未找到相关视频内容"}},
		}, nil
	}

	text := "找到以下相关视频场景：\n\n"
	for i, r := range results {
		text += fmt.Sprintf("%d. [场景%d] 时间: %.1fs-%.1fs\n   描述: %s\n   字幕: %s\n\n",
			i+1, r.SceneIndex, r.StartTime, r.EndTime, r.Description, r.Transcript)
	}

	return CallToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}, nil
}

// toolVideoSummary 视频摘要工具
func (s *Server) toolVideoSummary(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	videoID := int64(getFloatArg(args, "video_id", 0))
	if videoID <= 0 {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "video_id is required"}},
		}, nil
	}

	video, err := s.store.GetVideo(ctx, videoID)
	if err != nil {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "video not found: " + err.Error()}},
		}, nil
	}

	text := fmt.Sprintf("视频：%s\n\n", video.Title)
	if video.Summary != "" {
		text += "【摘要】\n" + video.Summary + "\n\n"
	}
	if video.Transcript != "" {
		text += "【完整文字内容】\n" + video.Transcript
	}
	if video.Summary == "" && video.Transcript == "" {
		text += "暂无摘要和文字内容"
	}

	return CallToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}, nil
}

// toolListAgents 列出智能体
func (s *Server) toolListAgents(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	agents, _, err := s.store.ListAgents(ctx, model.AgentStatusPublished, "", 1, 100)
	if err != nil {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "failed: " + err.Error()}},
		}, nil
	}

	if len(agents) == 0 {
		return CallToolResult{
			Content: []ToolContent{{Type: "text", Text: "暂无可用智能体"}},
		}, nil
	}

	text := "可用智能体列表：\n\n"
	for _, a := range agents {
		text += fmt.Sprintf("- ID: %d, 名称: %s, 描述: %s, 视频数: %d\n",
			a.ID, a.Name, a.Description, a.VideoCount)
	}

	return CallToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}, nil
}

// toolListVideos 列出视频
func (s *Server) toolListVideos(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	agentID := int64(getFloatArg(args, "agent_id", 0))
	if agentID <= 0 {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "agent_id is required"}},
		}, nil
	}

	videos, _, err := s.store.ListVideos(ctx, agentID, 0, model.VideoStatusReady, "", 1, 100)
	if err != nil {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "failed: " + err.Error()}},
		}, nil
	}

	if len(videos) == 0 {
		return CallToolResult{
			Content: []ToolContent{{Type: "text", Text: "该智能体暂无视频"}},
		}, nil
	}

	text := fmt.Sprintf("智能体 %d 的视频列表：\n\n", agentID)
	for _, v := range videos {
		text += fmt.Sprintf("- ID: %d, 标题: %s, 时长: %.0fs, 场景数: %d\n",
			v.ID, v.Title, v.Duration, v.SceneCount)
	}

	return CallToolResult{
		Content: []ToolContent{{Type: "text", Text: text}},
	}, nil
}

// toolGenerateReport 生成报告
func (s *Server) toolGenerateReport(ctx context.Context, args map[string]interface{}) (CallToolResult, error) {
	agentID := int64(getFloatArg(args, "agent_id", 0))
	title := getStringArg(args, "title", "")
	videoIDs := getStringArg(args, "video_ids", "")
	reportType := getStringArg(args, "report_type", "analysis")

	if agentID <= 0 || title == "" {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "agent_id and title are required"}},
		}, nil
	}

	report := &model.Report{
		AgentID:     agentID,
		Title:       title,
		ReportType:  reportType,
		Status:      model.ReportStatusGenerating,
		VideoIDs:    videoIDs,
		CreatorID:   0,
		CreatorName: "mcp",
	}
	if err := s.store.CreateReport(ctx, report); err != nil {
		return CallToolResult{
			IsError: true,
			Content: []ToolContent{{Type: "text", Text: "create report failed: " + err.Error()}},
		}, nil
	}

	return CallToolResult{
		Content: []ToolContent{{
			Type: "text",
			Text: fmt.Sprintf("报告生成任务已提交，报告ID: %d，状态: 生成中", report.ID),
		}},
	}, nil
}

// handleListPrompts 列出 Prompt
func (s *Server) handleListPrompts(ctx context.Context, params json.RawMessage) (interface{}, error) {
	configs, err := s.store.ListPromptConfigs(ctx, 0, "")
	if err != nil {
		return nil, err
	}

	prompts := make([]map[string]interface{}, 0, len(configs))
	for _, c := range configs {
		prompts = append(prompts, map[string]interface{}{
			"name":        c.Name,
			"description": c.Description,
			"type":        c.PromptType,
			"arguments":   []map[string]interface{}{},
		})
	}

	return map[string]interface{}{
		"prompts": prompts,
	}, nil
}

// handleGetPrompt 获取 Prompt
func (s *Server) handleGetPrompt(ctx context.Context, params json.RawMessage) (interface{}, error) {
	var req struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}

	configs, err := s.store.ListPromptConfigs(ctx, 0, "")
	if err != nil {
		return nil, err
	}

	for _, c := range configs {
		if c.Name == req.Name {
			return map[string]interface{}{
				"messages": []map[string]interface{}{
					{
						"role":    "system",
						"content": map[string]interface{}{"type": "text", "text": c.SystemPrompt},
					},
				},
			}, nil
		}
	}

	return nil, fmt.Errorf("prompt not found: %s", req.Name)
}

// handleListResources 列出资源
func (s *Server) handleListResources(ctx context.Context, params json.RawMessage) (interface{}, error) {
	return map[string]interface{}{
		"resources": []interface{}{},
	}, nil
}

// ---------- 工具函数 ----------

func getStringArg(args map[string]interface{}, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

func getFloatArg(args map[string]interface{}, key string, def float64) float64 {
	if v, ok := args[key]; ok {
		switch val := v.(type) {
		case float64:
			return val
		case int:
			return float64(val)
		case float32:
			return float64(val)
		}
	}
	return def
}
