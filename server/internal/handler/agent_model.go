package handler

import (
	"encoding/json"
	"net/http"
	"strconv"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// AgentModelHandler 智能体多模型绑定接口。
//
// 一个智能体可绑定多个模型：按用途（对话 / 向量化 / 视觉 / 重排 / 兜底）路由，
// 同一用途内按 priority 形成回退链。保存时把各用途主模型回写到 agents 表，
// 保证既有「单模型」代码路径继续可用。
type AgentModelHandler struct {
	store *store.Store
}

// NewAgentModelHandler 创建模型绑定 Handler。
func NewAgentModelHandler(s *store.Store) *AgentModelHandler {
	return &AgentModelHandler{store: s}
}

// RegisterRoute 注册路由（与 AgentHandler 共用 /agents 前缀）。
func (h *AgentModelHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/agents")
	{
		group.GET("/:id/models", h.List)
		group.PUT("/:id/models", h.Save)
	}
}

// List 返回已绑定模型（带模型名称等展示字段）与可选择的模型清单。
func (h *AgentModelHandler) List(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := h.store.GetAgent(ctx, id); err != nil {
		jsonErr(c, http.StatusNotFound, "智能体不存在")
		return
	}

	bindings, err := h.store.ListAgentModels(ctx, id)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	available, err := h.store.ListModelConfigs(ctx, "")
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	// 模型名用于列表展示，避免前端再拉一次模型配置
	names := make(map[int64]gin.H, len(available))
	options := make([]gin.H, 0, len(available))
	for _, m := range available {
		names[m.ID] = gin.H{
			"modelName": m.ModelName, "provider": m.Provider,
			"modelType": m.ModelType, "isActive": m.IsActive,
		}
		options = append(options, gin.H{
			"id": m.ID, "modelName": m.ModelName, "provider": m.Provider,
			"modelType": m.ModelType, "isActive": m.IsActive,
		})
	}

	items := make([]gin.H, 0, len(bindings))
	for _, b := range bindings {
		info, _ := names[b.ModelID]
		if info == nil {
			info = gin.H{}
		}
		items = append(items, gin.H{
			"id": b.ID, "agentId": b.AgentID, "modelId": b.ModelID,
			"role": b.Role, "roleText": model.ModelRoleText(b.Role),
			"isPrimary": b.IsPrimary, "priority": b.Priority,
			"params": b.Params, "enabled": b.Enabled,
			"modelName": info["modelName"], "provider": info["provider"],
			"modelType": info["modelType"],
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"models": items, "available": options},
	})
}

// Save 整表保存模型绑定（前端一次性提交整个列表）。
func (h *AgentModelHandler) Save(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(ctx, id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "智能体不存在")
		return
	}

	var req struct {
		Models []struct {
			ModelID   int64  `json:"modelId"`
			Role      string `json:"role"`
			IsPrimary bool   `json:"isPrimary"`
			Priority  int    `json:"priority"`
			Params    string `json:"params"`
			Enabled   *bool  `json:"enabled"`
		} `json:"models"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误")
		return
	}

	// 校验：用途合法、模型存在、同一用途最多一个主模型、(modelId, role) 不重复
	available, err := h.store.ListModelConfigs(ctx, "")
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	modelExists := make(map[int64]bool, len(available))
	for _, m := range available {
		modelExists[m.ID] = true
	}

	seen := map[string]bool{}
	primaryByRole := map[string]bool{}
	items := make([]*model.AgentModel, 0, len(req.Models))
	for _, m := range req.Models {
		role := m.Role
		if role == "" {
			role = model.ModelRoleChat
		}
		if !model.IsValidModelRole(role) {
			jsonErr(c, http.StatusBadRequest, "非法的模型用途："+role)
			return
		}
		if !modelExists[m.ModelID] {
			jsonErr(c, http.StatusBadRequest, "模型不存在")
			return
		}
		key := strconv.FormatInt(m.ModelID, 10) + "/" + role
		if seen[key] {
			jsonErr(c, http.StatusBadRequest, "同一用途下不能重复绑定同一个模型")
			return
		}
		seen[key] = true
		if m.IsPrimary {
			if primaryByRole[role] {
				jsonErr(c, http.StatusBadRequest, "用途「"+model.ModelRoleText(role)+"」只能有一个主模型")
				return
			}
			primaryByRole[role] = true
		}
		if m.Params != "" {
			var p model.AgentModelParams
			if err := json.Unmarshal([]byte(m.Params), &p); err != nil {
				jsonErr(c, http.StatusBadRequest, "参数覆写必须是合法 JSON")
				return
			}
		}
		enabled := true
		if m.Enabled != nil {
			enabled = *m.Enabled
		}
		priority := m.Priority
		if priority <= 0 {
			priority = 10
		}
		items = append(items, &model.AgentModel{
			AgentID: id, ModelID: m.ModelID, Role: role,
			IsPrimary: m.IsPrimary, Priority: priority,
			Params: m.Params, Enabled: enabled,
		})
	}

	if err := h.store.ReplaceAgentModels(ctx, agent, items); err != nil {
		ilog.Errorf("replace agent models: %v", err)
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	bindings, err := h.store.ListAgentModels(ctx, id)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": bindings, "message": "模型配置已保存"})
}
