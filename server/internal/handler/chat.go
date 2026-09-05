package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/approval"
	memoryapi "aiagent/internal/memory"
	"aiagent/internal/middleware"
	"aiagent/internal/model"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/internal/toolkit"
	"aiagent/pkg/app/config"
	"aiagent/pkg/fsutil"
	"aiagent/pkg/ilog"
	"aiagent/pkg/tracex"

	"github.com/cloudwego/eino/schema"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var wsUpgrader = websocket.Upgrader{
	CheckOrigin:     func(r *http.Request) bool { return true },
	ReadBufferSize:  1024,
	WriteBufferSize: 4096,
}

// agentGracePeriod 是连接断开后允许 agent 继续跑完的宽限期。
//
// 断网、切走页面、写超时都会让连接退出，但那并不等于用户想中止生成。
// 若一断开就取消，本来能跑完的回复会被掐成半截，刷新后只剩残缺内容。
// 给一段时间让它跑完落库，用户刷新 / 重连后即可拿到完整结果；
// 用户主动点「停止」不受它影响，会立即取消。
const agentGracePeriod = 2 * time.Minute

// ChatHandler 对话接口。
type ChatHandler struct {
	store *store.Store
	svc   *service.Service
	// approvals 人工确认中心：Agent 执行副作用操作（如执行主机命令）时挂起等待用户在聊天框确认。
	approvals *approval.Broker
}

// NewChatHandler 创建对话 Handler。broker 为 nil 时，所有需审批的工具都会因为缺少交互通道被拒绝。
func NewChatHandler(s *store.Store, svc *service.Service, broker *approval.Broker) *ChatHandler {
	return &ChatHandler{store: s, svc: svc, approvals: broker}
}

// chatApprover 把工具层的人工确认请求桥接到当前聊天的事件流：
// 向前端推 approval_request，并阻塞等待用户在聊天框做出决策。
type chatApprover struct {
	broker    *approval.Broker
	emit      func(string, map[string]any)
	userID    int64
	agentID   int64
	sessionID int64
}

func (a chatApprover) RequestApproval(ctx context.Context, req toolkit.ApprovalRequest) (bool, bool, string, error) {
	res, err := a.broker.RequestApproval(ctx, approval.Request{
		SessionID: a.sessionID,
		AgentID:   a.agentID,
		UserID:    a.userID,
		ToolName:  req.ToolName,
		Summary:   req.Summary,
		Detail:    req.Detail,
		Risk:      req.Risk,
		Reason:    req.Reason,
	}, a.emit)
	if err != nil {
		return false, false, "", err
	}
	return res.Approved, res.Remember, res.Comment, nil
}

// RegisterRoute 注册路由。
func (h *ChatHandler) RegisterRoute(g *gin.RouterGroup) {
	group := g.Group("/chat")
	{
		group.POST("/stream", h.StreamChat)                 // 流式对话（SSE）
		group.GET("/ws", h.WSChat)                          // WebSocket 流式对话
		group.POST("/send", h.SendChat)                     // 非流式对话
		group.POST("/agent", h.AgentChat)                   // Agent 模式对话（多轮工具调用）
		group.GET("/sessions", h.ListSessions)              // 会话列表
		group.POST("/sessions", h.CreateSession)            // 创建会话
		group.DELETE("/sessions/:id", h.DeleteSession)      // 删除会话
		group.PUT("/sessions/:id/pin", h.TogglePin)         // 置顶/取消置顶
		group.GET("/sessions/:id/messages", h.ListMessages) // 消息列表
		// 导出助手消息里的 HTML 页面：落盘后重定向到免登录的静态文件地址
		group.GET("/messages/:id/export", h.ExportMessage)
		// 人工确认：Agent 执行危险操作时，用户在聊天框提交允许/拒绝
		group.POST("/approvals/:id/resolve", h.ResolveApproval)
	}
}

// RegisterPublicRoute 注册免登录路由：导出文件的静态下载地址。
// 这里刻意不挂鉴权中间件——导出的页面本来就是要直接下载 / 分享给别人的。
// 安全性由不可枚举的文件名保证（见 ExportStatic 的说明）。
func (h *ChatHandler) RegisterPublicRoute(g *gin.RouterGroup) {
	g.GET("/chat/exports/*filename", h.ExportStatic)
}

// recordChatCallLog 落一条对话调用观测（Eino 运行时专用）。
//
// 兼容性说明：兼容运行时走 service.AgentRuntime.writeLLMLog；
// Eino 运行时原本完全没有落库，平台内部对话因此在「调用观测」里查不到任何记录。
// 成功、失败、被中断都会记，观测页才能统计错误率。
func (h *ChatHandler) recordChatCallLog(reqCtx context.Context, agentID int64, mcfg *service.ModelConfig, usage agentscope.RunUsage, answerChars int) {
	if h.store == nil {
		return
	}
	// 执行上下文可能因 stop / 断连已取消，落库必须用独立 context
	ctx := context.Background()

	traceID := tracex.TraceIDFromContext(reqCtx)
	modelID := int64(0)
	modelName := ""
	if mcfg != nil {
		modelName = mcfg.ModelName
	}
	if m, e := h.store.GetActiveModelConfig(ctx, model.ModelTypeChat); e == nil {
		modelID = m.ID
		if modelName == "" {
			modelName = m.ModelName
		}
	}

	promptTokens, outputTokens := usage.PromptTokens, usage.OutputTokens
	// 模型不返回 usage 时按字符估算（与兼容运行时同口径），避免成本恒为 0
	if promptTokens <= 0 && outputTokens <= 0 && answerChars > 0 {
		outputTokens = int(float64(answerChars) * 1.3)
	}

	status, errMsg := 1, ""
	if usage.Err != nil {
		status = 0
		errMsg = truncateStr(usage.Err.Error(), 500)
	}

	go func() {
		if err := h.store.RecordCallLog(ctx, &model.CallLog{
			AgentID:      agentID,
			CallType:     model.CallTypeLLM,
			ModelID:      modelID,
			ModelName:    modelName,
			PromptTokens: promptTokens,
			OutputTokens: outputTokens,
			TotalTokens:  promptTokens + outputTokens,
			CostCents:    h.store.EstimateCostCents(ctx, modelID, int64(promptTokens), int64(outputTokens)),
			LatencyMs:    usage.LatencyMs,
			Status:       status,
			ErrorMsg:     errMsg,
			TraceID:      traceID,
			CreatedAt:    time.Now(),
		}); err != nil {
			ilog.Warnf("write chat call log: %v", err)
		}
	}()
}

// normalizeSessionScope 归一化会话作用域：只有带上合法 ID 的 host / host_group 才生效，
// 其余一律按全局会话处理，避免出现「声明了类型却没有目标」的半吊子作用域。
func normalizeSessionScope(scopeType string, scopeID int64) string {
	switch strings.TrimSpace(scopeType) {
	case model.SessionScopeHost, model.SessionScopeHostGroup:
		if scopeID > 0 {
			return scopeType
		}
	}
	return model.SessionScopeGlobal
}

func scopeIDOf(scopeType string, scopeID int64) int64 {
	if normalizeSessionScope(scopeType, scopeID) == model.SessionScopeGlobal {
		return 0
	}
	return scopeID
}

// describeSessionTarget 描述会话绑定的操作目标（运维工作台按主机 / 主机组开会话）。
// 返回空串表示不限制目标（全局会话或目标已失效）。
func (h *ChatHandler) describeSessionTarget(ctx context.Context, sessionID int64) string {
	if sessionID <= 0 {
		return ""
	}
	session, err := h.store.GetChatSession(ctx, sessionID)
	if err != nil || session == nil {
		return ""
	}
	switch session.ScopeType {
	case model.SessionScopeHost:
		host, err := h.store.GetHost(ctx, session.ScopeID)
		if err != nil || host == nil {
			// 主机被删掉了：明确告知，避免模型凭记忆去操作不存在的机器
			return fmt.Sprintf("当前会话原本绑定主机 ID=%d，但该主机已不存在。请先让用户重新选择主机。", session.ScopeID)
		}
		return fmt.Sprintf(
			"当前会话的操作目标是主机「%s」（host_id=%d，%s@%s:%d，系统 %s）。"+
				"用户没有指明主机时，默认对这台主机执行命令；不要操作其它主机。",
			host.Name, host.ID, host.Username, host.Hostname, host.Port, host.OS)
	case model.SessionScopeHostGroup:
		return fmt.Sprintf(
			"当前会话的操作目标是主机组「%s」（group_id=%d）。"+
				"用户没有指明主机时，先用 list_hosts 列出组内主机，再按需要选择具体主机操作；不要操作组外的主机。",
			session.ScopeName, session.ScopeID)
	default:
		return ""
	}
}

