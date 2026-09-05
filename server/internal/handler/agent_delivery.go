package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// AgentDeliveryHandler 智能体对外交付接口：版本发布、交付方式、客户授权、访问凭据、用量。
//
// 产品模型（见 model/product.go）：
//
//	Agent(编辑态) ──发布──▶ Release(不可变版本) ──包装──▶ Product ──▶ Plan ──▶ Subscription(客户×版本)
//	                                                                          └──▶ AgentClient(API Key) ──▶ 调用计量
type AgentDeliveryHandler struct {
	store *store.Store
}

// NewAgentDeliveryHandler 创建交付 Handler。
func NewAgentDeliveryHandler(s *store.Store) *AgentDeliveryHandler {
	return &AgentDeliveryHandler{store: s}
}

// RegisterRoute 注册路由（挂在同一 /agents 前缀下，与 AgentHandler 共存）。
func (h *AgentDeliveryHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/agents")
	{
		// 版本
		group.GET("/:id/versions", h.ListVersions)
		group.POST("/:id/versions", h.PublishVersion)
		group.POST("/:id/versions/:rid/rollback", h.RollbackVersion)
		// 轻量发布状态：详情页头部展示「未发布改动」徽标与待发布清单
		group.GET("/:id/release-status", h.ReleaseStatus)

		// 发布与交付
		group.GET("/:id/delivery", h.DeliveryInfo)
		group.PUT("/:id/delivery", h.UpdateDelivery)

		// 客户授权（订阅）
		group.GET("/:id/subscriptions", h.ListSubscriptions)
		group.POST("/:id/subscriptions", h.CreateSubscription)
		group.PUT("/subscriptions/:sid", h.UpdateSubscription)
		group.DELETE("/subscriptions/:sid", h.DeleteSubscription)

		// 访问凭据
		group.GET("/:id/clients", h.ListClients)
		group.POST("/:id/clients", h.CreateClient)
		group.DELETE("/:id/clients/:cid", h.DeleteClient)
		group.POST("/:id/clients/:cid/revoke", h.RevokeClient)

		// 用量
		group.GET("/:id/usage", h.Usage)
	}
}

// ---------- 版本 ----------

// ListVersions 版本列表 + 草稿状态 + 发布前校验。
func (h *AgentDeliveryHandler) ListVersions(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	releases, err := h.store.ListAgentReleases(ctx, agent.ID)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	hasChanges, _ := h.store.HasUnpublishedChanges(ctx, agent)
	// 待发布变更清单：让操作人在点「发布」之前看清这次到底改了什么
	changes, _ := h.store.DraftReleaseDiff(ctx, agent)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"agentId":               agent.ID,
			"currentReleaseId":      agent.CurrentReleaseID,
			"hasUnpublishedChanges": hasChanges,
			"pendingChanges":        changes,
			"publishedAt":           agent.PublishedAt,
			"releases":              releases,
			"checklist":             h.buildChecklist(ctx, agent),
		},
	})
}

// PublishVersion 发布新版本：把草稿快照固化为不可变 Release，并设为默认版本。
func (h *AgentDeliveryHandler) PublishVersion(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	var req struct {
		Changelog string `json:"changelog"`
	}
	_ = c.ShouldBindJSON(&req)

	// 无改动不发布：内容完全相同的版本会污染版本历史，也让回滚时难以分辨差异。
	// 这里以快照逐字节比对为准，与列表页「有未发布改动」的判定完全一致。
	if hasChanges, err := h.store.HasUnpublishedChanges(ctx, agent); err == nil && !hasChanges {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "当前配置与线上版本一致，没有需要发布的改动"})
		return
	}

	// 发布前校验：硬性拦截不达标的发布，避免客户侧拿到空能力的版本
	checklist := h.buildChecklist(ctx, agent)
	if !checklist["canPublish"].(bool) {
		c.JSON(http.StatusBadRequest, gin.H{
			"code": 2, "message": "发布前校验未通过", "data": gin.H{"checklist": checklist},
		})
		return
	}

	username, _ := c.Get("username")
	agent.OwnerName = defaultString(toString(username), agent.OwnerName)

	rel, err := h.store.PublishAgentRelease(ctx, agent, req.Changelog)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rel, "message": "已发布 " + rel.Version})
}

