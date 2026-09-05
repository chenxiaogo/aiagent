package mcpclient

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"aiagent/internal/model"
)

// Client MCP 客户端：用于连接外部 MCP 服务器（与 internal/mcp 的 Server 方向相反）。
// 支持两种传输：
//   - streamable_http：POST JSON-RPC；高德等有状态 server 需先 initialize 再带 Mcp-Session-Id 头
//   - sse：GET /sse 建立事件流后再 POST /messages（此处用短连接的简化实现：
//     直接对同一端点发起带 Accept: text/event-stream 的 POST，兼容多数服务端）
type Client struct {
	client *http.Client
	mu     sync.Mutex
	// sessions 按 server URL 缓存 Mcp-Session-Id（仅 streamable_http 有状态模式需要）。
	sessions map[string]string
}

// Tool 外部 MCP 服务器暴露的工具
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	InputSchema map[string]interface{} `json:"inputSchema,omitempty"`
}

// NewClient 创建 MCP 客户端。
func NewClient() *Client {
	return &Client{
		client:  &http.Client{Timeout: 30 * time.Second},
		sessions: make(map[string]string),
	}
}

// rpcRequest JSON-RPC 请求
type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int         `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

// rpcResponse JSON-RPC 响应
type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error,omitempty"`
}

// ListTools 列出远端 MCP 服务器提供的工具。
func (c *Client) ListTools(srv *model.AgentMCPServer) ([]Tool, error) {
	result, err := c.call(srv, "tools/list", map[string]interface{}{})
	if err != nil {
		return nil, err
	}

	var out struct {
		Tools []Tool `json:"tools"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return nil, fmt.Errorf("parse tools/list: %w", err)
	}
	return out.Tools, nil
}

// CallTool 调用远端工具，返回其文本内容。
func (c *Client) CallTool(srv *model.AgentMCPServer, name string, args map[string]interface{}) (string, error) {
	result, err := c.call(srv, "tools/call", map[string]interface{}{
		"name":      name,
		"arguments": args,
	})
	if err != nil {
		return "", err
	}

	// MCP 规范：result = { content: [ { type:"text", text:"..." } ], isError: bool }
	var out struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		IsError bool `json:"isError"`
	}
	if err := json.Unmarshal(result, &out); err != nil {
		return "", fmt.Errorf("parse tools/call: %w", err)
	}

	var buf bytes.Buffer
	for _, item := range out.Content {
		if item.Text != "" {
			if buf.Len() > 0 {
				buf.WriteString("\n")
			}
			buf.WriteString(item.Text)
		}
	}
	return buf.String(), nil
}

// call 发起一次 JSON-RPC 调用。streamable_http 传输下会先确保已完成 initialize 握手
// （高德等有状态 server 要求后续请求携带 Mcp-Session-Id），无状态 server 忽略会话头即可。
func (c *Client) call(srv *model.AgentMCPServer, method string, params interface{}) (json.RawMessage, error) {
	if srv == nil || srv.URL == "" {
		return nil, fmt.Errorf("MCP 服务器地址为空")
	}
	if srv.Transport == model.MCPTransportStreamableHTTP {
		c.ensureSession(srv)
	}
	return c.doRequest(srv, method, params, true)
}

// ensureSession 对 streamable_http 传输先发 initialize 握手，缓存服务端返回的 Mcp-Session-Id。
// 已缓存则跳过；无状态 server 不返回该头，不影响后续调用。
func (c *Client) ensureSession(srv *model.AgentMCPServer) {
	c.mu.Lock()
	if c.sessions[srv.URL] != "" {
		c.mu.Unlock()
		return
	}
	c.mu.Unlock()

	// initialize 本身不带会话头（attachSession=false），响应头里的 Mcp-Session-Id 由 doRequest 写回 sessions。
	_, _ = c.doRequest(srv, "initialize", map[string]interface{}{
		"protocolVersion": "2024-11-05",
		"capabilities":    map[string]interface{}{},
		"clientInfo":      map[string]interface{}{"name": "aiagent", "version": "1.0"},
	}, false)
}

// doRequest 实际发送一次 JSON-RPC POST。
// attachSession 为 true 时携带已缓存的 Mcp-Session-Id（initialize 握手本身传 false）。
// streamable_http 有状态 server 可能在任意响应回写新的会话 ID，此处统一捕获更新。
func (c *Client) doRequest(srv *model.AgentMCPServer, method string, params interface{}, attachSession bool) (json.RawMessage, error) {
	body, err := json.Marshal(rpcRequest{
		JSONRPC: "2.0",
		ID:      1,
		Method:  method,
		Params:  params,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, srv.URL, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	if attachSession {
		c.mu.Lock()
		sid := c.sessions[srv.URL]
		c.mu.Unlock()
		if sid != "" {
			req.Header.Set("Mcp-Session-Id", sid)
		}
	}

	// 附加自定义请求头（常用于鉴权）
	if srv.Headers != "" {
		var headers map[string]string
		if err := json.Unmarshal([]byte(srv.Headers), &headers); err == nil {
			for k, v := range headers {
				req.Header.Set(k, v)
			}
		}
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request %s: %w", srv.URL, err)
	}
	defer resp.Body.Close()

	// 有状态 streamable_http：服务端可能在任意响应回写新的会话 ID，缓存之。
	if sid := resp.Header.Get("Mcp-Session-Id"); sid != "" {
		c.mu.Lock()
		c.sessions[srv.URL] = sid
		c.mu.Unlock()
	}

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("MCP 返回 %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}

	// SSE 传输下响应形如 "data: {json}\r\n\r\n"，需要剥离前缀
	payload := extractJSON(string(raw))

	var rpcResp rpcResponse
	if err := json.Unmarshal([]byte(payload), &rpcResp); err != nil {
		return nil, fmt.Errorf("parse response: %w", err)
	}
	if rpcResp.Error != nil {
		return nil, fmt.Errorf("MCP error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
	}
	return rpcResp.Result, nil
}

// extractJSON 从可能带 SSE 前缀的文本中提取 JSON 部分。
func extractJSON(s string) string {
	for _, prefix := range []string{"data:", "event:"} {
		if idx := indexOf(s, prefix); idx >= 0 {
			rest := s[idx+len(prefix):]
			if end := indexOf(rest, "\n"); end >= 0 {
				rest = rest[:end]
			}
			return trimSpace(rest)
		}
	}
	return trimSpace(s)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

func trimSpace(s string) string {
	start, end := 0, len(s)
	for start < end && (s[start] == ' ' || s[start] == '\t' || s[start] == '\r' || s[start] == '\n') {
		start++
	}
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\r' || s[end-1] == '\n') {
		end--
	}
	return s[start:end]
}

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}
