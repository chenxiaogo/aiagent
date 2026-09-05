package handler

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// VideoHandler 视频数据源接口。
type VideoHandler struct {
	store *store.Store
	svc   *service.Service
}

// NewVideoHandler 创建视频 Handler。
func NewVideoHandler(s *store.Store, svc *service.Service) *VideoHandler {
	return &VideoHandler{store: s, svc: svc}
}

// RegisterRoute 注册路由。
func (h *VideoHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/videos")
	{
		group.GET("", h.List)
		group.POST("/upload", h.Upload)
		group.GET("/:id", h.Get)
		group.PUT("/:id", h.Update)
		group.DELETE("/:id", h.Delete)
		group.GET("/:id/scenes", h.ListScenes)
		group.POST("/:id/reprocess", h.Reprocess)
		group.GET("/:id/stream", h.Stream)     // 视频流式播放
		group.GET("/:id/frame/:time", h.Frame) // 获取指定时间的帧
		group.POST("/search", h.Search)        // 视频语义搜索
	}
}

func (h *VideoHandler) List(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	knowledgeID, _ := strconv.ParseInt(c.Query("knowledgeId"), 10, 64)
	status := c.Query("status")
	keyword := c.Query("keyword")
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))

	videos, total, err := h.store.ListVideos(tracex.FromRequest(c), agentID, knowledgeID, status, keyword, page, pageSize)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"list":     videos,
			"total":    total,
			"page":     page,
			"pageSize": pageSize,
		},
	})
}

func (h *VideoHandler) Upload(c *gin.Context) {
	agentID, _ := strconv.ParseInt(c.PostForm("agentId"), 10, 64)
	knowledgeID, _ := strconv.ParseInt(c.PostForm("knowledgeId"), 10, 64)
	title := c.PostForm("title")

	// 视频源归属知识库：提供了 knowledgeId 时，从知识库取 AgentID，保证归属一致
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
	if agentID <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "agentId 或 knowledgeId 不能为空"})
		return
	}

	file, header, err := c.Request.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "获取上传文件失败: " + err.Error()})
		return
	}
	defer file.Close()

	// 计算文件 hash
	hash := md5.New()
	tee := io.TeeReader(file, hash)

	// 保存到本地
	uploadDir := "./uploads/videos"
	os.MkdirAll(uploadDir, 0755)

	ext := filepath.Ext(header.Filename)
	uuidName := uuid.New().String() + ext
	savePath := filepath.Join(uploadDir, uuidName)

	out, err := os.Create(savePath)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建文件失败: " + err.Error()})
		return
	}
	defer out.Close()

	fileSize, err := io.Copy(out, tee)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "保存文件失败: " + err.Error()})
		return
	}

	fileHash := hex.EncodeToString(hash.Sum(nil))

	if title == "" {
		title = header.Filename
	}

	userID, _ := c.Get("userID")
	username, _ := c.Get("username")

	video := &model.VideoDatasource{
		AgentID:      agentID,
		KnowledgeID:  knowledgeID,
		Title:        title,
		FileName:     header.Filename,
		FilePath:     savePath,
		FileSize:     fileSize,
		FileHash:     fileHash,
		Status:       model.VideoStatusPending,
		UploaderID:   toInt64(userID),
		UploaderName: toString(username),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
	if err := h.store.CreateVideo(tracex.FromRequest(c), video); err != nil {
		os.Remove(savePath)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}

	// TODO: 异步触发视频处理（抽帧 + 语音转文字 + 向量化）
	go h.processVideo(video.ID)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": video})
}