// ReleaseStatus 轻量发布状态。
// 详情页头部要用它显示「有未发布改动」徽标，发布弹窗要用它展示待发布清单，
// 两处都不该为了一个标记去加载整个版本历史。
func (h *AgentDeliveryHandler) ReleaseStatus(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	hasChanges, _ := h.store.HasUnpublishedChanges(ctx, agent)
	changes, _ := h.store.DraftReleaseDiff(ctx, agent)
	currentVersion := ""
	if rel := h.currentRelease(ctx, agent); rel != nil {
		currentVersion = rel.Version
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"agentId":               agent.ID,
			"currentVersion":        currentVersion,
			"published":             agent.CurrentReleaseID > 0,
			"hasUnpublishedChanges": hasChanges,
			"pendingCount":          len(changes),
			"pendingChanges":        changes,
			"publishedAt":           agent.PublishedAt,
		},
	})
}

// currentRelease 取当前生效版本，取不到返回 nil（versionName 会渲染为「未发布」）。
func (h *AgentDeliveryHandler) currentRelease(ctx context.Context, agent *model.Agent) *model.AgentRelease {
	if agent.CurrentReleaseID <= 0 {
		return nil
	}
	rel, err := h.store.GetAgentRelease(ctx, agent.CurrentReleaseID)
	if err != nil {
		return nil
	}
	return rel
}

// RollbackVersion 回滚到指定版本（把它设为默认版本）。
func (h *AgentDeliveryHandler) RollbackVersion(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	rid, _ := strconv.ParseInt(c.Param("rid"), 10, 64)
	if err := h.store.SetDefaultRelease(ctx, agent.ID, rid); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	now := time.Now()
	_ = h.store.UpdateProductByAgent(ctx, agent.ID, map[string]interface{}{
		"default_release_id": rid, "updated_at": now,
	})
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已回滚到该版本"})
}

// ---------- 发布与交付 ----------

// DeliveryInfo 交付总览：产品信息、端点、接入示例、暴露工具、授权与凭据概况。
func (h *AgentDeliveryHandler) DeliveryInfo(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	product, err := h.store.EnsureProductForAgent(ctx, agent)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	base := requestBaseURL(c)
	releases, _ := h.store.ListAgentReleases(ctx, agent.ID)
	clients, _ := h.store.ListAgentClients(ctx, agent.ID)
	subs, _ := h.store.ListAgentSubscriptions(ctx, agent.ID)
	usage, _ := h.store.SumAgentUsage(ctx, agent.ID, 7)
	clientUsage, _ := h.store.ListAgentClientUsage(ctx, agent.ID)

	var current *model.AgentRelease
	for _, r := range releases {
		if r.ID == agent.CurrentReleaseID {
			current = r
			break
		}
	}

	tools := []string{}
	presetQuestions := []string{}
	if current != nil {
		if snap, err := model.DecodeAgentReleaseSnapshot(current.Snapshot); err == nil {
			tools = snap.ExposedTools
			presetQuestions = snap.PresetQuestions
		}
	}

	enrichedClients := make([]gin.H, 0, len(clients))
	for _, cl := range clients {
		stat := clientUsage[cl.ID]
		enrichedClients = append(enrichedClients, gin.H{
			"id": cl.ID, "name": cl.Name, "keyPrefix": cl.KeyPrefix,
			"tenantId": cl.TenantID, "tenantName": cl.TenantName,
			"scopes": cl.Scopes, "pinnedVersion": cl.PinnedVersion,
			"ipAllowList": cl.IPAllowList, "expiresAt": cl.ExpiresAt,
			"quotaRpm": cl.QuotaRPM, "quotaTpd": cl.QuotaTPD,
			"status": cl.Status, "lastUsedAt": cl.LastUsedAt, "createdAt": cl.CreatedAt,
			"requests": stat.Requests, "errors": stat.Errors, "toolCalls": stat.ToolCalls,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"agent": gin.H{
				"id": agent.ID, "name": agent.Name, "slug": agent.Slug,
				"category": agent.Category, "status": agent.Status,
				"visibility": agent.Visibility, "publishedAt": agent.PublishedAt,
			},
			"product":          product,
			"currentVersion":   versionName(current),
			"releases":         releases,
			"tools":            tools,
			"presetQuestions":  presetQuestions,
			"clients":          enrichedClients,
			"subscriptions":    subs,
			"usage":            usage,
			"endpoints":        buildEndpoints(base, agent.Slug),
			"snippets":         buildSnippets(base, agent.Slug, agent.Name),
			"deliveryModes":    model.ParseDeliveryModes(product.DeliveryModes),
			"supportedModes":   []string{model.DeliveryMCP}, // R1 已实现；API/Web/SDK/Embed 随 R2 客户门户开放
		},
	})
}

