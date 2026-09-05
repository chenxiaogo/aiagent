package middleware

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// 对外交付的调用鉴权：客户 API Key → 凭据 → 智能体 → 版本。
//
// 与 Auth（JWT，平台内部用户）完全独立：
//   - Auth        ：平台运营 / Agent 管理员登录后台
//   - ClientAuth  ：客户程序通过 MCP / API 调用已发布的智能体
//
// 中间件职责：解析凭据 → 校验状态/作用域/来源 → 配额限流 → 计量留痕。

const (
	CtxClient      = "client_credential"
	CtxClientAgent = "client_agent"
	CtxClientMCP   = "client_mcp_server"
)

// ClientAuth 客户凭据鉴权器。
type ClientAuth struct {
	store   *store.Store
	limiter *clientLimiter
}

// NewClientAuth 创建客户凭据鉴权器。
func NewClientAuth(s *store.Store) *ClientAuth {
	return &ClientAuth{store: s, limiter: newClientLimiter()}
}

// RequireScope 返回校验指定作用域的中间件。
// scope 取 model.ProtocolMCP / ProtocolChatAPI / ProtocolPortal。
func (a *ClientAuth) RequireScope(scope string) gin.HandlerFunc {
	return func(c *gin.Context) {
		token := ExtractClientToken(c)
		if token == "" {
			abortClient(c, http.StatusUnauthorized, "缺少 API Key")
			return
		}
		client, err := a.store.GetAgentClientByPlainKey(c.Request.Context(), token)
		if err != nil {
			abortClient(c, http.StatusUnauthorized, "API Key 无效")
			return
		}
		if !client.IsUsable(time.Now()) {
			abortClient(c, http.StatusForbidden, "API Key 已禁用或已过期")
			return
		}
		if !client.HasScope(scope) {
			abortClient(c, http.StatusForbidden, "API Key 未授权该调用方式")
			return
		}
		if !ipAllowed(client.IPAllowList, c.ClientIP()) {
			abortClient(c, http.StatusForbidden, "来源 IP 不在白名单内")
			return
		}
		if ok, reason := a.limiter.allow(a.store, client); !ok {
			ilog.Warnf("client %s(%d) over quota: %s", client.Name, client.ID, reason)
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
				"code": 429, "message": "调用超出配额：" + reason,
			})
			return
		}

		agent, err := a.store.GetAgent(c.Request.Context(), client.AgentID)
		if err != nil {
			abortClient(c, http.StatusNotFound, "智能体不存在")
			return
		}
		if agent.Status == model.AgentStatusOffline {
			abortClient(c, http.StatusForbidden, "智能体已下线")
			return
		}

		c.Set(CtxClient, client)
		c.Set(CtxClientAgent, agent)

		start := time.Now()
		c.Next()

		status := c.Writer.Status()
		errCount := 0
		if status >= http.StatusBadRequest {
			errCount = 1
		}
		a.store.RecordUsage(store.UsageEvent{
			AgentID:   client.AgentID,
			ClientID:  client.ID,
			TenantID:  client.TenantID,
			Protocol:  scope,
			Requests:  1,
			Errors:    errCount,
			LatencyMs: time.Since(start).Milliseconds(),
		})
		go a.store.TouchAgentClientUsedAt(client.ID)
	}
}

// ---------- 上下文取值辅助 ----------

// ClientFromContext 取当前请求的客户凭据。
func ClientFromContext(c *gin.Context) *model.AgentClient {
	v, _ := c.Get(CtxClient)
	client, _ := v.(*model.AgentClient)
	return client
}

// ClientAgentFromContext 取当前请求命中的智能体。
func ClientAgentFromContext(c *gin.Context) *model.Agent {
	v, _ := c.Get(CtxClientAgent)
	agent, _ := v.(*model.Agent)
	return agent
}

// ---------- 限流 ----------

// clientLimiter 进程内固定窗口限流。
// 说明：单实例部署足够；多实例时把分钟级窗口换到 Redis 即可，日配额仍以库为准（跨天/重启会用库里的值做基线）。
type clientLimiter struct {
	mu      sync.Mutex
	minutes map[int64]*windowCounter
	days    map[int64]*windowCounter
}

type windowCounter struct {
	window string
	count  int
}

func newClientLimiter() *clientLimiter {
	return &clientLimiter{
		minutes: make(map[int64]*windowCounter),
		days:    make(map[int64]*windowCounter),
	}
}

// allow 检查分钟级与日级配额，通过则计数 +1。
func (l *clientLimiter) allow(s *store.Store, client *model.AgentClient) (bool, string) {
	now := time.Now()
	minuteWin := now.Format("200601021504")
	dayWin := now.Format("20060102")

	l.mu.Lock()
	defer l.mu.Unlock()

	rpm := client.QuotaRPM
	if rpm <= 0 {
		rpm = 60
	}
	minCounter := l.bump(l.minutes, client.ID, minuteWin, 0)
	if minCounter.count > rpm {
		return false, "每分钟调用数超过配额"
	}

	tpd := client.QuotaTPD
	if tpd > 0 {
		dayCounter, fresh := l.bumpWithBaseline(l.days, client.ID, dayWin)
		if fresh {
			// 新的一天（或进程刚启动）：用库中当日累计值做基线，避免重启后配额被绕过
			dayCounter.count = int(s.SumClientRequestsToday(context.Background(), client.ID))
		}
		if dayCounter.count > tpd {
			return false, "每日调用数超过配额"
		}
	}
	return true, ""
}

// bump 计数 +1，窗口变化则重置。
func (l *clientLimiter) bump(m map[int64]*windowCounter, id int64, window string, base int) *windowCounter {
	c, ok := m[id]
	if !ok || c.window != window {
		c = &windowCounter{window: window, count: base}
		m[id] = c
	}
	c.count++
	return c
}

// bumpWithBaseline 计数 +1，并返回是否需要用库中数据重置基线。
func (l *clientLimiter) bumpWithBaseline(m map[int64]*windowCounter, id int64, window string) (*windowCounter, bool) {
	c, ok := m[id]
	fresh := !ok || c.window != window
	if fresh {
		c = &windowCounter{window: window}
		m[id] = c
	}
	c.count++
	return c, fresh
}

// ---------- 工具函数 ----------

// ExtractClientToken 从请求中提取客户 API Key。
// 支持三种携带方式，覆盖不同客户端的接入习惯：
//   - Authorization: Bearer <key>
//   - X-Api-Key: <key>
//   - URL 查询参数 ?key=<key>（高德 MCP 风格，只靠一条 URL 即可接入，无需自定义请求头）
//     / ?api_key=<key>（兼容早期命名）
//
// 导出是给 SSE 端点构造回传地址用的：它要把 key 原样带回给客户端，
// 否则客户端后续 POST 消息时会丢失凭据。
func ExtractClientToken(c *gin.Context) string {
	if v := strings.TrimSpace(strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.GetHeader("X-Api-Key")); v != "" {
		return v
	}
	if v := strings.TrimSpace(c.Query("key")); v != "" {
		return v
	}
	return strings.TrimSpace(c.Query("api_key"))
}

func ipAllowed(allowList, ip string) bool {
	allowList = strings.TrimSpace(allowList)
	if allowList == "" || ip == "" {
		return true
	}
	for _, item := range strings.Split(allowList, ",") {
		if strings.TrimSpace(item) == ip {
			return true
		}
	}
	return false
}

func abortClient(c *gin.Context, code int, msg string) {
	c.AbortWithStatusJSON(code, gin.H{"code": 1, "message": msg})
}
