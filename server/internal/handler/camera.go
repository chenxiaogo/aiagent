package handler

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/pkg/app/config"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// saveUploadedFile 保存上传文件到指定目录。
func saveCameraFile(file multipart.File, header *multipart.FileHeader, dir string) (string, error) {
	os.MkdirAll(dir, 0755)
	uuidName := uuid.New().String() + filepath.Ext(header.Filename)
	savePath := filepath.Join(dir, uuidName)
	out, err := os.Create(savePath)
	if err != nil {
		return "", err
	}
	defer out.Close()
	if _, err := io.Copy(out, file); err != nil {
		os.Remove(savePath)
		return "", err
	}
	return savePath, nil
}

// CameraHandler 摄像头事件接口。
type CameraHandler struct {
	store        *store.Store
	cameraEvent  *service.CameraEventService
	cameraSearch *service.CameraSearchService
	chat         *service.ChatService
	embedding    *service.EmbeddingService
}

// NewCameraHandler 创建摄像头 Handler。
func NewCameraHandler(s *store.Store, svc *service.Service) *CameraHandler {
	return &CameraHandler{
		store:        s,
		cameraEvent:  svc.CameraEvent,
		cameraSearch: svc.CameraSearch,
		chat:         svc.Chat,
		embedding:    svc.Embedding,
	}
}

// RegisterRoute 注册路由。
func (h *CameraHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/camera")
	{
		group.POST("/events", h.CreateEvent)                // 上传事件视频
		group.GET("/events", h.ListEvents)                  // 事件列表
		group.GET("/events/:id", h.GetEvent)                // 事件详情
		group.POST("/events/:id/process", h.ProcessEvent)   // 触发分析（重新分析）
		group.DELETE("/events/:id", h.DeleteEvent)          // 删除事件
		group.POST("/search", h.Search)                     // 混合搜索
		group.GET("/events/:id/stream", h.StreamVideo)      // 视频流
	}
}

// CreateEvent 创建摄像头事件。
func (h *CameraHandler) CreateEvent(c *gin.Context) {
	cameraID, _ := strconv.ParseInt(c.PostForm("cameraId"), 10, 64)
	agentID, _ := strconv.ParseInt(c.PostForm("agentId"), 10, 64)
	knowledgeID, _ := strconv.ParseInt(c.PostForm("knowledgeId"), 10, 64)
	cameraName := c.PostForm("cameraName")
	eventTimeStr := c.PostForm("eventTime")

	// 摄像头事件归属：agentId 或 knowledgeId 至少提供一个。
	// 提供了 knowledgeId 时，若 AgentID 为空则从知识库反查（平台级知识库 AgentID 可能为 0，
	// 此时以 knowledge_id 为隔离维度，同样放行）。
	if knowledgeID > 0 {
		kb, err := h.store.GetKnowledgeBase(tracex.FromRequest(c), knowledgeID)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "知识库不存在"})
			return
		}
		if agentID <= 0 {
			agentID = kb.AgentID
		}
	}
	if agentID <= 0 && knowledgeID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "agentId 或 knowledgeId 不能为空"})
		return
	}

	eventTime := time.Now()
	if eventTimeStr != "" {
		if t, err := time.Parse(time.RFC3339, eventTimeStr); err == nil {
			eventTime = t
		}
	}

	file, header, err := c.Request.FormFile("video")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请上传视频文件"})
		return
	}
	defer file.Close()

	// 保存视频
	videoPath, err := saveCameraFile(file, header, "./uploads/camera")
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存视频失败"})
		return
	}

	event := &model.CameraEvent{
		CameraID:    cameraID,
		AgentID:     agentID,
		KnowledgeID: knowledgeID,
		CameraName:  cameraName,
		EventTime:   eventTime,
		VideoPath:   videoPath,
		Processed:   false,
		CreatedAt:   time.Now(),
	}

	// 新建事件时 embedding 列写 NULL（pgvector-go 对空向量输出 "[]"，Postgres 会报
	// "vector must have at least 1 dimension"）。向量在分析完成后再写入。
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).Omit("Embedding").Create(event).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}

	// 上传后自动触发视频分析（异步）：视觉模型 JSON 结构化 + 向量化。
	// 分析失败不阻塞上传返回，事件保持 processed=false，前端仍可手动点「分析」重试。
	// 视觉模型优先取 VISION 类型配置（支持多模态），未配置时回退对话模型，避免纯文本对话模型报「not a VLM」。
	videoMcfg := service.ResolveVisionModelConfig(h.store, tracex.FromRequest(c))
	embedMcfg := h.resolveEmbedModelConfigForType(tracex.FromRequest(c), model.ModelTypeEmbedding)
	go func() {
		ctx := tracex.NewContext(context.Background())
		if err := h.cameraEvent.ProcessEvent(ctx, event.ID, videoMcfg, embedMcfg); err != nil {
			ilog.Errorf("camera event auto analysis %d: %v", event.ID, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": event})
}

// ProcessEvent 触发视频分析（已分析的事件可再次触发重新分析）。
func (h *CameraHandler) ProcessEvent(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)

	var event model.CameraEvent
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "事件不存在"})
		return
	}

	videoMcfg := service.ResolveVisionModelConfig(h.store, tracex.FromRequest(c))
	embedMcfg := h.resolveEmbedModelConfigForType(tracex.FromRequest(c), model.ModelTypeEmbedding)

	go func() {
		ctx := tracex.NewContext(context.Background())
		if err := h.cameraEvent.ProcessEvent(ctx, id, videoMcfg, embedMcfg); err != nil {
			ilog.Errorf("process camera event %d: %v", id, err)
		}
	}()

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已触发分析"})
}