// UpdateDelivery 更新产品信息（名称、简介、封面、交付方式、上下架）。
func (h *AgentDeliveryHandler) UpdateDelivery(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	product, err := h.store.EnsureProductForAgent(ctx, agent)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	var req struct {
		Name          *string  `json:"name"`
		Summary       *string  `json:"summary"`
		Cover         *string  `json:"cover"`
		Category      *string  `json:"category"`
		DeliveryModes []string `json:"deliveryModes"`
		Status        *string  `json:"status"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.Name != nil {
		product.Name = *req.Name
	}
	if req.Summary != nil {
		product.Summary = *req.Summary
	}
	if req.Cover != nil {
		product.Cover = *req.Cover
	}
	if req.Category != nil {
		product.Category = *req.Category
	}
	if req.Status != nil {
		product.Status = *req.Status
	}
	if req.DeliveryModes != nil {
		product.DeliveryModes = strings.Join(req.DeliveryModes, ",")
	}
	product.AgentSlug = agent.Slug
	if product.Status == model.ProductStatusOnline && agent.CurrentReleaseID == 0 {
		jsonErr(c, http.StatusBadRequest, "请先发布一个版本再上架产品")
		return
	}
	if err := h.store.UpdateProduct(ctx, product); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": product})
}

// ---------- 客户授权 ----------

// ListSubscriptions 列出该产品的客户授权。
func (h *AgentDeliveryHandler) ListSubscriptions(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	list, err := h.store.ListAgentSubscriptions(ctx, agent.ID)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 补充版本名，便于前端直接展示「客户 A → v1.2」
	releases, _ := h.store.ListAgentReleases(ctx, agent.ID)
	versionNames := map[int64]string{}
	for _, r := range releases {
		versionNames[r.ID] = r.Version
	}
	items := make([]gin.H, 0, len(list))
	for _, s := range list {
		v := "跟随最新版"
		if s.PinnedReleaseID > 0 {
			if name, ok := versionNames[s.PinnedReleaseID]; ok {
				v = name
			} else {
				v = "已钉版本（已删除）"
			}
		}
		items = append(items, gin.H{
			"id": s.ID, "tenantId": s.TenantID, "tenantName": s.TenantName,
			"productId": s.ProductID, "productName": s.ProductName,
			"planId": s.PlanID, "planName": s.PlanName,
			"pinnedReleaseId": s.PinnedReleaseID, "pinnedVersionText": v,
			"status": s.Status, "startedAt": s.StartedAt, "endedAt": s.EndedAt,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// CreateSubscription 授权客户使用该产品。
func (h *AgentDeliveryHandler) CreateSubscription(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	product, err := h.store.EnsureProductForAgent(ctx, agent)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	var req struct {
		TenantID        int64  `json:"tenantId"`
		UserID          int64  `json:"userId"`
		TenantName      string `json:"tenantName"`
		PlanID          int64  `json:"planId"`
		PinnedReleaseID int64  `json:"pinnedReleaseId"`
		EndedAt         string `json:"endedAt"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误")
		return
	}
	// 订阅是对某个客户的授权，必须落到具体客户上
	if req.TenantID <= 0 && req.UserID <= 0 && req.TenantName == "" {
		jsonErr(c, http.StatusBadRequest, "请选择客户")
		return
	}
	tenantID, tenantName, ok := h.resolveTenant(c, req.TenantID, req.UserID, req.TenantName)
	if !ok {
		return
	}

	sub := &model.Subscription{
		TenantID:        tenantID,
		TenantName:      tenantName,
		ProductID:       product.ID,
		ProductName:     product.Name,
		AgentID:         agent.ID,
		PlanID:          req.PlanID,
		PinnedReleaseID: req.PinnedReleaseID,
		Status:          model.SubscriptionStatusActive,
	}
	if req.PlanID > 0 {
		if plan, err := h.store.GetPlan(ctx, req.PlanID); err == nil {
			sub.PlanName = plan.Name
		}
	}
	if req.PinnedReleaseID > 0 {
		if rel, err := h.store.GetAgentRelease(ctx, req.PinnedReleaseID); err == nil {
			sub.PinnedVersion = rel.Version
		}
	}
	if req.EndedAt != "" {
		if t, err := time.Parse(time.RFC3339, req.EndedAt); err == nil {
			sub.EndedAt = &t
		}
	}
	if err := h.store.CreateSubscription(ctx, sub); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sub})
}

