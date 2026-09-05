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

// ModelConfigHandler 模型配置接口。
type ModelConfigHandler struct {
	store *store.Store
}

// NewModelConfigHandler 创建模型配置 Handler。
func NewModelConfigHandler(s *store.Store) *ModelConfigHandler {
	return &ModelConfigHandler{store: s}
}

// RegisterRoute 注册路由。
func (h *ModelConfigHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/model-configs")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
		group.PUT("/:id/activate", h.Activate)
	}
}

func (h *ModelConfigHandler) List(c *gin.Context) {
	modelType := c.Query("modelType")
	configs, err := h.store.ListModelConfigs(tracex.FromRequest(c), modelType)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": configs})
}

func (h *ModelConfigHandler) Create(c *gin.Context) {
	var req struct {
		Provider        string  `json:"provider"`
		BaseURL         string  `json:"baseUrl"`
		APIKey          string  `json:"apiKey"`
		ModelName       string  `json:"modelName"`
		Temperature     float64 `json:"temperature"`
		MaxTokens       int     `json:"maxTokens"`
		ModelType       string  `json:"modelType"`
		CompletionsPath string  `json:"completionsPath"`
		EmbeddingsPath  string  `json:"embeddingsPath"`
		ProxyEnabled    bool    `json:"proxyEnabled"`
		ProxyHost       string  `json:"proxyHost"`
		ProxyPort       int     `json:"proxyPort"`
		ProxyUsername   string  `json:"proxyUsername"`
		ProxyPassword   string  `json:"proxyPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Provider == "" || req.BaseURL == "" || req.ModelName == "" || req.ModelType == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "必填字段不能为空"})
		return
	}

	cfg := &model.ModelConfig{
		Provider:        req.Provider,
		BaseURL:         req.BaseURL,
		APIKey:          req.APIKey,
		ModelName:       req.ModelName,
		Temperature:     req.Temperature,
		MaxTokens:       req.MaxTokens,
		ModelType:       req.ModelType,
		CompletionsPath: req.CompletionsPath,
		EmbeddingsPath:  req.EmbeddingsPath,
		ProxyEnabled:    req.ProxyEnabled,
		ProxyHost:       req.ProxyHost,
		ProxyPort:       req.ProxyPort,
		ProxyUsername:   req.ProxyUsername,
		ProxyPassword:   req.ProxyPassword,
		IsActive:        false,
		CreatedAt:       time.Now(),
		UpdatedAt:       time.Now(),
	}
	if err := h.store.CreateModelConfig(tracex.FromRequest(c), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *ModelConfigHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetModelConfig(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "模型配置不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *ModelConfigHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetModelConfig(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "模型配置不存在"})
		return
	}
	var req struct {
		Provider        *string  `json:"provider"`
		BaseURL         *string  `json:"baseUrl"`
		APIKey          *string  `json:"apiKey"`
		ModelName       *string  `json:"modelName"`
		Temperature     *float64 `json:"temperature"`
		MaxTokens       *int     `json:"maxTokens"`
		ModelType       *string  `json:"modelType"`
		CompletionsPath *string  `json:"completionsPath"`
		EmbeddingsPath  *string  `json:"embeddingsPath"`
		ProxyEnabled    *bool    `json:"proxyEnabled"`
		ProxyHost       *string  `json:"proxyHost"`
		ProxyPort       *int     `json:"proxyPort"`
		ProxyUsername   *string  `json:"proxyUsername"`
		ProxyPassword   *string  `json:"proxyPassword"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Provider != nil {
		cfg.Provider = *req.Provider
	}
	if req.BaseURL != nil {
		cfg.BaseURL = *req.BaseURL
	}
	if req.APIKey != nil {
		cfg.APIKey = *req.APIKey
	}
	if req.ModelName != nil {
		cfg.ModelName = *req.ModelName
	}
	if req.Temperature != nil {
		cfg.Temperature = *req.Temperature
	}
	if req.MaxTokens != nil {
		cfg.MaxTokens = *req.MaxTokens
	}
	if req.ModelType != nil {
		cfg.ModelType = *req.ModelType
	}
	if req.CompletionsPath != nil {
		cfg.CompletionsPath = *req.CompletionsPath
	}
	if req.EmbeddingsPath != nil {
		cfg.EmbeddingsPath = *req.EmbeddingsPath
	}
	if req.ProxyEnabled != nil {
		cfg.ProxyEnabled = *req.ProxyEnabled
	}
	if req.ProxyHost != nil {
		cfg.ProxyHost = *req.ProxyHost
	}
	if req.ProxyPort != nil {
		cfg.ProxyPort = *req.ProxyPort
	}
	if req.ProxyUsername != nil {
		cfg.ProxyUsername = *req.ProxyUsername
	}
	if req.ProxyPassword != nil {
		cfg.ProxyPassword = *req.ProxyPassword
	}
	cfg.UpdatedAt = time.Now()
	if err := h.store.UpdateModelConfig(tracex.FromRequest(c), cfg); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

func (h *ModelConfigHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteModelConfig(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *ModelConfigHandler) Activate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetModelConfig(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "模型配置不存在"})
		return
	}
	if err := h.store.SetActiveModelConfig(tracex.FromRequest(c), id, cfg.ModelType); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已激活"})
}
