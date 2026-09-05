package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/mcpclient"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/internal/toolkit"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// AgentHandler 智能体管理接口。
type AgentHandler struct {
	store *store.Store
	svc   *service.Service
}

// NewAgentHandler 创建智能体 Handler。
func NewAgentHandler(s *store.Store, svc *service.Service) *AgentHandler {
	return &AgentHandler{store: s, svc: svc}
}

// RegisterRoute 注册路由。
func (h *AgentHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/agents")
	{
		group.GET("", h.List)
		group.POST("", h.Create)
		// 拖拽排序：用 POST /agents/reorder，避免与 PUT /agents/:id 的静态路由冲突
		group.POST("/reorder", h.Reorder)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
		group.PUT("/:id/status", h.UpdateStatus)
		group.POST("/:id/api-key", h.RegenerateAPIKey)
		group.DELETE("/:id/api-key", h.DisableAPIKey)
		group.GET("/:id/tools", h.ListEffectiveTools)

		// 预设问题
		group.GET("/:id/preset-questions", h.ListPresetQuestions)
		group.POST("/:id/preset-questions", h.CreatePresetQuestion)
		group.PUT("/preset-questions/:qid", h.UpdatePresetQuestion)
		group.DELETE("/preset-questions/:qid", h.DeletePresetQuestion)

		// MCP 工具（智能体接入的外部 MCP 服务器）
		group.GET("/:id/mcp", h.ListMCPServers)
		group.POST("/:id/mcp", h.CreateMCPServer)
		// 从平台 MCP 注册表导入（支持批量，按 registryId 幂等）
		group.POST("/:id/mcp/import", h.ImportMCPFromRegistry)
		group.PUT("/mcp/:mid", h.UpdateMCPServer)
		group.DELETE("/mcp/:mid", h.DeleteMCPServer)
		group.POST("/mcp/:mid/test", h.TestMCPServer)

		// 技能 Skills
		group.GET("/:id/skills", h.ListSkills)
		group.POST("/:id/skills", h.CreateSkill)
		group.PUT("/skills/:sid", h.UpdateSkill)
		group.DELETE("/skills/:sid", h.DeleteSkill)

		// 工具库挂载
		group.PUT("/:id/tool-lib-mounts", h.UpdateToolLibMounts)
	}
}

