package handler

import (
	"net/http"
	"strconv"

	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
)

// AgentResourceHandler 智能体运行数据绑定接口。
//
// 智能体对外交付（MCP / 门户）时，只应检索它「显式绑定」的知识库 / 视频源 / 摄像头事件，
// 否则会出现跨 Agent 数据泄漏（如文档型 Agent 能搜到全平台所有知识库）。
// 本 Handler 提供查看与编辑绑定关系的能力，发布时这些绑定会被冻结进版本快照。
type AgentResourceHandler struct {
	store *store.Store
}

// NewAgentResourceHandler 创建资源绑定 Handler。
func NewAgentResourceHandler(s *store.Store) *AgentResourceHandler {
	return &AgentResourceHandler{store: s}
}

// RegisterRoute 注册路由（与 AgentHandler 共用 /agents 前缀）。
func (h *AgentResourceHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/agents")
	{
		group.GET("/:id/resources", h.List)
		group.PUT("/:id/resources", h.Save)
	}
}

// resourceView 单个绑定资源的前端视图（带展示字段）。
type resourceView struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

// ResourceListResponse 绑定列表响应。
type ResourceListResponse struct {
	KnowledgeBases []resourceView `json:"knowledgeBases"` // 已绑定的知识库
	VideoSources   []resourceView `json:"videoSources"`   // 已绑定的视频源
	CameraEvents   []resourceView `json:"cameraEvents"`   // 已绑定的摄像头事件
	HostGroups     []resourceView `json:"hostGroups"`     // 已绑定的主机组
	// 可选清单（供前端下拉勾选）
	AvailableKnowledgeBases []resourceView `json:"availableKnowledgeBases"`
	AvailableVideoSources   []resourceView `json:"availableVideoSources"`
	AvailableCameraEvents   []resourceView `json:"availableCameraEvents"`
	AvailableHostGroups     []resourceView `json:"availableHostGroups"`
}

// List 返回智能体当前资源绑定，以及各类型可选择的资源清单。
func (h *AgentResourceHandler) List(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	agent, err := h.store.GetAgent(ctx, id)
	if err != nil {
		jsonErr(c, http.StatusNotFound, "智能体不存在")
		return
	}
	// 按智能体类型约束可绑定的知识库类型（视频 Agent 只能绑视频知识库，以此类推）
	kbType := agentCategoryToKBType(agent.Category)

	bound, err := h.store.ListAgentResources(ctx, id)
	if err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	kbBound := map[int64]bool{}
	vidBound := map[int64]bool{}
	camBound := map[int64]bool{}
	hgBound := map[int64]bool{}
	for _, r := range bound {
		switch r.ResourceType {
		case model.ResourceTypeKnowledgeBase:
			kbBound[r.ResourceID] = true
		case model.ResourceTypeVideoSource:
			vidBound[r.ResourceID] = true
		case model.ResourceTypeCameraEvent:
			camBound[r.ResourceID] = true
		case model.ResourceTypeHostGroup:
			hgBound[r.ResourceID] = true
		}
	}

	resp := ResourceListResponse{
		KnowledgeBases:          []resourceView{},
		VideoSources:            []resourceView{},
		CameraEvents:            []resourceView{},
		HostGroups:              []resourceView{},
		AvailableKnowledgeBases: []resourceView{},
		AvailableVideoSources:   []resourceView{},
		AvailableCameraEvents:   []resourceView{},
		AvailableHostGroups:     []resourceView{},
	}

	if kbs, e := h.store.ListKnowledgeBasesForBinding(ctx, id); e == nil {
		for _, kb := range kbs {
			// 仅展示内容型知识库（视频 / 摄像头 / 文件），且与智能体类型匹配
			if kb.Type != "video" && kb.Type != "camera" && kb.Type != "file" {
				continue
			}
			if kbType != "" && kb.Type != kbType {
				continue
			}
			v := resourceView{ID: kb.ID, Name: kb.Name}
			resp.AvailableKnowledgeBases = append(resp.AvailableKnowledgeBases, v)
			if kbBound[kb.ID] {
				resp.KnowledgeBases = append(resp.KnowledgeBases, v)
			}
		}
	}
	if vids, _, e := h.store.ListVideos(ctx, id, 0, "", "", 0, 0); e == nil {
		for _, v := range vids {
			vw := resourceView{ID: v.ID, Name: v.Title}
			resp.AvailableVideoSources = append(resp.AvailableVideoSources, vw)
			if vidBound[v.ID] {
				resp.VideoSources = append(resp.VideoSources, vw)
			}
		}
	}
	// 摄像头事件量可能很大，仅返回已绑定的作为可选项（避免一次性拉全平台事件）
	if len(camBound) > 0 {
		var events []model.CameraEvent
		if e := h.store.DB().WithContext(ctx).Where("id IN ?", keysOf(camBound)).Find(&events).Error; e == nil {
			for _, e := range events {
				v := resourceView{ID: e.ID, Name: cameraEventLabel(&e)}
				resp.AvailableCameraEvents = append(resp.AvailableCameraEvents, v)
				resp.CameraEvents = append(resp.CameraEvents, v)
			}
		}
	}

	// 主机组：运维型 Agent 的核心资源
	uid, _, _ := middleware.CurrentUser(c)
	if groups, e := h.store.ListHostGroups(ctx, uid, ""); e == nil {
		for _, g := range groups {
			v := resourceView{ID: g.ID, Name: g.Name}
			resp.AvailableHostGroups = append(resp.AvailableHostGroups, v)
			if hgBound[g.ID] {
				resp.HostGroups = append(resp.HostGroups, v)
			}
		}
	}

	jsonOK(c, resp)
}

