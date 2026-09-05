package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"aiagent/pkg/ilog"
)

// Gemini 原生 API 的 base64 inline_data 上限（20MB）。
// 超过此限制的视频/大图必须走 Files API：先上传拿到 file_uri，再在 generateContent 里引用。
const geminiInlineLimit = 20 * 1024 * 1024

// geminiAPIBase 规整 Gemini API 根地址：
//   - 用户可能在后台配 https://generativelanguage.googleapis.com 或带 /v1beta 的地址
//   - 统一补上 /v1beta
func geminiAPIBase(raw string) string {
	base := strings.TrimRight(raw, "/")
	if strings.Contains(base, "/v1beta") {
		return base
	}
	return base + "/v1beta"
}

// AnalyzeMedia 调用多模态模型分析图片或视频，返回模型的文本描述。
//
// 按供应商分支，请求格式差异对上层透明：
//   - Google Gemini（mcfg.isGemini()）：原生 generateContent；≤20MB 走 inline_data，大文件走 Files API
//   - 其它（Qwen-VL / 本地多模态网关等）：OpenAI 兼容 chat/completions，image_url / video_url base64
func (s *ChatService) AnalyzeMedia(ctx context.Context, prompt, mimeType string, data []byte, mcfg *ModelConfig) (string, error) {
	if mcfg == nil || mcfg.apiKey() == "" {
		return "", fmt.Errorf("请先在「系统设置 → 大模型配置」中配置并激活一个多模态（视觉）模型")
	}
	if len(data) == 0 {
		return "", fmt.Errorf("媒体数据为空")
	}
	if mcfg.isGemini() {
		return s.geminiAnalyze(ctx, prompt, mimeType, data, mcfg)
	}
	return s.openaiMultimodal(ctx, prompt, mimeType, data, mcfg)
}

// AnalyzeFrames 多帧图片一次请求分析（大视频抽帧兜底：整段 base64 超限时，
// 抽少量缩放帧打包进一次请求，供应商对上层透明）。
func (s *ChatService) AnalyzeFrames(ctx context.Context, prompt string, frames [][]byte, mcfg *ModelConfig) (string, error) {
	if mcfg == nil || mcfg.apiKey() == "" {
		return "", fmt.Errorf("请先在「系统设置 → 大模型配置」中配置并激活一个多模态（视觉）模型")
	}
	if len(frames) == 0 {
		return "", fmt.Errorf("帧数据为空")
	}
	if mcfg.isGemini() {
		return s.geminiAnalyzeFrames(ctx, prompt, frames, mcfg)
	}
	return s.openaiAnalyzeFrames(ctx, prompt, frames, mcfg)
}

// visionAPIError 构造视觉 API 错误。命中「模型不是视觉模型」（DashScope code 20041 /
// OpenAI 兼容 "not a VLM"）时附加配置指引，帮助用户定位到 VISION 模型配置。
func visionAPIError(modelName string, statusCode int, respBody []byte) error {
	msg := string(respBody)
	if strings.Contains(msg, "not a VLM") || strings.Contains(msg, "20041") || strings.Contains(msg, "does not support image") {
		return fmt.Errorf("当前模型（%s）不是视觉模型（VLM），不支持图片/视频输入。请在「系统设置 → 大模型配置」中新增并激活一个 VISION 类型模型（如 qwen-vl-max、qwen-vl-plus、gpt-4o、gemini-2.0-flash 等），摄像头/视频分析会自动优先使用该模型：%s",
			modelName, msg)
	}
	return fmt.Errorf("vision API error %d: %s", statusCode, msg)
}

// parseOpenAIResponse 解析 OpenAI 兼容 /chat/completions 响应，取首个 choice 的 content。
func parseOpenAIResponse(respBody []byte) (string, error) {
	var result struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("parse response: %w", err)
	}
	if len(result.Choices) == 0 {
		return "", fmt.Errorf("empty response")
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}

// parseGeminiResponse 解析 Gemini generateContent 响应，取首个候选首个 part 的 text。
func parseGeminiResponse(respBody []byte) (string, error) {
	var result struct {
		Candidates []struct {
			Content struct {
				Parts []struct {
					Text string `json:"text"`
				} `json:"parts"`
			} `json:"content"`
		} `json:"candidates"`
	}
	if err := json.Unmarshal(respBody, &result); err != nil {
		return "", fmt.Errorf("gemini parse response: %w", err)
	}
	if len(result.Candidates) == 0 || len(result.Candidates[0].Content.Parts) == 0 {
		return "", fmt.Errorf("gemini empty response")
	}
	return strings.TrimSpace(result.Candidates[0].Content.Parts[0].Text), nil
}

