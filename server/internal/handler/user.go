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
	"golang.org/x/crypto/bcrypt"
)

// UserHandler 用户管理接口。
type UserHandler struct {
	store *store.Store
}

func NewUserHandler(s *store.Store) *UserHandler {
	return &UserHandler{store: s}
}

// RegisterRoute 注册用户管理路由。
func (h *UserHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/users")
	{
		group.GET("", middleware.RequirePerm(model.PermUserManage), h.List)
		group.POST("", middleware.RequirePerm(model.PermUserManage), h.Create)
		group.PUT("/:id", middleware.RequirePerm(model.PermUserManage), h.Update)
		group.PUT("/:id/password", middleware.RequirePerm(model.PermUserManage), h.ResetPassword)
		group.DELETE("/:id", middleware.RequirePerm(model.PermUserManage), h.Delete)
	}
}

type userItem struct {
	ID        int64  `json:"id"`
	Username  string `json:"username"`
	Nickname  string `json:"nickname"`
	Email     string `json:"email"`
	IsAdmin   bool   `json:"isAdmin"`
	RoleID    int64  `json:"roleId"`
	RoleName  string `json:"roleName"`
	Status    int    `json:"status"`
	CreatedAt string `json:"createdAt"`
}

// List 用户列表。
func (h *UserHandler) List(c *gin.Context) {
	users := h.store.GetAllUsers(tracex.FromRequest(c))
	out := make([]*userItem, 0, len(users))
	for _, u := range users {
		item := &userItem{
			ID: u.ID, Username: u.Username, Nickname: u.Nickname, Email: u.Email,
			IsAdmin: u.IsAdmin, RoleID: u.RoleID, Status: u.Status,
			CreatedAt: u.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if r, ok := h.store.GetRole(tracex.FromRequest(c), u.RoleID); ok {
			item.RoleName = r.Name
		}
		out = append(out, item)
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": out})
}

type createUserReq struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"isAdmin"`
	RoleID   int64  `json:"roleId"`
	Status   int    `json:"status"`
}

// Create 创建用户。
func (h *UserHandler) Create(c *gin.Context) {
	var req createUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if _, ok := h.store.GetUserByUsername(tracex.FromRequest(c), req.Username); ok {
		c.JSON(http.StatusConflict, gin.H{"code": 6, "message": "用户名已存在"})
		return
	}
	pw, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "密码加密失败"})
		return
	}
	status := req.Status
	if status == 0 {
		status = 1
	}
	// 未指定角色时回落到内置 viewer，避免新建账号无任何权限点
	roleID := req.RoleID
	if roleID == 0 {
		if r, ok := h.store.GetRoleByCode(tracex.FromRequest(c), "viewer"); ok {
			roleID = r.ID
		}
	}
	u, ok := h.store.CreateUser(tracex.FromRequest(c), &model.User{
		Username: req.Username, Password: string(pw), Nickname: req.Nickname,
		Email: req.Email, IsAdmin: req.IsAdmin, RoleID: roleID, Status: status,
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建用户失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 创建用户 #%d (%s)", operator, u.ID, req.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "创建成功", "data": u.ID})
}

type updateUserReq struct {
	Nickname string `json:"nickname"`
	Email    string `json:"email"`
	IsAdmin  bool   `json:"isAdmin"`
	RoleID   int64  `json:"roleId"`
	Status   int    `json:"status"`
}

// Update 更新用户。
func (h *UserHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req updateUserReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	old, ok := h.store.GetUserByID(tracex.FromRequest(c), id)
	if !ok {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "用户不存在"})
		return
	}
	// 禁止取消最后一个 admin 的管理员权限
	if old.IsAdmin && !req.IsAdmin {
		if h.countAdmins() <= 1 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "至少保留一名管理员"})
			return
		}
	}
	status := req.Status
	if status == 0 {
		status = old.Status
	}
	u, ok := h.store.UpdateUser(tracex.FromRequest(c), &model.User{
		ID: id, Nickname: req.Nickname, Email: req.Email,
		IsAdmin: req.IsAdmin, RoleID: req.RoleID, Status: status,
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "更新失败"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 更新用户 #%d (%s)", operator, id, old.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "更新成功", "data": u.ID})
}

type resetPwdReq struct {
	Password string `json:"password" binding:"required"`
}

// ResetPassword 重置密码。
func (h *UserHandler) ResetPassword(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req resetPwdReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	pw, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "密码加密失败"})
		return
	}
	if !h.store.UpdateUserPassword(tracex.FromRequest(c), id, string(pw)) {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "用户不存在"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 重置用户 #%d 的密码", operator, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "密码已重置"})
}

// Delete 删除用户。
func (h *UserHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	uid, _, _ := middleware.CurrentUser(c)
	if id == uid {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "不能删除当前登录账号"})
		return
	}
	u, ok := h.store.GetUserByID(tracex.FromRequest(c), id)
	if ok && u.IsAdmin && h.countAdmins() <= 1 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "至少保留一名管理员"})
		return
	}
	if !h.store.DeleteUser(tracex.FromRequest(c), id) {
		c.JSON(http.StatusNotFound, gin.H{"code": 3, "message": "用户不存在"})
		return
	}
	_, operator, _ := middleware.CurrentUser(c)
	ilog.FromGin(c).Infof("用户 %s 删除用户 #%d", operator, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *UserHandler) countAdmins() int {
	var count int64
	h.store.DB().Model(&model.User{}).Where("is_admin = ?", true).Count(&count)
	return int(count)
}