// ResolveApproval 提交用户对某次工具调用的确认结果。
// 请求体：{ approved: bool, remember: bool, comment: string }
// remember 为 true 表示本次会话内同类操作不再询问。
func (h *ChatHandler) ResolveApproval(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	log := ilog.Trace(ctx)
	uid, _, _ := middleware.CurrentUser(c)
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "审批 ID 不能为空"})
		return
	}
	var body struct {
		Approved bool   `json:"approved"`
		Remember bool   `json:"remember"`
		Comment  string `json:"comment"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if h.approvals == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": 5, "message": "审批服务未启用"})
		return
	}
	if err := h.approvals.Resolve(id, uid, approval.Result{
		Approved: body.Approved,
		Remember: body.Remember,
		Comment:  body.Comment,
	}); err != nil {
		log.Warnf("resolve approval %s failed: %v", id, err)
		c.JSON(http.StatusBadRequest, gin.H{"code": 4, "message": err.Error()})
		return
	}
	log.Infof("approval %s resolved by user=%d approved=%v remember=%v", id, uid, body.Approved, body.Remember)
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"id": id, "approved": body.Approved}})
}

// resolveModelConfig 从数据库获取激活的对话模型配置，失败则用 config.yaml 兜底。
func (h *ChatHandler) resolveModelConfig(ctx context.Context) *service.ModelConfig {
	// 优先从数据库获取激活的对话模型
	mcfg, err := h.store.GetActiveModelConfig(ctx, model.ModelTypeChat)
	if err == nil {
		ilog.Infof("using model from db: %s", mcfg.ModelName)
		return &service.ModelConfig{
			BaseURL:     mcfg.BaseURL,
			APIKey:      mcfg.APIKey,
			ModelName:   mcfg.ModelName,
			MaxTokens:   mcfg.MaxTokens,
			Temperature: mcfg.Temperature,
		}
	}
	// 兜底：用 config.yaml 的 Qwen 配置
	cfg := config.GetCurrentConfig()
	ilog.Infof("using model from config.yaml: %s (db has no active model)", cfg.Qwen.ChatModel)
	return service.DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.ChatModel)
}

func (h *ChatHandler) resolveAgentModelConfig(ctx context.Context, agentID int64, role string) *service.ModelConfig {
	if agentID <= 0 {
		return nil
	}
	bindings, err := h.store.ListAgentModels(ctx, agentID)
	if err != nil {
		return nil
	}
	candidates := make([]*model.AgentModel, 0)
	for _, binding := range bindings {
		if binding.Enabled && binding.Role == role {
			candidates = append(candidates, binding)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].IsPrimary != candidates[j].IsPrimary {
			return candidates[i].IsPrimary
		}
		return candidates[i].Priority < candidates[j].Priority
	})
	for _, binding := range candidates {
		if result := h.buildModelConfigFromBinding(ctx, binding); result != nil {
			ilog.Infof("using agent %d %s model: %s", agentID, role, result.ModelName)
			return result
		}
	}
	return nil
}

// resolveAgentModelConfigByID 取该智能体指定用途下某个具体模型的配置。
// 用于工作台下拉临时切换对话模型：只认已绑定到该智能体且启用的模型，避免传入任意 modelId。
func (h *ChatHandler) resolveAgentModelConfigByID(ctx context.Context, agentID, modelID int64, role string) *service.ModelConfig {
	if agentID <= 0 || modelID <= 0 {
		return nil
	}
	bindings, err := h.store.ListAgentModels(ctx, agentID)
	if err != nil {
		return nil
	}
	for _, binding := range bindings {
		if !binding.Enabled || binding.Role != role || binding.ModelID != modelID {
			continue
		}
		if result := h.buildModelConfigFromBinding(ctx, binding); result != nil {
			ilog.Infof("using agent %d %s model (explicit): %s", agentID, role, result.ModelName)
			return result
		}
		return nil
	}
	return nil
}

// buildModelConfigFromBinding 把一条模型绑定解析成可执行的模型配置（含参数覆写），不可用返回 nil。
func (h *ChatHandler) buildModelConfigFromBinding(ctx context.Context, binding *model.AgentModel) *service.ModelConfig {
	configured, err := h.store.GetModelConfig(ctx, binding.ModelID)
	if err != nil || configured.APIKey == "" {
		return nil
	}
	result := &service.ModelConfig{
		BaseURL: configured.BaseURL, APIKey: configured.APIKey, ModelName: configured.ModelName,
		MaxTokens: configured.MaxTokens, Temperature: configured.Temperature,
	}
	if binding.Params != "" {
		var overrides model.AgentModelParams
		if json.Unmarshal([]byte(binding.Params), &overrides) == nil {
			if overrides.MaxTokens != nil {
				result.MaxTokens = *overrides.MaxTokens
			}
			if overrides.Temperature != nil {
				result.Temperature = *overrides.Temperature
			}
		}
	}
	return result
}

// resolveEmbedModelConfig 从数据库获取激活的向量模型配置。
func (h *ChatHandler) resolveEmbedModelConfig(ctx context.Context) *service.ModelConfig {
	// 优先从数据库获取激活的向量模型
	mcfg, err := h.store.GetActiveModelConfig(ctx, model.ModelTypeEmbedding)
	if err == nil {
		return &service.ModelConfig{
			BaseURL:   mcfg.BaseURL,
			APIKey:    mcfg.APIKey,
			ModelName: mcfg.ModelName,
		}
	}
	// 没有 EMBEDDING 模型，复用 CHAT 模型的 API Key
	chatMcfg, err := h.store.GetActiveModelConfig(ctx, model.ModelTypeChat)
	if err == nil {
		ilog.Infof("no EMBEDDING model, reusing CHAT config for embedding")
		return &service.ModelConfig{
			BaseURL:   chatMcfg.BaseURL,
			APIKey:    chatMcfg.APIKey,
			ModelName: chatMcfg.ModelName, // 直接复用 CHAT 模型名
		}
	}
	// 兜底
	cfg := config.GetCurrentConfig()
	return service.DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.EmbedModel)
}

// newMemoryProvider 构造会话记忆 Provider。
// 绑定智能体时以智能体的 memoryEnabled 为准；未绑定智能体的普通对话默认开启。
func (h *ChatHandler) newMemoryProvider(ctx context.Context, agentID int64, mcfg, embedMcfg *service.ModelConfig) memoryapi.Provider {
	if h.svc.Memory == nil || mcfg == nil {
		return nil
	}
	memoryParams := ""
	if agentID > 0 {
		// 生效快照优先：会话记忆开关同样受发布约束，未发布的改动不应影响线上
		if snap := model.EffectiveSnapshotFromContext(ctx); snap != nil {
			if !snap.MemoryEnabled {
				return nil
			}
			memoryParams = snap.MemoryParams
		} else if agent, err := h.store.GetAgent(ctx, agentID); err == nil && agent != nil {
			if !agent.MemoryEnabled {
				return nil
			}
			// 带上智能体自己的记忆参数，未配置时为空串，服务端按全局默认处理
			memoryParams = agent.MemoryParams
		}
	}
	return service.NewMemoryProviderAdapter(h.svc.Memory, mcfg, embedMcfg, memoryParams)
}

// loadMemory 载入会话记忆（历史 + 运行时上下文），失败只记日志，不阻断对话。
func (h *ChatHandler) loadMemory(ctx context.Context, provider memoryapi.Provider, scope memoryapi.Scope, beforeMessageID int64, query string) ([]*model.ChatMessage, string) {
	if provider == nil {
		return nil, ""
	}
	loaded, err := provider.Retrieve(ctx, scope, memoryapi.RetrieveRequest{
		BeforeMessageID: beforeMessageID, Query: query, Limit: 12,
	})
	if err != nil {
		ilog.Warnf("load chat memory: %v", err)
		return nil, ""
	}
	history := make([]*model.ChatMessage, 0, len(loaded.History))
	for _, m := range loaded.History {
		history = append(history, &model.ChatMessage{Role: string(m.Role), Content: m.Content})
	}
	return history, loaded.RuntimeContext
}

// memorize 在助手回复落库后异步回写记忆。
func (h *ChatHandler) memorize(ctx context.Context, provider memoryapi.Provider, scope memoryapi.Scope, userMessage, assistantMessage string, assistantMessageID int64) {
	if provider == nil {
		return
	}
	provider.Memorize(ctx, scope, memoryapi.MemorizeRequest{
		UserMessage: userMessage, AssistantMessage: assistantMessage, AssistantMessageID: assistantMessageID,
	})
}

// withRuntimeMemory 把运行时记忆以不可信数据标签拼到问题上，与 Agent 模式保持一致，
// 避免摘要/长期记忆内容被模型当作指令执行。
func withRuntimeMemory(question, runtimeMemory string) string {
	if runtimeMemory == "" {
		return question
	}
	return question + "\n\n<runtime_memory trust=\"untrusted-data\">\n" + runtimeMemory + "\n</runtime_memory>"
}

// StreamChat 流式对话（SSE）。
func (h *ChatHandler) StreamChat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "消息不能为空"})
		return
	}

	ctx := tracex.FromRequest(c)
	uid, username, _ := middleware.CurrentUser(c)
	// 检索范围按已发布快照隔离：未发布的资源绑定变更不影响线上检索
	ctx = withEffectiveSnapshot(ctx, h.store, req.AgentID)

	// 自动创建会话
	sessionID := req.SessionID
	if sessionID == 0 {
		var agentName string
		if req.AgentID > 0 {
			if agent, err := h.store.GetAgent(ctx, req.AgentID); err == nil {
				agentName = agent.Name
			}
		}
		session := &model.ChatSession{
			Title:       truncateStr(req.Message, 50),
			AgentID:     req.AgentID,
			AgentName:   agentName,
			KnowledgeID: req.KnowledgeID,
			UserID:      uid,
			Username:    username,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := h.store.CreateChatSession(ctx, session); err != nil {
			ilog.Errorf("create session: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建会话失败"})
			return
		}
		sessionID = session.ID
	}

	// 保存用户消息
	userMsg := &model.ChatMessage{
		SessionID:   sessionID,
		Role:        model.RoleUser,
		Content:     req.Message,
		ContentType: "text",
		CreatedAt:   time.Now(),
	}
	h.store.CreateChatMessage(ctx, userMsg)

	// 设置 SSE 头
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	c.Writer.Flush()

	// 历史消息：开启记忆时以记忆服务返回的（含会话摘要）为准，否则回退最近 20 条
	mcfg := h.resolveModelConfig(ctx)
	embedMcfg := h.resolveEmbedModelConfig(ctx)
	memoryScope := memoryapi.Scope{UserID: uid, AgentID: req.AgentID, SessionID: sessionID}
	memoryProvider := h.newMemoryProvider(ctx, req.AgentID, mcfg, embedMcfg)
	history, runtimeMemory := h.loadMemory(ctx, memoryProvider, memoryScope, userMsg.ID, req.Message)
	if len(history) == 0 {
		history, _ = h.store.GetRecentMessages(ctx, sessionID, 20)
	}
	question := withRuntimeMemory(req.Message, runtimeMemory)

	// 1. 检索：文档 + 视频场景 + 摄像头事件
	var sources []model.SearchResult
	queryEmbedding, err := h.svc.Embedding.EmbedQuery(ctx, req.Message, embedMcfg)
	if err != nil {
		ilog.Warnf("embed query failed: %v", err)
	} else {
		// 文档库检索（kid=0 表示搜索全部知识库）
		kid := req.KnowledgeID
		if kid == 0 && sessionID > 0 {
			if session, err := h.store.GetChatSessionForUser(ctx, sessionID, uid); err == nil {
				kid = session.KnowledgeID
			}
		}
		docResults, _ := h.store.VectorSearch(ctx, queryEmbedding, kid, 3, 0.45)
		sources = append(sources, docResults...)

		// 视频场景检索（资源授权：解析绑定知识库集合，无绑定回退 agent_id）
		if req.AgentID > 0 {
			videoKbIDs, _ := h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeKnowledgeBase)
			videoResults, _ := h.store.VideoVectorSearch(ctx, queryEmbedding, req.AgentID, 0, videoKbIDs, 3, 0.45)
			for i := range videoResults {
				videoResults[i].FileName = "🎬 " + videoResults[i].FileName
			}
			sources = append(sources, videoResults...)
		}

		// 摄像头事件检索
		cameraResults, _ := h.store.CameraVectorSearch(ctx, queryEmbedding, req.AgentID, 3, 0.45)
		// 文本搜索兜底
		if len(cameraResults) < 3 {
			textResults, _ := h.store.CameraTextSearch(ctx, req.Message, req.AgentID, 0, 3)
			seen := make(map[int64]bool)
			for _, r := range cameraResults {
				seen[r.ChunkID] = true
			}
			for _, r := range textResults {
				if !seen[r.ChunkID] {
					cameraResults = append(cameraResults, r)
				}
			}
		}
		for i := range cameraResults {
			cameraResults[i].FileName = "📷 " + cameraResults[i].FileName
		}
		sources = append(sources, cameraResults...)

		ilog.Infof("search returned %d sources (doc+video+camera)", len(sources))
	}

	// 发送搜索来源事件
	if len(sources) > 0 {
		sendSSE(c, "search", gin.H{"sources": sources})
	}

	// 2. 构建 RAG 提示词
	messages := service.BuildRAGPrompt(question, sources, history)

	// 3. 流式调用
	textCh, errCh := h.svc.Chat.StreamChat(ctx, messages, mcfg)

	var fullResponse string
	done := false
	for !done {
		select {
		case text, ok := <-textCh:
			if !ok {
				done = true
				break
			}
			fullResponse += text
			sendSSE(c, "text", gin.H{"content": text})
		case err := <-errCh:
			if err != nil {
				ilog.Errorf("stream chat error: %v", err)
				sendSSE(c, "error", gin.H{"error": err.Error()})
			}
			done = true
		}
	}

	// 保存助手回复
	assistantMsg := &model.ChatMessage{
		SessionID:   sessionID,
		Role:        model.RoleAssistant,
		Content:     fullResponse,
		ContentType: "text",
		CreatedAt:   time.Now(),
	}
	if len(sources) > 0 {
		srcJSON, _ := json.Marshal(sources)
		assistantMsg.Sources = string(srcJSON)
	}
	h.store.CreateChatMessage(ctx, assistantMsg)
	h.memorize(ctx, memoryProvider, memoryScope, req.Message, fullResponse, assistantMsg.ID)

	// 如果是新会话，生成标题
	if req.SessionID == 0 {
		titleMsgs := service.BuildTitlePrompt(req.Message)
		if title, err := h.svc.Chat.Chat(ctx, titleMsgs, mcfg); err == nil {
			h.store.UpdateSessionTitle(ctx, sessionID, truncateStr(title, 50))
		}
	}

	// 发送完成事件
	sendSSE(c, "done", gin.H{"sessionId": sessionID})
}

// SendChat 非流式对话。
func (h *ChatHandler) SendChat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}

	ctx := tracex.FromRequest(c)
	uid, username, _ := middleware.CurrentUser(c)
	// 检索范围按已发布快照隔离：未发布的资源绑定变更不影响线上检索
	ctx = withEffectiveSnapshot(ctx, h.store, req.AgentID)

	// 获取或创建会话
	sessionID := req.SessionID
	if sessionID == 0 {
		session := &model.ChatSession{
			Title:       truncateStr(req.Message, 50),
			KnowledgeID: req.KnowledgeID,
			UserID:      uid,
			Username:    username,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := h.store.CreateChatSession(ctx, session); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建会话失败"})
			return
		}
		sessionID = session.ID
	}

	// 保存用户消息
	userMsg := &model.ChatMessage{
		SessionID: sessionID, Role: model.RoleUser, Content: req.Message,
		ContentType: "text", CreatedAt: time.Now(),
	}
	h.store.CreateChatMessage(ctx, userMsg)

	// 检索：文档 + 视频场景 + 摄像头事件
	mcfg := h.resolveModelConfig(ctx)
	embedMcfg := h.resolveEmbedModelConfig(ctx)
	memoryScope := memoryapi.Scope{UserID: uid, AgentID: req.AgentID, SessionID: sessionID}
	memoryProvider := h.newMemoryProvider(ctx, req.AgentID, mcfg, embedMcfg)
	history, runtimeMemory := h.loadMemory(ctx, memoryProvider, memoryScope, userMsg.ID, req.Message)
	if len(history) == 0 {
		history, _ = h.store.GetRecentMessages(ctx, sessionID, 20)
	}
	var sources []model.SearchResult
	queryEmbedding, err := h.svc.Embedding.EmbedQuery(ctx, req.Message, embedMcfg)
	if err == nil {
		kid := req.KnowledgeID
		if kid == 0 && sessionID > 0 {
			if session, _ := h.store.GetChatSessionForUser(ctx, sessionID, uid); session != nil {
				kid = session.KnowledgeID
			}
		}
		if kid > 0 {
			docResults, _ := h.store.VectorSearch(ctx, queryEmbedding, kid, 3, 0.45)
			sources = append(sources, docResults...)
		}
		if req.AgentID > 0 {
			videoKbIDs, _ := h.store.ListBoundResourceIDs(ctx, req.AgentID, model.ResourceTypeKnowledgeBase)
			videoResults, _ := h.store.VideoVectorSearch(ctx, queryEmbedding, req.AgentID, 0, videoKbIDs, 3, 0.45)
			sources = append(sources, videoResults...)
		}
		cameraResults, _ := h.store.CameraVectorSearch(ctx, queryEmbedding, req.AgentID, 3, 0.45)
		if len(cameraResults) < 3 {
			textResults, _ := h.store.CameraTextSearch(ctx, req.Message, req.AgentID, 0, 3)
			seen := make(map[int64]bool)
			for _, r := range cameraResults {
				seen[r.ChunkID] = true
			}
			for _, r := range textResults {
				if !seen[r.ChunkID] {
					cameraResults = append(cameraResults, r)
				}
			}
		}
		sources = append(sources, cameraResults...)
	}

	// 构建提示词并调用（主链路：标记为 agent 用途，辅助调用默认归 llm_aux）
	messages := service.BuildRAGPrompt(withRuntimeMemory(req.Message, runtimeMemory), sources, history)
	response, err := h.svc.Chat.Chat(agentscope.WithCallPurpose(ctx, agentscope.CallPurposeAgent), messages, mcfg)
	if err != nil {
		ilog.Errorf("chat error: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "对话失败: " + err.Error()})
		return
	}

	// 保存助手回复
	assistantMsg := &model.ChatMessage{
		SessionID: sessionID, Role: model.RoleAssistant, Content: response,
		ContentType: "text", CreatedAt: time.Now(),
	}
	if len(sources) > 0 {
		srcJSON, _ := json.Marshal(sources)
		assistantMsg.Sources = string(srcJSON)
	}
	h.store.CreateChatMessage(ctx, assistantMsg)
	h.memorize(ctx, memoryProvider, memoryScope, req.Message, response, assistantMsg.ID)

	// 生成标题
	if req.SessionID == 0 {
		titleMsgs := service.BuildTitlePrompt(req.Message)
		if title, err := h.svc.Chat.Chat(ctx, titleMsgs, mcfg); err == nil {
			h.store.UpdateSessionTitle(ctx, sessionID, truncateStr(title, 50))
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0, "sessionId": sessionID,
		"response": response, "sources": sources,
	})
}

// defaultAgentSystemPrompt Agent 模式的默认系统提示词（Run 内部会追加工具清单）。
const defaultAgentSystemPrompt = `你是一个智能监控分析助手，可以按需调用工具检索摄像头事件和视频片段。

要求：
1. 用户问到具体事件、人物、车辆、包裹等监控内容时，优先调用 search_camera / search_videos 检索真实数据
2. 基于工具返回的真实结果回答，不要编造结果中不存在的内容
3. 若检索无结果，如实告知并建议调整关键词或时间范围
4. 回答简洁清晰，关键时间/位置/特征用结构化方式呈现`

// agentRunRequest 一次 Agent 执行的入参。
// ModelID 为可选的临时模型：必须已绑定在该智能体的对应用途上，否则忽略并沿用默认模型。
type agentRunRequest struct {
	Message     string
	AgentID     int64
	SessionID   int64
	KnowledgeID int64
	ModelID     int64
	// ApprovalMode 会话权限模式（manual / delegated / full_access），空值按 manual 处理
	ApprovalMode string
	// 运维工作台：会话绑定到某台主机 / 某个主机组，首轮自动建会话时写入
	ScopeType string
	ScopeID   int64
	ScopeName string
}

// agentRunResult 一次 Agent 执行的结果。Interrupted 表示被 stop 指令中断，
// 此时 Answer 是已生成的部分内容，且不会回写记忆。
type agentRunResult struct {
	SessionID     int64
	UserMessageID int64
	// MessageID 助手回复落库后的消息 ID，前端据此走 /chat/messages/:id/export 下载接口
	MessageID     int64
	Answer        string
	Runtime       string
	MemoryEnabled bool
	ToolCalls     []map[string]string
	AgentState    *service.AgentState
	Interrupted   bool
	Err           error
	Status        int // 失败时对应的 HTTP 状态码，0 表示成功
}

// runAgentStream 执行一次 Agent 对话，中间过程通过 emit 推送（emit 为 nil 表示非流式调用）。
// ctx 被取消时立即停止：已产生的部分答案会落库，但按中断处理，不回写记忆。
func (h *ChatHandler) runAgentStream(ctx context.Context, uid int64, username string, isAdmin bool, req agentRunRequest, emit func(string, map[string]any)) agentRunResult {
	result := agentRunResult{Status: http.StatusInternalServerError}
	notify := func(typ string, data map[string]any) {
		if emit != nil {
			emit(typ, data)
		}
	}

	// 获取或创建会话。已有会话必须同时匹配当前用户与 Agent，防止跨用户/跨 Agent 读取记忆。
	sessionID := req.SessionID
	if sessionID == 0 {
		session := &model.ChatSession{
			Title:       truncateStr(req.Message, 50),
			AgentID:     req.AgentID,
			KnowledgeID: req.KnowledgeID,
			UserID:      uid,
			Username:    username,
			// 运维工作台：会话绑定到选中的主机 / 主机组，之后按同一对象续聊
			ScopeType:   normalizeSessionScope(req.ScopeType, req.ScopeID),
			ScopeID:     scopeIDOf(req.ScopeType, req.ScopeID),
			ScopeName:   req.ScopeName,
			CreatedAt:   time.Now(),
			UpdatedAt:   time.Now(),
		}
		if err := h.store.CreateChatSession(ctx, session); err != nil {
			result.Err = fmt.Errorf("创建会话失败: %w", err)
			return result
		}
		sessionID = session.ID
		// 新会话一落库就把 ID 推给前端：左侧会话列表立即出现该条目并高亮，
		// 不必等本轮生成结束（done 事件）才刷新，避免「发完消息列表迟迟不变」。
		notify("session", map[string]any{
			"sessionId": sessionID,
			"title":     session.Title,
		})
	} else if _, err := h.store.GetScopedChatSession(ctx, store.MemoryScope{UserID: uid, AgentID: req.AgentID, SessionID: sessionID}); err != nil {
		result.Err = fmt.Errorf("无权访问该会话或会话不属于当前智能体")
		result.Status = http.StatusForbidden
		return result
	}
	result.SessionID = sessionID

	// 会话作用域以库里的会话记录为准（请求参数只用于首轮建会话），避免前端伪造作用域越权。
	sessionScopeType, sessionScopeID := normalizeSessionScope(req.ScopeType, req.ScopeID), scopeIDOf(req.ScopeType, req.ScopeID)
	if session, err := h.store.GetChatSession(ctx, sessionID); err == nil && session != nil {
		sessionScopeType, sessionScopeID = session.ScopeType, session.ScopeID
	}

	// 运维会话绑定了主机 / 主机组时，把操作目标说明注入本轮消息，
	// 这样模型知道这轮对话针对哪台机器，用户省略「在哪台机器上」也能正确执行。
	userMessage := req.Message
	if target := h.describeSessionTarget(ctx, sessionID); target != "" {
		userMessage += "\n\n<session_target trust=\"system\">\n" + target + "\n</session_target>"
	}

	// 保存用户消息并保留 ID；记忆检索以该 ID 为 before 游标，避免当前问题重复注入。
	userMsg := &model.ChatMessage{
		SessionID: sessionID, Role: model.RoleUser, Content: req.Message,
		ContentType: "text", CreatedAt: time.Now(),
	}
	if err := h.store.CreateChatMessage(ctx, userMsg); err != nil {
		result.Err = fmt.Errorf("保存消息失败: %w", err)
		return result
	}
	result.UserMessageID = userMsg.ID

	// 可信作用域仅由认证用户、已校验会话和服务端 AgentID 构造，模型/工具参数不能覆盖。
	// 交互式会话（有事件流）不预授权：副作用工具必须经用户在聊天框确认；
	// 非交互式调用（外部 API，emit 为 nil）没有交互通道，沿用服务端授权。
	interactive := emit != nil
	ctx = agentscope.WithScope(ctx, agentscope.Scope{
		UserID: uid, AgentID: req.AgentID, SessionID: sessionID,
		ReadOnly: false, CanApprove: !interactive, IsAdmin: isAdmin,
		Source: "internal",
		// 运维工作台：把会话绑定的主机 / 主机组带进运行作用域，作为主机工具的授权依据
		HostScopeType: sessionScopeType,
		HostScopeID:   sessionScopeID,
	})
	// 会话权限模式：manual(默认) 逐步确认 / delegated 委托审批 / full_access 完全权限
	approvalMode := toolkit.NormalizeApprovalMode(req.ApprovalMode)
	ctx = toolkit.WithApprovalMode(ctx, approvalMode)
	if interactive && h.approvals != nil {
		// 人工确认通道与风险判定：两者都只作用于本次会话的工具调用链
		ctx = toolkit.WithApprover(ctx, chatApprover{
			broker: h.approvals, emit: emit,
			userID: uid, agentID: req.AgentID, sessionID: sessionID,
		})
	}
	ctx = toolkit.WithRiskAssessor(ctx, func(toolName string, args map[string]any) (string, string, bool) {
		assessment := approval.AssessToolCall(toolName, args)
		return assessment.Risk, assessment.Reason, assessment.Block
	})

	// 运行态以「已发布版本快照」为准：注入后技能 / MCP / 数据资源 / 模型绑定 / 工具挂载
	// 全部只读发布内容，管理员在编辑态的改动必须点「发布新版本」才生效。
	// 从未发布过（snap 为 nil）时保持草稿预览模式，行为与改造前一致。
	ctx, snap := effectiveSnapshot(ctx, h.store, req.AgentID)

	// 系统提示词与运行时配置统一从当前 Agent 读取。
	systemPrompt := defaultAgentSystemPrompt
	agentConfig, err := h.store.GetAgent(ctx, req.AgentID)
	if err != nil || agentConfig == nil {
		result.Err = fmt.Errorf("智能体不存在")
		result.Status = http.StatusNotFound
		return result
	}
	if snap != nil {
		// 已发布：提示词与运行参数一律以快照为准，编辑态的改动不影响本次执行
		if snap.Prompt != "" {
			systemPrompt = snap.Prompt
		}
		agentConfig.RuntimeType = snap.RuntimeType
		agentConfig.MaxSteps = snap.MaxSteps
		agentConfig.MemoryEnabled = snap.MemoryEnabled
		agentConfig.MemoryParams = snap.MemoryParams
		if snap.ChatModelID > 0 {
			agentConfig.ChatModelID = snap.ChatModelID
		}
		if snap.EmbedModelID > 0 {
			agentConfig.EmbedModelID = snap.EmbedModelID
		}
	} else {
		if agentConfig.Prompt != "" {
			systemPrompt = agentConfig.Prompt
		}
	}
	if agentConfig.RuntimeType == "" {
		agentConfig.RuntimeType = model.AgentRuntimeEinoV2
	}
	if agentConfig.MaxSteps <= 0 {
		agentConfig.MaxSteps = 8
	}
	systemPrompt = h.svc.AgentRuntime.BuildAgentSystemPrompt(ctx, h.store, req.AgentID, systemPrompt)
	// 输出约定：生成完整 HTML 页面时直接以 HTML 文本回复（前端自动渲染下载/预览卡片），
	// 避免把大段内容塞进 write_local_file 的 JSON 参数而触发解析失败。
	systemPrompt += "\n\n[输出约定] 当你需要生成完整的 HTML 页面（如旅行攻略、报表、可视化）供用户下载或预览时，" +
		"请直接以完整 HTML 文本回复（以 <!DOCTYPE html> 开头），前端会自动提供下载/预览，无需调用 write_local_file 写入大段内容；" +
		"若确实需要将内容落盘为文件，调用 write_local_file 时 content 必须是合法 JSON 字符串（换行用 \\n、双引号用 \\\" 转义）。" +
		// 模型常在页面末尾加「点击查看完整版 / 分享攻略」这类按钮，指向一个并不存在的页面，
		// 用户点了没反应。这里明确禁止生成无法落地的入口。
		"\n\n[链接约定] 生成的页面里不要放置无法访问的链接或按钮：不要用 href=\"#\"、href=\"\"、javascript:void(0) 占位，" +
		"不要写指向本地文件（如 攻略.html）、编造网址的链接，也不要放「点击查看完整版」「分享攻略」「查看更多」这类点了没有对应页面的入口。" +
		"需要跳转时只使用真实可用的地址（如高德地图 https://uri.amap.com/... ）；" +
		"页面本身就是完整内容，不要再引导用户去别的页面查看。" +
		"尤其不要写 /output/、/files/、/static/ 这类本地路径再配一个 .html 文件名" +
		"（例如 /output/beijing-1day-trip.html）：那些文件并不存在，点了必然 404。" +
		"如果确实要放「查看完整攻略 / 下载 / 分享」入口，固定使用 href=\"__EXPORT_URL__\"，" +
		"后端会自动替换成这份页面真实可分享的地址。"

	// 模型配置优先使用 AgentModel 多模型绑定，未配置时才回退全局激活模型。
	mcfg := h.resolveAgentModelConfig(ctx, req.AgentID, model.ModelRoleChat)
	if mcfg == nil {
		mcfg = h.resolveModelConfig(ctx)
	}
	// 请求显式指定了对该智能体已绑定的对话模型（工作台下拉临时切换），优先使用；
	// 未绑定或不可用时静默回退，避免用任意 modelId 调用未授权的模型。
	if req.ModelID > 0 {
		if picked := h.resolveAgentModelConfigByID(ctx, req.AgentID, req.ModelID, model.ModelRoleChat); picked != nil {
			mcfg = picked
		} else {
			ilog.Warnf("agent %d: chat model %d not bound or unusable, fallback to default", req.AgentID, req.ModelID)
		}
	}
	embedMcfg := h.resolveAgentModelConfig(ctx, req.AgentID, model.ModelRoleEmbedding)
	if embedMcfg == nil {
		embedMcfg = h.resolveEmbedModelConfig(ctx)
	}
	toolRegistry, tools := h.svc.AgentRuntime.RegisterTools(ctx, h.store, req.AgentID, embedMcfg)
	// 工具路由：按用户意图从远程 MCP 工具中挑选最相关的，避免把所有接口都塞进模型 prompt；
	// 同时同步过滤 toolRegistry（EinoV2 分支复用），保证两条运行时路径一致。
	if routed, names := h.svc.AgentRuntime.RouteTools(ctx, tools, req.Message, embedMcfg, 12); len(routed) > 0 {
		tools = routed
		toolRegistry = toolRegistry.Select(names)
	}

	memoryHistory := []service.ChatMessage{}
	runtimeMemory := ""
	memoryScope := memoryapi.Scope{UserID: uid, AgentID: req.AgentID, SessionID: sessionID}
	var memoryProvider memoryapi.Provider
	if agentConfig.MemoryEnabled && h.svc.Memory != nil {
		memoryProvider = service.NewMemoryProviderAdapter(h.svc.Memory, mcfg, embedMcfg, agentConfig.MemoryParams)
		if loaded, err := memoryProvider.Retrieve(ctx, memoryScope, memoryapi.RetrieveRequest{
			BeforeMessageID: userMsg.ID, Query: req.Message, Limit: 12,
		}); err != nil {
			ilog.Warnf("load agent memory: %v", err)
		} else {
			for _, message := range loaded.History {
				memoryHistory = append(memoryHistory, service.ChatMessage{Role: string(message.Role), Content: message.Content})
			}
			runtimeMemory = loaded.RuntimeContext
		}
	}

	// 按 Agent 配置分流：Eino V2 使用原生 function calling / ToolsNode，legacy 保留兼容回退。
	var toolCalls []map[string]string
	var answer string
	var state *service.AgentState
	if agentConfig.RuntimeType == model.AgentRuntimeEinoV2 {
		einoModel, buildErr := h.svc.Chat.BuildToolCallingModel(ctx, mcfg)
		if buildErr != nil {
			err = buildErr
		} else {
			notify("thinking", map[string]any{"content": "正在分析..."})
			messages := make([]*schema.Message, 0, len(memoryHistory)+1)
			for _, message := range memoryHistory {
				switch message.Role {
				case model.RoleAssistant:
					messages = append(messages, schema.AssistantMessage(message.Content, nil))
				default:
					messages = append(messages, schema.UserMessage(message.Content))
				}
			}
			currentMessage := userMessage
			if runtimeMemory != "" {
				currentMessage += "\n\n<runtime_memory trust=\"untrusted-data\">\n" + runtimeMemory + "\n</runtime_memory>"
			}
			messages = append(messages, schema.UserMessage(currentMessage))
			answer, err = agentscope.NewRuntime(einoModel, toolRegistry).
				WithName(agentConfig.Name).
				WithMaxSteps(agentConfig.MaxSteps).
				// 调用观测：Eino 路径此前没有任何落库，导致平台内部对话在观测页查不到记录
				WithUsageSink(func(u agentscope.RunUsage) {
					h.recordChatCallLog(ctx, req.AgentID, mcfg, u, len(answer))
				}).
				RunWithEvents(ctx, systemPrompt, messages, func(ev agentscope.AgentEvent) {
					switch ev.Type {
					case "thinking":
						notify("thinking", map[string]any{"content": ev.Content})
					case "tool_call":
						toolCalls = append(toolCalls, map[string]string{"name": ev.Name, "output": "执行中..."})
						notify("tool", map[string]any{"name": ev.Name, "input": ev.Input})
					case "tool_result":
						notify("tool_result", map[string]any{"name": ev.Name, "output": truncateStr(ev.Output, 1000)})
					case "text":
						notify("text", map[string]any{"content": ev.Content})
					}
				})
			state = &service.AgentState{Current: "finalize", Steps: []string{"eino_adk_v2", "done"}, StartTime: time.Now()}
		}
	} else {
		answer, state, err = h.svc.AgentRuntime.Run(
			ctx, req.AgentID, userMessage, memoryHistory, runtimeMemory, tools, systemPrompt, mcfg, embedMcfg,
			func(thinking string) {
				ilog.Infof("agent thinking: %s", thinking)
				notify("thinking", map[string]any{"content": thinking})
			},
			func(text string) {
				ilog.Infof("agent final text: %d chars", len([]rune(text)))
				notify("text", map[string]any{"content": text})
			},
			func(tr service.ToolResult) {
				if tr.Output == "执行中..." {
					notify("tool", map[string]any{"name": tr.Name})
					return
				}
				entry := map[string]string{"name": tr.Name, "output": truncateStr(tr.Output, 1000)}
				if tr.Error != "" {
					entry["error"] = truncateStr(tr.Error, 500)
				}
				toolCalls = append(toolCalls, entry)
				notify("tool_result", map[string]any{"name": tr.Name, "output": entry["output"], "error": tr.Error})
			},
		)
	}

	// 中断：ctx 被取消（用户点「停止」或超时）时按中断处理，而不是报错。
	interrupted := false
	if err != nil {
		if ctx.Err() != nil {
			// 只要是被取消就按中断处理：用户点停止时通常还没有任何输出，
			// 若按失败处理，界面会莫名冒出一条「执行失败」。
			interrupted = true
			err = nil
		} else {
			ilog.Errorf("agent chat error: %v", err)
			// 失败也要留痕：用户的问题已经入库，这里补一条错误回复，
			// 否则刷新页面后这轮对话会「凭空消失」，用户会以为消息丢了。
			// 失败常伴随 ctx 已取消，这里用不带取消的 ctx，保证这次写入不受影响。
			saveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
			defer cancel()
			failMsg := &model.ChatMessage{
				SessionID:   sessionID,
				Role:        model.RoleAssistant,
				Content:     fmt.Sprintf("（本次回答生成失败）%s", err.Error()),
				ContentType: "text",
				CreatedAt:   time.Now(),
			}
			if cerr := h.store.CreateChatMessage(saveCtx, failMsg); cerr != nil {
				ilog.Warnf("保存失败回复: %v", cerr)
			}
			result.Err = fmt.Errorf("Agent 执行失败: %w", err)
			return result
		}
	}
	result.Interrupted = interrupted

	// 长 HTML 页面（攻略、报表、可视化）常被模型单次输出长度上限截断，
	// 用户下载到的是半截文件。这里对未闭合的文档追加续写，直到 </html> 闭合。
	if err == nil && !interrupted && htmlTruncated(answer) {
		notify("thinking", map[string]any{"content": "内容较长，正在继续生成剩余部分..."})
		answer = h.continueTruncatedHTML(ctx, mcfg, systemPrompt, userMessage, answer, notify)
	}

	// 保存助手回复（中断时保存已产生的部分内容）；中间工具调用不进入长期记忆。
	// 中断且完全没有输出时不落库，避免留下一条空白气泡。
	var assistantMsg *model.ChatMessage
	if !(interrupted && answer == "") {
		assistantMsg = &model.ChatMessage{
			SessionID: sessionID, Role: model.RoleAssistant, Content: answer,
			ContentType: "text", CreatedAt: time.Now(),
		}
		if err := h.store.CreateChatMessage(ctx, assistantMsg); err != nil {
			result.Err = fmt.Errorf("保存回复失败: %w", err)
			return result
		}
		// 供前端用 /chat/messages/:id/export 下载（比 Blob 下载兼容性更好）
		result.MessageID = assistantMsg.ID
	}

	// 中断时不回写记忆：摘要与长期记忆需要完整的一问一答，半截内容会污染记忆。
	if memoryProvider != nil && !interrupted && assistantMsg != nil {
		memoryProvider.Memorize(ctx, memoryScope, memoryapi.MemorizeRequest{
			UserMessage: req.Message, AssistantMessage: answer, AssistantMessageID: assistantMsg.ID,
		})
	}

	// 新会话生成标题（标题只依赖用户问题，中断时同样生成）
	if req.SessionID == 0 {
		titleMsgs := service.BuildTitlePrompt(req.Message)
		if title, err := h.svc.Chat.Chat(ctx, titleMsgs, mcfg); err == nil {
			h.store.UpdateSessionTitle(ctx, sessionID, truncateStr(title, 50))
		}
	}

	ilog.Infof("agent chat done: session=%d toolCalls=%d interrupted=%v steps=%v",
		sessionID, state.ToolCalls, interrupted, state.Steps)

	result.Answer = answer
	result.Runtime = agentConfig.RuntimeType
	result.MemoryEnabled = agentConfig.MemoryEnabled
	result.ToolCalls = toolCalls
	result.AgentState = state
	result.Status = 0
	return result
}

// AgentChat Agent 模式对话（非流式 HTTP，供外部 API 调用）。
func (h *ChatHandler) AgentChat(c *gin.Context) {
	var req model.ChatRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	if req.Message == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "消息不能为空"})
		return
	}

	ctx := tracex.FromRequest(c)
	uid, username, _ := middleware.CurrentUser(c)

	res := h.runAgentStream(ctx, uid, username, middleware.CurrentIsAdmin(c), agentRunRequest{
		Message: req.Message, AgentID: req.AgentID, SessionID: req.SessionID,
		KnowledgeID: req.KnowledgeID, ModelID: req.ModelID,
	}, nil)
	if res.Err != nil {
		status := res.Status
		if status == 0 {
			status = http.StatusInternalServerError
		}
		c.JSON(status, gin.H{"code": 5, "message": res.Err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":          0,
		"sessionId":     res.SessionID,
		"response":      res.Answer,
		"runtime":       res.Runtime,
		"memoryEnabled": res.MemoryEnabled,
		"toolCalls":     res.ToolCalls,
		"agentState": gin.H{
			"current":   res.AgentState.Current,
			"toolCalls": res.AgentState.ToolCalls,
			"commands":  res.AgentState.Commands,
			"steps":     res.AgentState.Steps,
		},
	})
}

// ListSessions 会话列表（支持按 agentId 筛选）。
func (h *ChatHandler) ListSessions(c *gin.Context) {
	uid, _, _ := middleware.CurrentUser(c)
	agentID, _ := strconv.ParseInt(c.Query("agentId"), 10, 64)
	// 运维工作台按作用域取会话：scopeType=host|host_group 时必须带 scopeId
	scopeType := strings.TrimSpace(c.Query("scopeType"))
	scopeID, _ := strconv.ParseInt(c.Query("scopeId"), 10, 64)

	var sessions []*model.ChatSession
	var err error
	if agentID > 0 {
		if scopeType != "" && (scopeType == model.SessionScopeGlobal || scopeID > 0) {
			sessions, err = h.store.ListChatSessionsByScope(tracex.FromRequest(c), agentID, uid, scopeType, scopeID)
		} else {
			sessions, err = h.store.ListChatSessionsByAgent(tracex.FromRequest(c), agentID, uid)
		}
	} else {
		sessions, err = h.store.ListChatSessions(tracex.FromRequest(c), uid)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": sessions})
}

// CreateSession 创建会话。
func (h *ChatHandler) CreateSession(c *gin.Context) {
	var req struct {
		Title       string `json:"title"`
		AgentID     int64  `json:"agentId"`
		KnowledgeID int64  `json:"knowledgeId"`
		// 运维会话作用域：host / host_group / global（空按 global 处理）
		ScopeType string `json:"scopeType"`
		ScopeID   int64  `json:"scopeId"`
		ScopeName string `json:"scopeName"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	scopeType := strings.TrimSpace(req.ScopeType)
	if scopeType == "" {
		scopeType = model.SessionScopeGlobal
	}
	// 绑定具体机器时必须给 ID，否则作用域无意义，直接按全局会话处理
	if scopeType != model.SessionScopeGlobal && req.ScopeID <= 0 {
		scopeType = model.SessionScopeGlobal
		req.ScopeID = 0
		req.ScopeName = ""
	}

	uid, username, _ := middleware.CurrentUser(c)
	var agentName string
	if req.AgentID > 0 {
		if agent, err := h.store.GetAgent(tracex.FromRequest(c), req.AgentID); err == nil {
			agentName = agent.Name
		}
	}
	session := &model.ChatSession{
		Title:       req.Title,
		AgentID:     req.AgentID,
		AgentName:   agentName,
		KnowledgeID: req.KnowledgeID,
		UserID:      uid,
		Username:    username,
		ScopeType:   scopeType,
		ScopeID:     req.ScopeID,
		ScopeName:   req.ScopeName,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}
	if err := h.store.CreateChatSession(tracex.FromRequest(c), session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": session})
}

// DeleteSession 删除会话。必须归属当前登录用户，防止越权删除他人会话。
func (h *ChatHandler) DeleteSession(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := h.store.GetChatSessionForUser(ctx, id, uid); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "会话不存在"})
		return
	}
	if err := h.store.DeleteChatSession(ctx, id); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "message": "删除成功"})
}

