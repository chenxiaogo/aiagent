package handler

import (
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"aiagent/internal/knowledge"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

// FileHandler 文件管理接口。
type FileHandler struct {
	store     *store.Store
	svc       *service.Service
	uploadDir string
}

// NewFileHandler 创建文件 Handler。
func NewFileHandler(s *store.Store, svc *service.Service) *FileHandler {
	dir := "./uploads"
	os.MkdirAll(dir, 0755)
	return &FileHandler{store: s, svc: svc, uploadDir: dir}
}

// RegisterRoute 注册路由。
func (h *FileHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/files")
	{
		group.POST("/upload", h.Upload)       // 上传文件
		group.GET("", h.List)                 // 文件列表
		group.GET("/:id", h.Get)              // 文件详情
		group.DELETE("/:id", h.Delete)        // 删除文件
		group.POST("/:id/reindex", h.Reindex) // 重新索引
		group.POST("/search", h.Search)       // 文档语义检索（纯检索，不调 LLM）
		group.GET("/:id/chunks", h.ListChunks) // 查看切片（分块内容）
		group.PUT("/:id/tags", h.UpdateTags)   // 更新文件标签
	}
}

// ListChunks 查看文件的切片内容（按原文顺序分页）。
// 传入 chunkIndex 时自动定位到该分块所在页（检索命中后「在原文中定位」）。
func (h *FileHandler) ListChunks(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "20"))
	offset, _ := strconv.Atoi(c.DefaultQuery("offset", "0"))
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}

	// 按分块序号定位：chunk_index 从 0 开始连续递增，目标块所在页 = (idx/limit)*limit
	if v := c.Query("chunkIndex"); v != "" {
		if idx, err := strconv.Atoi(v); err == nil && idx >= 0 {
			offset = (idx / limit) * limit
		}
	}

	chunks, total, err := h.store.ListFileChunks(tracex.FromRequest(c), id, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": chunks, "total": total})
}

