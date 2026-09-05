package route

import (
	"context"
	"net/http"

	"aiagent/internal/handler"
	"aiagent/internal/middleware"
	"aiagent/internal/store"
	"aiagent/pkg/app/config"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
)

// Registrar 业务 Handler 实现该接口以注册路由。
type Registrar interface {
	RegisterRoute(*gin.RouterGroup)
}

// Route 封装 Gin 路由。
type Route struct {
	engine *gin.Engine
	logic  *handler.Logic
}

// NewRoute 创建路由并注册所有 Handler。
func NewRoute(ctx context.Context, conf *config.Config, dataStore *store.Store) (*Route, error) {
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	engine.Use(gin.Recovery())
	engine.Use(tracex.Middleware())
	engine.Use(corsMiddleware())

	r := &Route{engine: engine}
	r.logic = handler.NewLogic(ctx, conf, dataStore)

	api := engine.Group("/api")
	api.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	engine.GET("/ping", func(c *gin.Context) {
		c.String(http.StatusOK, "pong")
	})

	// 认证路由（无需登录）
	authGroup := api.Group("/auth")
	r.logic.Auth.RegisterRoute(authGroup)

	// MCP 路由（无需登录，通过 API Key 验证）
	r.logic.MCP.RegisterRoute(api)

	// 智能体对外 MCP 端点（无需登录，通过客户 API Key 验证）
	r.logic.AgentMCP.RegisterRoute(api)

	// 导出的 HTML 文件静态下载（无需登录，文件名不可枚举）
	r.logic.Chat.RegisterPublicRoute(api)

	// 业务路由：挂载 JWT 鉴权 + API 权限（casbin）中间件
	authMiddleware := middleware.Auth(r.logic.JWT)
	protected := api.Group("")
	protected.Use(authMiddleware)
	protected.Use(middleware.CasbinAuth(dataStore.Enforcer, dataStore))

	// 需登录的认证路由（当前用户信息）
	r.logic.Auth.RegisterProtectedRoute(protected.Group("/auth"))

	for _, reg := range []Registrar{
		r.logic.Chat,
		r.logic.File,
		r.logic.Knowledge,
		r.logic.Agent,
		r.logic.Video,
		r.logic.ModelConfig,
		r.logic.PromptConfig,
		r.logic.Report,
		r.logic.Analysis,
		r.logic.Camera,
		r.logic.AgentDelivery,
		r.logic.AgentModel,
		r.logic.AgentResource,
		r.logic.Market,
		r.logic.Host,
		// 权限管理（RBAC）
		r.logic.User,
		r.logic.Role,
		r.logic.Menu,
		r.logic.Api,
		// 客户管理（对外交付的授权对象）
		r.logic.Tenant,
		// 索引重建（文档 / 视频 / 摄像头向量）
		r.logic.Reindex,
	} {
		reg.RegisterRoute(protected)
	}

	return r, nil
}

// Handler 返回 HTTP Handler。
func (r *Route) Handler() http.Handler {
	return r.engine
}

// Engine 返回 Gin Engine。
func (r *Route) Engine() *gin.Engine {
	return r.engine
}

// AttachHandler 挂载额外的 HTTP 处理逻辑（如静态文件 SPA）。
func (r *Route) AttachHandler(h func(w http.ResponseWriter, req *http.Request)) {
	r.engine.NoRoute(func(c *gin.Context) {
		h(c.Writer, c.Request)
	})
}

func logMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Next()
		tid := tracex.TraceIDShort(c.Request.Context())
		ilog.Infof("[%s] %s %s %d", tid, c.Request.Method, c.Request.URL.Path, c.Writer.Status())
	}
}

func corsMiddleware() gin.HandlerFunc {
	cfg := cors.DefaultConfig()
	cfg.AllowAllOrigins = true
	cfg.AllowMethods = []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"}
	cfg.AllowHeaders = []string{"Origin", "Content-Type", "Accept", "Authorization", "Token"}
	cfg.ExposeHeaders = []string{"Content-Length"}
	return cors.New(cfg)
}