// TogglePin 切换置顶。必须归属当前登录用户，防止越权篡改他人会话。
func (h *ChatHandler) TogglePin(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	session, err := h.store.GetChatSessionForUser(ctx, id, uid)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "会话不存在"})
		return
	}
	session.IsPinned = !session.IsPinned
	if err := h.store.UpdateChatSession(ctx, session); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": session})
}

// ListMessages 消息列表。必须归属当前登录用户，防止越权读取他人会话内容。
func (h *ChatHandler) ListMessages(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if _, err := h.store.GetChatSessionForUser(ctx, id, uid); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "会话不存在"})
		return
	}
	messages, err := h.store.ListChatMessages(ctx, id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": messages})
}

// ExportMessage 把助手消息里的 HTML 页面导出成文件下载。
//
// 前端用 Blob 下载在部分环境（IDE 内置浏览器 / webview）会被静默吞掉，
// 用户点了「下载」却什么都没发生。这里提供普通 HTTP 下载链接：
//
//	<a href="/api/chat/messages/123/export?token=...">
//
// 它不依赖 JS，也就不受 Blob 限制；鉴权中间件支持 query token，链接可直接使用。
// 带 ?inline=1 时在浏览器里直接打开预览，不触发下载。
func (h *ChatHandler) ExportMessage(c *gin.Context) {
	ctx := tracex.FromRequest(c)
	uid, _, _ := middleware.CurrentUser(c)
	id, _ := strconv.ParseInt(c.Param("id"), 10, 64)
	if id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 2, "message": "参数错误"})
		return
	}
	msg, err := h.store.GetChatMessage(ctx, id)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "消息不存在"})
		return
	}
	// 越权校验：消息所属会话必须归当前用户，防止拿到别人的内容
	if _, err := h.store.GetChatSessionForUser(ctx, msg.SessionID, uid); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "消息不存在"})
		return
	}
	html := extractHTMLDocument(msg.Content)
	if html == "" {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "该消息不是可导出的 HTML 页面"})
		return
	}
	// 去掉「点击查看 / 分享攻略」这类点了没反应的占位链接（对历史消息同样生效）
	html = sanitizePlaceholderLinks(html)

	// 落盘成真实文件，随后重定向到静态地址：
	// 之后的下载 / 分享都不再需要登录，链接也可以直接发给别人。
	if err := os.MkdirAll(fsutil.ExportsDir(), 0o755); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "创建导出目录失败"})
		return
	}
	// 落盘名带消息 ID 和时间戳：同一消息可导出多版而不互相覆盖，也便于按时间区分
	fileName := fmt.Sprintf("%d-%s-%s.html",
		msg.ID, htmlFileName(html), time.Now().Format("20060102-150405"))
	target := "/api/chat/exports/" + url.PathEscape(fileName)
	// 分享用的绝对地址（不带 inline，拿到即下载）
	abs := absoluteURL(c, target)
	// 模型按约定写的 __EXPORT_URL__ 占位符：换成这份页面真实可分享的绝对地址
	html = strings.ReplaceAll(html, "__EXPORT_URL__", abs)
	// 页面里指向「另一个本地 html」的链接（如 /output/beijing-1day-trip.html）
	// 对应的文件从没生成过，点了必然 404。这份单文件 HTML 本身就是完整攻略，
	// 兜底改写成指向本页。
	html = rewriteLocalHTMLLinks(html, url.PathEscape(fileName))
	if err := os.WriteFile(filepath.Join(fsutil.ExportsDir(), fileName), []byte(html), 0o644); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 5, "message": "写入导出文件失败"})
		return
	}
	// ?json=1：只返回静态地址供前端「复制链接」使用，不做跳转。
	// 必须这样拿——直接把带 token 的导出链接发给别人等于泄露自己的登录凭据。
	if c.Query("json") == "1" {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"url": abs, "fileName": fileName}})
		return
	}
	if c.Query("inline") == "1" {
		abs += "?inline=1"
	}
	// 绝对地址：复制地址栏就能直接分享，接收方不用自己拼后端域名
	c.Redirect(http.StatusFound, abs)
}