// UpdateSubscription 修改授权（含改钉版本、续期、取消）。
func (h *AgentDeliveryHandler) UpdateSubscription(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	sid, _ := strconv.ParseInt(c.Param("sid"), 10, 64)
	sub, err := h.store.GetSubscription(ctx, sid)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "授权不存在")
		return
	}
	var req struct {
		PinnedReleaseID *int64  `json:"pinnedReleaseId"`
		PlanID          *int64  `json:"planId"`
		Status          *string `json:"status"`
		EndedAt         *string `json:"endedAt"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "参数错误")
		return
	}
	if req.PinnedReleaseID != nil {
		sub.PinnedReleaseID = *req.PinnedReleaseID
		sub.PinnedVersion = ""
		if sub.PinnedReleaseID > 0 {
			if rel, err := h.store.GetAgentRelease(ctx, sub.PinnedReleaseID); err == nil {
				sub.PinnedVersion = rel.Version
			}
		}
	}
	if req.PlanID != nil {
		sub.PlanID = *req.PlanID
		if sub.PlanID > 0 {
			if plan, err := h.store.GetPlan(ctx, sub.PlanID); err == nil {
				sub.PlanName = plan.Name
			}
		}
	}
	if req.Status != nil {
		sub.Status = *req.Status
	}
	if req.EndedAt != nil {
		if *req.EndedAt == "" {
			sub.EndedAt = nil
		} else if t, err := time.Parse(time.RFC3339, *req.EndedAt); err == nil {
			sub.EndedAt = &t
		}
	}
	if err := h.store.UpdateSubscription(ctx, sub); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sub})
}

// DeleteSubscription 取消授权。
func (h *AgentDeliveryHandler) DeleteSubscription(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	sid, _ := strconv.ParseInt(c.Param("sid"), 10, 64)
	if err := h.store.DeleteSubscription(ctx, sid); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已取消授权"})
}

// ---------- 访问凭据 ----------

// ListClients 列出访问凭据。
func (h *AgentDeliveryHandler) ListClients(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	list, err := h.store.ListAgentClients(ctx, agent.ID)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	usage, _ := h.store.ListAgentClientUsage(ctx, agent.ID)
	items := make([]gin.H, 0, len(list))
	for _, cl := range list {
		stat := usage[cl.ID]
		items = append(items, gin.H{
			"id": cl.ID, "name": cl.Name, "keyPrefix": cl.KeyPrefix,
			"tenantName": cl.TenantName, "scopes": cl.Scopes,
			"pinnedVersion": cl.PinnedVersion, "ipAllowList": cl.IPAllowList,
			"expiresAt": cl.ExpiresAt, "quotaRpm": cl.QuotaRPM, "quotaTpd": cl.QuotaTPD,
			"status": cl.Status, "lastUsedAt": cl.LastUsedAt, "createdAt": cl.CreatedAt,
			"requests": stat.Requests, "errors": stat.Errors, "toolCalls": stat.ToolCalls,
		})
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": items})
}

// CreateClient 创建访问凭据。明文 Key 只在创建响应里返回一次。
// resolveTenant 解析凭据/订阅归属的客户。
//
// 优先级：tenantId（已存在的客户）> userId（按平台用户建/取客户）> tenantName（兼容早期手填）。
// 前两者是正规路径：客户主体就是系统用户，授权界面从用户列表里选人即可。
// 三者都为空时返回 (0, "", true)，表示「不挂客户」，凭据仍可创建（个人调试用）。
func (h *AgentDeliveryHandler) resolveTenant(c *gin.Context, tenantID, userID int64, tenantName string) (int64, string, bool) {
	ctx := tracex.FromRequest(c)
	if tenantID > 0 {
		t, ok := h.store.GetTenant(ctx, tenantID)
		if !ok {
			jsonErr(c, http.StatusNotFound, "客户不存在")
			return 0, "", false
		}
		return t.ID, t.Name, true
	}
	if userID > 0 {
		t, err := h.store.EnsureTenantForUser(ctx, userID)
		if err != nil {
			jsonErr(c, http.StatusBadRequest, err.Error())
			return 0, "", false
		}
		return t.ID, t.Name, true
	}
	if tenantName != "" {
		t, err := h.store.EnsureTenantByName(ctx, tenantName)
		if err != nil {
			jsonErr(c, http.StatusInternalServerError, err.Error())
			return 0, "", false
		}
		return t.ID, t.Name, true
	}
	return 0, "", true
}

func (h *AgentDeliveryHandler) CreateClient(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	var req struct {
		Name          string   `json:"name"`
		TenantID      int64    `json:"tenantId"`
		UserID        int64    `json:"userId"`
		TenantName    string   `json:"tenantName"`
		Scopes        []string `json:"scopes"`
		PinnedVersion string   `json:"pinnedVersion"`
		IPAllowList   string   `json:"ipAllowList"`
		ExpiresAt     string   `json:"expiresAt"`
		QuotaRPM      int      `json:"quotaRpm"`
		QuotaTPD      int      `json:"quotaTpd"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Name == "" {
		jsonErr(c, http.StatusBadRequest, "凭据名称必填")
		return
	}

	tenantID, tenantName, ok := h.resolveTenant(c, req.TenantID, req.UserID, req.TenantName)
	if !ok {
		return
	}

	plainKey, err := newClientKey()
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, "生成密钥失败")
		return
	}
	scopes := req.Scopes
	if len(scopes) == 0 {
		scopes = model.DefaultClientScopes
	}

	now := time.Now()
	client := &model.AgentClient{
		AgentID:       agent.ID,
		TenantID:      tenantID,
		TenantName:    tenantName,
		Name:          req.Name,
		KeyPrefix:     plainKey[:10],
		KeyHash:       store.HashClientKey(plainKey),
		Scopes:        strings.Join(scopes, ","),
		PinnedVersion: req.PinnedVersion,
		IPAllowList:   req.IPAllowList,
		QuotaRPM:      req.QuotaRPM,
		QuotaTPD:      req.QuotaTPD,
		Status:        model.ClientStatusActive,
		CreatedBy:     toString(c.GetString("username")),
		CreatedAt:     now,
		UpdatedAt:     now,
	}
	if req.ExpiresAt != "" {
		if t, err := time.Parse(time.RFC3339, req.ExpiresAt); err == nil {
			client.ExpiresAt = &t
		}
	}
	if client.QuotaRPM <= 0 {
		client.QuotaRPM = 60
	}
	if client.QuotaTPD <= 0 {
		client.QuotaTPD = 10000
	}
	if err := h.store.CreateAgentClient(ctx, client); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	// 明文 Key 只在本次响应出现，这里顺带拼出可直接复制的接入地址：
	// 单端点 ?key= 形式（高德 MCP 风格），客户端无需再配自定义请求头。
	mcpURL := requestBaseURL(c) + "/api/mcp?key=" + plainKey
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"client":   client,
			"plainKey": plainKey,
			"mcpUrl":   mcpURL,
		},
		"message": "请立即保存 Key，关闭后无法再次查看",
	})
}

