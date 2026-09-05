package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"
)

// ChatService 对话服务（基于 cloudwego/eino 的 ChatModel 抽象，支持多模型动态切换）。
type ChatService struct {
	Client *http.Client

	// einoChatModels 按「apiKey|baseURL|modelName」缓存已构造的 eino ChatModel，避免每次请求重建客户端。
	mu             sync.Mutex
	einoChatModels map[string]*openai.ChatModel

	// store 运行时注入，用于写调用观测日志（CallLog/llm）
	store *store.Store
}

// NewChatService 创建对话服务。
func NewChatService() *ChatService {
	return &ChatService{
		Client:         &http.Client{Timeout: 120 * time.Second},
		einoChatModels: make(map[string]*openai.ChatModel),
	}
}

// SetStore 注入数据仓库。所有经 ChatService 的模型调用都会落一条观测记录，
// 这样会话标题生成、记忆摘要、视频分析等辅助调用也能被统计到成本里。
func (s *ChatService) SetStore(st *store.Store) {
	s.store = st
}

// chatModelCacheKey 生成 eino ChatModel 的缓存键。
func (s *ChatService) chatModelCacheKey(mcfg *ModelConfig) string {
	return strings.Join([]string{mcfg.apiKey(), mcfg.baseURL(), mcfg.modelName()}, "|")
}

// buildChatModel 基于运行时模型配置构造（或取缓存的）eino OpenAI 兼容 ChatModel。
// eino 的 openai 组件支持自定义 BaseURL，因此可同时兼容 OpenAI / 阿里云 DashScope / 自建兼容网关。
func (s *ChatService) buildChatModel(ctx context.Context, mcfg *ModelConfig) (*openai.ChatModel, error) {
	if mcfg == nil || mcfg.apiKey() == "" {
		return nil, fmt.Errorf("请先在「系统设置 → 模型配置」中配置并激活一个对话模型")
	}
	key := s.chatModelCacheKey(mcfg)
	s.mu.Lock()
	if cm, ok := s.einoChatModels[key]; ok {
		s.mu.Unlock()
		return cm, nil
	}
	s.mu.Unlock()

	temp := float32(mcfg.temperature())
	maxTokens := mcfg.maxTokens()
	cm, err := openai.NewChatModel(ctx, &openai.ChatModelConfig{
		APIKey:      mcfg.apiKey(),
		BaseURL:     mcfg.baseURL(),
		Model:       mcfg.modelName(),
		Temperature: &temp,
		MaxTokens:   &maxTokens,
		HTTPClient:  s.Client,
	})
	if err != nil {
		return nil, fmt.Errorf("构造 eino ChatModel 失败: %w", err)
	}
	s.mu.Lock()
	s.einoChatModels[key] = cm
	s.mu.Unlock()
	return cm, nil
}

// BuildToolCallingModel 为 Eino ADK Builder 提供与具体厂商解耦的 ToolCallingChatModel。
func (s *ChatService) BuildToolCallingModel(ctx context.Context, mcfg *ModelConfig) (einomodel.ToolCallingChatModel, error) {
	return s.buildChatModel(ctx, mcfg)
}

// toEinoMessages 把业务层 ChatMessage 列表转换为 eino schema.Message 列表。
func toEinoMessages(messages []ChatMessage) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, m := range messages {
		switch m.Role {
		case "system":
			out = append(out, schema.SystemMessage(m.Content))
		case "assistant":
			out = append(out, schema.AssistantMessage(m.Content, nil))
		default:
			out = append(out, schema.UserMessage(m.Content))
		}
	}
	return out
}

// ChatMessage 对话消息。
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ModelConfig 运行时模型配置（从数据库读取）。
type ModelConfig struct {
	BaseURL     string
	APIKey      string
	ModelName   string
	MaxTokens   int
	Temperature float64
	Provider    string // 厂商标识，用于区分请求格式（google=Gemini 原生 API）
}

// isGemini 判断该模型是否为 Google Gemini（原生 API 格式，与 OpenAI 兼容层不同）。
// 判据：provider 含 google/gemini，或 BaseURL 指向 generativelanguage.googleapis.com。
func (m *ModelConfig) isGemini() bool {
	if m == nil {
		return false
	}
	p := strings.ToLower(m.Provider)
	if strings.Contains(p, "google") || strings.Contains(p, "gemini") {
		return true
	}
	return strings.Contains(strings.ToLower(m.baseURL()), "generativelanguage")
}

