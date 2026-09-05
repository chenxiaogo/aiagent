package handler

import (
	"net/http"
	"strconv"
	"time"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// MarketHandler 能力市场（MCP 注册表 / 技能库 / Agent 模板）与模型路由、调用观测。
type MarketHandler struct {
	store *store.Store
}

// NewMarketHandler 创建市场 Handler。
func NewMarketHandler(s *store.Store) *MarketHandler {
	return &MarketHandler{store: s}
}

// RegisterRoute 注册路由（市场与治理相关，挂在 /market 与 /ops 前缀下）。
func (h *MarketHandler) RegisterRoute(g *gin.RouterGroup) {
	market := g.Group("/market")
	{
		// MCP 注册表
		mcp := market.Group("/mcp-registry")
		{
			mcp.GET("", h.ListMCPRegistry)
			mcp.POST("", h.CreateMCPRegistry)
			mcp.PUT("/:id", h.UpdateMCPRegistry)
			mcp.DELETE("/:id", h.DeleteMCPRegistry)
		}
		// 技能库
		skill := market.Group("/skills")
		{
			skill.GET("", h.ListSkillLibrary)
			skill.POST("", h.CreateSkillLibrary)
			skill.PUT("/:id", h.UpdateSkillLibrary)
			skill.DELETE("/:id", h.DeleteSkillLibrary)
		}
		// 工具库
		toolLib := market.Group("/tools")
		{
			toolLib.GET("", h.ListToolLibrary)
			toolLib.POST("", h.CreateToolLibrary)
			toolLib.PUT("/:id", h.UpdateToolLibrary)
			toolLib.DELETE("/:id", h.DeleteToolLibrary)
		}
		// Agent 模板
		tpl := market.Group("/templates")
		{
			tpl.GET("", h.ListAgentTemplates)
			tpl.POST("", h.CreateAgentTemplate)
			tpl.GET("/:id", h.GetAgentTemplate)
			tpl.PUT("/:id", h.UpdateAgentTemplate)
			tpl.DELETE("/:id", h.DeleteAgentTemplate)
		}
		// 模型目录（含价格），复用 model-configs 概念，这里提供带价格的列表
		market.GET("/models", h.ListMarketModels)
		market.PUT("/models/:id/price", h.UpdateModelPrice)
		// 模型路由规则
		routing := market.Group("/routing")
		{
			routing.GET("", h.ListRoutingRules)
			routing.POST("", h.SaveRoutingRule)
			routing.DELETE("/:id", h.DeleteRoutingRule)
		}
	}

	ops := g.Group("/ops")
	{
		ops.GET("/call-logs", h.ListCallLogs)
		ops.GET("/call-logs/summary", h.SummaryCallLogs)
	}
}

// ---------- MCP 注册表 ----------

func (h *MarketHandler) ListMCPRegistry(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	list, err := h.store.ListMCPRegistry(ctx, c.Query("keyword"), c.Query("category"))
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *MarketHandler) CreateMCPRegistry(c *gin.Context) {
	var item model.MCPRegistry
	if err := c.ShouldBindJSON(&item); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if item.Name == "" || item.URL == "" {
		jsonErr(c, http.StatusBadRequest, "名称与 URL 必填")
		return
	}
	if item.Visibility == "" {
		item.Visibility = model.AssetVisibilityPublic
	}
	if item.Status == 0 {
		item.Status = 1
	}
	_, username, _ := middleware.CurrentUser(c)
	item.OwnerName = username
	if err := h.store.CreateMCPRegistry(tracex.FromRequest(c), &item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) UpdateMCPRegistry(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetMCPRegistry(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "注册项不存在")
		return
	}
	var patch model.MCPRegistry
	if err := c.ShouldBindJSON(&patch); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if patch.Name != "" {
		item.Name = patch.Name
	}
	if patch.Description != "" {
		item.Description = patch.Description
	}
	if patch.Transport != "" {
		item.Transport = patch.Transport
	}
	if patch.URL != "" {
		item.URL = patch.URL
	}
	if patch.Headers != "" {
		item.Headers = patch.Headers
	}
	if patch.Category != "" {
		item.Category = patch.Category
	}
	if patch.Version != "" {
		item.Version = patch.Version
	}
	if patch.Visibility != "" {
		item.Visibility = patch.Visibility
	}
	if patch.Status != 0 {
		item.Status = patch.Status
	}
	// 显式传 false 表示免审批，传 true 表示需审批，不传保持原值
	if patch.ApprovalRequired != nil {
		item.ApprovalRequired = patch.ApprovalRequired
	}
	if err := h.store.UpdateMCPRegistry(tracex.FromRequest(c), item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) DeleteMCPRegistry(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteMCPRegistry(tracex.FromRequest(c), id); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 技能库 ----------

func (h *MarketHandler) ListSkillLibrary(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	list, err := h.store.ListSkillLibrary(ctx, c.Query("keyword"), c.Query("category"))
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *MarketHandler) CreateSkillLibrary(c *gin.Context) {
	var item model.SkillLibrary
	if err := c.ShouldBindJSON(&item); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if item.Name == "" || item.Kind == "" {
		jsonErr(c, http.StatusBadRequest, "名称与类型必填")
		return
	}
	if item.Visibility == "" {
		item.Visibility = model.AssetVisibilityPublic
	}
	if item.Status == 0 {
		item.Status = 1
	}
	item.OwnerName = func() string { _, u, _ := middleware.CurrentUser(c); return u }()
	if err := h.store.CreateSkillLibrary(tracex.FromRequest(c), &item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) UpdateSkillLibrary(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetSkillLibrary(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "技能不存在")
		return
	}
	var patch model.SkillLibrary
	if err := c.ShouldBindJSON(&patch); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if patch.Name != "" {
		item.Name = patch.Name
	}
	if patch.Description != "" {
		item.Description = patch.Description
	}
	if patch.Summary != "" {
		item.Summary = patch.Summary
	}
	if patch.Kind != "" {
		item.Kind = patch.Kind
	}
	if patch.Category != "" {
		item.Category = patch.Category
	}
	if patch.Content != "" {
		item.Content = patch.Content
	}
	if patch.Visibility != "" {
		item.Visibility = patch.Visibility
	}
	if patch.Status != 0 {
		item.Status = patch.Status
	}
	if err := h.store.UpdateSkillLibrary(tracex.FromRequest(c), item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) DeleteSkillLibrary(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteSkillLibrary(tracex.FromRequest(c), id); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 工具库 ----------

func (h *MarketHandler) ListToolLibrary(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	list, err := h.store.ListToolLibrary(ctx, c.Query("keyword"), c.Query("category"))
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *MarketHandler) CreateToolLibrary(c *gin.Context) {
	var item model.ToolLibrary
	if err := c.ShouldBindJSON(&item); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if item.Name == "" {
		jsonErr(c, http.StatusBadRequest, "名称不能为空")
		return
	}
	if item.ToolType == "" {
		item.ToolType = "builtin"
	}
	if item.Visibility == "" {
		item.Visibility = model.AssetVisibilityPublic
	}
	if item.Status == 0 {
		item.Status = 1
	}
	if err := h.store.CreateToolLibrary(tracex.FromRequest(c), &item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) UpdateToolLibrary(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetToolLibrary(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "工具不存在")
		return
	}
	var req model.ToolLibrary
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	item.Name = req.Name
	item.Description = req.Description
	item.Category = req.Category
	item.ToolType = req.ToolType
	item.Parameters = req.Parameters
	item.Config = req.Config
	item.Metadata = req.Metadata
	item.Status = req.Status
	if err := h.store.UpdateToolLibrary(tracex.FromRequest(c), item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) DeleteToolLibrary(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteToolLibrary(tracex.FromRequest(c), id); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- Agent 模板 ----------

func (h *MarketHandler) ListAgentTemplates(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	list, err := h.store.ListAgentTemplates(ctx, c.Query("keyword"), c.Query("category"))
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *MarketHandler) GetAgentTemplate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetAgentTemplate(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "模板不存在")
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) CreateAgentTemplate(c *gin.Context) {
	var item model.AgentTemplate
	if err := c.ShouldBindJSON(&item); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if item.Name == "" || item.Category == "" {
		jsonErr(c, http.StatusBadRequest, "名称与分类必填")
		return
	}
	if item.Visibility == "" {
		item.Visibility = model.AssetVisibilityPublic
	}
	item.OwnerName = func() string { _, u, _ := middleware.CurrentUser(c); return u }()
	if err := h.store.CreateAgentTemplate(tracex.FromRequest(c), &item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) UpdateAgentTemplate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	item, err := h.store.GetAgentTemplate(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "模板不存在")
		return
	}
	var patch model.AgentTemplate
	if err := c.ShouldBindJSON(&patch); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if patch.Name != "" {
		item.Name = patch.Name
	}
	if patch.Description != "" {
		item.Description = patch.Description
	}
	if patch.Category != "" {
		item.Category = patch.Category
	}
	if patch.Icon != "" {
		item.Icon = patch.Icon
	}
	if patch.Snapshot != "" {
		item.Snapshot = patch.Snapshot
	}
	if patch.Visibility != "" {
		item.Visibility = patch.Visibility
	}
	if err := h.store.UpdateAgentTemplate(tracex.FromRequest(c), item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) DeleteAgentTemplate(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteAgentTemplate(tracex.FromRequest(c), id); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 模型目录（含价格） ----------

func (h *MarketHandler) ListMarketModels(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	cfgs, err := h.store.ListModelConfigs(ctx, "")
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfgs})
}

func (h *MarketHandler) UpdateModelPrice(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	cfg, err := h.store.GetModelConfig(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "模型不存在")
		return
	}
	var req struct {
		PriceInPer1K  float64 `json:"priceInPer1k"`
		PriceOutPer1K float64 `json:"priceOutPer1k"`
		BillingType   string  `json:"billingType"`
		Currency      string  `json:"currency"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	cfg.PriceInPer1K = req.PriceInPer1K
	cfg.PriceOutPer1K = req.PriceOutPer1K
	if req.BillingType != "" {
		cfg.BillingType = req.BillingType
	}
	if req.Currency != "" {
		cfg.Currency = req.Currency
	}
	if err := h.store.UpdateModelConfig(tracex.FromRequest(c), cfg); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cfg})
}

// ---------- 模型路由规则 ----------

func (h *MarketHandler) ListRoutingRules(c *gin.Context) {
	list, err := h.store.ListModelRoutingRules(tracex.FromRequest(c))
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

func (h *MarketHandler) SaveRoutingRule(c *gin.Context) {
	var item model.ModelRoutingRule
	if err := c.ShouldBindJSON(&item); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误: "+err.Error())
		return
	}
	if !model.IsValidRoutingStrategy(item.Strategy) {
		jsonErr(c, http.StatusBadRequest, "路由策略非法")
		return
	}
	if item.Name == "" {
		item.Name = "规则-" + strconv.FormatInt(time.Now().Unix(), 10)
	}
	if err := h.store.SaveModelRoutingRule(tracex.FromRequest(c), &item); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

func (h *MarketHandler) DeleteRoutingRule(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteModelRoutingRule(tracex.FromRequest(c), id); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 调用观测 ----------

func (h *MarketHandler) ListCallLogs(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	q := store.CallLogQuery{
		CallType:        c.Query("callType"),
		ExcludeCallType: c.Query("excludeCallType"),
		DayFrom:         c.Query("dayFrom"),
		DayTo:           c.Query("dayTo"),
	}
	if v := c.Query("agentId"); v != "" {
		q.AgentID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("clientId"); v != "" {
		q.ClientID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("tenantId"); v != "" {
		q.TenantID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("status"); v != "" {
		q.Status, _ = strconv.Atoi(v)
	}
	q.Page, _ = strconv.Atoi(c.Query("page"))
	q.PageSize, _ = strconv.Atoi(c.Query("pageSize"))
	list, total, err := h.store.ListCallLogs(ctx, q)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
}

func (h *MarketHandler) SummaryCallLogs(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	q := store.CallLogQuery{
		CallType:        c.Query("callType"),
		ExcludeCallType: c.Query("excludeCallType"),
		DayFrom:         c.Query("dayFrom"),
		DayTo:           c.Query("dayTo"),
	}
	if v := c.Query("agentId"); v != "" {
		q.AgentID, _ = strconv.ParseInt(v, 10, 64)
	}
	if v := c.Query("tenantId"); v != "" {
		q.TenantID, _ = strconv.ParseInt(v, 10, 64)
	}
	sum, err := h.store.SumCallLogs(ctx, q)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sum})
}