func (h *VideoHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	video, err := h.store.GetVideo(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": video})
}

func (h *VideoHandler) Update(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	video, err := h.store.GetVideo(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频不存在"})
		return
	}
	var req struct {
		Title      *string `json:"title"`
		Summary    *string `json:"summary"`
		Transcript *string `json:"transcript"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Title != nil {
		video.Title = *req.Title
	}
	if req.Summary != nil {
		video.Summary = *req.Summary
	}
	if req.Transcript != nil {
		video.Transcript = *req.Transcript
	}
	video.UpdatedAt = time.Now()
	if err := h.store.UpdateVideo(tracex.FromRequest(c), video); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": video})
}

func (h *VideoHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	video, err := h.store.GetVideo(tracex.FromRequest(c), id)
	if err == nil && video.FilePath != "" {
		os.Remove(video.FilePath)
		// 清理帧截图目录
		frameDir := fmt.Sprintf("./uploads/frames/%d", id)
		os.RemoveAll(frameDir)
	}
	if err := h.store.DeleteVideo(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

func (h *VideoHandler) ListScenes(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	scenes, err := h.store.ListVideoScenes(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": scenes})
}

func (h *VideoHandler) Reprocess(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := h.store.GetVideo(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频不存在"})
		return
	}
	h.store.UpdateVideoStatus(tracex.FromRequest(c), id, model.VideoStatusPending, "")
	go h.processVideo(id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已触发重新处理"})
}

// processVideo 异步处理视频（抽帧+向量化），完成后触发摄像头事件分析。
func (h *VideoHandler) processVideo(videoID int64) {
	ctx := tracex.NewContext(context.Background())
	// 后台任务没有 HTTP 请求上下文，需自行解析向量模型配置
	sceneEmbedCfg := ResolveEmbedModelConfig(h.store, ctx)
	visionCfg := ResolveVisionModelConfig(h.store, ctx)
	if err := h.svc.VideoProcess.ProcessVideo(ctx, videoID, sceneEmbedCfg, visionCfg); err != nil {
		ilog.Errorf("process video %d failed: %v", videoID, err)
		return
	}

	// 视频处理完成后，如果是短视频（< 60s），自动触发摄像头事件分析
	video, err := h.store.GetVideo(ctx, videoID)
	if err != nil {
		return
	}

	// 创建 CameraEvent 记录
	event := &model.CameraEvent{
		CameraID:    video.AgentID, // 复用 AgentID 作为 camera_id
		AgentID:     video.AgentID,
		KnowledgeID: video.KnowledgeID,
		CameraName:  video.Title,
		EventTime:  video.CreatedAt,
		Duration:   video.Duration,
		VideoPath:  video.FilePath,
		Processed:  false,
		CreatedAt:  time.Now(),
	}
	// embedding 列留空写 NULL（空向量写库会报 "vector must have at least 1 dimension"），
	// 向量在 ProcessEvent 分析完成后写入。
	if err := h.store.DB().WithContext(ctx).Omit("Embedding").Create(event).Error; err != nil {
		ilog.Errorf("create camera event: %v", err)
		return
	}

	// 触发视觉模型分析
	videoMcfg := h.resolveVideoModelConfig(ctx)
	embedMcfg := h.resolveVideoEmbedConfig(ctx)
	if err := h.svc.CameraEvent.ProcessEvent(ctx, event.ID, videoMcfg, embedMcfg); err != nil {
		ilog.Errorf("camera event analysis %d: %v", event.ID, err)
	}
}

// resolveVideoModelConfig 获取视觉模型配置：优先 VISION 类型，回退对话模型，config.yaml 兜底。
func (h *VideoHandler) resolveVideoModelConfig(ctx context.Context) *service.ModelConfig {
	return service.ResolveVisionModelConfig(h.store, ctx)
}

// resolveVideoEmbedConfig 从数据库获取向量模型配置。
func (h *VideoHandler) resolveVideoEmbedConfig(ctx context.Context) *service.ModelConfig {
	mcfg, err := h.store.GetActiveModelConfig(ctx, model.ModelTypeEmbedding)
	if err == nil {
		return &service.ModelConfig{
			BaseURL:   mcfg.BaseURL,
			APIKey:    mcfg.APIKey,
			ModelName: mcfg.ModelName,
		}
	}
	return nil
}

// Stream 视频流式播放（HTTP Range 支持）。
func (h *VideoHandler) Stream(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	video, err := h.store.GetVideo(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频不存在"})
		return
	}

	if _, err := os.Stat(video.FilePath); os.IsNotExist(err) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频文件不存在"})
		return
	}

	c.File(video.FilePath)
}

// Frame 获取指定时间点的帧截图。
func (h *VideoHandler) Frame(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	timeStr := c.Param("time")
	timestamp, err := strconv.ParseFloat(timeStr, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "时间格式错误"})
		return
	}

	video, err := h.store.GetVideo(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "视频不存在"})
		return
	}

	// 先尝试从已抽帧中找最近的
	framesDir := fmt.Sprintf("./uploads/frames/%d", id)
	if entries, err := os.ReadDir(framesDir); err == nil && len(entries) > 0 {
		// 计算最接近的帧
		frameIndex := int(timestamp / 10.0) // 10 秒间隔
		if frameIndex < 0 {
			frameIndex = 0
		}
		framePath := h.svc.FFmpeg.GetFramePath(framesDir, frameIndex)
		if _, err := os.Stat(framePath); err == nil {
			c.File(framePath)
			return
		}
	}

	// 实时抽帧
	tmpFrame := fmt.Sprintf("./uploads/frames/%d/tmp_%.0f.jpg", id, timestamp)
	if err := h.svc.FFmpeg.ExtractFrameAtTime(tracex.FromRequest(c), video.FilePath, timestamp, tmpFrame); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "抽帧失败"})
		return
	}
	defer os.Remove(tmpFrame)
	c.File(tmpFrame)
}

// searchResultWithVideo 视频语义搜索结果（补充真实时间戳与命中场景数）。
type searchResultWithVideo struct {
	model.SearchResult
	VideoID    int64   `json:"videoId"`
	VideoTitle string  `json:"videoTitle"`
	StartTime  float64 `json:"startTime"`
	EndTime    float64 `json:"endTime"`
	HitScenes  int     `json:"hitScenes"` // 同一视频命中的场景数（清洗前）
}

// Search 视频语义搜索。
func (h *VideoHandler) Search(c *gin.Context) {
	var req struct {
		Query       string  `json:"query"`
		AgentID     int64   `json:"agentId"`
		KnowledgeID int64   `json:"knowledgeId"`
		TopK        int     `json:"topK"`
		Threshold   float64 `json:"threshold"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Query == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	ctx := tracex.FromRequest(c)
	// 检索范围按已发布快照隔离：未发布的资源绑定变更不影响线上检索
	ctx = withEffectiveSnapshot(ctx, h.store, req.AgentID)

	// 服务端资源授权：智能体检索解析其绑定的知识库集合（视频场景归属 knowledge_id）；
	// 无知识库绑定回退 agent_id 直接归属。
	var kbIDs []int64
	if req.AgentID > 0 {
		kbIDs, _ = h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeKnowledgeBase)
		ilog.Infof("video search auth: agentId=%d kbIDs=%v", req.AgentID, kbIDs)
	}

	// 解析向量模型配置（此前这里硬编码传 nil，导致视频搜索必然失败）
	embedMcfg := ResolveEmbedModelConfig(h.store, ctx)
	results, err := h.svc.VideoProcess.SearchVideos(ctx, req.Query, req.AgentID, req.KnowledgeID, kbIDs, req.TopK, req.Threshold, embedMcfg)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": cleanVideoSearchResults(results)})
}

// cleanVideoSearchResults 视频检索结果清洗，提升命中准确性：
//  1. 时间戳：优先用场景真实 start_time/end_time（metadata 携带），解析失败才回退估算
//  2. 内容：trim、压缩空白、截断到 120 字，避免长描述刷屏
//  3. 去重：同一视频只保留分数最高的场景，避免整个结果都是同一视频；
//     命中场景数通过 hitScenes 透出，前端可提示「该视频共 N 处相关片段」
func cleanVideoSearchResults(results []model.SearchResult) []searchResultWithVideo {
	finalResults := make([]searchResultWithVideo, 0, len(results))
	hitCount := make(map[int64]int, len(results))
	for _, r := range results {
		hitCount[r.FileID]++
	}

	seen := make(map[int64]bool, len(results))
	for _, r := range results {
		// 同一视频只保留 score 最高的场景（结果已按 score 降序）
		if seen[r.FileID] {
			continue
		}
		seen[r.FileID] = true

		start, end := sceneTimeFromMetadata(r.Metadata)
		// file_id 在这里存的是 video_id
		finalResults = append(finalResults, searchResultWithVideo{
			SearchResult: model.SearchResult{
				ChunkID:    r.ChunkID,
				FileID:     r.FileID,
				FileName:   r.FileName,
				Content:    cleanSceneContent(r.Content),
				Score:      r.Score,
				ChunkIndex: r.ChunkIndex,
				Metadata:   r.Metadata,
			},
			VideoID:    r.FileID,
			VideoTitle: r.FileName,
			StartTime:  start,
			EndTime:    end,
			HitScenes:  hitCount[r.FileID],
		})
	}
	return finalResults
}

// sceneTimeFromMetadata 解析 metadata 中的场景真实起止时间；解析失败回退为按场景序号估算。
func sceneTimeFromMetadata(metadata string) (start, end float64) {
	var t struct {
		StartTime float64 `json:"startTime"`
		EndTime   float64 `json:"endTime"`
	}
	if metadata != "" && json.Unmarshal([]byte(metadata), &t) == nil && t.StartTime >= 0 && t.EndTime > t.StartTime {
		return t.StartTime, t.EndTime
	}
	return 0, 0 // 无真实时间时由上层回退（0 表示未知）
}

// cleanSceneContent 场景描述清洗：压缩空白、去重、截断。
func cleanSceneContent(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// 压缩连续空白/换行，段落保留单空格
	s = strings.Join(strings.Fields(s), " ")
	// 截断到 120 个字符
	runes := []rune(s)
	if len(runes) > 120 {
		s = string(runes[:120]) + "…"
	}
	return s
}
