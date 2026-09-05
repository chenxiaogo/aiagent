package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/pgvector/pgvector-go"

	"aiagent/internal/document"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// 重建索引的类型
const (
	IndexTypeFiles   = "files"   // 知识库文档（PDF / Word / Excel / 文本）
	IndexTypeVideos  = "videos"  // 视频场景
	IndexTypeCameras = "cameras" // 摄像头事件
)

// ReindexResult 一次重建任务的结果。
type ReindexResult struct {
	Type      string `json:"type"`
	Total     int    `json:"total"`
	Succeeded int    `json:"succeeded"`
	Failed    int    `json:"failed"`
	Items     int    `json:"items"`  // 产生的分块数 / 重建的向量条数
	Skipped   int    `json:"skipped"`
	Message   string `json:"message,omitempty"`
}

// ProgressFunc 进度回调，done/total 为已处理与总数。
type ProgressFunc func(done, total int)

// IndexerService 统一索引服务：文档分块、视频场景、摄像头事件三类向量的写入与重建。
//
// 之所以集中在这里：上传后建索引和后台重建索引走的是同一套逻辑。
// 早期 Reindex 自己抄了一份分块与向量化，结果新上传的 PDF 走真解析、
// 重新索引却仍写占位文本 —— 两份实现必然发散。
type IndexerService struct {
	store     *store.Store
	embedding *EmbeddingService
	ffmpeg    *FFmpegService
	chat      *ChatService // 视频帧视觉理解
}

func NewIndexerService(st *store.Store, emb *EmbeddingService, ffmpeg *FFmpegService, chat *ChatService) *IndexerService {
	return &IndexerService{store: st, embedding: emb, ffmpeg: ffmpeg, chat: chat}
}

// IndexFile 解析并索引单个文件，返回写入的分块数。
// 调用方需自行先清理旧分块（见 ReindexFiles）。
func (s *IndexerService) IndexFile(ctx context.Context, file *model.File) (int, error) {
	// 1. Parser → Eino Document。
	// 解析产物必须先清洗再分块：PDF 页眉页脚、分隔线、HTML 标签残留如果不在这里抹掉，
	// 会被 Embedding 原样编码进向量，检索时以高分命中，Agent 拿到的全是 "第 3 页" 这类噪声。
	docs, err := document.Parse(file.FilePath, file.FileType, file.FileName)
	if err != nil {
		return 0, err
	}
	docs = cleanDocuments(docs)
	if len(docs) == 0 {
		return 0, nil
	}
	// 解析元信息（片段数 / 页数 / 行数 / 工作表）落库，供知识库页面展示
	file.Meta = summarizeDocMeta(docs)

	// 2. Chunk：块继承所属文档的 MetaData（来源页码 / 行号等）。
	// 分块会切开行，可能重新切出短噪声块，所以分块后再过滤一次。
	type piece struct {
		text string
		meta map[string]any
	}
	pieces := make([]piece, 0, len(docs)*4)
	for _, d := range docs {
		for i, c := range s.embedding.ChunkText(d.Content, 512, 64) {
			c = document.CleanText(c)
			if c == "" || document.IsLowQuality(c) {
				continue
			}
			meta := make(map[string]any, len(d.MetaData)+1)
			for k, v := range d.MetaData {
				meta[k] = v
			}
			meta[document.MetaChunkIndex] = i
			pieces = append(pieces, piece{text: c, meta: meta})
		}
	}
	if len(pieces) == 0 {
		return 0, nil
	}

	// 3. Embedding → pgvector
	embedMcfg := ResolveEmbedModelConfig(s.store, ctx)
	written := 0
	const batchSize = 20
	for i := 0; i < len(pieces); i += batchSize {
		end := i + batchSize
		if end > len(pieces) {
			end = len(pieces)
		}
		batch := pieces[i:end]

		texts := make([]string, 0, len(batch))
		for _, p := range batch {
			texts = append(texts, p.text)
		}
		embeddings, err := s.embedding.Embed(ctx, texts, embedMcfg)
		if err != nil {
			ilog.Errorf("index file %d embed batch %d-%d: %v", file.ID, i, end, err)
			continue
		}

		chunks := make([]*model.DocumentChunk, 0, len(batch))
		for j, p := range batch {
			if j >= len(embeddings) || embeddings[j] == nil {
				continue
			}
			metaJSON := ""
			if b, mErr := json.Marshal(p.meta); mErr == nil {
				metaJSON = string(b)
			}
			chunks = append(chunks, &model.DocumentChunk{
				FileID:      file.ID,
				KnowledgeID: file.KnowledgeID,
				ChunkIndex:  i + j,
				Content:     p.text,
				ContentLen:  len([]rune(p.text)),
				Embedding:   pgvector.NewVector(toFloat32(embeddings[j])),
				Metadata:    metaJSON,
				CreatedAt:   time.Now(),
			})
		}
		if len(chunks) > 0 {
			if err := s.store.CreateChunks(ctx, chunks); err != nil {
				ilog.Errorf("index file %d save chunks: %v", file.ID, err)
				continue
			}
			written += len(chunks)
		}
	}
	return written, nil
}

