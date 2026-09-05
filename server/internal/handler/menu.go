package handler

import (
	"net/http"
	"strconv"
	"time"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// MenuHandler 菜单管理接口。
type MenuHandler struct {
	store *store.Store
}

func NewMenuHandler(s *store.Store) *MenuHandler {
	return &MenuHandler{store: s}
}

// RegisterRoute 注册菜单路由。
func (h *MenuHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/menus")
	{
		group.GET("/my", h.MyMenus) // 当前用户动态菜单（登录后拉取）
		group.GET("", middleware.RequirePerm(model.PermRoleManage), h.List)
		group.POST("", middleware.RequirePerm(model.PermRoleManage), h.Create)
		group.PUT("/:id", middleware.RequirePerm(model.PermRoleManage), h.Update)
		group.PUT("/:id/btns", middleware.RequirePerm(model.PermRoleManage), h.SaveBtns)
		group.DELETE("/:id", middleware.RequirePerm(model.PermRoleManage), h.Delete)
	}
}

// MyMenus 当前用户可访问的菜单树（含按钮授权）。
func (h *MenuHandler) MyMenus(c *gin.Context) {
	roleID := middleware.CurrentRoleID(c)
	isAdmin := middleware.CurrentIsAdmin(c)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": h.store.GetRoleMenuTree(tracex.FromRequest(c), roleID, isAdmin)})
}

// List 菜单树（管理端）。
func (h *MenuHandler) List(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": h.store.ListMenus(tracex.FromRequest(c))})
}

type menuReq struct {
	ParentID  int64  `json:"parentId"`
	Name      string `json:"name" binding:"required"`
	Path      string `json:"path"`
	Component string `json:"component"`
	Title     string `json:"title" binding:"required"`
	Icon      string `json:"icon"`
	Sort      int    `json:"sort"`
	Hidden    bool   `json:"hidden"`
	Type      string `json:"type"`
	PermCode  string `json:"permCode"`
}

// Create 新增菜单。
func (h *MenuHandler) Create(c *gin.Context) {
	var req menuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	menuType := req.Type
	if menuType == "" {
		menuType = "menu"
	}
	m, ok := h.store.CreateMenu(tracex.FromRequest(c), &model.Menu{
		ParentID: req.ParentID, Name: req.Name, Path: req.Path,
		Component: req.Component, Title: req.Title, Icon: req.Icon,
		Sort: req.Sort, Hidden: req.Hidden, Type: menuType, PermCode: req.PermCode,
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建菜单失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 创建菜单 #%d (%s, path=%s)", operator, m.ID, m.Title, m.Path)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": m.ID})
}

// Update 更新菜单。
func (h *MenuHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetMenu(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "菜单不存在"})
		return
	}
	var req menuReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	m, ok := h.store.UpdateMenu(tracex.FromRequest(c), &model.Menu{
		ID: id, ParentID: req.ParentID, Name: req.Name, Path: req.Path,
		Component: req.Component, Title: req.Title, Icon: req.Icon,
		Sort: req.Sort, Hidden: req.Hidden, Type: req.Type, PermCode: req.PermCode,
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "更新失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新菜单 #%d (%s)", operator, id, m.Title)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功", "data": m.ID})
}

type saveBtnsReq struct {
	Btns []struct {
		Name     string `json:"name"`
		Desc     string `json:"desc"`
		PermCode string `json:"permCode"`
	} `json:"btns"`
}

// SaveBtns 保存菜单按钮定义。
func (h *MenuHandler) SaveBtns(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetMenu(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "菜单不存在"})
		return
	}
	var req saveBtnsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	now := time.Now()
	btns := make([]*model.MenuBtn, 0, len(req.Btns))
	for _, b := range req.Btns {
		btns = append(btns, &model.MenuBtn{Name: b.Name, Desc: b.Desc, PermCode: b.PermCode, CreatedAt: now})
	}
	if err := h.store.SaveMenuBtns(tracex.FromRequest(c), id, btns); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存按钮失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 保存菜单 #%d 的按钮定义 (%d 个)", operator, id, len(btns))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "按钮已保存"})
}

// Delete 删除菜单。
func (h *MenuHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if !h.store.DeleteMenu(tracex.FromRequest(c), id) {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "菜单不存在"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 删除菜单 #%d", operator, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}
