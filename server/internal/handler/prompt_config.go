package handler

import (
	"net/http"
	"strconv"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// PromptConfigHandler Prompt 配置接口。
type PromptConfigHandler struct {
	store *store.Store
}

// NewPromptConfigHandler 创建 Prompt 配置 Handler。
func NewPromptConfigHandler(s *store.Store) *PromptConfigHandler {
	return &PromptConfigHandler{store: s}
}

// RegisterRoute 注册路由。
func (h *PromptConfigHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/prompt-configs")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
	}
}

func (h *PromptConfigHandler) List(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	promptType := c.Query("promptType")

	configs, err := h.store.ListPromptConfigs(tracex.FromRequest(c), agentID, promptType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": configs})
}

func (h *PromptConfigHandler) Create(c *gin.Context) {
	var req struct {
		Name         string `json:"name"`
		PromptType   string `json:"promptType"`
		AgentID      int64  `json:"agentId"`
		SystemPrompt string `json:"systemPrompt"`
		Description  string `json:"description"`
		Priority     int    `json:"priority"`
		DisplayOrder int    `json:"displayOrder"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Name == "" || req.PromptType == "" || req.SystemPrompt == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "必填字段不能为空"})
		return
	}

	userID, _ := c.Get("userID")
	username, _ := c.Get("username")

	cfg := &model.PromptConfig{
		Name:         req.Name,
		PromptType:   req.PromptType,
		AgentID:      req.AgentID,
		SystemPrompt: req.SystemPrompt,
		Description:  req.Description,
		Priority:     req.Priority,
		DisplayOrder: req.DisplayOrder,
		Enabled:      true,
		CreatorID:    toInt64(userID),
		CreatorName:  toString(username),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := h.store.CreatePromptConfig(tracex.FromRequest(c), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *PromptConfigHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetPromptConfig(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "Prompt 配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *PromptConfigHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetPromptConfig(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "Prompt 配置不存在"})
		return
	}
	var req struct {
		Name         *string `json:"name"`
		PromptType   *string `json:"promptType"`
		SystemPrompt *string `json:"systemPrompt"`
		Description  *string `json:"description"`
		Priority     *int    `json:"priority"`
		DisplayOrder *int    `json:"displayOrder"`
		Enabled      *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Name != nil {
		cfg.Name = *req.Name
	}
	if req.PromptType != nil {
		cfg.PromptType = *req.PromptType
	}
	if req.SystemPrompt != nil {
		cfg.SystemPrompt = *req.SystemPrompt
	}
	if req.Description != nil {
		cfg.Description = *req.Description
	}
	if req.Priority != nil {
		cfg.Priority = *req.Priority
	}
	if req.DisplayOrder != nil {
		cfg.DisplayOrder = *req.DisplayOrder
	}
	if req.Enabled != nil {
		cfg.Enabled = *req.Enabled
	}
	cfg.UpdatedAt = time.Now()
	if err := h.store.UpdatePromptConfig(tracex.FromRequest(c), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *PromptConfigHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeletePromptConfig(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}
