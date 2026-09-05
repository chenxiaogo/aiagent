package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"

	"aiagent/internal/mcp"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// MCPHandler MCP 服务器 HTTP 接口。
// 支持两种传输方式：
// 1. SSE 模式：GET /mcp/sse 建立 SSE 连接，客户端通过 POST /mcp/messages 发送消息
// 2. 流式模式：POST /mcp/stream 直接 JSON-RPC over HTTP
type MCPHandler struct {
	store  *store.Store
	server *mcp.Server
	// SSE 连接管理
	clients   map[string]chan []byte
	clientsMu sync.RWMutex
}

// NewMCPHandler 创建 MCP Handler。
func NewMCPHandler(s *store.Store) *MCPHandler {
	return &MCPHandler{
		store:   s,
		server:  mcp.NewServer(s),
		clients: make(map[string]chan []byte),
	}
}

// RegisterRoute 注册路由（MCP 接口不需要登录鉴权，通过 API Key 验证）。
func (h *MCPHandler) RegisterRoute(g *gin.RouterGroup) {
	// 公开 MCP 端点
	mcpGroup := g.Group("/mcp")
	{
		mcpGroup.GET("/sse", h.SSE)
		mcpGroup.POST("/messages", h.PostMessage)
		mcpGroup.POST("/stream", h.Stream)
	}
}

// SSE 建立 SSE 连接（MCP 标准 SSE 传输）
func (h *MCPHandler) SSE(c *gin.Context) {
	sessionID := c.GetHeader("Mcp-Session-Id")
	if sessionID == "" {
		sessionID = "mcp-" + tracex.NewTraceID()[:16]
	}

	// 创建消息通道
	msgCh := make(chan []byte, 100)
	h.clientsMu.Lock()
	h.clients[sessionID] = msgCh
	h.clientsMu.Unlock()

	defer func() {
		h.clientsMu.Lock()
		delete(h.clients, sessionID)
		h.clientsMu.Unlock()
		close(msgCh)
		ilog.Infof("MCP SSE client disconnected: %s", sessionID)
	}()

	c.Writer.Header().Set("Content-Type", "text/event-stream")
	c.Writer.Header().Set("Cache-Control", "no-cache")
	c.Writer.Header().Set("Connection", "keep-alive")
	c.Writer.Header().Set("X-Accel-Buffering", "no")
	c.Writer.Header().Set("Mcp-Session-Id", sessionID)

	// 发送 endpoint 事件（MCP 协议要求）
	fmt.Fprintf(c.Writer, "event: endpoint\ndata: %s\n\n", "/api/mcp/messages?sessionId="+sessionID)
	c.Writer.Flush()

	ilog.Infof("MCP SSE client connected: %s", sessionID)

	// 保持连接并转发消息
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

// PostMessage SSE 模式下客户端发送消息的端点
func (h *MCPHandler) PostMessage(c *gin.Context) {
	sessionID := c.Query("sessionId")
	if sessionID == "" {
		sessionID = c.GetHeader("Mcp-Session-Id")
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

	ctx := tracex.FromRequest(c)
	resp := h.server.HandleRequest(ctx, &req)

	respBytes, _ := json.Marshal(resp)

	// 如果有 SSE 连接，通过 SSE 发送响应
	h.clientsMu.RLock()
	ch, ok := h.clients[sessionID]
	h.clientsMu.RUnlock()

	if ok {
		select {
		case ch <- respBytes:
			c.JSON(http.StatusAccepted, gin.H{"status": "sent via SSE"})
		default:
			c.JSON(http.StatusOK, resp)
		}
	} else {
		c.JSON(http.StatusOK, resp)
	}
}

// Stream 直接 HTTP POST 模式（简单 JSON-RPC 请求响应）
func (h *MCPHandler) Stream(c *gin.Context) {
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

	ctx := tracex.FromRequest(c)
	resp := h.server.HandleRequest(ctx, &req)

	c.JSON(http.StatusOK, resp)
}
