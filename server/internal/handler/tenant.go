package handler

import (
	"net/http"
	"strconv"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// TenantHandler 客户管理。
//
// 客户主体是平台用户：授权谁能调用某个智能体，就是从用户列表里挑人。
// 早先版本靠手填客户名称建客户，客户与用户两套实体互不相干，
// 这里把它们统一到 UserID 上，手填路径仅在没传 userId 时作兼容保留。
type TenantHandler struct {
	store *store.Store
}

func NewTenantHandler(s *store.Store) *TenantHandler {
	return &TenantHandler{store: s}
}

// RegisterRoute 注册客户管理路由。
func (h *TenantHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/tenants")
	{
		group.GET("", middleware.RequirePerm(model.PermUserManage), h.List)
		group.GET("/candidates", middleware.RequirePerm(model.PermUserManage), h.Candidates)
		group.POST("", middleware.RequirePerm(model.PermUserManage), h.Create)
		group.PUT("/:id/bind", middleware.RequirePerm(model.PermUserManage), h.Bind)
		group.DELETE("/:id", middleware.RequirePerm(model.PermUserManage), h.Delete)
	}
}

// List 客户列表（带关联用户信息与授权计数）。
func (h *TenantHandler) List(c *gin.Context) {
	items, err := h.store.ListTenantItems(tracex.FromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// Candidates 可作为客户的系统用户列表（含「已是客户」标记）。
func (h *TenantHandler) Candidates(c *gin.Context) {
	items, err := h.store.ListTenantCandidates(tracex.FromRequest(c))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

type createTenantReq struct {
	UserID int64  `json:"userId"`
	Name   string `json:"name"`
}

// Create 从系统用户创建客户。
// 传 userId 时按用户建立/复用客户；不传则退回按名称创建（兼容早期调用）。
func (h *TenantHandler) Create(c *gin.Context) {
	var req createTenantReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	ctx := tracex.FromRequest(c)

	var tenant *model.Tenant
	var err error
	if req.UserID > 0 {
		tenant, err = h.store.EnsureTenantForUser(ctx, req.UserID)
	} else if req.Name != "" {
		tenant, err = h.store.EnsureTenantByName(ctx, req.Name)
	} else {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "userId 或 name 必填其一"})
		return
	}
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 创建客户 #%d (%s, userId=%d)", operator, tenant.ID, tenant.Name, tenant.UserID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": tenant.ID})
}

type bindTenantReq struct {
	UserID int64 `json:"userId"`
}

// Bind 把客户绑定到指定系统用户。
// 用于历史遗留客户（迁移时按名称没匹配上用户、userId 仍为 0）补关联。
func (h *TenantHandler) Bind(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req bindTenantReq
	if err := c.ShouldBindJSON(&req); err != nil || req.UserID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "userId 必填"})
		return
	}
	if _, ok := h.store.GetTenant(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "客户不存在"})
		return
	}
	if err := h.store.BindTenantUser(tracex.FromRequest(c), id, req.UserID); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 将客户 #%d 绑定到用户 #%d", operator, id, req.UserID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已绑定"})
}

// Delete 删除客户（其下仍有凭据或订阅时拒绝）。
func (h *TenantHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	tenant, ok := h.store.GetTenant(tracex.FromRequest(c), id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "客户不存在"})
		return
	}
	if err := h.store.DeleteTenant(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": err.Error()})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 删除客户 #%d (%s)", operator, id, tenant.Name)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}
