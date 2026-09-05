package service

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"aiagent/pkg/ilog"
)

// FFmpegService FFmpeg 封装服务。
type FFmpegService struct {
	ffmpegPath  string
	ffprobePath string
}

// NewFFmpegService 创建 FFmpeg 服务。
func NewFFmpegService() *FFmpegService {
	ffmpeg, _ := exec.LookPath("ffmpeg")
	ffprobe, _ := exec.LookPath("ffprobe")
	if ffmpeg == "" {
		ffmpeg = "ffmpeg"
	}
	if ffprobe == "" {
		ffprobe = "ffprobe"
	}
	return &FFmpegService{ffmpegPath: ffmpeg, ffprobePath: ffprobe}
}

// newCmd 构造 exec 命令，并剔除 LD_LIBRARY_PATH 中 IDE/运行时自带的库目录。
// 背景：CodeBuddy 服务器自带 lib/libstdc++.so.6（GLIBCXX 仅到 3.4.28），其 bin 目录会
// 被写入 shell 的 LD_LIBRARY_PATH；后端 fork 的 ffmpeg/ffprobe 继承后加载到旧版 libstdc++，
// 报 version `GLIBCXX_3.4.30' not found 而无法启动。
func (f *FFmpegService) newCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = sanitizeEnv()
	return cmd
}

// sanitizeEnv 过滤环境变量里 LD_LIBRARY_PATH 指向运行时自带目录（路径含 .codebuddy）的项。
func sanitizeEnv() []string {
	env := os.Environ()
	cleaned := make([]string, 0, len(env))
	for _, kv := range env {
		key, val, ok := strings.Cut(kv, "=")
		if ok && strings.EqualFold(key, "LD_LIBRARY_PATH") {
			kept := make([]string, 0, 4)
			for _, p := range strings.Split(val, ":") {
				if p != "" && !strings.Contains(p, ".codebuddy") {
					kept = append(kept, p)
				}
			}
			if len(kept) == 0 {
				continue
			}
			kv = key + "=" + strings.Join(kept, ":")
		}
		cleaned = append(cleaned, kv)
	}
	return cleaned
}

// Available 检查 FFmpeg 是否可用。
func (f *FFmpegService) Available() bool {
	_, err := exec.LookPath(f.ffmpegPath)
	return err == nil
}