// RevokeClient 吊销凭据。
func (h *AgentDeliveryHandler) RevokeClient(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	cid, _ := strconv.ParseInt(c.Param("cid"), 10, 64)
	client, err := h.store.GetAgentClient(ctx, cid)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "凭据不存在")
		return
	}
	client.Status = model.ClientStatusRevoked
	client.UpdatedAt = time.Now()
	if err := h.store.UpdateAgentClient(ctx, client); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已吊销"})
}

// DeleteClient 删除凭据。
func (h *AgentDeliveryHandler) DeleteClient(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	cid, _ := strconv.ParseInt(c.Param("cid"), 10, 64)
	if err := h.store.DeleteAgentClient(ctx, cid); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// ---------- 用量 ----------

// Usage 用量汇总（近 N 天）。
func (h *AgentDeliveryHandler) Usage(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	agent, ok := h.loadAgent(c)
	if !ok {
		return
	}
	days, _ := strconv.Atoi(c.DefaultQuery("days", "7"))
	usage, err := h.store.SumAgentUsage(ctx, agent.ID, days)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": usage})
}

// ---------- 内部辅助 ----------

func (h *AgentDeliveryHandler) loadAgent(c *gin.Context) (*model.Agent, bool) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(tracex.FromRequest(c), id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "智能体不存在")
		return nil, false
	}
	return agent, true
}