// ExportStatic 免登录下载导出目录里的 HTML 文件。
//
// 安全性说明：文件名形如「<消息ID>-<标题>.html」，标题由模型生成、无法枚举，
// 因此未拿到链接的人构造不出有效地址；而链接本身就是用来下载和分享的，
// 再要求登录只会让接收方打不开。这里用 filepath.Clean + 前缀校验兜底，杜绝路径穿越
// （既支持扁平文件名，也支持 write_local_file 写入的子目录，如 trip2026/amap.html）。
func (h *ChatHandler) ExportStatic(c *gin.Context) {
	// 通配符参数可能含斜杠（子目录），先收成相对根的安全路径
	rel := strings.TrimPrefix(filepath.Clean("/"+c.Param("filename")), "/")
	if rel == "" || rel == "." || strings.HasPrefix(rel, "..") ||
		strings.Contains(rel, "..") || !strings.HasSuffix(strings.ToLower(rel), ".html") {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}
	root := fsutil.ExportsDir()
	absRoot, _ := filepath.Abs(root)
	target := filepath.Join(root, rel)
	absTarget, err := filepath.Abs(target)
	if err != nil || (absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator))) {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}
	data, err := os.ReadFile(absTarget)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": 4, "message": "文件不存在"})
		return
	}
	disposition := "attachment"
	if c.Query("inline") == "1" {
		disposition = "inline"
		// 预览的 HTML 由模型生成，却和前端同源：直接渲染时页面里的脚本
		// 能读到 localStorage 里的登录 token。sandbox 会让它运行在唯一不透明源，
		// 拿不到本站的任何数据，同时保留页面自身的脚本能力。
		c.Header("Content-Security-Policy",
			"sandbox allow-scripts allow-popup allow-popup-to-escape-sandbox allow-forms")
	}
	// 存盘名带 ID 和时间戳，用户下载时看到的是干净的标题名
	shown := downloadNameFrom(filepath.Base(rel))
	// filename 用 ASCII 兜底（部分浏览器不认非 ASCII），filename* 用 UTF-8 原名
	c.Header("Content-Disposition", fmt.Sprintf(`%s; filename="%s"; filename*=UTF-8''%s`,
		disposition, htmlASCIIName(shown), url.QueryEscape(shown)))
	c.Header("Content-Type", "text/html; charset=utf-8")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

