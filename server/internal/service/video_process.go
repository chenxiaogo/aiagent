package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/pgvector/pgvector-go"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// videoFramePrompt 单帧画面理解提示词（知识库视频与视频数据源共用）。
const videoFramePrompt = "请用中文描述这个视频画面的主要内容，按要点覆盖：人物（性别、年龄段、衣着颜色、动作、朝向）、车辆（类型、颜色、行驶或停放状态）、宠物（品种、毛色）、包裹、环境地点、异常情况。突出颜色、数量、方向等可检索关键词。只输出客观描述，不超过 120 字，不要输出 JSON。"

// VideoProcessService 视频处理流水线服务。
// 流水线：FFmpeg 抽帧 + 提取音频 → 场景切片 → 场景描述 → 向量化 → 存库
type VideoProcessService struct {
	store     *store.Store
	ffmpeg    *FFmpegService
	embedding *EmbeddingService
	chat      *ChatService
}

// NewVideoProcessService 创建视频处理服务。
func NewVideoProcessService(s *store.Store, ffmpeg *FFmpegService, emb *EmbeddingService, chat *ChatService) *VideoProcessService {
	return &VideoProcessService{
		store:     s,
		ffmpeg:    ffmpeg,
		embedding: emb,
		chat:      chat,
	}
}

// ProcessVideo 处理视频：抽帧 → 逐帧视觉理解 → 生成场景描述 → 向量化 → 落库。
// embedMcfg 为向量模型配置（传 nil 会导致场景向量化必然失败）；
// visionMcfg 为视觉理解模型配置（传 nil 时场景描述退化为时间戳占位，检索无语义，不建议）。
func (v *VideoProcessService) ProcessVideo(ctx context.Context, videoID int64, embedMcfg, visionMcfg *ModelConfig) error {
	video, err := v.store.GetVideo(ctx, videoID)
	if err != nil {
		return fmt.Errorf("get video: %w", err)
	}

	// 更新状态为处理中
	v.store.UpdateVideoStatus(ctx, videoID, model.VideoStatusProcessing, "")

	// 1. 获取视频信息
	info, err := v.ffmpeg.GetVideoInfo(ctx, video.FilePath)
	if err != nil {
		v.store.UpdateVideoStatus(ctx, videoID, model.VideoStatusFailed, "获取视频信息失败: "+err.Error())
		return fmt.Errorf("get video info: %w", err)
	}
	video.Duration = info.Duration
	video.Resolution = fmt.Sprintf("%dx%d", info.Width, info.Height)
	video.FPS = info.FPS
	v.store.UpdateVideo(ctx, video)

	// 2. 抽帧（每 10 秒一帧，可配置）
	framesDir := filepath.Join("./uploads/frames", fmt.Sprintf("%d", videoID))
	framePattern := filepath.Join(framesDir, "frame_%04d.jpg")
	frameInterval := 10.0 // 每 10 秒一帧
	if info.Duration < 60 {
		frameInterval = 5.0 // 短视频 5 秒一帧
	}

	frameCount, err := v.ffmpeg.ExtractFrames(ctx, video.FilePath, framePattern, frameInterval)
	if err != nil {
		v.store.UpdateVideoStatus(ctx, videoID, model.VideoStatusFailed, "抽帧失败: "+err.Error())
		return fmt.Errorf("extract frames: %w", err)
	}

	// 3. 生成场景切片（基于抽帧时间间隔）
	scenes := make([]*model.VideoScene, 0, frameCount)
	for i := 0; i < frameCount; i++ {
		startTime := float64(i) * frameInterval
		endTime := float64(i+1) * frameInterval
		if endTime > video.Duration {
			endTime = video.Duration
		}

		framePath := v.ffmpeg.GetFramePath(framesDir, i)
		scenes = append(scenes, &model.VideoScene{
			VideoID:    videoID,
			AgentID:    video.AgentID,
			SceneIndex: i,
			StartTime:  startTime,
			EndTime:    endTime,
			Duration:   endTime - startTime,
			FramePath:  framePath,
		})
	}

	// 4. 场景描述：逐帧调用多模态模型（Gemini / Qwen-VL 等），带时间戳前缀。
	// 帧是小图（几百 KB），天然支持大视频 —— 视频本体不进模型 API，只有抽出的帧走 base64。
	// 帧描述提示词：优先取「提示词库」配置（frame-description），未配置时回退内置默认
	framePrompt := v.store.GetEnabledPromptByType(ctx, model.PromptTypeFrameDescription)
	if framePrompt == "" {
		framePrompt = videoFramePrompt
	}

	if visionMcfg != nil && visionMcfg.apiKey() != "" {
		const concurrency = 4
		sem := make(chan struct{}, concurrency)
		var mu sync.Mutex
		var wg sync.WaitGroup
		for i, sc := range scenes {
			wg.Add(1)
			go func(i int, sc *model.VideoScene) {
				defer wg.Done()
				select {
				case sem <- struct{}{}:
					defer func() { <-sem }()
				case <-ctx.Done():
					return
				}
				desc := fmt.Sprintf("画面识别失败: %v", ctx.Err())
				img, err := os.ReadFile(sc.FramePath)
				if err == nil {
					desc, err = v.chat.AnalyzeMedia(ctx, framePrompt, "image/jpeg", img, visionMcfg)
					if err != nil {
						desc = fmt.Sprintf("画面识别失败: %v", err)
					}
				}
				mu.Lock()
				sc.Description = fmt.Sprintf("[%s-%s] %s", FormatTime(sc.StartTime), FormatTime(sc.EndTime), desc)
				mu.Unlock()
			}(i, sc)
		}
		wg.Wait()
	} else {
		ilog.Warnf("video %d: 未配置视觉模型，场景描述使用时间戳占位（检索无语义）", videoID)
		for i, s := range scenes {
			s.Description = fmt.Sprintf("场景 %d (时间 %s - %s)", i+1,
				FormatTime(s.StartTime), FormatTime(s.EndTime))
		}
	}

	// 5. 向量化场景描述
	descriptions := make([]string, len(scenes))
	for i, s := range scenes {
		descriptions[i] = s.Description
	}

	embeddings, err := v.embedding.Embed(ctx, descriptions, embedMcfg)
	if err != nil {
		ilog.Warnf("embed scenes failed: %v", err)
		// 向量化失败不中断，标记部分失败
	} else {
		for i, emb := range embeddings {
			scenes[i].Embedding = pgvector.NewVector(toFloat32(emb))
			scenes[i].TokenCount = len(descriptions[i]) / 4 // 粗略估算
		}
	}

	// 6. 保存场景到数据库
	if len(scenes) > 0 {
		if err := v.store.CreateVideoScenes(ctx, scenes); err != nil {
			v.store.UpdateVideoStatus(ctx, videoID, model.VideoStatusFailed, "保存场景失败: "+err.Error())
			return fmt.Errorf("create video scenes: %w", err)
		}
	}

	// 7. 生成视频摘要（基于场景描述拼接）
	summary := v.generateSummary(video.Title, scenes)
	video.Summary = summary
	video.SceneCount = len(scenes)
	video.ChunkCount = len(scenes)
	video.Status = model.VideoStatusReady
	video.UpdatedAt = time.Now()
	if err := v.store.UpdateVideo(ctx, video); err != nil {
		return fmt.Errorf("update video: %w", err)
	}

	ilog.Infof("video %d processed: %d scenes", videoID, len(scenes))
	return nil
}