// videoPiece 视频帧的语义描述（带时间区间）。
type videoPiece struct {
	start float64
	end   float64
	text  string
}

// IndexVideoFile 索引知识库上传的视频：
// FFmpeg 抽帧 → 逐帧视觉理解（Gemini / Qwen-VL 等）→ 带时间戳文本 → 向量化 → 写 document_chunks。
//
// 视频本体不进模型 API（大文件 base64 有 20MB 上限），只有抽出的帧（jpg，几百 KB）走模型，
// 因此天然支持大视频。metadata 记录时间区间，检索命中后可定位到具体片段。
func (s *IndexerService) IndexVideoFile(ctx context.Context, file *model.File) (int, error) {
	// 1. 抽帧：短视频 5 秒一帧，长视频 10 秒一帧
	info, err := s.ffmpeg.GetVideoInfo(ctx, file.FilePath)
	if err != nil {
		return 0, fmt.Errorf("读取视频信息失败: %w", err)
	}
	framesDir := filepath.Join("./uploads/frames", "file_"+strconv.FormatInt(file.ID, 10))
	framePattern := filepath.Join(framesDir, "frame_%04d.jpg")
	interval := 10.0
	if info.Duration < 60 {
		interval = 5.0
	}
	frameCount, err := s.ffmpeg.ExtractFrames(ctx, file.FilePath, framePattern, interval)
	if err != nil {
		return 0, fmt.Errorf("抽帧失败: %w", err)
	}
	if frameCount == 0 {
		return 0, nil
	}

	// 2. 逐帧视觉理解 → 带时间戳描述
	visionMcfg := ResolveVisionModelConfig(s.store, ctx)
	if visionMcfg == nil || visionMcfg.apiKey() == "" {
		return 0, fmt.Errorf("请先在「大模型配置」配置并激活视觉模型，用于视频内容理解")
	}
	pieces := make([]videoPiece, 0, frameCount)
	// 帧描述提示词：优先取「提示词库」配置（frame-description），未配置时回退内置默认
	framePrompt := s.store.GetEnabledPromptByType(ctx, model.PromptTypeFrameDescription)
	if framePrompt == "" {
		framePrompt = videoFramePrompt
	}
	{
		const concurrency = 4
		sem := make(chan struct{}, concurrency)
		var mu sync.Mutex
		var wg sync.WaitGroup
		descs := make([]string, frameCount)
		for i := 0; i < frameCount; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				img, err := os.ReadFile(s.ffmpeg.GetFramePath(framesDir, i))
				if err != nil {
					return
				}
				desc, err := s.chat.AnalyzeMedia(ctx, framePrompt, "image/jpeg", img, visionMcfg)
				if err != nil {
					desc = "画面识别失败: " + err.Error()
				}
				mu.Lock()
				descs[i] = desc
				mu.Unlock()
			}(i)
		}
		wg.Wait()
		for i, d := range descs {
			start := float64(i) * interval
			end := start + interval
			if end > info.Duration {
				end = info.Duration
			}
			pieces = append(pieces, videoPiece{
				start: start, end: end,
				text: fmt.Sprintf("[t=%s-%s] %s", FormatTime(start), FormatTime(end), d),
			})
		}
	}
	if len(pieces) == 0 {
		return 0, nil
	}

	// 3. 批量向量化 → 写 document_chunks（每帧一块，检索粒度 = 一个帧区间）
	embedMcfg := ResolveEmbedModelConfig(s.store, ctx)
	written := 0
	const batchSize = 20
	for i := 0; i < len(pieces); i += batchSize {
		end := i + batchSize
		if end > len(pieces) {
			end = len(pieces)
		}
		batch := pieces[i:end]
		texts := make([]string, 0, len(batch))
		for _, p := range batch {
			texts = append(texts, p.text)
		}
		embeddings, err := s.embedding.Embed(ctx, texts, embedMcfg)
		if err != nil {
			ilog.Errorf("index video file %d embed batch %d-%d: %v", file.ID, i, end, err)
			continue
		}
		chunks := make([]*model.DocumentChunk, 0, len(batch))
		for j, p := range batch {
			if j >= len(embeddings) || embeddings[j] == nil {
				continue
			}
			meta, _ := json.Marshal(map[string]any{
				"media": "video", "start": p.start, "end": p.end,
			})
			chunks = append(chunks, &model.DocumentChunk{
				FileID:      file.ID,
				KnowledgeID: file.KnowledgeID,
				ChunkIndex:  i + j,
				Content:     p.text,
				ContentLen:  len([]rune(p.text)),
				Embedding:   pgvector.NewVector(toFloat32(embeddings[j])),
				Metadata:    string(meta),
				CreatedAt:   time.Now(),
			})
		}
		if len(chunks) > 0 {
			if err := s.store.CreateChunks(ctx, chunks); err != nil {
				ilog.Errorf("index video file %d save chunks: %v", file.ID, err)
				continue
			}
			written += len(chunks)
		}
	}
	return written, nil
}