// ---------------- Google Gemini 原生 API ----------------

// geminiAnalyze Gemini 视频/图片理解。
func (s *ChatService) geminiAnalyze(ctx context.Context, prompt, mimeType string, data []byte, mcfg *ModelConfig) (string, error) {
	apiBase := geminiAPIBase(mcfg.baseURL())
	key := mcfg.apiKey()

	var part map[string]any
	if len(data) > geminiInlineLimit {
		// 大视频：Files API 上传 → 轮询 ACTIVE → file_data 引用 → 用后删除
		name, uri, err := s.geminiUploadAndWait(ctx, mcfg, mimeType, data)
		if err != nil {
			return "", err
		}
		part = map[string]any{
			"file_data": map[string]any{"mime_type": mimeType, "file_uri": uri},
		}
		defer func() {
			if err := s.geminiDeleteFile(ctx, mcfg, name); err != nil {
				ilog.Warnf("gemini delete file %s: %v", name, err)
			}
		}()
	} else {
		part = map[string]any{
			"inline_data": map[string]any{
				"mime_type": mimeType,
				"data":      base64.StdEncoding.EncodeToString(data),
			},
		}
	}

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": []map[string]any{{"text": prompt}, part}},
		},
		"generationConfig": map[string]any{
			"temperature":    0.3,
			"maxOutputTokens": 2048,
		},
	}
	jsonBody, _ := json.Marshal(body)
	url := apiBase + "/models/" + mcfg.modelName() + ":generateContent?key=" + key

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("gemini create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini API error %d: %s", resp.StatusCode, string(respBody))
	}
	return parseGeminiResponse(respBody)
}

// geminiUploadAndWait 上传文件到 Files API 并等待就绪，返回 (fileName, fileURI)。
func (s *ChatService) geminiUploadAndWait(ctx context.Context, mcfg *ModelConfig, mimeType string, data []byte) (string, string, error) {
	uploadBase := strings.Replace(geminiAPIBase(mcfg.baseURL()), "/v1beta", "/upload/v1beta", 1)
	key := mcfg.apiKey()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	meta, _ := json.Marshal(map[string]any{
		"file": map[string]any{"display_name": "media", "mime_type": mimeType},
	})
	if err := mw.WriteField("metadata", string(meta)); err != nil {
		return "", "", err
	}
	fw, err := mw.CreateFormFile("file", "media")
	if err != nil {
		return "", "", err
	}
	if _, err := fw.Write(data); err != nil {
		return "", "", err
	}
	if err := mw.Close(); err != nil {
		return "", "", err
	}

	url := uploadBase + "/files?key=" + key
	req, err := http.NewRequestWithContext(ctx, "POST", url, &buf)
	if err != nil {
		return "", "", fmt.Errorf("gemini upload create request: %w", err)
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("gemini upload: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", "", fmt.Errorf("gemini upload error %d: %s", resp.StatusCode, string(respBody))
	}

	var up struct {
		File struct {
			Name  string `json:"name"`
			State string `json:"state"`
			URI   string `json:"uri"`
		} `json:"file"`
	}
	if err := json.Unmarshal(respBody, &up); err != nil {
		return "", "", fmt.Errorf("gemini upload parse: %w", err)
	}
	if up.File.Name == "" {
		return "", "", fmt.Errorf("gemini upload: empty file name")
	}

	// 轮询直到 ACTIVE（大视频处理可能耗时数秒到数分钟）
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		if up.File.State == "ACTIVE" {
			return up.File.Name, up.File.URI, nil
		}
		if up.File.State == "FAILED" {
			return "", "", fmt.Errorf("gemini file %s state=FAILED", up.File.Name)
		}
		select {
		case <-ctx.Done():
			return "", "", ctx.Err()
		case <-time.After(2 * time.Second):
		}
		state, err := s.geminiFileState(ctx, mcfg, up.File.Name)
		if err != nil {
			return "", "", err
		}
		up.File.State = state
	}
	return "", "", fmt.Errorf("gemini file %s not ready within 5m", up.File.Name)
}