// extractHTMLDocument 从消息内容里提取 HTML 文档主体。
// 兼容三种形态：直接输出、```html 代码块包裹、以及前面带引导语的混合内容。
func extractHTMLDocument(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return ""
	}
	if idx := strings.Index(strings.ToLower(t), "<!doctype html"); idx >= 0 {
		return cutHTMLAtClose(t[idx:])
	}
	if idx := htmlStartIndex(t); idx >= 0 {
		return cutHTMLAtClose(t[idx:])
	}
	// ```html ... ``` 包裹（流式未闭合时没有收尾围栏）
	if idx := strings.Index(t, "```"); idx >= 0 {
		rest := strings.TrimLeft(t[idx+3:], "hHtTmMlL \r\n")
		if strings.HasPrefix(strings.ToLower(rest), "<!doctype html") || strings.HasPrefix(strings.ToLower(rest), "<html") {
			if end := strings.Index(rest, "```"); end >= 0 {
				return strings.TrimSpace(rest[:end])
			}
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// htmlStartIndex 找到 <html> / <html ...> 的起始下标，避免把 <html-foo> 之类的串误判成标签
func htmlStartIndex(s string) int {
	lower := strings.ToLower(s)
	for i := 0; i+5 <= len(lower); i++ {
		if !strings.HasPrefix(lower[i:], "<html") {
			continue
		}
		next := i + 5
		if next >= len(lower) || lower[next] == '>' || lower[next] == ' ' || lower[next] == '\n' || lower[next] == '\r' {
			return i
		}
	}
	return -1
}

// cutHTMLAtClose 截取到 </html> 结束；没有结束标签时取到末尾（此时内容可能未生成完）
func cutHTMLAtClose(s string) string {
	if idx := strings.Index(strings.ToLower(s), "</html>"); idx >= 0 {
		return strings.TrimSpace(s[:idx+len("</html>")])
	}
	return strings.TrimSpace(s)
}

// htmlFileName 取 <title> 作为文件名主体（不含扩展名），并替换掉文件系统不允许的字符。
// 调用方自行拼接消息 ID、时间戳与 .html 后缀。
func htmlFileName(html string) string {
	name := "智能体生成的页面"
	lower := strings.ToLower(html)
	if i := strings.Index(lower, "<title>"); i >= 0 {
		start := i + len("<title>")
		if end := strings.Index(lower[start:], "</title>"); end >= 0 {
			if t := strings.TrimSpace(html[start : start+end]); t != "" {
				name = t
			}
		}
	}
	name = strings.Map(func(r rune) rune {
		switch r {
		case '\\', '/', ':', '*', '?', '"', '<', '>', '|', '\r', '\n', '\t':
			return '_'
		}
		return r
	}, name)
	if runes := []rune(name); len(runes) > 80 {
		name = string(runes[:80])
	}
	return name
}

// absoluteURL 把相对路径补全成带后端域名的绝对地址。
// 导出链接是要复制出去分享的，只有相对路径时接收方根本打不开。
func absoluteURL(c *gin.Context, path string) string {
	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host
	// 反向代理后面 Request.Host 往往是内部地址，优先用代理透传的原始 Host，
	// 否则生成出来的分享链接别人打不开
	if fh := strings.TrimSpace(c.GetHeader("X-Forwarded-Host")); fh != "" {
		host = fh
	}
	if host == "" {
		return path
	}
	return scheme + "://" + host + path
}

// downloadNameFrom 把落盘文件名还原成用户看到的文件名：
//
//	"123-北京3日旅行攻略-20260830-113000.html" → "北京3日旅行攻略.html"
//
// 存盘时需要 ID 和时间戳来保证唯一、可区分版本，但用户下载时不该看到这些噪音。
func downloadNameFrom(name string) string {
	base := strings.TrimSuffix(name, ".html")
	// 去掉末尾的 -HHMMSS
	if idx := strings.LastIndex(base, "-"); idx > 0 && isAllDigits(base[idx+1:]) && len(base[idx+1:]) == 6 {
		base = base[:idx]
		// 再去掉 -YYYYMMDD
		if idx = strings.LastIndex(base, "-"); idx > 0 && isAllDigits(base[idx+1:]) && len(base[idx+1:]) == 8 {
			base = base[:idx]
		}
	}
	// 去掉开头的 <消息ID>-
	if idx := strings.Index(base, "-"); idx > 0 && isAllDigits(base[:idx]) {
		base = base[idx+1:]
	}
	if base == "" {
		return name
	}
	return base + ".html"
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// placeholderLinkRe 匹配点了不会有反应的占位链接：href="#" / href="" / javascript:void(0)。
// 刻意不匹配 href="#top" 这类页内锚点——那是合法且有用的跳转。
var placeholderLinkRe = regexp.MustCompile(`(?is)<a\s[^>]*?href\s*=\s*(?:"#?"|'#?'|"javascript:void\(0\)"|'javascript:void\(0\)'|"about:blank"|'about:blank')[^>]*>(.*?)</a>`)

// sanitizePlaceholderLinks 把无效占位链接降级为普通文本（保留文字，去掉链接）。
//
// 模型常在生成的攻略里放「点击查看 / 分享攻略」之类的按钮，href 是 # 或空，
// 用户点了没有任何反应，看着像功能坏了。这里把这类链接去掉只留文字，
// 至少不会再出现「点了没反应」的困惑；真实外链（高德地图等）不受影响。
func sanitizePlaceholderLinks(html string) string {
	return placeholderLinkRe.ReplaceAllString(html, "<span>$1</span>")
}

// localHTMLHrefRe 匹配指向本地 html 文件的链接，如 /output/guangzhou-1day-trip.html
var localHTMLHrefRe = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+\.html)["']`)

// rewriteLocalHTMLLinks 把页面里指向「另一个本地 html 文件」的链接改写成指向本页。
//
// 模型常写 <a href="/output/xxx.html">查看完整攻略</a>，但那个文件从未被生成过，
// 点了必然 404。导出的这份单文件 HTML 本身就是完整内容，
// 所以指向本页是唯一真实可用的目标。
//
// 这里刻意用「相对文件名」而不是绝对地址：
//   - 在线打开时，相对路径解析成 /api/chat/exports/<文件>，可用；
//   - 下载到本地用 file:// 打开时，相对路径指向同目录的同名文件，同样可用。
//     若写成 /api/... 这种绝对路径，本地打开就会跳到 file:///api/... 而失效。
//
// 真实外链（http/https/mailto/data/锚点）一律保持原样。
func rewriteLocalHTMLLinks(html, selfHref string) string {
	return localHTMLHrefRe.ReplaceAllStringFunc(html, func(m string) string {
		sub := localHTMLHrefRe.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		u := strings.TrimSpace(sub[1])
		lower := strings.ToLower(u)
		if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") ||
			strings.HasPrefix(lower, "mailto:") || strings.HasPrefix(lower, "data:") {
			return m
		}
		return `href="` + selfHref + `"`
	})
}

// htmlASCIIName 把非 ASCII 字符换成下划线，作为 Content-Disposition 的 ASCII 兜底文件名
func htmlASCIIName(name string) string {
	return strings.Map(func(r rune) rune {
		if r > 127 {
			return '_'
		}
		return r
	}, name)
}

// sendSSE 发送 SSE 事件。
func sendSSE(c *gin.Context, eventType string, data interface{}) {
	jsonData, _ := json.Marshal(data)
	fmt.Fprintf(c.Writer, "event: %s\ndata: %s\n\n", eventType, string(jsonData))
	c.Writer.Flush()
}

func truncateStr(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// agentStreamRequest WebSocket 上行消息。Type 取值：agent / stop。
type agentStreamRequest struct {
	Type         string `json:"type"`
	Message      string `json:"message"`
	AgentID      int64  `json:"agentId"`
	SessionID    int64  `json:"sessionId"`
	KnowledgeID  int64  `json:"knowledgeId"`
	ModelID      int64  `json:"modelId"`
	ApprovalMode string `json:"approvalMode"`
	ScopeType    string `json:"scopeType"`
	ScopeID      int64  `json:"scopeId"`
	ScopeName    string `json:"scopeName"`
}

// WSChat Agent 流式对话。
// 连接模型：
//   - 读循环只负责收指令，不执行长任务，保证随时能收到 stop
//   - 每次 agent 执行在独立 goroutine 中运行并持有 cancel，收到 stop 时取消
//   - 所有写操作由 writeMu 串行化（websocket.Conn 不支持并发写）
func (h *ChatHandler) WSChat(c *gin.Context) {
	conn, err := wsUpgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		ilog.Errorf("ws upgrade: %v", err)
		return
	}
	defer conn.Close()

	ctx := tracex.FromRequest(c)
	uid, username, _ := middleware.CurrentUser(c)
	ilog.Infof("ws chat connected: user=%d(%s)", uid, username)

	// 写串行化：读循环、执行 goroutine、心跳共用一把锁。
	var writeMu sync.Mutex
	// 写失败后这条连接已推不动数据：后续每条事件都会再阻塞满写超时并重复报警，
	// 这里只标记并跳过后续推送。
	//
	// 注意不能在这里关闭连接：连接退出会触发下面的 defer 取消正在执行的 agent，
	// 让还没跑完的回复被掐成半截。正确做法是让 agent 跑完并落库，
	// 前端重连后从历史消息取完整结果；真正停止只由用户点「停止」或宽限期超时触发。
	var writeFailed bool
	emit := func(typ string, data map[string]any) {
		writeMu.Lock()
		defer writeMu.Unlock()
		if writeFailed {
			return
		}
		payload := map[string]any{"type": typ}
		for k, v := range data {
			payload[k] = v
		}
		if err := writeWS(conn, payload); err != nil {
			writeFailed = true
			ilog.Warnf("ws write %s: %v, skip remaining events (agent keeps running)", typ, err)
		}
	}

	// ping/pong 心跳保活
	conn.SetReadDeadline(time.Now().Add(60 * time.Second))
	conn.SetPongHandler(func(string) error {
		conn.SetReadDeadline(time.Now().Add(60 * time.Second))
		return nil
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			writeMu.Lock()
			// 必须设置写超时：对端不消费数据时 WriteMessage 会无限阻塞，
			// 而它此时正持有写锁，会让所有 emit（含停止后的 done 收尾）全部卡死，
			// 表现为前端点了「停止生成」却一直转圈。
			_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			pingErr := conn.WriteMessage(websocket.PingMessage, nil)
			writeMu.Unlock()
			if pingErr != nil {
				// 同样不关闭连接：关闭会连带取消正在执行的 agent，让回复只剩半截。
				// 推送不出去就停止心跳，让 agent 安静跑完并落库。
				return
			}
		}
	}()

	emit("connected", map[string]any{"message": "WebSocket 已连接"})

	// 当前执行中的请求；同一连接同时只允许一个。
	var mu sync.Mutex
	var wg sync.WaitGroup
	var cancelCurrent context.CancelFunc
	var currentID uint64 // 当前执行请求的序号，受 mu 保护
	// 连接退出时不再立刻取消执行中的 agent：断网 / 切走页面 / 写超时都会走到这里，
	// 但那不等于用户想停止生成，立刻取消会把本来能跑完的回复掐成半截。
	// 先给一个宽限期让它跑完落库，超时仍未结束才真正取消，避免 goroutine 泄漏。
	// 用户主动点「停止」走的是下面的 case "stop"，立即取消，不受宽限期影响。
	defer func() {
		stopped := make(chan struct{})
		go func() { wg.Wait(); close(stopped) }()
		select {
		case <-stopped:
			return
		case <-time.After(agentGracePeriod):
		}
		mu.Lock()
		if cancelCurrent != nil {
			ilog.Warnf("ws agent still running after %v, cancelling", agentGracePeriod)
			cancelCurrent()
			cancelCurrent = nil
		}
		mu.Unlock()
		stopped2 := make(chan struct{})
		go func() { wg.Wait(); close(stopped2) }()
		select {
		case <-stopped2:
		case <-time.After(10 * time.Second):
			ilog.Warnf("ws agent did not stop within 10s after cancel")
		}
	}()

	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			// 1005（CloseNoStatusReceived）是浏览器刷新 / 关闭标签页的常见表现，
			// 属于预期行为，不该报警刷屏
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway,
				websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
				ilog.Warnf("ws read: %v", err)
			}
			break
		}

		var req agentStreamRequest
		if err := json.Unmarshal(msgBytes, &req); err != nil {
			emit("error", map[string]any{"error": "无效消息"})
			continue
		}

		switch req.Type {
		case "stop":
			mu.Lock()
			if cancelCurrent != nil {
				cancelCurrent()
				cancelCurrent = nil
				ilog.Infof("ws agent stopped by user=%d", uid)
			}
			mu.Unlock()
			continue
		case "agent":
			if req.Message == "" {
				emit("error", map[string]any{"error": "消息不能为空"})
				continue
			}
		default:
			emit("error", map[string]any{"error": "不支持的消息类型: " + req.Type})
			continue
		}

		// 新请求到达：先停掉上一个，保证同一连接只有一个执行中的 agent。
		mu.Lock()
		if cancelCurrent != nil {
			cancelCurrent()
		}
		currentID++
		runID := currentID
		// 解绑连接生命周期：ctx 会随连接断开而取消，直接继承会让断网掐断 agent。
		// 这里只受 cancelCurrent 控制（用户点停止 / 宽限期超时）。
		reqCtx, cancel := context.WithCancel(context.WithoutCancel(ctx))
		cancelCurrent = cancel
		mu.Unlock()

		agentReq := agentRunRequest{
			Message:      req.Message,
			AgentID:      req.AgentID,
			SessionID:    req.SessionID,
			KnowledgeID:  req.KnowledgeID,
			ModelID:      req.ModelID,
			ApprovalMode: req.ApprovalMode,
			ScopeType:    req.ScopeType,
			ScopeID:      req.ScopeID,
			ScopeName:    req.ScopeName,
		}
		wg.Add(1)
		go func(runID uint64) {
			defer wg.Done()
			defer func() {
				mu.Lock()
				// 只有仍是当前请求时才清空，避免误清后发起的新请求
				if currentID == runID {
					cancelCurrent = nil
				}
				mu.Unlock()
				cancel()
			}()

			res := h.runAgentStream(reqCtx, uid, username, middleware.CurrentIsAdmin(c), agentReq, emit)
			if res.Err != nil {
				emit("error", map[string]any{"error": res.Err.Error()})
				emit("done", map[string]any{"sessionId": res.SessionID, "interrupted": false})
				return
			}
			emit("done", map[string]any{
				"sessionId":     res.SessionID,
				"messageId":     res.MessageID,
				"runtime":       res.Runtime,
				"memoryEnabled": res.MemoryEnabled,
				"interrupted":   res.Interrupted,
				"toolCalls":     res.ToolCalls,
			})
		}(runID)
	}
}

