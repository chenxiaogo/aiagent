package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"aiagent/internal/mcp"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// AgentMCPHandler 单个已发布智能体的对外 MCP 端点（客户侧接入）。
//
// 两种寻址方式（都用客户 API Key 鉴权，不走 JWT）：
//
// 1) 单端点，只用 ?key= 区分智能体（高德 MCP 风格，推荐）：
//
//	POST /api/mcp?key=xxx       JSON-RPC 请求响应（Streamable HTTP 风格）
//	GET  /api/mcp?key=xxx       SSE 事件流（Accept: text/event-stream）
//	POST /api/mcp/rpc?key=xxx   SSE 模式下客户端回传消息
//
// key 即凭据，凭据本身已绑定 AgentID，因此无需在 URL 里再写智能体标识。
//
// 2) 旧式按 slug 寻址（保留兼容）：
//
//	GET  /api/mcp/agents/:slug/sse
//	POST /api/mcp/agents/:slug/messages
//	POST /api/mcp/agents/:slug/stream
//
// 版本由「凭据钉版本 > 客户订阅钉版本 > 默认版本」决定，
// 因此同一智能体的不同客户可以稳定跑在不同版本上。
type AgentMCPHandler struct {
	store      *store.Store
	clientAuth *middleware.ClientAuth

	// SSE 会话：sessionID → 消息通道
	sessions   map[string]chan []byte
	sessionsMu sync.RWMutex
}

// NewAgentMCPHandler 创建智能体 MCP 端点 Handler。
func NewAgentMCPHandler(s *store.Store) *AgentMCPHandler {
	return &AgentMCPHandler{
		store:      s,
		clientAuth: middleware.NewClientAuth(s),
		sessions:   make(map[string]chan []byte),
	}
}

// RegisterRoute 注册对外 MCP 路由（挂在未鉴权的 /api 分组下，内部用客户凭据鉴权）。
func (h *AgentMCPHandler) RegisterRoute(g *gin.RouterGroup) {
	auth := h.clientAuth.RequireScope(model.ProtocolMCP)

	// 旧式：URL 里带智能体 slug
	grp := g.Group("/mcp/agents/:slug")
	{
		grp.GET("/sse", auth, h.SSE)
		grp.POST("/messages", auth, h.PostMessage)
		grp.POST("/stream", auth, h.Stream)
	}

	// 单端点：只用 ?key= 区分智能体（高德 MCP 风格）。
	// 注意复用 /api/mcp/messages 会与平台全局 MCP 端点冲突，因此 SSE 回传消息走 /api/mcp/rpc。
	single := g.Group("/mcp")
	{
		single.POST("", auth, h.Stream)
		single.GET("", auth, h.SSE)
		single.POST("/rpc", auth, h.PostMessage)
	}
}

// Stream 简单 JSON-RPC over HTTP（推荐：最容易联调）。
func (h *AgentMCPHandler) Stream(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	client, srv, ok := h.resolve(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var req mcp.Request
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON-RPC request"})
		return
	}
	toolName := ""
	if req.Method == mcp.MethodCallTool {
		toolName = mcp.ToolNameFromRequest(&req)
	}
	h.countToolCall(ctx, client, srv, req.Method, toolName)
	// 回显客户端会话 ID：便于 Streamable HTTP 客户端保持会话粘滞（平台本身无状态）。
	if sid := c.GetHeader("Mcp-Session-Id"); sid != "" {
		c.Header("Mcp-Session-Id", sid)
	}
	c.JSON(http.StatusOK, srv.HandleRequest(ctx, &req))
}