// DefaultModelConfig 默认模型配置（从 config.yaml 读取）。
func DefaultModelConfig(apiKey, baseURL, chatModel string) *ModelConfig {
	return &ModelConfig{
		BaseURL:     baseURL,
		APIKey:      apiKey,
		ModelName:   chatModel,
		MaxTokens:   4096,
		Temperature: 0.7,
	}
}

// modelConfig 给业务方法用，统一取默认值
func (m *ModelConfig) baseURL() string {
	if m == nil || m.BaseURL == "" {
		return ""
	}
	return strings.TrimRight(m.BaseURL, "/")
}

func (m *ModelConfig) apiKey() string {
	if m == nil {
		return ""
	}
	return m.APIKey
}

func (m *ModelConfig) modelName() string {
	if m == nil || m.ModelName == "" {
		return "qwen-plus"
	}
	return m.ModelName
}

func (m *ModelConfig) maxTokens() int {
	if m == nil || m.MaxTokens <= 0 {
		return 4096
	}
	return m.MaxTokens
}

func (m *ModelConfig) temperature() float64 {
	if m == nil || m.Temperature <= 0 {
		return 0.7
	}
	return m.Temperature
}

// Chat 同步对话（非流式）。内部使用 eino ChatModel.Generate。
func (s *ChatService) Chat(ctx context.Context, messages []ChatMessage, mcfg *ModelConfig) (string, error) {
	cm, err := s.buildChatModel(ctx, mcfg)
	if err != nil {
		return "", err
	}

	start := time.Now()
	resp, err := cm.Generate(ctx, toEinoMessages(messages))
	s.writeChatLog(ctx, mcfg, resp, time.Since(start).Milliseconds(), err)
	if err != nil {
		return "", fmt.Errorf("eino chat generate: %w", err)
	}
	if resp == nil || resp.Content == "" {
		return "", fmt.Errorf("empty response from chat model")
	}
	return resp.Content, nil
}

// writeChatLog 异步写入一次对话调用观测记录（调用明细 + 成本估算）。
//
// 这里刻意放在 ChatService 层：所有模型调用最终都汇聚到本方法，
// 一次性覆盖主链路之外的辅助调用（会话标题生成、记忆摘要、视频分析等），
// 避免每个调用点各自记得遗漏。Eino Agent 主链路不走这里，由 Runtime 的用量回执单独记录。
func (s *ChatService) writeChatLog(ctx context.Context, mcfg *ModelConfig, resp *schema.Message, latencyMs int64, callErr error) {
	if s.store == nil {
		return
	}
	promptTokens, outputTokens := 0, 0
	if resp != nil && resp.ResponseMeta != nil && resp.ResponseMeta.Usage != nil {
		promptTokens = resp.ResponseMeta.Usage.PromptTokens
		outputTokens = resp.ResponseMeta.Usage.CompletionTokens
	}
	// 部分兼容网关不返回 usage：按字符估算，保证成本不会恒为 0
	if promptTokens <= 0 && outputTokens <= 0 && resp != nil && callErr == nil {
		outputTokens = int(float64(len([]rune(resp.Content))) * 1.3)
	}

	// 调用链上有可信作用域时带上 AgentID；后台任务（记忆摘要）没有作用域，记 0
	agentID := int64(0)
	if scope, scopeErr := agentscope.RequireScope(ctx); scopeErr == nil {
		agentID = scope.AgentID
	}
	// 只有显式声明为主链路的调用才算 llm，其余（标题生成 / 记忆 / 分析）归到 llm_aux，
	// 这样观测页默认只看主链路，辅助记录不会淹没列表。
	callType := model.CallTypeLLMAux
	if agentscope.CallPurposeFrom(ctx) == agentscope.CallPurposeAgent {
		callType = model.CallTypeLLM
	}

	status := 1
	errMsg := ""
	if callErr != nil {
		status = 0
		errMsg = callErr.Error()
		if len(errMsg) > 500 {
			errMsg = errMsg[:500]
		}
	}
	modelName := ""
	if mcfg != nil {
		modelName = mcfg.modelName()
	}
	traceID := tracex.TraceIDFromContext(ctx)

	go func() {
		// 落库用独立 context：调用方 ctx 可能已被取消（stop / 断连）
		bg := context.Background()
		modelID := int64(0)
		if m, e := s.store.GetActiveModelConfig(bg, model.ModelTypeChat); e == nil {
			modelID = m.ID
			if modelName == "" {
				modelName = m.ModelName
			}
		}
		if err := s.store.RecordCallLog(bg, &model.CallLog{
			AgentID:      agentID,
			CallType:     callType,
			ModelID:      modelID,
			ModelName:    modelName,
			PromptTokens: promptTokens,
			OutputTokens: outputTokens,
			TotalTokens:  promptTokens + outputTokens,
			CostCents:    s.store.EstimateCostCents(bg, modelID, int64(promptTokens), int64(outputTokens)),
			LatencyMs:    latencyMs,
			Status:       status,
			ErrorMsg:     errMsg,
			TraceID:      traceID,
			CreatedAt:    time.Now(),
		}); err != nil {
			ilog.Warnf("write chat call log: %v", err)
		}
	}()
}