// DeleteEvent 删除摄像头事件：删除数据库记录，并清理视频/缩略图文件。
// 已发布快照（AgentReleaseSnapshot）中冻结的 cameraEventId 为历史数据，不做级联改动。
func (h *CameraHandler) DeleteEvent(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var event model.CameraEvent
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "事件不存在"})
		return
	}

	// 清理文件（失败不阻塞删除，仅告警）
	for _, p := range []string{event.VideoPath, event.ThumbnailPath} {
		if p == "" {
			continue
		}
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			ilog.Warnf("remove camera event file %s: %v", p, err)
		}
	}

	if err := h.store.DB().WithContext(tracex.FromRequest(c)).Delete(&event).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已删除"})
}

// Search 混合搜索（自然语言解析 + SQL 条件 + 向量搜索）。
func (h *CameraHandler) Search(c *gin.Context) {
	var req model.CameraEventSearchRequest
	if err := c.ShouldBindJSON(&req); err != nil || req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	ctx := tracex.FromRequest(c)
	// 检索范围按已发布快照隔离：未发布的资源绑定变更不影响线上检索
	ctx = withEffectiveSnapshot(ctx, h.store, req.AgentID)
	ilog.Infof("camera search request: query=%q agentId=%d knowledgeId=%d cameraId=%d topK=%d threshold=%v",
		req.Query, req.AgentID, req.KnowledgeID, req.CameraID, req.TopK, req.Threshold)

	// 1. LLM 解析自然语言
	chatMcfg := h.resolveModelConfigForType(ctx, model.ModelTypeChat)
	condition, searchQuery, err := h.cameraSearch.ParseNaturalLanguage(ctx, req.Query, h.chat, chatMcfg)
	if err != nil {
		ilog.Warnf("parse query: %v", err)
		condition = &service.SearchCondition{}
		searchQuery = req.Query
	}
	ilog.Infof("camera search parsed: searchQuery=%q condition=%+v", searchQuery, condition)

	// 2. 合并手动筛选条件
	if req.CameraID > 0 && len(condition.CameraIDs) == 0 {
		condition.CameraIDs = []int64{req.CameraID}
	}
	if req.StartTime != "" && condition.StartTime.IsZero() {
		condition.StartTime, _ = time.Parse(time.RFC3339, req.StartTime)
	}
	if req.EndTime != "" && condition.EndTime.IsZero() {
		condition.EndTime, _ = time.Parse(time.RFC3339, req.EndTime)
	}
	if req.HasPerson != nil && condition.HasPerson == nil {
		condition.HasPerson = req.HasPerson
	}
	if req.HasVehicle != nil && condition.HasVehicle == nil {
		condition.HasVehicle = req.HasVehicle
	}
	if req.HasPackage != nil && condition.HasPackage == nil {
		condition.HasPackage = req.HasPackage
	}
	if req.Action != "" {
		condition.Action = req.Action
	}
	if len(req.Colors) > 0 {
		condition.Colors = req.Colors
	}
	if req.AgentID > 0 {
		// 服务端资源授权：智能体检索只能命中其显式绑定的摄像头事件；
		// 无事件绑定但绑定了知识库 → 按知识库过滤（事件归属 knowledge_id）；
		// 均无绑定 → 回退按 agent_id 直接归属过滤。
		eventIDs, _ := h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeCameraEvent)
		kbIDs, _ := h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeKnowledgeBase)
		switch {
		case len(eventIDs) > 0:
			condition.EventIDs = eventIDs
		case len(kbIDs) > 0:
			condition.KnowledgeIDs = kbIDs
		default:
			condition.AgentID = req.AgentID
		}
		ilog.Infof("camera search auth: agentId=%d eventIDs=%v kbIDs=%v", req.AgentID, eventIDs, kbIDs)
	}
	if req.KnowledgeID > 0 {
		condition.KnowledgeID = req.KnowledgeID
	}

	// 3. 向量化搜索查询（失败时用文本搜索兜底）
	embedMcfg := h.resolveEmbedModelConfigForType(ctx, model.ModelTypeEmbedding)
	embeddings, err := h.embedding.EmbedQuery(ctx, searchQuery, embedMcfg)
	if err != nil {
		ilog.Warnf("embed query failed, using text search: %v", err)
		// 直接文本搜索（与向量检索保持一致：最多 5 条）
		textResults, _ := h.store.CameraTextSearch(ctx, searchQuery, req.AgentID, req.KnowledgeID, 5)
		ilog.Infof("camera search done (text fallback): hits=%d", len(textResults))
		c.JSON(http.StatusOK, gin.H{
			"code": 0,
			"data": gin.H{"results": textResults, "total": len(textResults), "searchQuery": searchQuery},
		})
		return
	}

	// 4. 混合搜索
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 0.45
	}

	results, err := h.cameraSearch.HybridSearch(ctx, embeddings, condition, topK, threshold)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	ilog.Infof("camera search done: hits=%d threshold=%.2f", len(results), threshold)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"results":     results,
			"total":       len(results),
			"searchQuery": searchQuery,
			"condition":   condition,
		},
	})
}