// geminiFileState 查询文件状态。
func (s *ChatService) geminiFileState(ctx context.Context, mcfg *ModelConfig, name string) (string, error) {
	url := geminiAPIBase(mcfg.baseURL()) + "/files/" + name + "?key=" + mcfg.apiKey()
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return "", err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini file state error %d: %s", resp.StatusCode, string(respBody))
	}
	var f struct {
		State string `json:"state"`
	}
	_ = json.Unmarshal(respBody, &f)
	return f.State, nil
}

// geminiDeleteFile 删除已上传的文件（释放云端存储）。
func (s *ChatService) geminiDeleteFile(ctx context.Context, mcfg *ModelConfig, name string) error {
	url := geminiAPIBase(mcfg.baseURL()) + "/files/" + name + "?key=" + mcfg.apiKey()
	req, err := http.NewRequestWithContext(ctx, "DELETE", url, nil)
	if err != nil {
		return err
	}
	resp, err := s.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	return nil
}

// ---------------- OpenAI 兼容 ----------------

// openaiMultimodal OpenAI 兼容多模态调用（Qwen-VL / 本地网关等）。
func (s *ChatService) openaiMultimodal(ctx context.Context, prompt, mimeType string, data []byte, mcfg *ModelConfig) (string, error) {
	b64 := base64.StdEncoding.EncodeToString(data)
	var media map[string]any
	if strings.HasPrefix(mimeType, "image/") {
		media = map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:" + mimeType + ";base64," + b64,
			},
		}
	} else {
		media = map[string]any{
			"type": "video_url",
			"video_url": map[string]any{
				"url": "data:" + mimeType + ";base64," + b64,
			},
		}
	}

	body := map[string]any{
		"model": mcfg.modelName(),
		"messages": []map[string]any{
			{
				"role": "user",
				"content": []map[string]any{
					{"type": "text", "text": prompt},
					media,
				},
			},
		},
		"max_tokens":  2048,
		"temperature": 0.3,
	}
	jsonBody, _ := json.Marshal(body)
	url := mcfg.baseURL() + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mcfg.apiKey())

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", visionAPIError(mcfg.modelName(), resp.StatusCode, respBody)
	}
	return parseOpenAIResponse(respBody)
}

// ---------------- 多帧分析（大视频抽帧兜底） ----------------

// geminiAnalyzeFrames 多帧图片打包进一次 Gemini 请求（帧为缩放小图，总量 < 20MB，inline 即可）。
func (s *ChatService) geminiAnalyzeFrames(ctx context.Context, prompt string, frames [][]byte, mcfg *ModelConfig) (string, error) {
	apiBase := geminiAPIBase(mcfg.baseURL())
	key := mcfg.apiKey()

	parts := []map[string]any{{"text": prompt}}
	for _, f := range frames {
		parts = append(parts, map[string]any{
			"inline_data": map[string]any{
				"mime_type": "image/jpeg",
				"data":      base64.StdEncoding.EncodeToString(f),
			},
		})
	}

	body := map[string]any{
		"contents": []map[string]any{
			{"parts": parts},
		},
		"generationConfig": map[string]any{
			"temperature":     0.3,
			"maxOutputTokens": 2048,
		},
	}
	jsonBody, _ := json.Marshal(body)
	url := apiBase + "/models/" + mcfg.modelName() + ":generateContent?key=" + key

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("gemini create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gemini request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("gemini API error %d: %s", resp.StatusCode, string(respBody))
	}
	return parseGeminiResponse(respBody)
}

// openaiAnalyzeFrames 多帧图片打包进一次 OpenAI 兼容请求（content 数组多个 image_url）。
func (s *ChatService) openaiAnalyzeFrames(ctx context.Context, prompt string, frames [][]byte, mcfg *ModelConfig) (string, error) {
	content := []map[string]any{{"type": "text", "text": prompt}}
	for _, f := range frames {
		content = append(content, map[string]any{
			"type": "image_url",
			"image_url": map[string]any{
				"url": "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(f),
			},
		})
	}

	body := map[string]any{
		"model": mcfg.modelName(),
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
		"max_tokens":  2048,
		"temperature": 0.3,
	}
	jsonBody, _ := json.Marshal(body)
	url := mcfg.baseURL() + "/chat/completions"

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(jsonBody))
	if err != nil {
		return "", fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+mcfg.apiKey())

	resp, err := s.Client.Do(req)
	if err != nil {
		return "", fmt.Errorf("vision request: %w", err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 200 {
		return "", visionAPIError(mcfg.modelName(), resp.StatusCode, respBody)
	}
	return parseOpenAIResponse(respBody)
}
