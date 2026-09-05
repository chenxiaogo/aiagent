package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pgvector/pgvector-go"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// maxAnalysisFrames 大视频抽帧分析的最大帧数（均匀抽取）。
const maxAnalysisFrames = 12

// analysisFrameWidth 抽帧缩放宽度（控制单帧体积，-2 由 ffmpeg 保持偶数高度）。
const analysisFrameWidth = 640

// CameraEventService 摄像头事件处理流水线。
// 视频上传 → 视觉模型分析 → 结构化 + 向量化 → 存储
type CameraEventService struct {
	store     *store.Store
	ffmpeg    *FFmpegService
	chat      *ChatService
	embedding *EmbeddingService
}

// NewCameraEventService 创建摄像头事件服务。
func NewCameraEventService(s *store.Store, ffmpeg *FFmpegService, chat *ChatService, emb *EmbeddingService) *CameraEventService {
	return &CameraEventService{store: s, ffmpeg: ffmpeg, chat: chat, embedding: emb}
}

// visualAnalysisBasePrompt 视频分析提示词（JSON 结构部分）。
const visualAnalysisBasePrompt = `你是一个专业的监控视频分析助手。请分析这段监控视频，输出以下 JSON 格式的结果（只输出 JSON）：

{
  "summary": "视频内容的完整中文描述（描述事件时标注视频内大致秒数，如「第 12-18 秒，一辆白色轿车驶入」）",
  "event_start_sec": 0,
  "event_end_sec": 0,
  "has_person": true/false,
  "person_count": 0,
  "person_desc": "人物描述，至少包含：性别年龄段（老人/成人/儿童）、上衣与裤子颜色和款式、发型、是否戴帽子/背包/打伞/手持物品、移动方向",
  "has_vehicle": true/false,
  "vehicle_type": "car/bike/e-bike/truck/bus/motorcycle/other",
  "vehicle_desc": "车辆描述，至少包含：车身颜色、车型（轿车/SUV/面包车/皮卡/货车等）、行驶方向或停放位置、是否有人上下车",
  "has_pet": true/false,
  "pet_type": "cat/dog/bird/other",
  "pet_desc": "宠物描述，至少包含：品种、毛色、体型大小、是否被牵引/拴住、是否有主人陪同",
  "has_package": true/false,
  "package_desc": "包裹描述，至少包含：大小、颜色、材质、摆放位置、有无快递单或标记",
  "action": "walking/running/stopped/picking_up/delivering/entering/leaving/none",
  "action_desc": "动作详细描述（谁+做什么+在哪个区域，标注大致秒数）",
  "dominant_colors": ["red", "blue"],
  "color_desc": "画面主要颜色描述",
  "zone": "entrance/yard/gate/front_door/driveway/indoor/other"
}

规则：
1. 只输出 JSON，不要 Markdown、不要解释
2. 字段自洽：has_person 为 true 时 person_count 必须大于 0 且 person_desc 不能为空；同理 has_vehicle/has_pet/has_package 为 true 时，对应的 type 和 desc 都必须填；为 false 时一律填 false / 0 / 空字符串
3. person_desc、vehicle_desc、pet_desc、package_desc 必须写成简短、可直接检索的中文短语，优先突出颜色、类型、数量、方向、状态等关键词（例：「穿红色短袖的成年男性，走向大门」「白色SUV停在大门口」「棕色泰迪犬，被牵引」）
4. 没有某类对象时，对应字段设为 false/null/空
5. 看不清或不确定的细节不要编造，可写「模糊」「看不清」
6. summary 要按时间顺序详细描述画面中实际发生的事件
7. 不要编造视频中不存在的内容
8. event_start_sec / event_end_sec 表示事件在视频内的起止秒数（相对视频开头 0 起算，end 须大于 start）；无法判断或没有事件时都填 0`

// buildVisualAnalysisPrompt 组装分析提示词：注入视频总时长与抽帧时间锚点，
// 让模型输出带「秒数」的事件摘要与视频内事件区间，供检索跳转播放使用。
func buildVisualAnalysisPrompt(base string, duration float64, frameTimeline string) string {
	p := base
	if duration > 0 {
		p += fmt.Sprintf("\n时间说明：视频总长约 %.0f 秒。", duration)
	}
	if frameTimeline != "" {
		p += fmt.Sprintf("\n帧时间说明：你收到的画面按时间顺序取自视频内约 %s 秒处（仅供参考，事件起止可按画面连续性估计）。", frameTimeline)
	}
	return p
}