// cameraEventLabel 生成摄像头事件的简短展示名。
func cameraEventLabel(e *model.CameraEvent) string {
	label := e.CameraName
	if label == "" {
		label = "摄像头#" + strconv.FormatInt(e.CameraID, 10)
	}
	return label + " @ " + e.EventTime.Format("2006-01-02 15:04")
}

// SaveRequest 保存绑定请求。
type SaveRequest struct {
	KnowledgeBaseIDs []int64 `json:"knowledgeBaseIds"`
	VideoSourceIDs   []int64 `json:"videoSourceIds"`
	CameraEventIDs   []int64 `json:"cameraEventIds"`
	HostGroupIDs     []int64 `json:"hostGroupIds"`
}

// Save 重置智能体的三类资源绑定。调用方应在保存后重新发布版本，使绑定生效到对外快照。
func (h *AgentResourceHandler) Save(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := h.store.GetAgent(ctx, id); err != nil {
		jsonErr(c, http.StatusNotFound, "智能体不存在")
		return
	}
	var req SaveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		jsonErr(c, http.StatusBadRequest, "请求格式错误: "+err.Error())
		return
	}

	if err := h.store.SetAgentResources(ctx, id, model.ResourceTypeKnowledgeBase, req.KnowledgeBaseIDs); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SetAgentResources(ctx, id, model.ResourceTypeVideoSource, req.VideoSourceIDs); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SetAgentResources(ctx, id, model.ResourceTypeCameraEvent, req.CameraEventIDs); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}
	if err := h.store.SetAgentResources(ctx, id, model.ResourceTypeHostGroup, req.HostGroupIDs); err != nil {
		jsonErr(c, http.StatusInternalServerError, err.Error())
		return
	}

	jsonOK(c, gin.H{"message": "资源绑定已保存，请重新发布版本使其在对外交付中生效"})
}

func jsonOK(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": data})
}

// agentCategoryToKBType 将智能体分类映射到可绑定的知识库类型。
// 视频→video，摄像头→camera，文档/报告/运维/通用→file。
func agentCategoryToKBType(category string) string {
	switch category {
	case model.AgentCategoryVideo:
		return "video"
	case model.AgentCategoryCamera:
		return "camera"
	default:
		return "file"
	}
}

func keysOf(m map[int64]bool) []int64 {
	ids := make([]int64, 0, len(m))
	for k := range m {
		ids = append(ids, k)
	}
	return ids
}