// ListEffectiveTools 返回完整工具清单（工具库内置工具 + MCP 工具）及启用状态。
// 内置工具全部返回，通过 enabled 字段标记是否被当前 Agent 挂载；MCP 工具只返回已启用服务器的。
func (h *AgentHandler) ListEffectiveTools(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	uid, _, _ := middleware.CurrentUser(c)
	ctx = agentscope.WithScope(ctx, agentscope.Scope{UserID: uid, AgentID: agentID, ReadOnly: true, IsAdmin: middleware.CurrentIsAdmin(c), Source: "tool_preview"})
	embedConfig := ResolveEmbedModelConfig(h.store, ctx)

	// 运行时注册（按配置过滤），得到实际生效的工具名集合
	activeRegistry, _ := h.svc.AgentRuntime.RegisterTools(ctx, h.store, agentID, embedConfig)
	enabledSet := make(map[string]bool)
	for _, spec := range activeRegistry.List() {
		enabledSet[spec.Name] = true
	}

	// 全量内置工具（从 tool_libraries 表读取，带 ID）
	libItems, _ := h.store.ListAllEnabledBuiltinTools(ctx)
	list := make([]gin.H, 0, len(libItems))
	for _, tl := range libItems {
		meta := model.ToolLibraryMetadata{}
		if tl.Metadata != "" {
			json.Unmarshal([]byte(tl.Metadata), &meta)
		}
		list = append(list, gin.H{
			"id":               tl.ID,
			"name":             tl.Name,
			"description":      tl.Description,
			"readOnly":         meta.ReadOnly,
			"sideEffect":       meta.SideEffect,
			"approvalRequired": meta.ApprovalRequired,
			"source":           toolkit.SourceBuiltin,
			"resourceTypes":    meta.ResourceTypes,
			"toolType":         tl.ToolType,
			"category":         tl.Category,
			"enabled":          enabledSet[tl.Name],
		})
	}
	// 追加 MCP 工具（已在 activeRegistry 里，都是启用的）
	for _, spec := range activeRegistry.List() {
		if spec.Metadata.Source == toolkit.SourceBuiltin {
			continue
		}
		list = append(list, gin.H{
			"name": spec.Name, "description": spec.Description,
			"readOnly": spec.Metadata.ReadOnly, "sideEffect": spec.Metadata.SideEffect,
			"approvalRequired": spec.Metadata.ApprovalRequired,
			"source": spec.Metadata.Source, "resourceTypes": spec.Metadata.ResourceTypes,
			"enabled": true,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// UpdateToolLibMounts 保存智能体挂载的工具库 ID 列表。
// 传 null 或空=全部内置工具（默认）；传具体 ID 数组=只挂载指定工具；传空数组=不挂载任何内置工具。
func (h *AgentHandler) UpdateToolLibMounts(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	var req struct {
		ToolIds []int64 `json:"toolIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	// 序列化：nil 存空串（全部启用），空数组存 "[]"（全部禁用）
	raw := ""
	if req.ToolIds != nil {
		if b, err := json.Marshal(req.ToolIds); err == nil {
			raw = string(b)
		}
	}
	agent.ToolLibIDs = raw
	agent.UpdatedAt = time.Now()
	if err := h.store.UpdateAgent(ctx, agent); err != nil {
		log.Errorf("update tool lib mounts failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	log.Infof("agent %d tool lib mounts updated: %s", agentID, raw)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已保存"})
}

// ---------- MCP 工具 ----------

// ListMCPServers 列出智能体的 MCP 服务器。
func (h *AgentHandler) ListMCPServers(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	list, err := h.store.ListAgentMCPServers(tracex.FromRequest(c), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// CreateMCPServer 新增 MCP 服务器。
func (h *AgentHandler) CreateMCPServer(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Name      string `json:"name"`
		Transport string `json:"transport"`
		URL       string `json:"url"`
		Headers   string `json:"headers"`
		Enabled   *bool  `json:"enabled"`
		// ApprovalRequired false 表示该 MCP 的工具免审批、直接执行；
		// nil / true 为需审批（保守默认，运行时按 nil 处理成需审批）。
		ApprovalRequired *bool `json:"approvalRequired"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" || req.URL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "名称和地址必填"})
		return
	}

	transport := req.Transport
	if transport == "" {
		transport = model.MCPTransportSSE
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	item := &model.AgentMCPServer{
		AgentID:          agentID,
		Name:             req.Name,
		Transport:        transport,
		URL:              req.URL,
		Headers:          req.Headers,
		Enabled:          enabled,
		ApprovalRequired: req.ApprovalRequired,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}
	if err := h.store.CreateAgentMCPServer(tracex.FromRequest(c), item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

// ImportMCPFromRegistry 从平台 MCP 注册表导入 MCP 服务器到该智能体。
//
// 请求体：{"registryId": 1} 或 {"registryIds": [1,2,3]}
//
// 幂等策略：
//   - 命中已有配置的 registry_id → 同步更新连接信息（不新增重复项）；
//   - 命中历史手工配置（同名 + 同 URL）→ 补记 registry_id，避免同一服务出现两条；
//   - 其余 → 新建一条启用的 MCP 配置。
func (h *AgentHandler) ImportMCPFromRegistry(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agentID, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || agentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "agent id 非法"})
		return
	}
	var req struct {
		RegistryID  *int64  `json:"registryId"`
		RegistryIDs []int64 `json:"registryIds"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请求体解析失败：" + err.Error()})
		return
	}
	ids := req.RegistryIDs
	if req.RegistryID != nil {
		ids = append(ids, *req.RegistryID)
	}
	if len(ids) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请提供 registryId 或 registryIds"})
		return
	}

	existing, err := h.store.ListAgentMCPServers(ctx, agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	byRegistry := make(map[int64]*model.AgentMCPServer, len(existing))
	byNameURL := make(map[string]*model.AgentMCPServer, len(existing))
	for _, e := range existing {
		if e.RegistryID != nil {
			byRegistry[*e.RegistryID] = e
		}
		byNameURL[e.Name+"\x00"+e.URL] = e
	}

	created, updated, linked := 0, 0, 0
	failed := make([]gin.H, 0)
	for _, rid := range ids {
		reg, gerr := h.store.GetMCPRegistry(ctx, rid)
		if gerr != nil {
			failed = append(failed, gin.H{"registryId": rid, "reason": "注册表项不存在"})
			continue
		}
		if reg.Status != 1 {
			failed = append(failed, gin.H{"registryId": rid, "name": reg.Name, "reason": "该 MCP 服务已停用"})
			continue
		}
		transport := reg.Transport
		if transport == "" {
			transport = model.MCPTransportSSE
		}

		if cur, ok := byRegistry[rid]; ok {
			cur.Name, cur.Transport, cur.URL, cur.Headers = reg.Name, transport, reg.URL, reg.Headers
			// 继承注册表的免审批设置，让注册表成为权威来源
			cur.ApprovalRequired = reg.ApprovalRequired
			cur.Enabled, cur.UpdatedAt = true, time.Now()
			if uerr := h.store.UpdateAgentMCPServer(ctx, cur); uerr != nil {
				failed = append(failed, gin.H{"registryId": rid, "name": reg.Name, "reason": uerr.Error()})
				continue
			}
			updated++
			continue
		}
		if cur, ok := byNameURL[reg.Name+"\x00"+reg.URL]; ok {
			registryID := rid
			cur.RegistryID = &registryID
			cur.ApprovalRequired = reg.ApprovalRequired
			cur.Enabled, cur.UpdatedAt = true, time.Now()
			if uerr := h.store.UpdateAgentMCPServer(ctx, cur); uerr != nil {
				failed = append(failed, gin.H{"registryId": rid, "name": reg.Name, "reason": uerr.Error()})
				continue
			}
			linked++
			continue
		}
		registryID := rid
		item := &model.AgentMCPServer{
			AgentID:          agentID,
			RegistryID:       &registryID,
			Name:             reg.Name,
			Transport:        transport,
			URL:              reg.URL,
			Headers:          reg.Headers,
			Enabled:          true,
			ApprovalRequired: reg.ApprovalRequired,
			CreatedAt:        time.Now(),
			UpdatedAt:        time.Now(),
		}
		if cerr := h.store.CreateAgentMCPServer(ctx, item); cerr != nil {
			failed = append(failed, gin.H{"registryId": rid, "name": reg.Name, "reason": cerr.Error()})
			continue
		}
		created++
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"created": created, // 新建
			"updated": updated, // 已导入过，本次同步了连接信息
			"linked":  linked,  // 命中历史手工配置，补记了来源
			"failed":  failed,
		},
	})
}

// UpdateMCPServer 更新 MCP 服务器。
func (h *AgentHandler) UpdateMCPServer(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("mid"), 10, 64)
	existing, err := h.store.GetAgentMCPServer(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "MCP 配置不存在"})
		return
	}
	var req struct {
		Name      *string `json:"name"`
		Transport *string `json:"transport"`
		URL       *string `json:"url"`
		Headers   *string `json:"headers"`
		Enabled   *bool   `json:"enabled"`
		// ApprovalRequired false 表示免审批；true 表示恢复需审批；不传则保持原值。
		ApprovalRequired *bool `json:"approvalRequired"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Transport != nil {
		existing.Transport = *req.Transport
	}
	if req.URL != nil {
		existing.URL = *req.URL
	}
	if req.Headers != nil {
		existing.Headers = *req.Headers
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	if req.ApprovalRequired != nil {
		existing.ApprovalRequired = req.ApprovalRequired
	}
	existing.UpdatedAt = time.Now()
	// 连接信息变了，之前缓存的远端工具数不再可信，等下次「测试连接」重新写入
	if req.Transport != nil || req.URL != nil || req.Headers != nil {
		existing.ToolsCount = 0
		if err := h.store.UpdateMCPServerTools(ctx, id, 0); err != nil {
			ilog.Warnf("reset mcp tools cache: %v", err)
		}
	}

	if err := h.store.UpdateAgentMCPServer(tracex.FromRequest(c), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": existing})
}

// DeleteMCPServer 删除 MCP 服务器。
func (h *AgentHandler) DeleteMCPServer(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("mid"), 10, 64)
	if err := h.store.DeleteAgentMCPServer(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

// TestMCPServer 测试 MCP 连通性并列出远端工具。
func (h *AgentHandler) TestMCPServer(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("mid"), 10, 64)
	srv, err := h.store.GetAgentMCPServer(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "MCP 配置不存在"})
		return
	}

	ctx := tracex.FromRequest(c)
	tools, err := mcpclient.NewClient().ListTools(srv)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{"ok": false, "error": err.Error()},
		})
		return
	}
	// 连通成功就顺便把工具数缓存下来，供列表页「工具数」统计免网络读取
	if uerr := h.store.UpdateMCPServerTools(ctx, id, len(tools)); uerr != nil {
		ilog.Warnf("cache mcp tools count: %v", uerr)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"ok": true, "tools": tools},
	})
}