// ProcessEvent 处理摄像头事件：视频分析 → 结构化 → 向量化 → 存储。
func (s *CameraEventService) ProcessEvent(ctx context.Context, eventID int64, videoMcfg, embedMcfg *ModelConfig) error {
	// 1. 获取事件记录
	var event model.CameraEvent
	if err := s.store.DB().WithContext(ctx).First(&event, eventID).Error; err != nil {
		return fmt.Errorf("get event: %w", err)
	}

	// 3. 调用视觉模型分析（小视频整段 / 大视频抽帧多图，JSON 结构一致）
	analysis, err := s.analyzeWithVisionModel(ctx, event.VideoPath, videoMcfg)
	if err != nil {
		// 记录失败原因。注意必须排除 Embedding 列：event 从 DB 读出的向量为空，
		// Save 全字段会把空向量写进 pgvector 列，报 "vector must have at least 1 dimension"。
		event.ProcessError = err.Error()
		if e := s.store.DB().WithContext(ctx).Omit("Embedding").Save(&event).Error; e != nil {
			ilog.Warnf("save event %d error state: %v", eventID, e)
		}
		return fmt.Errorf("vision analysis: %w", err)
	}

	// 4. 填充结构化字段
	event.Summary = analysis.Summary
	// 事件视频内区间：清洗非法值（负值/区间倒挂），保证搜索跳转锚点可靠
	event.EventStartSec = analysis.EventStartSec
	event.EventEndSec = analysis.EventEndSec
	if event.EventStartSec < 0 {
		event.EventStartSec = 0
	}
	if event.EventEndSec <= event.EventStartSec {
		event.EventEndSec = 0
	}
	event.HasPerson = analysis.HasPerson
	event.PersonCount = analysis.PersonCount
	event.PersonDesc = analysis.PersonDesc
	event.HasVehicle = analysis.HasVehicle
	event.VehicleType = analysis.VehicleType
	event.VehicleDesc = analysis.VehicleDesc
	event.HasPet = analysis.HasPet
	event.PetType = analysis.PetType
	event.PetDesc = analysis.PetDesc
	event.HasPackage = analysis.HasPackage
	event.PackageDesc = analysis.PackageDesc
	event.Action = analysis.Action
	event.ActionDesc = analysis.ActionDesc
	event.DominantColors = strings.Join(analysis.DominantColors, ",")
	event.ColorDesc = analysis.ColorDesc
	event.Zone = analysis.Zone

	jsonData, _ := json.Marshal(analysis)
	event.JSONData = string(jsonData)

	// 5. 向量化摘要
	embeddings, err := s.embedding.Embed(ctx, []string{event.Summary}, embedMcfg)
	hasEmbedding := err == nil && len(embeddings) > 0
	if err != nil {
		ilog.Warnf("embed event %d: %v", eventID, err)
	} else if hasEmbedding {
		event.Embedding = pgvector.NewVector(toFloat32(embeddings[0]))
		event.TokenCount = len(embeddings[0])
	}

	// 6. 标记完成。向量未生成时排除 Embedding 列（空向量写库会报
	// "vector must have at least 1 dimension"），保持 NULL。
	event.Processed = true
	event.ProcessError = ""
	db := s.store.DB().WithContext(ctx)
	if !hasEmbedding {
		db = db.Omit("Embedding")
	}
	if err := db.Save(&event).Error; err != nil {
		return fmt.Errorf("save event: %w", err)
	}

	ilog.Infof("camera event %d processed: person=%v vehicle=%v package=%v action=%s",
		eventID, event.HasPerson, event.HasVehicle, event.HasPackage, event.Action)
	return nil
}