// ReindexFiles 重建知识库文档索引。knowledgeID 为 0 表示全部文件。
func (s *IndexerService) ReindexFiles(ctx context.Context, knowledgeID int64, onProgress ProgressFunc) *ReindexResult {
	res := &ReindexResult{Type: IndexTypeFiles}
	files, err := s.store.ListFilesForReindex(ctx, knowledgeID)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.Total = len(files)

	for i, file := range files {
		if !document.SupportedTypes[strings.ToLower(file.FileType)] {
			res.Skipped++
			continue
		}
		// 先清旧分块，避免重建后新旧并存导致重复命中
		if err := s.store.DeleteChunksByFileID(ctx, file.ID); err != nil {
			ilog.Errorf("reindex file %d clear chunks: %v", file.ID, err)
			res.Failed++
			continue
		}
		file.Status = model.FileStatusProcessing
		file.ErrorMessage = ""
		s.store.UpdateFile(ctx, file)

		n, err := s.IndexFile(ctx, file)
		if err != nil {
			ilog.Warnf("reindex file %d (%s): %v", file.ID, file.FileName, err)
			file.Status = model.FileStatusFailed
			file.ErrorMessage = err.Error()
			s.store.UpdateFile(ctx, file)
			res.Failed++
			continue
		}
		file.Status = model.FileStatusReady
		file.ChunkCount = n
		s.store.UpdateFile(ctx, file)
		s.store.UpdateFileChunkCount(ctx, file.ID, n)
		res.Succeeded++
		res.Items += n

		if onProgress != nil {
			onProgress(i+1, res.Total)
		}
	}
	return res
}

// ReindexVideoScenes 重建视频场景向量。
//
// 注意：这里只重新向量化「已有的场景描述/字幕」，不重新抽帧、不重新生成描述。
// 如果场景描述本身是占位文本（Qwen-VL 尚未接入时的产物），重建出来的仍是低质量向量 ——
// 那种情况需要先补齐视觉理解链路，再重建才有意义。
func (s *IndexerService) ReindexVideoScenes(ctx context.Context, agentID int64, onProgress ProgressFunc) *ReindexResult {
	res := &ReindexResult{Type: IndexTypeVideos}
	scenes, err := s.store.ListVideoScenesForReindex(ctx, agentID)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.Total = len(scenes)

	embedMcfg := ResolveEmbedModelConfig(s.store, ctx)
	const batchSize = 20
	for i := 0; i < len(scenes); i += batchSize {
		end := i + batchSize
		if end > len(scenes) {
			end = len(scenes)
		}
		batch := scenes[i:end]

		texts := make([]string, 0, len(batch))
		for _, sc := range batch {
			texts = append(texts, sceneText(sc))
		}
		embeddings, err := s.embedding.Embed(ctx, texts, embedMcfg)
		if err != nil {
			ilog.Errorf("reindex scenes batch %d-%d: %v", i, end, err)
			res.Failed += len(batch)
			continue
		}
		for j, sc := range batch {
			if j >= len(embeddings) || embeddings[j] == nil {
				res.Failed++
				continue
			}
			vec := pgvector.NewVector(toFloat32(embeddings[j]))
			if err := s.store.UpdateSceneEmbedding(ctx, sc.ID, vec, len(embeddings[j])); err != nil {
				ilog.Errorf("update scene %d embedding: %v", sc.ID, err)
				res.Failed++
				continue
			}
			res.Succeeded++
			res.Items++
		}
		if onProgress != nil {
			onProgress(end, res.Total)
		}
	}
	return res
}