// ---------- 技能 Skills ----------

// ListSkills 列出智能体技能。
func (h *AgentHandler) ListSkills(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	list, err := h.store.ListAgentSkills(tracex.FromRequest(c), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": list})
}

// CreateSkill 新增技能。
func (h *AgentHandler) CreateSkill(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Name        string `json:"name"`
		Description string `json:"description"`
		Summary     string `json:"summary"`
		Kind        string `json:"kind"`
		Content     string `json:"content"`
		SortOrder   int    `json:"sortOrder"`
		Enabled     *bool  `json:"enabled"`
		SkillLibID  int64  `json:"skillLibId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "名称必填"})
		return
	}

	kind := req.Kind
	if kind == "" {
		kind = model.SkillKindPrompt
	}
	enabled := true
	if req.Enabled != nil {
		enabled = *req.Enabled
	}

	item := &model.AgentSkill{
		AgentID:     agentID,
		SkillLibID:  req.SkillLibID,
		Name:        req.Name,
		Description: req.Description,
		Summary:     req.Summary,
		Kind:        kind,
		Content:     req.Content,
		SortOrder:   req.SortOrder,
		Enabled:     enabled,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.store.CreateAgentSkill(tracex.FromRequest(c), item); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": item})
}

// UpdateSkill 更新技能。
func (h *AgentHandler) UpdateSkill(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("sid"), 10, 64)
	existing, err := h.store.GetAgentSkill(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "技能不存在"})
		return
	}
	var req struct {
		Name        *string `json:"name"`
		Description *string `json:"description"`
		Summary     *string `json:"summary"`
		Kind        *string `json:"kind"`
		Content     *string `json:"content"`
		SortOrder   *int    `json:"sortOrder"`
		Enabled     *bool   `json:"enabled"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Name != nil {
		existing.Name = *req.Name
	}
	if req.Description != nil {
		existing.Description = *req.Description
	}
	if req.Summary != nil {
		existing.Summary = *req.Summary
	}
	if req.Kind != nil {
		existing.Kind = *req.Kind
	}
	if req.Content != nil {
		existing.Content = *req.Content
	}
	if req.SortOrder != nil {
		existing.SortOrder = *req.SortOrder
	}
	if req.Enabled != nil {
		existing.Enabled = *req.Enabled
	}
	existing.UpdatedAt = time.Now()

	if err := h.store.UpdateAgentSkill(tracex.FromRequest(c), existing); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": existing})
}

// DeleteSkill 删除技能。
func (h *AgentHandler) DeleteSkill(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("sid"), 10, 64)
	if err := h.store.DeleteAgentSkill(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *AgentHandler) List(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	status := c.Query("status")
	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	agents, total, err := h.store.ListAgents(ctx, status, keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	// 聚合卡片上的能力计数：技能数、MCP 服务器数、工具数
	// 技能/MCP 批量统计；工具数=内置工具 + MCP 缓存的远端工具数（不连远端，失败不影响主列表）
	ids := make([]int64, 0, len(agents))
	for _, a := range agents {
		ids = append(ids, a.ID)
	}
	counts, _ := h.store.CountAgentCapabilities(ctx, ids)
	for _, a := range agents {
		if c, ok := counts[a.ID]; ok {
			a.SkillCount = c.SkillCount
			a.MCPCount = c.MCPCount
			// 兜底：历史智能体只填了 agents.chat_model_id / embed_model_id，
			// 尚未被 EnsureAgentModelBaseline 迁移成 agent_models 行时，按这两个字段计
			a.ModelCount = c.ModelCount
			if a.ModelCount == 0 {
				if a.ChatModelID > 0 {
					a.ModelCount++
				}
				if a.EmbedModelID > 0 {
					a.ModelCount++
				}
			}
		}
		a.ToolCount = h.svc.AgentRuntime.CountAgentTools(ctx, h.store, a.ID)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"list":     agents,
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
		},
	})
}