// analyzeWithVisionModel 调用视觉模型分析视频。
// 按供应商分两条路径，输出 JSON 结构一致：
//   - Gemini：整段视频 AnalyzeMedia（≤20MB inline_data，>20MB 内部走 Files API）
//   - 其它（Qwen-VL / SiliconFlow / 本地多模态网关等）：OpenAI 兼容接口对 video_url 支持参差
//     不齐（常直接忽略视频 part 导致「未提供视频内容」），统一 FFmpeg 抽 ≤12 帧缩放图 →
//     AnalyzeFrames 多图（image_url）一次请求，兼容性最好。
func (s *CameraEventService) analyzeWithVisionModel(ctx context.Context, videoPath string, mcfg *ModelConfig) (*model.GeminiVideoAnalysis, error) {
	if mcfg == nil || mcfg.apiKey() == "" {
		return nil, fmt.Errorf("请先在「系统设置 → 模型配置」中配置并激活一个视觉模型")
	}

	// 分析提示词：优先取「提示词库」配置（camera-vision-analysis），未配置时回退内置默认
	basePrompt := s.store.GetEnabledPromptByType(ctx, model.PromptTypeCameraVision)
	if basePrompt == "" {
		basePrompt = visualAnalysisBasePrompt
	}

	// 视频总时长：注入 prompt 让模型输出的秒数与真实时间轴对齐
	var duration float64
	if info, err := s.ffmpeg.GetVideoInfo(ctx, videoPath); err == nil && info.Duration > 0 {
		duration = info.Duration
	}

	var content string
	var err error
	if mcfg.isGemini() {
		var videoData []byte
		videoData, err = os.ReadFile(videoPath)
		if err == nil {
			// Gemini 整段视频自带时间轴，只需告知总时长
			content, err = s.chat.AnalyzeMedia(ctx, buildVisualAnalysisPrompt(basePrompt, duration, ""), "video/mp4", videoData, mcfg)
		}
	} else {
		var frames []analysisFrame
		frames, err = s.extractAnalysisFrames(ctx, videoPath)
		if err == nil {
			images := make([][]byte, 0, len(frames))
			timeline := make([]string, 0, len(frames))
			for _, f := range frames {
				images = append(images, f.Image)
				timeline = append(timeline, fmt.Sprintf("%.0f", f.Time))
			}
			content, err = s.chat.AnalyzeFrames(ctx, buildVisualAnalysisPrompt(basePrompt, duration, strings.Join(timeline, "、")), images, mcfg)
		}
	}
	if err != nil {
		return nil, err
	}
	content = extractJSON(content)

	var analysis model.GeminiVideoAnalysis
	if err := json.Unmarshal([]byte(content), &analysis); err != nil {
		ilog.Warnf("parse vision json: %v, raw: %s", err, content)
		analysis = model.GeminiVideoAnalysis{Summary: content, DominantColors: []string{}}
	}

	return &analysis, nil
}

// analysisFrame 抽帧结果：帧图像 + 帧在视频内的时间（秒），作为摘要时间锚点。
type analysisFrame struct {
	Time  float64
	Image []byte
}

// extractAnalysisFrames 大视频抽帧：按时长均匀抽 ≤12 帧并缩放到 640 宽。
// 返回带视频内秒数的帧列表（第 i 帧时间与 ffmpeg fps=1/interval 抽帧逻辑对齐）。
func (s *CameraEventService) extractAnalysisFrames(ctx context.Context, videoPath string) ([]analysisFrame, error) {
	dir, err := os.MkdirTemp("", "camera_frames_*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(dir)

	// 与 ExtractFramesLimited 内部一致：interval = duration/maxFrames（下限 1s）
	interval := 1.0
	if info, err := s.ffmpeg.GetVideoInfo(ctx, videoPath); err == nil && info.Duration > 0 {
		interval = info.Duration / float64(maxAnalysisFrames)
		if interval < 1 {
			interval = 1
		}
	}

	pattern := filepath.Join(dir, "frame_%04d.jpg")
	n, err := s.ffmpeg.ExtractFramesLimited(ctx, videoPath, pattern, maxAnalysisFrames, analysisFrameWidth)
	if err != nil {
		return nil, fmt.Errorf("抽帧失败: %w", err)
	}
	if n == 0 {
		return nil, fmt.Errorf("抽帧失败: 未生成任何帧")
	}

	frames := make([]analysisFrame, 0, n)
	for i := 0; i < n; i++ {
		img, rerr := os.ReadFile(s.ffmpeg.GetFramePath(dir, i))
		if rerr != nil {
			continue
		}
		frames = append(frames, analysisFrame{
			Time:  float64(i+1) * interval, // 第 i 帧 ≈ (i+1)*interval 秒处
			Image: img,
		})
	}
	if len(frames) == 0 {
		return nil, fmt.Errorf("抽帧失败: 帧文件不可读")
	}
	return frames, nil
}