// buildChecklist 发布前校验：拦截「发出去但不能用的版本」。
func (h *AgentDeliveryHandler) buildChecklist(ctx context.Context, agent *model.Agent) gin.H {
	items := []gin.H{}

	add := func(key, label string, passed bool, hint string) {
		items = append(items, gin.H{"key": key, "label": label, "passed": passed, "hint": hint})
	}

	add("prompt", "已填写系统提示词", agent.Prompt != "", "在「基础配置」中填写，决定智能体的角色与边界")
	add("model", "已绑定对话模型", agent.ChatModelID > 0, "在「基础配置」中选择一个已激活的对话模型")

	snap, err := h.store.BuildAgentReleaseSnapshot(ctx, agent)
	toolOK := err == nil && len(snap.ExposedTools) > 0
	add("tools", "至少暴露一个工具", toolOK, "在「MCP 工具」中勾选对外暴露的能力")

	dsOK, dsHint := h.checkDatasource(ctx, agent, snap)
	add("datasource", "已绑定数据源", dsOK, dsHint)

	clients, _ := h.store.ListAgentClients(ctx, agent.ID)
	activeKeys := 0
	for _, cl := range clients {
		if cl.Status == model.ClientStatusActive {
			activeKeys++
		}
	}
	add("key", "已创建访问凭据", activeKeys > 0, "在「发布与交付」中创建 API Key，否则客户无法调用")

	canPublish := true
	for _, it := range items {
		if !it["passed"].(bool) {
			canPublish = false
			break
		}
	}
	return gin.H{"items": items, "canPublish": canPublish}
}