// ListEvents 事件列表。
func (h *CameraHandler) ListEvents(c *gin.Context) {
	cameraID, _ := strconv.ParseInt(c.Query("cameraId"), 10, 64)
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	knowledgeID, _ := strconv.ParseInt(c.Query("knowledgeId"), 10, 64)
	processed := c.Query("processed")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	db := h.store.DB().WithContext(tracex.FromRequest(c)).Model(&model.CameraEvent{})

	if cameraID > 0 {
		db = db.Where("camera_id = ?", cameraID)
	}
	if agentID > 0 {
		db = db.Where("agent_id = ?", agentID)
	}
	if knowledgeID > 0 {
		db = db.Where("knowledge_id = ?", knowledgeID)
	}
	if processed == "true" {
		db = db.Where("processed = ?", true)
	} else if processed == "false" {
		db = db.Where("processed = ?", false)
	}

	var total int64
	db.Count(&total)

	var events []model.CameraEvent
	db.Order("event_time DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&events)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"list": events, "total": total, "page": page, "pageSize": pageSize},
	})
}

// GetEvent 事件详情。
func (h *CameraHandler) GetEvent(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var event model.CameraEvent
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "事件不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": event})
}

// StreamVideo 视频流。
func (h *CameraHandler) StreamVideo(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var event model.CameraEvent
	if err := h.store.DB().WithContext(tracex.FromRequest(c)).First(&event, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "事件不存在"})
		return
	}
	c.File(event.VideoPath)
}

// resolveModelConfigForType 从数据库获取指定类型的模型配置。
func (h *CameraHandler) resolveModelConfigForType(ctx context.Context, modelType string) *service.ModelConfig {
	mcfg, err := h.store.GetActiveModelConfig(ctx, modelType)
	if err == nil {
		return &service.ModelConfig{
			BaseURL:     mcfg.BaseURL,
			APIKey:      mcfg.APIKey,
			ModelName:   mcfg.ModelName,
			MaxTokens:   mcfg.MaxTokens,
			Temperature: mcfg.Temperature,
		}
	}
	cfg := config.GetCurrentConfig()
	return service.DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.ChatModel)
}

// resolveEmbedModelConfigForType 从数据库获取向量模型配置。
func (h *CameraHandler) resolveEmbedModelConfigForType(ctx context.Context, modelType string) *service.ModelConfig {
	mcfg, err := h.store.GetActiveModelConfig(ctx, modelType)
	if err == nil {
		return &service.ModelConfig{
			BaseURL:   mcfg.BaseURL,
			APIKey:    mcfg.APIKey,
			ModelName: mcfg.ModelName,
		}
	}
	cfg := config.GetCurrentConfig()
	return service.DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.EmbedModel)
}