// sceneText 拼出场景的可检索文本：描述 + 字幕。
func sceneText(sc *model.VideoScene) string {
	parts := make([]string, 0, 2)
	if d := strings.TrimSpace(sc.Description); d != "" {
		parts = append(parts, d)
	}
	if t := strings.TrimSpace(sc.Transcript); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, "\n")
}

// ReindexCameraEvents 重建摄像头事件向量（基于已生成的 AI 摘要）。
func (s *IndexerService) ReindexCameraEvents(ctx context.Context, agentID int64, onProgress ProgressFunc) *ReindexResult {
	res := &ReindexResult{Type: IndexTypeCameras}
	events, err := s.store.ListCameraEventsForReindex(ctx, agentID)
	if err != nil {
		res.Message = err.Error()
		return res
	}
	res.Total = len(events)

	embedMcfg := ResolveEmbedModelConfig(s.store, ctx)
	const batchSize = 20
	for i := 0; i < len(events); i += batchSize {
		end := i + batchSize
		if end > len(events) {
			end = len(events)
		}
		batch := events[i:end]

		texts := make([]string, 0, len(batch))
		for _, e := range batch {
			texts = append(texts, e.Summary)
		}
		embeddings, err := s.embedding.Embed(ctx, texts, embedMcfg)
		if err != nil {
			ilog.Errorf("reindex camera events batch %d-%d: %v", i, end, err)
			res.Failed += len(batch)
			continue
		}
		for j, e := range batch {
			if j >= len(embeddings) || embeddings[j] == nil {
				res.Failed++
				continue
			}
			vec := pgvector.NewVector(toFloat32(embeddings[j]))
			if err := s.store.UpdateCameraEventEmbedding(ctx, e.ID, vec, len(embeddings[j])); err != nil {
				ilog.Errorf("update camera event %d embedding: %v", e.ID, err)
				res.Failed++
				continue
			}
			res.Succeeded++
			res.Items++
		}
		if onProgress != nil {
			onProgress(end, res.Total)
		}
	}
	return res
}

// DescribeIndex 返回当前各类可检索数据的规模，供重建前预览。
func (s *IndexerService) DescribeIndex(ctx context.Context) (map[string]int64, error) {
	out := map[string]int64{}
	var files int64
	if err := s.store.DB().WithContext(ctx).Model(&model.File{}).Count(&files).Error; err == nil {
		out["files"] = files
	}
	var scenes int64
	if err := s.store.DB().WithContext(ctx).Model(&model.VideoScene{}).Count(&scenes).Error; err == nil {
		out["videoScenes"] = scenes
	}
	var events int64
	if err := s.store.DB().WithContext(ctx).Model(&model.CameraEvent{}).Where("processed = ?", true).Count(&events).Error; err == nil {
		out["cameraEvents"] = events
	}
	if chunks, err := s.store.CountChunks(ctx); err == nil {
		out["docChunks"] = chunks
	}
	return out, nil
}

// cleanDocuments 清洗解析结果：抹掉解析噪声，并丢弃清洗后为空 / 低质量的片段。
// 复用传入 slice 的底层数组，返回的文档顺序与输入一致。
func cleanDocuments(docs []*schema.Document) []*schema.Document {
	out := docs[:0]
	for _, d := range docs {
		d.Content = document.CleanText(d.Content)
		if d.Content == "" || document.IsLowQuality(d.Content) {
			continue
		}
		out = append(out, d)
	}
	return out
}

// summarizeDocMeta 汇总解析元信息为 JSON：片段数 / 页数 / 行数 / 工作表名。
// 写进 files.meta，知识库页面据此展示「这份文件被拆成了什么」。
func summarizeDocMeta(docs []*schema.Document) string {
	info := map[string]any{"segments": len(docs)}
	pages, rows := 0, 0
	sheets := make([]string, 0, 4)
	seenSheet := map[string]bool{}
	for _, d := range docs {
		if d.MetaData == nil {
			continue
		}
		if _, ok := d.MetaData[document.MetaPage]; ok {
			pages++
		}
		if _, ok := d.MetaData[document.MetaRow]; ok {
			rows++
		}
		if v, ok := d.MetaData[document.MetaSheet].(string); ok && v != "" && !seenSheet[v] {
			seenSheet[v] = true
			sheets = append(sheets, v)
		}
	}
	if pages > 0 {
		info["pages"] = pages
	}
	if rows > 0 {
		info["rows"] = rows
	}
	if len(sheets) > 0 {
		info["sheets"] = sheets
	}
	b, err := json.Marshal(info)
	if err != nil {
		return ""
	}
	return string(b)
}

var _ = fmt.Sprint
