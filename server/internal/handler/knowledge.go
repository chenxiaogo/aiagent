package handler

import (
	"net/http"
	"strconv"

	"aiagent/internal/knowledge"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// KnowledgeHandler 知识库管理接口。
type KnowledgeHandler struct {
	manager *knowledge.Manager
}

// NewKnowledgeHandler 创建知识库 Handler。
func NewKnowledgeHandler(manager *knowledge.Manager) *KnowledgeHandler {
	return &KnowledgeHandler{manager: manager}
}

func knowledgeOwnerScope(c *gin.Context) knowledge.OwnerScope {
	uid, username, isAdmin := middleware.CurrentUser(c)
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	return knowledge.OwnerScope{UserID: uid, Username: username, IsAdmin: isAdmin, AgentID: agentID}
}

// RegisterRoute 注册路由。
func (h *KnowledgeHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/knowledge")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
		group.GET("/:id/files", h.ListFiles)
	}
}

func (h *KnowledgeHandler) List(c *gin.Context) {
	scope := knowledgeOwnerScope(c)
	kbs, err := h.manager.List(tracex.FromRequest(c), scope, scope.AgentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": kbs})
}

func (h *KnowledgeHandler) Create(c *gin.Context) {
	var req struct {
		Name        string   `json:"name"`
		Description string   `json:"description"`
		Icon        string   `json:"icon"`
		AgentID     int64    `json:"agentId"`
		Type        string   `json:"type"`
		Tags        []string `json:"tags"`
		Meta        string   `json:"meta"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	ctx := tracex.FromRequest(c)
	kb, err := h.manager.Create(ctx, knowledgeOwnerScope(c), req.AgentID, req.Type, req.Name, req.Description, req.Icon)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	// 标签与元信息在 Create 之后补写：Manager.Create 的参数已经很长，
	// 为两个可选字段再各加一个参数不划算。
	if len(req.Tags) > 0 || req.Meta != "" {
		if updated, uErr := h.manager.Update(ctx, knowledgeOwnerScope(c), kb.ID, func(item *model.KnowledgeBase) {
			if len(req.Tags) > 0 {
				item.Tags = normalizeTags(req.Tags)
			}
			if req.Meta != "" {
				item.Meta = req.Meta
			}
		}); uErr == nil {
			kb = updated
		}
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": kb})
}

func (h *KnowledgeHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	kb, err := h.manager.Get(tracex.FromRequest(c), knowledgeOwnerScope(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "知识库不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": kb})
}

func (h *KnowledgeHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Icon        string `json:"icon"`
		Status      *int   `json:"status"`
		// 指针语义：nil 表示本次不改动，非 nil（含空数组）表示覆盖为给定值，空数组即清空
		Tags *[]string `json:"tags"`
		Meta *string   `json:"meta"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	kb, err := h.manager.Update(tracex.FromRequest(c), knowledgeOwnerScope(c), id, func(kb *model.KnowledgeBase) {
		if req.Name != "" {
			kb.Name = req.Name
		}
		if req.Description != "" {
			kb.Description = req.Description
		}
		if req.Icon != "" {
			kb.Icon = req.Icon
		}
		if req.Status != nil {
			kb.Status = *req.Status
		}
		if req.Tags != nil {
			kb.Tags = normalizeTags(*req.Tags)
		}
		if req.Meta != nil {
			kb.Meta = *req.Meta
		}
	})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "知识库不存在或无权访问"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": kb})
}

func (h *KnowledgeHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.manager.Delete(tracex.FromRequest(c), knowledgeOwnerScope(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *KnowledgeHandler) ListFiles(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	scope := knowledgeOwnerScope(c)
	files, err := h.manager.ListFiles(tracex.FromRequest(c), scope, id, scope.AgentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": files})
}