// Reorder 批量保存智能体的手动排序结果（卡片拖拽排序）。
func (h *AgentHandler) Reorder(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	var req struct {
		IDs   []int64 `json:"ids"`
		Start int     `json:"start"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if len(req.IDs) > 500 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "单次排序数量过多"})
		return
	}
	if err := h.store.ReorderAgents(ctx, req.IDs, req.Start); err != nil {
		log.Errorf("reorder agents failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	log.Infof("reorder agents: count=%d start=%d", len(req.IDs), req.Start)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "排序已保存"})
}

func (h *AgentHandler) Create(c *gin.Context) {
	var req struct {
		Name          string `json:"name"`
		Description   string `json:"description"`
		Avatar        string `json:"avatar"`
		Prompt        string `json:"prompt"`
		Category      string `json:"category"`
		Tags          string `json:"tags"`
		ChatModelID   int64  `json:"chatModelId"`
		EmbedModelID  int64  `json:"embedModelId"`
		RuntimeType   string `json:"runtimeType"`
		MaxSteps      int    `json:"maxSteps"`
		MemoryEnabled *bool  `json:"memoryEnabled"`
		MemoryParams  *string `json:"memoryParams"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	userID, _ := c.Get("userID")
	username, _ := c.Get("username")

	runtimeType := req.RuntimeType
	if runtimeType != model.AgentRuntimeLegacy && runtimeType != model.AgentRuntimeEinoV2 {
		runtimeType = model.AgentRuntimeEinoV2
	}
	maxSteps := req.MaxSteps
	if maxSteps <= 0 || maxSteps > 30 {
		maxSteps = 8
	}
	memoryEnabled := true
	if req.MemoryEnabled != nil {
		memoryEnabled = *req.MemoryEnabled
	}
	memoryParams := ""
	if req.MemoryParams != nil {
		memoryParams = *req.MemoryParams
	}
	agent := &model.Agent{
		Name:          req.Name,
		Description:   req.Description,
		Avatar:        req.Avatar,
		Status:        model.AgentStatusDraft,
		Prompt:        req.Prompt,
		Category:      model.NormalizeAgentCategory(req.Category), // 非法/空值归一为 general
		Tags:          req.Tags,
		ChatModelID:   req.ChatModelID,
		EmbedModelID:  req.EmbedModelID,
		RuntimeType:   runtimeType,
		MaxSteps:      maxSteps,
		MemoryEnabled: memoryEnabled,
		MemoryParams:  memoryParams,
		OwnerID:       toInt64(userID),
		OwnerName:     toString(username),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}
	// 新建的智能体追加到列表末尾（手动排序位为当前最大值 + 1）
	if maxOrder, err := h.store.MaxAgentSortOrder(tracex.FromRequest(c)); err == nil {
		agent.SortOrder = maxOrder + 1
	}
	if err := h.store.CreateAgent(tracex.FromRequest(c), agent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": agent})
}

func (h *AgentHandler) Get(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	// 与列表保持一致：补齐运行时聚合的能力计数（技能 / 模型 / MCP / 工具）
	if counts, cerr := h.store.CountAgentCapabilities(ctx, []int64{id}); cerr == nil {
		if c, ok := counts[id]; ok {
			agent.SkillCount = c.SkillCount
			agent.MCPCount = c.MCPCount
			agent.ModelCount = c.ModelCount
			if agent.ModelCount == 0 {
				if agent.ChatModelID > 0 {
					agent.ModelCount++
				}
				if agent.EmbedModelID > 0 {
					agent.ModelCount++
				}
			}
		}
	}
	agent.ToolCount = h.svc.AgentRuntime.CountAgentTools(ctx, h.store, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": agent})
}

func (h *AgentHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	var req struct {
		Name          *string `json:"name"`
		Description   *string `json:"description"`
		Avatar        *string `json:"avatar"`
		Prompt        *string `json:"prompt"`
		Category      *string `json:"category"`
		Tags          *string `json:"tags"`
		ChatModelID   *int64  `json:"chatModelId"`
		EmbedModelID  *int64  `json:"embedModelId"`
		RuntimeType   *string `json:"runtimeType"`
		MaxSteps      *int    `json:"maxSteps"`
		MemoryEnabled *bool   `json:"memoryEnabled"`
		MemoryParams  *string `json:"memoryParams"`
		SortOrder     *int    `json:"sortOrder"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Name != nil {
		agent.Name = *req.Name
	}
	if req.Description != nil {
		agent.Description = *req.Description
	}
	if req.Avatar != nil {
		agent.Avatar = *req.Avatar
	}
	if req.Prompt != nil {
		agent.Prompt = *req.Prompt
	}
	if req.Category != nil {
		agent.Category = model.NormalizeAgentCategory(*req.Category)
	}
	if req.Tags != nil {
		agent.Tags = *req.Tags
	}
	if req.ChatModelID != nil {
		agent.ChatModelID = *req.ChatModelID
	}
	if req.EmbedModelID != nil {
		agent.EmbedModelID = *req.EmbedModelID
	}
	if req.RuntimeType != nil {
		if *req.RuntimeType != model.AgentRuntimeLegacy && *req.RuntimeType != model.AgentRuntimeEinoV2 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "无效的运行时类型"})
			return
		}
		agent.RuntimeType = *req.RuntimeType
	}
	if req.MaxSteps != nil {
		if *req.MaxSteps <= 0 || *req.MaxSteps > 30 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "最大步骤数应为 1-30"})
			return
		}
		agent.MaxSteps = *req.MaxSteps
	}
	if req.MemoryEnabled != nil {
		agent.MemoryEnabled = *req.MemoryEnabled
	}
	if req.MemoryParams != nil {
		agent.MemoryParams = *req.MemoryParams
	}
	if req.SortOrder != nil {
		if *req.SortOrder < 0 {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "排序位置不能为负数"})
			return
		}
		agent.SortOrder = *req.SortOrder
	}
	agent.UpdatedAt = time.Now()
	if err := h.store.UpdateAgent(tracex.FromRequest(c), agent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": agent})
}

func (h *AgentHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if err := h.store.DeleteAgent(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *AgentHandler) UpdateStatus(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Status string `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Status != model.AgentStatusDraft && req.Status != model.AgentStatusPublished && req.Status != model.AgentStatusOffline {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "无效的状态"})
		return
	}
	if err := h.store.UpdateAgentStatus(tracex.FromRequest(c), id, req.Status); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "状态更新成功"})
}

