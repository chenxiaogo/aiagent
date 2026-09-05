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

// ApiHandler 接口（API）管理。
type ApiHandler struct {
	store *store.Store
}

func NewApiHandler(s *store.Store) *ApiHandler {
	return &ApiHandler{store: s}
}

// RegisterRoute 注册接口管理路由。
func (h *ApiHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/apis")
	{
		group.GET("", middleware.RequirePerm(model.PermRoleManage), h.List)
		group.POST("", middleware.RequirePerm(model.PermRoleManage), h.Create)
		group.PUT("/:id", middleware.RequirePerm(model.PermRoleManage), h.Update)
		group.DELETE("/:id", middleware.RequirePerm(model.PermRoleManage), h.Delete)
	}
}

// List 接口列表。
func (h *ApiHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": h.store.ListApis(tracex.FromRequest(c))})
}

type apiReq struct {
	Path        string `json:"path" binding:"required"`
	Method      string `json:"method" binding:"required"`
	Group       string `json:"group"`
	Description string `json:"description"`
}

// Create 新增接口。
func (h *ApiHandler) Create(c *gin.Context) {
	var req apiReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	a, ok := h.store.CreateApi(tracex.FromRequest(c), &model.Api{Path: req.Path, Method: req.Method, Group: req.Group, Description: req.Description})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "新增接口失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 新增接口 #%d (%s %s)", operator, a.ID, req.Method, req.Path)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "新增成功", "data": a.ID})
}

// Update 更新接口。
func (h *ApiHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetApi(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "接口不存在"})
		return
	}
	var req apiReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	a, ok := h.store.UpdateApi(tracex.FromRequest(c), &model.Api{ID: id, Path: req.Path, Method: req.Method, Group: req.Group, Description: req.Description})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "更新失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新接口 #%d (%s %s)", operator, id, req.Method, req.Path)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功", "data": a.ID})
}

// Delete 删除接口。
func (h *ApiHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if !h.store.DeleteApi(tracex.FromRequest(c), id) {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "接口不存在"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 删除接口 #%d", operator, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}