// VideoInfo 视频信息
type VideoInfo struct {
	Duration   float64 `json:"duration"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	FPS        float64 `json:"fps"`
	Codec      string  `json:"codec"`
	HasAudio   bool    `json:"hasAudio"`
	AudioCodec string  `json:"audioCodec"`
	Size       int64   `json:"size"`
	Format     string  `json:"format"`
}

// ffprobe 输出结构
type ffprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
		RFrameRate string `json:"r_frame_rate"`
		Duration  string `json:"duration"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
		Size     string `json:"size"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

// GetVideoInfo 获取视频信息。
func (f *FFmpegService) GetVideoInfo(ctx context.Context, videoPath string) (*VideoInfo, error) {
	cmd := f.newCmd(ctx, f.ffprobePath,
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		videoPath,
	)

	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe: %w", err)
	}

	var probe ffprobeOutput
	if err := json.Unmarshal(output, &probe); err != nil {
		return nil, fmt.Errorf("parse ffprobe output: %w", err)
	}

	info := &VideoInfo{}

	// 从 format 取时长
	if probe.Format.Duration != "" {
		info.Duration, _ = strconv.ParseFloat(probe.Format.Duration, 64)
	}
	if probe.Format.Size != "" {
		size, _ := strconv.ParseInt(probe.Format.Size, 10, 64)
		info.Size = size
	}
	info.Format = probe.Format.FormatName

	// 从 streams 取视频流信息
	for _, s := range probe.Streams {
		if s.CodecType == "video" {
			info.Width = s.Width
			info.Height = s.Height
			info.Codec = s.CodecName
			info.FPS = parseFPS(s.RFrameRate)
			if info.Duration == 0 && s.Duration != "" {
				info.Duration, _ = strconv.ParseFloat(s.Duration, 64)
			}
		} else if s.CodecType == "audio" {
			info.HasAudio = true
			info.AudioCodec = s.CodecName
		}
	}

	return info, nil
}

// parseFPS 解析帧率（如 "30000/1001"）。
func parseFPS(fps string) float64 {
	if fps == "" || fps == "0/0" {
		return 0
	}
	parts := strings.Split(fps, "/")
	if len(parts) == 2 {
		num, _ := strconv.ParseFloat(parts[0], 64)
		den, _ := strconv.ParseFloat(parts[1], 64)
		if den > 0 {
			return num / den
		}
	}
	f, _ := strconv.ParseFloat(fps, 64)
	return f
}

// ExtractAudio 提取音频为 WAV 格式（16kHz 单声道，便于 Whisper 处理）。
func (f *FFmpegService) ExtractAudio(ctx context.Context, videoPath, outputPath string) error {
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	cmd := f.newCmd(ctx, f.ffmpegPath,
		"-y",
		"-i", videoPath,
		"-vn",
		"-acodec", "pcm_s16le",
		"-ar", "16000",
		"-ac", "1",
		outputPath,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract audio: %w: %s", err, string(out))
	}

	ilog.Infof("audio extracted: %s", outputPath)
	return nil
}

// ExtractFrames 按间隔抽取关键帧。
// interval: 抽帧间隔（秒）
// outputPattern: 输出文件路径模式，如 /path/to/frames/frame_%04d.jpg
func (f *FFmpegService) ExtractFrames(ctx context.Context, videoPath, outputPattern string, interval float64) (int, error) {
	os.MkdirAll(filepath.Dir(outputPattern), 0755)

	// 使用 fps 滤镜控制抽帧频率（1/interval = 每秒帧数）
	fps := fmt.Sprintf("fps=1/%.2f", interval)
	cmd := f.newCmd(ctx, f.ffmpegPath,
		"-y",
		"-i", videoPath,
		"-vf", fps,
		"-q:v", "2",
		outputPattern,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("extract frames: %w: %s", err, string(out))
	}

	// 统计生成的帧数
	dir := filepath.Dir(outputPattern)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") {
			count++
		}
	}

	ilog.Infof("extracted %d frames to %s", count, dir)
	return count, nil
}

// ExtractFramesLimited 按视频时长均匀抽取最多 maxFrames 帧，并缩放到指定宽度（-2 保持偶数）。
// 用于大视频分析：整段 base64 超限时，抽少量缩放帧供多模态模型一次请求分析。
func (f *FFmpegService) ExtractFramesLimited(ctx context.Context, videoPath, outputPattern string, maxFrames int, width int) (int, error) {
	os.MkdirAll(filepath.Dir(outputPattern), 0755)

	interval := 1.0
	if info, err := f.GetVideoInfo(ctx, videoPath); err == nil && info.Duration > 0 {
		interval = info.Duration / float64(maxFrames)
		if interval < 1 {
			interval = 1
		}
	}
	scale := fmt.Sprintf("fps=1/%.2f,scale=%d:-2", interval, width)
	cmd := f.newCmd(ctx, f.ffmpegPath,
		"-y",
		"-i", videoPath,
		"-vf", scale,
		"-q:v", "3",
		outputPattern,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return 0, fmt.Errorf("extract limited frames: %w: %s", err, string(out))
	}

	dir := filepath.Dir(outputPattern)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".jpg") {
			count++
		}
	}
	ilog.Infof("extracted %d limited frames to %s", count, dir)
	return count, nil
}

// ExtractFrameAtTime 抽取指定时间点的单帧。
func (f *FFmpegService) ExtractFrameAtTime(ctx context.Context, videoPath string, timestamp float64, outputPath string) error {
	os.MkdirAll(filepath.Dir(outputPath), 0755)

	timeStr := fmt.Sprintf("%.3f", timestamp)
	cmd := f.newCmd(ctx, f.ffmpegPath,
		"-y",
		"-ss", timeStr,
		"-i", videoPath,
		"-vframes", "1",
		"-q:v", "2",
		outputPath,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("extract frame at %.2fs: %w: %s", timestamp, err, string(out))
	}
	return nil
}

// GetFramePath 获取帧文件路径。
func (f *FFmpegService) GetFramePath(framesDir string, index int) string {
	return filepath.Join(framesDir, fmt.Sprintf("frame_%04d.jpg", index+1))
}

// FormatTime 格式化秒数为 mm:ss。
func FormatTime(seconds float64) string {
	mins := int(seconds) / 60
	secs := int(seconds) % 60
	return fmt.Sprintf("%02d:%02d", mins, secs)
}