func writeWS(conn *websocket.Conn, data interface{}) error {
	jsonData, _ := json.Marshal(data)
	// 写超时不宜过长：一旦对端不消费数据，这里每阻塞一次，
	// 同连接上的其他事件（尤其是停止后的 done 收尾）就要多等一次。
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	if err := conn.WriteMessage(websocket.TextMessage, jsonData); err != nil {
		return err
	}
	return nil
}

// htmlContinueMaxRounds 单个回答允许的续写轮数上限，避免模型反复输出无效内容导致耗时失控。
const htmlContinueMaxRounds = 3

// htmlTruncated 判断回答中的 HTML 文档是否未闭合：模型单次输出受 maxTokens 限制时，
// 长页面常被从中间截断，用户下载到的就是半截文件。
func htmlTruncated(s string) bool {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return false
	}
	if !strings.Contains(t, "<!doctype html") && !strings.Contains(t, "<html") {
		return false
	}
	return !strings.Contains(t, "</html>")
}

// continueTruncatedHTML 对未闭合的 HTML 文档追加续写，直到闭合或达到轮次上限。
// 与其要求模型一次吐完几万字符（受 maxTokens 硬限制，且容易再次截断），
// 不如从断点接着生成并拼接。续写片段同样通过 notify 推送，
// 保证界面流式看到的内容与最终落库的一致。
func (h *ChatHandler) continueTruncatedHTML(
	ctx context.Context, mcfg *service.ModelConfig,
	systemPrompt, userMessage, answer string,
	notify func(string, map[string]any),
) string {
	if h == nil || h.svc == nil || h.svc.Chat == nil || mcfg == nil {
		return answer
	}
	for i := 0; i < htmlContinueMaxRounds && htmlTruncated(answer); i++ {
		msgs := []service.ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: userMessage},
			{Role: "assistant", Content: answer},
			{Role: "user", Content: "上面的 HTML 页面因单次输出长度限制被截断了。请严格从被截断的位置继续输出剩余的 HTML 源码：" +
				"不要重复已经输出过的内容，不要添加任何解释，也不要用 markdown 代码块包裹，" +
				"直接接着输出，直到 </html> 完整闭合。"},
		}
		cont, cerr := h.svc.Chat.Chat(ctx, msgs, mcfg)
		if cerr != nil {
			ilog.Warnf("continue truncated html round %d failed: %v", i+1, cerr)
			break
		}
		cont = strings.TrimSpace(cont)
		// 模型可能仍用 ```html 包裹续写内容，去掉围栏后再拼接
		if strings.HasPrefix(cont, "```") {
			if idx := strings.Index(cont, "\n"); idx >= 0 {
				cont = cont[idx+1:]
			}
			cont = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(cont), "```"))
		}
		// 模型没按要求续写、而是重新输出了整个文档：拼接会出现两份文档，直接放弃续写
		if cont == "" || strings.HasPrefix(strings.ToLower(cont), "<!doctype") {
			ilog.Warnf("continue truncated html round %d: model restarted the document, abort", i+1)
			break
		}
		answer += cont
		notify("text", map[string]any{"content": cont})
	}
	return answer
}

func maskKey(key string) string {
	if key == "" {
		return "(empty)"
	}
	if len(key) <= 8 {
		return "***"
	}
	return key[:4] + "..." + key[len(key)-4:]
}