// checkDatasource 按智能体分类校验其「实际绑定的数据源」是否可用。
// 以发布快照里冻结的资源绑定为准，保证校验口径与运行时 MCP 工具能检索到的数据一致。
func (h *AgentDeliveryHandler) checkDatasource(ctx context.Context, agent *model.Agent, snap *model.AgentReleaseSnapshot) (bool, string) {
	if snap == nil {
		return false, "配置读取失败，请刷新页面后重试"
	}
	switch model.NormalizeAgentCategory(agent.Category) {
	case model.AgentCategoryVideo:
		_, total, err := h.store.ListVideos(ctx, agent.ID, 0, model.VideoStatusReady, "", 1, 1)
		return err == nil && total > 0, "在「数据 → 视频源」上传并处理完成至少一个视频"
	case model.AgentCategoryCamera:
		n, err := h.store.CountProcessedCameraEvents(ctx)
		return err == nil && n > 0, "在「数据 → 摄像头」中接入并分析至少一个事件"
	case model.AgentCategoryDoc:
		kbIDs := snap.Resources.KnowledgeBaseIDs
		if len(kbIDs) == 0 {
			return false, "在「数据 → 知识库」中先为本智能体绑定知识库"
		}
		// 直接统计文件表：KnowledgeBase.FileCount 是未维护的冗余字段，恒为 0，不能作为判据。
		n, err := h.store.CountFilesInKnowledgeBases(ctx, kbIDs)
		return err == nil && n > 0, "在已绑定的知识库中上传文档，并等待索引完成（状态为「就绪」）"
	default:
		return true, "通用对话型无需绑定数据源"
	}
}

func buildEndpoints(base, slug string) gin.H {
	return gin.H{
		// 推荐：单端点 + URL 携带 key（高德 MCP 风格），无需 slug，也无需自定义请求头
		"mcp":       base + "/api/mcp?key=<你的API-Key>",
		"mcpSse":    base + "/api/mcp/agents/" + slug + "/sse",
		"mcpStream": base + "/api/mcp/agents/" + slug + "/stream",
		"mcpMessages": base + "/api/mcp/agents/" + slug + "/messages",
		"chatApi":   base + "/api/v1/chat/completions",
		"portal":    base + "/portal/agents/" + slug,
	}
}

func buildSnippets(base, slug, name string) gin.H {
	// 对外统一为「单端点 + URL 携带 key」，与高德 MCP 的接入形态一致：
	// 一条 URL 即可完成接入，客户端无需再配自定义请求头。
	// 旧式 /api/mcp/agents/<slug>/stream 仍保留，用于兼容已接入的客户。
	keyURL := base + "/api/mcp?key=你的API-Key"
	claude := fmt.Sprintf(`{
  "mcpServers": {
    "%s": {
      "command": "npx",
      "args": ["-y", "mcp-remote", "%s"]
    }
  }
}`, slug, keyURL)

	curl := fmt.Sprintf(`curl -X POST "%s" \
  -H "Content-Type: application/json" \
  -d '{"jsonrpc":"2.0","id":1,"method":"tools/list"}'`, keyURL)

	python := fmt.Sprintf(`import requests

resp = requests.post(
    "%s",
    json={"jsonrpc": "2.0", "id": 1, "method": "tools/call",
          "params": {"name": "agent_info", "arguments": {}}},
).json()
print(resp)`, keyURL)

	return gin.H{"claude": claude, "cursor": claude, "curl": curl, "python": python, "agentName": name}
}

func requestBaseURL(c *gin.Context) string {
	scheme := c.GetHeader("X-Forwarded-Proto")
	if scheme == "" {
		if c.Request.TLS != nil {
			scheme = "https"
		} else {
			scheme = "http"
		}
	}
	host := c.GetHeader("X-Forwarded-Host")
	if host == "" {
		host = c.Request.Host
	}
	return scheme + "://" + host
}

func versionName(rel *model.AgentRelease) string {
	if rel == nil {
		return "未发布"
	}
	return rel.Version
}

func newClientKey() (string, error) {
	b := make([]byte, 20)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "ak-" + hex.EncodeToString(b), nil
}

func jsonErr(c *gin.Context, code int, msg string) {
	c.JSON(code, gin.H{"code": 1, "message": msg})
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
