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

// RoleHandler 角色管理接口。
type RoleHandler struct {
	store *store.Store
}

func NewRoleHandler(s *store.Store) *RoleHandler {
	return &RoleHandler{store: s}
}

// RegisterRoute 注册角色管理路由。
func (h *RoleHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/roles")
	{
		group.GET("", middleware.RequirePerm(model.PermRoleManage), h.List)
		group.GET("/permissions", h.ListPermissions)
		group.GET("/:id", middleware.RequirePerm(model.PermRoleManage), h.Get)
		group.POST("", middleware.RequirePerm(model.PermRoleManage), h.Create)
		group.PUT("/:id", middleware.RequirePerm(model.PermRoleManage), h.Update)
		group.PUT("/:id/perms", middleware.RequirePerm(model.PermRoleManage), h.SetPerms)
		// 菜单/按钮/接口授权
		group.PUT("/:id/menus", middleware.RequirePerm(model.PermRoleManage), h.SetMenus)
		group.PUT("/:id/menus/:menuId/btns", middleware.RequirePerm(model.PermRoleManage), h.SetMenuBtns)
		group.PUT("/:id/apis", middleware.RequirePerm(model.PermRoleManage), h.SetApis)
		group.DELETE("/:id", middleware.RequirePerm(model.PermRoleManage), h.Delete)
	}
}

// ListPermissions 权限点列表（供角色授权表单）。
func (h *RoleHandler) ListPermissions(c *gin.Context) {
	perms := h.store.ListPermissions(tracex.FromRequest(c))
	out := make([]gin.H, 0, len(perms))
	for _, p := range perms {
		out = append(out, gin.H{
			"id": p.ID, "code": p.Code, "name": p.Name, "type": p.Type, "description": p.Description,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

type roleItem struct {
	ID          int64             `json:"id"`
	Code        string            `json:"code"`
	Name        string            `json:"name"`
	Description string            `json:"description"`
	BuiltIn     bool              `json:"builtIn"`
	PermIDs     []int64           `json:"permIds"`
	PermCodes   []string          `json:"permCodes"`
	MenuIDs     []int64           `json:"menuIds"`
	BtnMap      map[int64][]int64 `json:"btnMap"`
	ApiIDs      []int64           `json:"apiIds"`
	CreatedAt   string            `json:"createdAt"`
}

// List 角色列表。
func (h *RoleHandler) List(c *gin.Context) {
	roles := h.store.ListRoles(tracex.FromRequest(c))
	out := make([]*roleItem, 0, len(roles))
	for _, r := range roles {
		item := toRoleItem(r)
		item.MenuIDs = h.store.GetRoleMenuIDs(tracex.FromRequest(c), r.ID)
		item.BtnMap = h.store.GetRoleMenuBtnMap(tracex.FromRequest(c), r.ID)
		item.ApiIDs = h.store.GetRoleApiIds(tracex.FromRequest(c), r.ID)
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

// Get 角色详情。
func (h *RoleHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	r, ok := h.store.GetRole(tracex.FromRequest(c), id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": toRoleItem(r)})
}

type roleReq struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
}

// Create 创建角色。
func (h *RoleHandler) Create(c *gin.Context) {
	var req roleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if _, ok := h.store.GetRoleByCode(tracex.FromRequest(c), req.Code); ok {
		c.JSON(http.StatusConflict, gin.H{"code": 6, "message": "角色编码已存在"})
		return
	}
	r := &model.Role{
		Code: req.Code, Name: req.Name, Description: req.Description,
		CreatedAt: time.Now(),
	}
	if err := h.store.DB().Create(r).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建角色失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 创建角色 #%d (%s)", operator, r.ID, r.Name)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": r.ID})
}

// Update 更新角色。
func (h *RoleHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	r, ok := h.store.GetRole(tracex.FromRequest(c), id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	var req roleReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	r.Code = req.Code
	r.Name = req.Name
	r.Description = req.Description
	r.UpdatedAt = time.Now()
	if err := h.store.DB().Save(r).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "更新失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新角色 #%d (%s)", operator, r.ID, r.Name)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功"})
}

type setPermsReq struct {
	PermIDs []int64 `json:"permIds"`
}

// SetPerms 设置角色权限。
func (h *RoleHandler) SetPerms(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetRole(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	var req setPermsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if err := h.store.SaveRolePerms(tracex.FromRequest(c), id, req.PermIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存权限失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新角色 #%d 的权限点 (%d 个)", operator, id, len(req.PermIDs))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "权限已更新"})
}

type setMenusReq struct {
	MenuIDs []int64 `json:"menuIds"`
}

// SetMenus 设置角色菜单。
func (h *RoleHandler) SetMenus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetRole(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	var req setMenusReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if err := h.store.SaveRoleMenus(tracex.FromRequest(c), id, req.MenuIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存菜单失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新角色 #%d 的菜单授权 (%d 个)", operator, id, len(req.MenuIDs))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "菜单已更新"})
}

type setMenuBtnsReq struct {
	BtnIDs []int64 `json:"btnIds"`
}

// SetMenuBtns 设置角色在某菜单下的按钮授权。
func (h *RoleHandler) SetMenuBtns(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	menuID, _ := strconv.ParseInt(c.Param("menuId"), 10, 64)
	if _, ok := h.store.GetRole(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	var req setMenuBtnsReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if err := h.store.SaveRoleMenuBtns(tracex.FromRequest(c), id, menuID, req.BtnIDs); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存按钮失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新角色 #%d 菜单 #%d 的按钮授权 (%d 个)", operator, id, menuID, len(req.BtnIDs))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "按钮已更新"})
}

type setApisReq struct {
	ApiIDs []int64 `json:"apiIds"`
}

// SetApis 设置角色接口权限（casbin 策略）。
func (h *RoleHandler) SetApis(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, ok := h.store.GetRole(tracex.FromRequest(c), id); !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	if h.store.Enforcer == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "接口鉴权未初始化"})
		return
	}
	var req setApisReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if err := h.store.SetRoleApis(tracex.FromRequest(c), id, req.ApiIDs, h.store.Enforcer); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存接口权限失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新角色 #%d 的接口授权 (%d 个)", operator, id, len(req.ApiIDs))
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "接口权限已更新"})
}

// Delete 删除角色（内置角色不可删）。
func (h *RoleHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	r, ok := h.store.GetRole(tracex.FromRequest(c), id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "角色不存在"})
		return
	}
	if r.BuiltIn {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "内置角色不可删除"})
		return
	}
	// 检查是否被用户引用
	var cnt int64
	h.store.DB().Model(&model.User{}).Where("role_id = ?", id).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "该角色已被用户使用，无法删除"})
		return
	}
	if err := h.store.DB().Delete(&model.Role{}, id).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "删除失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 删除角色 #%d (%s)", operator, id, r.Name)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func toRoleItem(r *model.Role) *roleItem {
	item := &roleItem{
		ID: r.ID, Code: r.Code, Name: r.Name, Description: r.Description,
		BuiltIn: r.BuiltIn, PermIDs: r.PermIDs,
		CreatedAt: r.CreatedAt.Format("2006-01-02 15:04:05"),
	}
	for _, p := range r.Permissions {
		item.PermCodes = append(item.PermCodes, p.Code)
	}
	return item
}