// generateSummary 生成视频摘要（基于场景描述）。
func (v *VideoProcessService) generateSummary(title string, scenes []*model.VideoScene) string {
	if len(scenes) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("视频「%s」共包含 %d 个场景片段。\n\n", title, len(scenes)))
	sb.WriteString("主要场景概览：\n")

	// 取前 10 个场景做概览
	limit := len(scenes)
	if limit > 10 {
		limit = 10
	}
	for i := 0; i < limit; i++ {
		s := scenes[i]
		sb.WriteString(fmt.Sprintf("%d. [%s - %s] %s\n",
			i+1, FormatTime(s.StartTime), FormatTime(s.EndTime), s.Description))
	}

	if len(scenes) > 10 {
		sb.WriteString(fmt.Sprintf("\n... 还有 %d 个场景", len(scenes)-10))
	}

	return sb.String()
}

// SearchVideos 视频场景语义搜索。
// embedMcfg 为向量模型配置，由调用方从「模型配置」解析后传入；传 nil 会导致必然失败
// （Embed 内部对 nil 配置直接报「请配置并激活一个向量模型」）。
func (v *VideoProcessService) SearchVideos(ctx context.Context, query string, agentID, knowledgeID int64, knowledgeIDs []int64, topK int, threshold float64, embedMcfg *ModelConfig) ([]model.SearchResult, error) {
	if topK <= 0 {
		topK = 5
	}
	if threshold <= 0 {
		threshold = 0.45
	}

	// 向量化查询
	embeddings, err := v.embedding.Embed(ctx, []string{query}, embedMcfg)
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}

	// 向量搜索
	results, err := v.store.VideoVectorSearch(ctx, embeddings[0], agentID, knowledgeID, knowledgeIDs, topK, threshold)
	if err != nil {
		return nil, fmt.Errorf("vector search: %w", err)
	}

	return results, nil
}

// CleanupVideo 清理视频相关资源（文件 + 场景）。
func (v *VideoProcessService) CleanupVideo(ctx context.Context, videoID int64) error {
	// 清理帧目录
	frameDir := filepath.Join("./uploads/frames", fmt.Sprintf("%d", videoID))
	os.RemoveAll(frameDir)

	// 清理音频
	audioPath := filepath.Join("./uploads/audio", fmt.Sprintf("%d.wav", videoID))
	os.Remove(audioPath)

	return nil
}