// SSE 建立 SSE 连接（MCP 标准 SSE 传输）。
// 版本在建立连接时确定（校验凭据与版本归属即可），后续消息走 PostMessage 单独解析。
func (h *AgentMCPHandler) SSE(c *gin.Context) {
	if _, _, ok := h.resolve(c); !ok {
		return
	}

	sessionID := c.GetHeader("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = "ags-" + tracex.NewTraceID()[:16]
	}
	msgCh := make(chan []byte, 100)
	h.sessionsMu.Lock()
	h.sessions[sessionID] = msgCh
	h.sessionsMu.Unlock()

	defer func() {
		h.sessionsMu.Lock()
		delete(h.sessions, sessionID)
		h.sessionsMu.Unlock()
		close(msgCh)
		ilog.Infof("agent MCP SSE disconnected: %s", sessionID)
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Mcp-Session-Id", sessionID)

	// 回传端点取决于本次接入方式：
	//   - 带 slug 的旧式端点沿用 /api/mcp/agents/<slug>/messages
	//   - 单端点接入走 /api/mcp/rpc；若 key 来自 URL 查询参数则原样带回，
	//     保证只靠一条 URL 也能完成后续握手（高德 MCP 的接入习惯）。
	endpoint := fmt.Sprintf("/api/mcp/rpc?sessionId=%s", sessionID)
	if slug := c.Param("slug"); slug != "" {
		endpoint = fmt.Sprintf("/api/mcp/agents/%s/messages?sessionId=%s", slug, sessionID)
	}
	if qKey := strings.TrimSpace(c.Query("key")); qKey != "" {
		endpoint += "&key=" + url.QueryEscape(qKey)
	} else if qKey := strings.TrimSpace(c.Query("api_key")); qKey != "" {
		endpoint += "&api_key=" + url.QueryEscape(qKey)
	}
	fmt.Fprintf(c.Writer, "event: endpoint\ndata: %s\n\n", endpoint)
	c.Writer.Flush()
	ilog.Infof("agent MCP SSE connected: %s (%s)", sessionID, c.Param("slug"))

	for {
		select {
		case msg, ok := <-msgCh:
			if !ok {
				return
			}
			fmt.Fprintf(c.Writer, "event: message\ndata: %s\n\n", string(msg))
			c.Writer.Flush()
		case <-c.Request.Context().Done():
			return
		}
	}
}

// PostMessage SSE 模式下客户端发送消息的端点。
func (h *AgentMCPHandler) PostMessage(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	client, srv, ok := h.resolve(c)
	if !ok {
		return
	}
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid body"})
		return
	}
	var req mcp.Request
	if err := json.Unmarshal(body, &req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON-RPC request"})
		return
	}
	toolName := ""
	if req.Method == mcp.MethodCallTool {
		toolName = mcp.ToolNameFromRequest(&req)
	}
	h.countToolCall(ctx, client, srv, req.Method, toolName)

	resp := srv.HandleRequest(ctx, &req)
	respBytes, _ := json.Marshal(resp)

	sessionID := c.Query("sessionId")
	if sessionID == "" {
		sessionID = c.GetHeader("Mcp-Session-Id")
	}
	h.sessionsMu.RLock()
	ch, ok := h.sessions[sessionID]
	h.sessionsMu.RUnlock()

	if ok {
		select {
		case ch <- respBytes:
			c.JSON(http.StatusAccepted, gin.H{"status": "sent via SSE"})
		default:
			c.JSON(http.StatusOK, resp)
		}
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ---------- 内部辅助 ----------

// resolve 解析本次调用生效的凭据、智能体与 MCP 服务器。
//
// 智能体有两种来源：URL 里的 slug（旧式），或凭据自身绑定的 AgentID（?key= 单端点）。
// 走 slug 时额外校验凭据确实指向该智能体，防止用 A 的 key 访问 B。
func (h *AgentMCPHandler) resolve(c *gin.Context) (*model.AgentClient, *mcp.AgentServer, bool) {
	ctx := tracex.FromRequest(c)
	client := middleware.ClientFromContext(c)
	if client == nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "message": "缺少有效的 API Key"})
		return nil, nil, false
	}
	// 鉴权中间件已按 key 解析出凭据绑定的智能体
	agent := middleware.ClientAgentFromContext(c)
	if slug := c.Param("slug"); slug != "" {
		bySlug, err := h.store.GetAgentBySlug(ctx, slug)
		if err != nil {
			c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "智能体不存在或未发布"})
			return nil, nil, false
		}
		if client.AgentID != bySlug.ID {
			c.JSON(http.StatusForbidden, gin.H{"code": 403, "message": "API Key 无权访问该智能体"})
			return nil, nil, false
		}
		agent = bySlug
	}
	if agent == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "智能体不存在或未发布"})
		return nil, nil, false
	}
	release, err := h.store.ResolveReleaseForClient(ctx, agent, client)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 404, "message": "智能体尚未发布任何版本"})
		return nil, nil, false
	}
	srv, err := mcp.NewAgentServer(h.store, agent, release)
	if err != nil {
		ilog.Errorf("build agent mcp server: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 500, "message": "构建智能体服务失败"})
		return nil, nil, false
	}
	return client, srv, true
}

// countToolCall 工具调用单独计量（与中间件的请求计量合并到同一条日记录），
// 同时写一条 mcp_tool 类型的调用观测日志（CallLog）。
func (h *AgentMCPHandler) countToolCall(ctx context.Context, client *model.AgentClient, srv *mcp.AgentServer, method, toolName string) {
	if method != mcp.MethodCallTool {
		return
	}
	h.store.RecordUsage(store.UsageEvent{
		AgentID:   client.AgentID,
		ClientID:  client.ID,
		TenantID:  client.TenantID,
		Protocol:  model.ProtocolMCP,
		ToolCalls: 1,
	})
	// 调用观测：记录该次 MCP 工具调用（用于排障与成本下钻）
	releaseID := int64(0)
	if srv != nil && srv.Release() != nil {
		releaseID = srv.Release().ID
	}
	go func() {
		_ = h.store.RecordCallLog(context.Background(), &model.CallLog{
			TenantID:   client.TenantID,
			AgentID:    client.AgentID,
			ClientID:   client.ID,
			ReleaseID:  releaseID,
			CallType:   model.CallTypeMCPTool,
			ToolName:   toolName,
			Status:     1,
			TraceID:    tracex.TraceIDFromContext(ctx),
			CreatedAt:  time.Now(),
		})
	}()
}