// UpdateTags 更新文件标签（传入覆盖式，空数组表示清空）。
func (h *FileHandler) UpdateTags(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	var req struct {
		Tags []string `json:"tags"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	tags := normalizeTags(req.Tags)
	if err := h.store.UpdateFileTags(tracex.FromRequest(c), id, tags); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"tags": tags}})
}

// normalizeTags 标签归一化：去空白、去重、去空串，输出逗号分隔字符串。
// 标签用逗号分隔的普通列存，不建关联表 —— 标签量级小、查询以 LIKE 为主，关联表是过度设计。
func normalizeTags(in []string) string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, t := range in {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	return strings.Join(out, ",")
}


// Search 文档语义检索：向量 + 全文混合，不调用 LLM 生成回答。
// 与对话链路的 RAG 区别：这里只返回命中的文档分块，供「智能检索」页展示。
func (h *FileHandler) Search(c *gin.Context) {
	var req struct {
		Query       string  `json:"query"`
		KnowledgeID int64   `json:"knowledgeId"`
		AgentID     int64   `json:"agentId"`
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
	topK := req.TopK
	if topK <= 0 {
		topK = 5
	}
	threshold := req.Threshold
	if threshold <= 0 {
		threshold = 0.45
	}

	// 服务端资源授权：带 agentId 时解析该智能体绑定的知识库集合，
	// 文档检索范围限定在这些知识库内；未带 agentId 保持原逻辑（按 knowledgeId 过滤或全库）。
	var kbIDs []int64
	if req.AgentID > 0 {
		kbIDs, _ = h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeKnowledgeBase)
		ilog.Infof("file search auth: agentId=%d kbIDs=%v", req.AgentID, kbIDs)
	}

	// 解析向量模型配置（与上传/重建索引一致，避免传 nil 导致必然失败）
	embedMcfg := ResolveEmbedModelConfig(h.store, ctx)
	emb, err := h.svc.Embedding.EmbedQuery(ctx, req.Query, embedMcfg)
	if err != nil {
		ilog.Warnf("doc search embed failed, fallback to fulltext: %v", err)
		// 向量化失败时降级为纯全文检索
		var results []model.SearchResult
		var ferr error
		if len(kbIDs) > 0 {
			results, ferr = h.store.FullTextSearchInKBs(ctx, req.Query, kbIDs, topK*2)
		} else {
			results, ferr = h.store.FullTextSearch(ctx, req.Query, req.KnowledgeID, topK*2)
		}
		if ferr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": ferr.Error()})
			return
		}
		results = knowledge.CleanSearchResults(results, topK)
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": results, "total": len(results), "fallback": true})
		return
	}

	// 多召回一倍：清洗（去噪 / 去重）会淘汰一部分，召回量不足会让最终条数明显偏少
	var results []model.SearchResult
	if len(kbIDs) > 0 {
		results, err = h.store.HybridSearchInKBs(ctx, emb, req.Query, kbIDs, topK*2, threshold)
	} else {
		results, err = h.store.HybridSearch(ctx, emb, req.Query, req.KnowledgeID, topK*2, threshold)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	results = knowledge.CleanSearchResults(results, topK)

	c.JSON(http.StatusOK, gin.H{
		"code":  0,
		"data":  results,
		"total": len(results),
	})
}

// Upload 上传文件并索引。
func (h *FileHandler) Upload(c *gin.Context) {
	knowledgeID, _ := strconv.ParseInt(c.PostForm("knowledgeId"), 10, 64)
	agentID, _ := strconv.ParseInt(c.PostForm("agentId"), 10, 64)

	form, err := c.MultipartForm()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请选择文件"})
		return
	}

	files := form.File["files"]
	if len(files) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "请选择文件"})
		return
	}

	ctx := tracex.FromRequest(c)
	uidInt, usernameStr, _ := middleware.CurrentUser(c)

	var uploaded []*model.File
	for _, fh := range files {
		file, err := h.saveFile(ctx, fh, knowledgeID, agentID, uidInt, usernameStr)
		if err != nil {
			ilog.Errorf("save file %s: %v", fh.Filename, err)
			continue
		}
		uploaded = append(uploaded, file)

		// 异步处理文件（分块 + 向量化）。
		// 必须用独立的 context：接口一返回，gin 就会 cancel 请求 ctx，
		// 复用它的话 goroutine 里第一次写库就是 context canceled，
		// 文件会永远停在「待处理」、分块数为 0。
		taskCtx := tracex.NewContext(context.Background())
		go h.processFile(taskCtx, file, fh)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": uploaded})
}

func (h *FileHandler) saveFile(ctx context.Context, fh *multipart.FileHeader, knowledgeID int64, agentID int64, uid int64, username string) (*model.File, error) {
	ext := strings.ToLower(filepath.Ext(fh.Filename))
	fileType := strings.TrimPrefix(ext, ".")
	if fileType == "" {
		fileType = "txt"
	}

	// 生成唯一文件名
	saveName := uuid.New().String() + ext
	savePath := filepath.Join(h.uploadDir, saveName)

	src, err := fh.Open()
	if err != nil {
		return nil, err
	}
	defer src.Close()

	dst, err := os.Create(savePath)
	if err != nil {
		return nil, err
	}
	defer dst.Close()

	if _, err := io.Copy(dst, src); err != nil {
		return nil, err
	}

	file := &model.File{
		KnowledgeID:  knowledgeID,
		AgentID:      agentID,
		FileName:     fh.Filename,
		FilePath:     savePath,
		FileType:     fileType,
		FileSize:     fh.Size,
		StorageType:  "local",
		Status:       model.FileStatusPending,
		UploaderID:   uid,
		UploaderName: username,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	if err := h.store.CreateFile(ctx, file); err != nil {
		return nil, err
	}
	return file, nil
}

// videoExts 视频格式。视频要走 FFmpeg 抽帧 → 视觉理解 → ASR 的完整流水线
// （见 service.VideoProcessService），知识库的文件链路只做文本抽取，
// 不能把视频二进制当文本分块向量化 —— 那样会往 pgvector 里灌一堆无语义的噪声。
var videoExts = map[string]bool{
	"mp4": true, "avi": true, "mov": true, "mkv": true, "flv": true,
	"wmv": true, "webm": true, "m4v": true, "mpg": true, "mpeg": true, "ts": true,
}

// isVideoFile 判断文件是否为视频格式。
func isVideoFile(fileType string) bool {
	return videoExts[strings.ToLower(strings.TrimPrefix(fileType, "."))]
}

func (h *FileHandler) processFile(ctx context.Context, file *model.File, fh *multipart.FileHeader) {
	ilog.Infof("processing file %d: %s", file.ID, file.FileName)

	// 更新状态为处理中
	file.Status = model.FileStatusProcessing
	h.store.UpdateFile(ctx, file)

	// 视频走独立流水线：FFmpeg 抽帧 → 视觉模型逐帧理解 → 带时间戳文本 → 向量化。
	// 不能把视频二进制当文本分块 —— 会往向量库里灌无语义噪声。
	if isVideoFile(file.FileType) {
		h.processVideoFile(ctx, file)
		return
	}

	// Parser → Eino Document → Chunk → Qwen Embedding → pgvector。
	// 逻辑统一放在 service.IndexerService，上传建索引与后台重建共用同一份实现，
	// 避免两处各写一份导致行为发散（历史上 Reindex 就抄出过占位文本版本）。
	n, err := h.svc.Indexer.IndexFile(ctx, file)
	if err != nil {
		ilog.Errorf("index file %s: %v", file.FileName, err)
		file.Status = model.FileStatusFailed
		file.ErrorMessage = err.Error()
		h.store.UpdateFile(ctx, file)
		return
	}
	if n == 0 {
		file.Status = model.FileStatusFailed
		file.ErrorMessage = "未从文件中提取到可索引内容"
		h.store.UpdateFile(ctx, file)
		return
	}

	h.store.UpdateFileChunkCount(ctx, file.ID, n)
	ilog.Infof("file %d indexed: %d chunks", file.ID, n)
}

// processVideoFile 知识库视频：抽帧 → 视觉理解 → 向量化，复用 IndexerService.IndexVideoFile。
func (h *FileHandler) processVideoFile(ctx context.Context, file *model.File) {
	visionCfg := service.ResolveVisionModelConfig(h.store, ctx)
	if visionCfg == nil || visionCfg.APIKey == "" {
		file.Status = model.FileStatusFailed
		file.ErrorMessage = "请先在「系统设置 → 大模型配置」中配置并激活视觉模型，用于视频内容理解"
		h.store.UpdateFile(ctx, file)
		return
	}

	n, err := h.svc.Indexer.IndexVideoFile(ctx, file)
	if err != nil {
		ilog.Errorf("index video file %s: %v", file.FileName, err)
		file.Status = model.FileStatusFailed
		file.ErrorMessage = err.Error()
		h.store.UpdateFile(ctx, file)
		return
	}
	if n == 0 {
		file.Status = model.FileStatusFailed
		file.ErrorMessage = "未从视频中提取到可索引内容（可能抽帧失败）"
		h.store.UpdateFile(ctx, file)
		return
	}
	h.store.UpdateFileChunkCount(ctx, file.ID, n)
	ilog.Infof("video file %d indexed: %d chunks", file.ID, n)
}

func float64ToFloat32(f []float64) []float32 {
	r := make([]float32, len(f))
	for i, v := range f {
		r[i] = float32(v)
	}
	return r
}

// List 文件列表。
func (h *FileHandler) List(c *gin.Context) {
	knowledgeID, _ := strconv.ParseInt(c.Query("knowledgeId"), 10, 64)
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	files, err := h.store.ListFiles(tracex.FromRequest(c), knowledgeID, agentID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": files})
}

// Get 文件详情。
func (h *FileHandler) Get(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	file, err := h.store.GetFile(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": file})
}

// Delete 删除文件。
func (h *FileHandler) Delete(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	file, err := h.store.GetFile(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}

	// 删除物理文件
	os.Remove(file.FilePath)

	if err := h.store.DeleteFile(tracex.FromRequest(c), id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

// Reindex 重新索引。
func (h *FileHandler) Reindex(c *gin.Context) {
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	file, err := h.store.GetFile(tracex.FromRequest(c), id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}

	// 删除旧分块
	h.store.DeleteChunksByFileID(tracex.FromRequest(c), id)

	// 重新处理
	file.Status = model.FileStatusPending
	h.store.UpdateFile(tracex.FromRequest(c), file)

	// 复用上传后的处理链路（Parser → Eino Document → Chunk → Embedding → pgvector）。
	// 早先这里重复实现了一份分块与向量化，两条链路很容易走出不同结果，
	// 例如新上传的 PDF 走真解析、重新索引却仍写占位文本。
	// context 同样要独立：接口返回后请求 ctx 会被 cancel。
	go h.processFile(tracex.NewContext(context.Background()), file, nil)

	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "已触发重新索引"})
}
