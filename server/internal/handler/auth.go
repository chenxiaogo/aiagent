package handler

import (
	"context"
	"net/http"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/auth"
	"aiagent/pkg/ilog"
	"aiagent/pkg/rbac"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
)

// AuthHandler 认证接口。
type AuthHandler struct {
	store *store.Store
	jwt   *auth.JWT
}

// NewAuthHandler 创建认证 Handler。
func NewAuthHandler(s *store.Store, jwt *auth.JWT) *AuthHandler {
	return &AuthHandler{store: s, jwt: jwt}
}

// RegisterRoute 注册公开路由。
func (h *AuthHandler) RegisterRoute(g *gin.RouterGroup) {
	g.POST("/login", h.Login)
}

// RegisterProtectedRoute 注册需要登录的认证路由。
func (h *AuthHandler) RegisterProtectedRoute(g *gin.RouterGroup) {
	g.GET("/me", h.Me)
}

type loginReq struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Login 登录。
func (h *AuthHandler) Login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	ctx := tracex.FromRequest(c)
	user, ok := h.store.GetUserByUsername(ctx, req.Username)
	if !ok {
		ilog.Warnf("login failed: user %q not found", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 4, "message": "用户名或密码错误"})
		return
	}
	if user.Status == 0 {
		ilog.Warnf("login failed: user %q disabled", req.Username)
		c.JSON(http.StatusForbidden, gin.H{"code": 4, "message": "账号已被禁用"})
		return
	}
	if bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)) != nil {
		ilog.Warnf("login failed: user %q wrong password", req.Username)
		c.JSON(http.StatusUnauthorized, gin.H{"code": 4, "message": "用户名或密码错误"})
		return
	}

	// 解析角色与权限点并写入 token，后续 RequirePerm / 动态菜单都依赖它
	roleID, roleName, perms := resolveUserRole(ctx, h.store, user)
	token, err := h.jwt.Generate(user.ID, user.Username, user.IsAdmin, roleID, roleName, perms)
	if err != nil {
		ilog.Errorf("generate token for %q: %v", req.Username, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "签发 token 失败"})
		return
	}

	ilog.Infof("user %q logged in (uid=%d, isAdmin=%v, role=%s, perms=%d)",
		user.Username, user.ID, user.IsAdmin, roleName, len(perms))
	c.JSON(http.StatusOK, gin.H{
		"code": 0, "token": token,
		"user": gin.H{
			"uid": user.ID, "username": user.Username,
			"nickname": user.Nickname, "isAdmin": user.IsAdmin,
			"roleId": roleID, "roleName": roleName, "perms": perms,
		},
	})
}

// resolveUserRole 解析用户的角色 ID、角色名与权限码列表。
// admin 恒返回全部权限；普通用户取所绑定角色的权限点；未分配角色时返回空权限。
func resolveUserRole(ctx context.Context, s *store.Store, user *model.User) (int64, string, []string) {
	role, perms, err := s.GetUserRole(ctx, user.ID)
	if err != nil || role == nil {
		// 兜底：admin 至少要有完整权限，避免角色表异常时 admin 被降级
		if user.IsAdmin {
			codes := make([]string, 0, len(rbac.AllPermissions))
			for _, p := range rbac.AllPermissions {
				codes = append(codes, p.Code)
			}
			return 0, "管理员", codes
		}
		return 0, "", nil
	}
	// GetUserRole 对 admin 回传的 ID 取自 user.role_id，可能为 0，这里补成内置 admin 角色
	if user.IsAdmin && role.ID == 0 {
		if r, ok := s.GetRoleByCode(ctx, "admin"); ok {
			role.ID = r.ID
		}
	}
	return role.ID, role.Name, perms
}

// Me 当前用户信息。
func (h *AuthHandler) Me(c *gin.Context) {
	uid, username, isAdmin := middleware.CurrentUser(c)
	var nickname string
	roleID, roleName := middleware.CurrentRoleID(c), middleware.CurrentRoleName(c)
	perms := middleware.CurrentPerms(c)
	// token 里的角色信息可能已过期（角色被改/权限被调），以数据库为准重新解析
	if user, ok := h.store.GetUserByUsername(tracex.FromRequest(c), username); ok {
		nickname = user.Nickname
		if r, p, err := h.store.GetUserRole(tracex.FromRequest(c), user.ID); err == nil && r != nil {
			perms = p
			roleName = r.Name
			if user.IsAdmin && r.ID == 0 {
				if ar, ok := h.store.GetRoleByCode(tracex.FromRequest(c), "admin"); ok {
					r.ID = ar.ID
				}
			}
			roleID = r.ID
		}
	}
	c.JSON(http.StatusOK, gin.H{
		"uid": uid, "username": username, "nickname": nickname, "isAdmin": isAdmin,
		"roleId": roleID, "roleName": roleName, "perms": perms,
	})
}