func (h *AgentHandler) RegenerateAPIKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	apiKey := "sk-" + generateRandomString(32)
	agent.APIKey = apiKey
	agent.APIKeyEnabled = true
	agent.UpdatedAt = time.Now()
	if err := h.store.UpdateAgent(tracex.FromRequest(c), agent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"apiKey": apiKey}})
}

func (h *AgentHandler) DisableAPIKey(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "智能体不存在"})
		return
	}
	agent.APIKeyEnabled = false
	agent.UpdatedAt = time.Now()
	if err := h.store.UpdateAgent(tracex.FromRequest(c), agent); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "API Key 已禁用"})
}

// ---------- 预设问题 ----------

func (h *AgentHandler) ListPresetQuestions(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	questions, err := h.store.ListAgentPresetQuestions(tracex.FromRequest(c), agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": questions})
}

func (h *AgentHandler) CreatePresetQuestion(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Question  string `json:"question"`
		SortOrder int    `json:"sortOrder"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Question == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	q := &model.AgentPresetQuestion{
		AgentID:   agentID,
		Question:  req.Question,
		SortOrder: req.SortOrder,
		IsActive:  true,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := h.store.CreateAgentPresetQuestion(tracex.FromRequest(c), q); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": q})
}

func (h *AgentHandler) UpdatePresetQuestion(c *gin.Context) {
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	var req struct {
		Question  *string `json:"question"`
		SortOrder *int    `json:"sortOrder"`
		IsActive  *bool   `json:"isActive"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	// 简化：直接查出来更新
	var q model.AgentPresetQuestion
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).First(&q, qid).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "预设问题不存在"})
		return
	}
	if req.Question != nil {
		q.Question = *req.Question
	}
	if req.SortOrder != nil {
		q.SortOrder = *req.SortOrder
	}
	if req.IsActive != nil {
		q.IsActive = *req.IsActive
	}
	q.UpdatedAt = time.Now()
	if err := h.store.UpdateAgentPresetQuestion(tracex.FromRequest(c), &q); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": q})
}

func (h *AgentHandler) DeletePresetQuestion(c *gin.Context) {
	qid, _ := strconv.ParseInt(c.Param("qid"), 10, 64)
	if err := h.store.DeleteAgentPresetQuestion(tracex.FromRequest(c), qid); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

// ---------- 工具函数 ----------

func generateRandomString(length int) string {
	b := make([]byte, length/2)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func toInt64(v interface{}) int64 {
	switch val := v.(type) {
	case int64:
		return val
	case float64:
		return int64(val)
	case int:
		return int64(val)
	default:
		return 0
	}
}

func toString(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