// StreamChat 流式对话（基于 eino ChatModel.Stream）。
func (s *ChatService) StreamChat(ctx context.Context, messages []ChatMessage, mcfg *ModelConfig) (<-chan string, <-chan error) {
	textCh := make(chan string, 100)
	errCh := make(chan error, 1)

	go func() {
		defer close(textCh)
		defer close(errCh)

		cm, err := s.buildChatModel(ctx, mcfg)
		if err != nil {
			errCh <- err
			return
		}

		streamReader, err := cm.Stream(ctx, toEinoMessages(messages))
		if err != nil {
			ilog.Errorf("eino chat stream: %v", err)
			errCh <- fmt.Errorf("eino chat stream: %w", err)
			return
		}
		defer streamReader.Close()

		ilog.Info("stream started, reading chunks...")
		chunkCount := 0
		for {
			chunk, err := streamReader.Recv()
			if err != nil {
				// eino 流结束以 io.EOF 标识（Recv 返回 nil chunk）；其余错误记录日志后退出。
				if err != io.EOF {
					ilog.Errorf("stream recv: %v", err)
				}
				break
			}
			if chunk != nil && chunk.Content != "" {
				chunkCount++
				textCh <- chunk.Content
			}
		}
		ilog.Infof("stream ended: %d chunks total", chunkCount)
	}()

	return textCh, errCh
}

// BuildRAGPrompt 构建 RAG 提示词。
func BuildRAGPrompt(question string, sources []model.SearchResult, history []*model.ChatMessage) []ChatMessage {
	var messages []ChatMessage

	// 系统提示
	messages = append(messages, ChatMessage{
		Role: "system",
		Content: `你是一个智能监控视频分析助手。基于提供的摄像头事件和视频场景片段回答用户问题。

你有以下数据来源：
- 📷 摄像头事件：包含人物、车辆、宠物、包裹、动作、颜色等结构化标签
- 🎬 视频场景：视频抽帧的场景描述

回答规则：
1. **优先引用搜索结果中的具体事件**，包括时间、地点、人物描述
2. 如果搜索到了相关事件，直接描述事件内容（如"前门摄像头在8月26日记录到一位穿红色外套的男性在门口取包裹"）
3. 如果搜索结果为空，告知用户未找到，并建议调整搜索条件
4. 用清晰的结构化方式呈现，关键信息用**加粗**标注
5. 不要编造搜索结果中不存在的内容`,
	})

	// 检索上下文
	if len(sources) > 0 {
		var ctx strings.Builder
		ctx.WriteString("相关视频场景片段：\n\n")
		for i, src := range sources {
			ctx.WriteString(fmt.Sprintf("【片段%d】来源: %s, 相似度: %.0f%%\n%s\n\n",
				i+1, src.FileName, src.Score*100, src.Content))
		}
		messages = append(messages, ChatMessage{
			Role:    "system",
			Content: ctx.String(),
		})
	}

	// 历史消息
	for _, msg := range history {
		messages = append(messages, ChatMessage{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	// 附加当前问题
	messages = append(messages, ChatMessage{
		Role:    "user",
		Content: question,
	})

	return messages
}

// BuildTitlePrompt 构建标题生成提示词。
func BuildTitlePrompt(content string) []ChatMessage {
	return []ChatMessage{
		{
			Role:    "system",
			Content: "你是一个标题生成助手。根据用户的第一条消息，生成一个简短的对话标题（不超过15个字）。只输出标题文字，不要输出其他内容。",
		},
		{
			Role:    "user",
			Content: content,
		},
	}
}
