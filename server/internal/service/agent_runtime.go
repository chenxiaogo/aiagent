package service

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/knowledge"
	"aiagent/internal/mcpclient"
	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/internal/toolkit"
	"aiagent/pkg/fsutil"
	"aiagent/pkg/ilog"

	"github.com/cloudwego/eino/schema"
)

// AgentRuntime Agent 运行时状态机（参考 1Shell 的 agent-runtime）。
// 状态流转：understand → observe → plan → decide → act → verify → finalize
type AgentRuntime struct {
	MaxToolRounds int
	MaxRuntime    time.Duration
	MaxToolCalls  int // 工具调用总次数硬上限（防止 token 爆炸）
	OutputDir     string // 本地产物输出根目录（write_local_file 落盘，同源 /api/chat/exports 静态访问）
	chat          *ChatService
	embedding     *EmbeddingService
	search        *CameraSearchService // 运行时注入（需要 store），可能为 nil
	store         *store.Store         // 运行时注入，用于写调用观测日志（CallLog）
}

// SetSearch 注入摄像头检索服务（需 store，由组装层在 New 之后调用）。
func (r *AgentRuntime) SetSearch(s *CameraSearchService) {
	r.search = s
}

// SetStore 注入数据仓库（写 LLM 调用观测日志用）。
func (r *AgentRuntime) SetStore(s *store.Store) {
	r.store = s
}

// 常量：单条观察/回复允许的字符上限，避免长文本在每轮循环里被反复回传烧 token
const (
	obsTruncateLen   = 800  // 工具观察结果截断长度
	replyTruncateLen = 2000 // 存回的 assistant 回复截断长度
	factTruncateLen  = 300  // 结构化 fact 文本截断长度（喂回模型用，更省 token）
)

// 检索增强提示词：查询改写 + 聚合压缩（带出处），用于缓解文件检索「内容模糊」。
const (
	docSearchRewriteSystem = "你是知识库检索查询优化器。把用户的口语化或模糊问题改写成一条最利于向量检索的简洁查询：" +
		"保留核心实体与关键词，去掉寒暄与无关评论，必要时补全同义词。只输出改写后的一条查询，不要任何解释或引号。"
	docSearchAggregateSystem = "你是知识库检索结果整理助手。根据用户问题，从下列知识片段中提取与问题直接相关的事实要点。" +
		"要求：1) 每条要点后用「来源：文件名 页码/行号」标注出处；2) 仅依据所给片段，片段未提及的内容不要编造，可写「知识库未提及」；" +
		"3) 总字数控制在 600 字以内；4) 使用简洁中文。"
)

// modelConfigContextKey 用于在 ctx 中传递本次请求的模型配置（配置每次请求不同，不存 runtime 字段避免并发复用污染）
type modelConfigContextKey struct{}
type agentIDContextKey struct{}

func agentIDFromContext(ctx context.Context) int64 {
	id, _ := ctx.Value(agentIDContextKey{}).(int64)
	return id
}

// agentModels 本次请求用到的模型配置：对话模型 + 向量模型。
// 二者必须分开：用对话模型名去请求 /embeddings 会被网关拒绝（返回 HTML 错误页）。
type agentModels struct {
	chat  *ModelConfig
	embed *ModelConfig
}

// WithModelConfigs 把对话模型和向量模型配置注入 ctx。
func WithModelConfigs(ctx context.Context, chatMcfg, embedMcfg *ModelConfig) context.Context {
	return context.WithValue(ctx, modelConfigContextKey{}, &agentModels{chat: chatMcfg, embed: embedMcfg})
}

// modelConfigFromContext 从 ctx 取出对话模型配置。
func modelConfigFromContext(ctx context.Context) *ModelConfig {
	if m, ok := ctx.Value(modelConfigContextKey{}).(*agentModels); ok {
		return m.chat
	}
	return nil
}

// embedModelConfigFromContext 从 ctx 取出向量模型配置；未配置时回退到对话模型配置。
func embedModelConfigFromContext(ctx context.Context) *ModelConfig {
	if m, ok := ctx.Value(modelConfigContextKey{}).(*agentModels); ok {
		if m.embed != nil && m.embed.apiKey() != "" {
			return m.embed
		}
		return m.chat
	}
	return nil
}

// AgentState Agent 状态
type AgentState struct {
	Current   string    `json:"current"`
	Steps     []string  `json:"steps"`
	ToolCalls int       `json:"toolCalls"`
	Commands  int       `json:"commands"`
	StartTime time.Time `json:"startTime"`
}

// AgentTool 工具定义
type AgentTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Execute     func(ctx context.Context, args map[string]interface{}) (string, error)
}

// ToolResult 工具调用结果
type ToolResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Output  string `json:"output"`
	Error   string `json:"error,omitempty"`
}

// NewAgentRuntime 创建 Agent 运行时。
func NewAgentRuntime(chat *ChatService, embedding *EmbeddingService) *AgentRuntime {
	return &AgentRuntime{
		MaxToolRounds: 8,
		MaxRuntime:    5 * time.Minute,
		MaxToolCalls:  24, // 工具调用总次数硬上限，避免多轮循环 token 失控
		OutputDir:     fsutil.ExportsDir(),
		chat:          chat,
		embedding:     embedding,
	}
}

// Run 执行 Agent 主循环。返回最终回答、运行时状态（含预算消耗）和错误。
// mcfg 为对话模型配置；embedMcfg 为向量模型配置（工具检索时用），传 nil 则工具内部回退到对话模型配置。
// agentID 为发起对话的智能体（无则传 0），用于写入调用观测日志（CallLog）。
func (r *AgentRuntime) Run(ctx context.Context, agentID int64, userMessage string, history []ChatMessage, runtimeContext string, tools []AgentTool, systemPrompt string, mcfg *ModelConfig, embedMcfg *ModelConfig, onThinking func(string), onText func(string), onToolCall func(ToolResult)) (string, *AgentState, error) {
	startTime := time.Now()
	state := &AgentState{Current: "understand", StartTime: startTime}
	// Budget 必须是每次 Run 独立创建，禁止跨请求共享工具次数和计时状态。
	budget := &Budget{
		MaxToolCalls: r.MaxToolCalls,
		MaxCommands:  r.MaxToolCalls,
		MaxRuntime:   r.MaxRuntime,
		StartTime:    startTime,
	}
	// 把本次请求的模型配置注入 ctx，供工具闭包（search_camera/search_videos）取用。
	// embedMcfg 为 nil 时工具内部会回退到 chat 配置。
	ctx = WithModelConfigs(ctx, mcfg, embedMcfg)
	ctx = context.WithValue(ctx, agentIDContextKey{}, agentID)

	contextBudget := DefaultContextBudget(mcfg.maxTokens())
	// 动态记忆上下文属于不可信参考数据，追加在当前 user 消息中，不改变稳定 system prompt。
	currentUserMessage := userMessage
	if strings.TrimSpace(runtimeContext) != "" {
		currentUserMessage += "\n\n<runtime_memory>\n" + contextBudget.TrimRuntimeMemory(runtimeContext) + "\n</runtime_memory>"
	}
	messages := make([]ChatMessage, 0, len(history)+2)
	messages = append(messages, ChatMessage{Role: "system", Content: r.buildSystemPrompt(systemPrompt, tools)})
	messages = append(messages, history...)
	messages = append(messages, ChatMessage{Role: "user", Content: currentUserMessage})
	messages = contextBudget.TrimMessages(messages)

	var finalResponse string

	for round := 0; round < r.MaxToolRounds; round++ {
		// 停止信号：ctx 被上游取消（WS 的 stop 指令）
		if err := ctx.Err(); err != nil {
			state.Current = "interrupted"
			state.Steps = append(state.Steps, "interrupted")
			return finalResponse, state, err
		}
		// 统一预算门禁：超时 / 工具调用次数 / 命令次数（接入 agent_core 的 Budget）
		if ok, reason := budget.CheckBudget(); !ok {
			ilog.Warnf("agent runtime budget exhausted: %s", reason)
			state.Current = "blocked"
			state.Steps = append(state.Steps, "budget:"+reason)
			break
		}

		state.Current = "decide"
		onThinking("分析中...")

		// 调用 LLM。调用观测统一由 ChatService 落库（带真实 token 用量），
		// 这里不再重复记录，否则同一轮对话会在观测页出现两条。
		// 显式声明为主链路用途：未声明的调用（标题生成/记忆/分析）会被归到 llm_aux。
		response, err := r.chat.Chat(agentscope.WithCallPurpose(ctx, agentscope.CallPurposeAgent), messages, mcfg)
		if err != nil {
			ilog.Errorf("agent round %d: %v", round, err)
			state.Current = "error"
			state.Steps = append(state.Steps, fmt.Sprintf("round%d:error:%v", round, err))
			return "", state, fmt.Errorf("agent error: %w", err)
		}

		// 检查是否有工具调用（解析 JSON 格式的工具调用）
		toolCalls := r.parseToolCalls(response)
		if len(toolCalls) == 0 {
			// 没有工具调用 = 最终回答
			state.Current = "finalize"
			state.Steps = append(state.Steps, fmt.Sprintf("round%d:answer", round))
			finalResponse = response
			onText(response)
			break
		}

		// 执行工具调用
		state.Current = "act"

		for _, tc := range toolCalls {
			if err := ctx.Err(); err != nil {
				state.Current = "interrupted"
				state.Steps = append(state.Steps, "interrupted")
				return finalResponse, state, err
			}
			if ok, reason := budget.CheckBudget(); !ok {
				state.Current = "blocked"
				state.Steps = append(state.Steps, "budget:"+reason)
				break
			}
			budget.RecordToolCall()
			budget.RecordCommand()
			state.ToolCalls++
			state.Commands++
			onToolCall(ToolResult{
				Name:    tc.name,
				Success: false,
				Output:  "执行中...",
			})

			// 查找并执行工具
			result := r.dispatchTool(ctx, tc.name, tc.args, tools)
			onToolCall(result)

			// 用 InterpretObservation 把原始结果转成结构化 Interpretation，
			// 再用事实(facts)压缩成简短观察文本喂回模型，避免原始长文本在每轮被反复回传烧 token
			obs := Observation{
				ID:         fmt.Sprintf("%s#%d", tc.name, state.ToolCalls),
				Turn:       round + 1,
				Kind:       "tool_result",
				ToolName:   result.Name,
				Text:       result.Output,
				OK:         result.Success,
				IsError:    result.Error != "",
				ObservedAt: time.Now().Format(time.RFC3339),
			}
			interp := InterpretObservation(obs)
			observation := r.observationToText(result, interp)

			// assistant 回复原文也可能很长，截断后存回，防止下一轮上下文继续膨胀
			assistantMsg := truncate(response, replyTruncateLen)
			messages = append(messages, ChatMessage{Role: "assistant", Content: assistantMsg})
			messages = append(messages, ChatMessage{Role: "user", Content: observation})
		}

		if state.Current == "blocked" {
			break
		}
		state.Current = "observe"
		state.Steps = append(state.Steps, fmt.Sprintf("round%d:observe", round))
	}

	if state.Current == "blocked" {
		return finalResponse, state, fmt.Errorf("agent budget exhausted")
	}
	if finalResponse == "" {
		state.Current = "error"
		state.Steps = append(state.Steps, "no_final_response")
		return "", state, fmt.Errorf("agent 未在最大轮次内生成最终回答")
	}
	state.Current = "finalize"
	state.Steps = append(state.Steps, "done")
	return finalResponse, state, nil
}

// observationToText 把工具执行结果（含 InterpretObservation 结构化事实）压缩成喂给模型的简短观察文本。
// 优先用 Interpretation.Facts 里的事实摘要，原始长文本只做兜底且截断，最大限度省 token。
func (r *AgentRuntime) observationToText(result ToolResult, interp *Interpretation) string {
	// doc_search 等检索类工具的输出已是 LLM 聚合后的精炼文本（含出处），
	// 不走 facts 压缩分支（会被压到 300 字丢失细节与引用），直接透传。
	if result.Name == "doc_search" {
		status := map[bool]string{true: "成功", false: "失败"}[result.Success]
		var b strings.Builder
		fmt.Fprintf(&b, "工具调用结果 [doc_search]: %s\n", status)
		if result.Error != "" {
			b.WriteString("错误: ")
			b.WriteString(truncate(result.Error, 500))
		} else {
			b.WriteString(truncate(result.Output, 1400))
		}
		return b.String()
	}

	status := map[bool]string{true: "成功", false: "失败"}[result.Success]
	var b strings.Builder
	fmt.Fprintf(&b, "工具调用结果 [%s]: %s", result.Name, status)

	if len(interp.Facts) > 0 {
		// 结构化事实：只取关键文本，单条截断，避免原始 payload 回传
		for _, f := range interp.Facts {
			b.WriteString("\n- ")
			b.WriteString(truncate(f.Text, factTruncateLen))
		}
	} else if result.Output != "" {
		// 无结构化事实时，用截断后的原始输出兜底
		b.WriteString("\n")
		b.WriteString(truncate(result.Output, obsTruncateLen))
	}

	if result.Error != "" {
		b.WriteString("\n错误: ")
		b.WriteString(truncate(result.Error, 500))
	}
	return b.String()
}

// buildSystemPrompt 构建系统提示词。
func (r *AgentRuntime) buildSystemPrompt(basePrompt string, tools []AgentTool) string {
	prompt := basePrompt + "\n\n你可以使用以下工具：\n\n"
	for _, t := range tools {
		prompt += fmt.Sprintf("- **%s**: %s\n", t.Name, t.Description)
	}
	prompt += "\n当需要使用工具时，请用以下 JSON 格式回复：\n"
	prompt += `{"tool_calls":[{"name":"工具名","args":{"参数名":"值"}}]}`
	prompt += "\n\n如果不需要工具，直接回答问题即可。"
	return prompt
}

// toolCall 工具调用解析
type toolCall struct {
	name string
	args map[string]interface{}
}

// parseToolCalls 从 LLM 回复中解析工具调用。
func (r *AgentRuntime) parseToolCalls(response string) []toolCall {
	// 尝试从 JSON 中解析
	var parsed struct {
		ToolCalls []struct {
			Name string                 `json:"name"`
			Args map[string]interface{} `json:"args"`
		} `json:"tool_calls"`
	}

	// 提取 JSON 部分
	jsonStr := extractJSON(response)
	if err := json.Unmarshal([]byte(jsonStr), &parsed); err != nil {
		return nil
	}

	calls := make([]toolCall, 0, len(parsed.ToolCalls))
	for _, tc := range parsed.ToolCalls {
		if tc.Name != "" {
			calls = append(calls, toolCall{name: tc.Name, args: tc.Args})
		}
	}
	return calls
}

// dispatchTool 分发工具调用。
func (r *AgentRuntime) dispatchTool(ctx context.Context, name string, args map[string]interface{}, tools []AgentTool) ToolResult {
	for _, t := range tools {
		if t.Name == name {
			output, err := t.Execute(ctx, args)
			if err != nil {
				return ToolResult{Name: name, Success: false, Error: err.Error()}
			}
			return ToolResult{Name: name, Success: true, Output: output}
		}
	}
	return ToolResult{Name: name, Success: false, Error: fmt.Sprintf("未知工具: %s", name)}
}

// toolEmbedCache 缓存工具描述的 embedding，避免同进程内重复计算（工具描述相对固定）。
var toolEmbedCache = struct {
	mu sync.Mutex
	m  map[string][]float64
}{m: make(map[string][]float64)}

// RouteTools 根据用户意图从候选工具中挑选最相关的注入模型，避免把远程 MCP 服务器暴露的
// 全部接口都塞进 system prompt（prompt 膨胀、易误调无关接口）。
//
// 策略：内置工具（名称不含 mcp_ 前缀）全保留（数量少且常驻）；远端(MCP)工具按 query 与
// 工具描述的语义相似度排序取 topK；无 embedding 配置 / 空 query / 相似度过低时回落全部，避免漏选。
// 返回路由后的工具列表 + 被选中的工具名集合（用于同步过滤 Registry，使 EinoV2 分支路径一致）。
func (r *AgentRuntime) RouteTools(ctx context.Context, tools []AgentTool, query string, embedMcfg *ModelConfig, topK int) ([]AgentTool, map[string]bool) {
	names := make(map[string]bool, len(tools))
	if topK <= 0 {
		topK = 12
	}
	fallback := func() ([]AgentTool, map[string]bool) {
		for _, t := range tools {
			names[t.Name] = true
		}
		return tools, names
	}
	if query == "" || r.embedding == nil || embedMcfg == nil {
		return fallback()
	}
	qVec, err := r.embedding.EmbedQuery(ctx, query, embedMcfg)
	if err != nil || len(qVec) == 0 {
		ilog.Warnf("tool routing: embed query failed, fallback to all tools: %v", err)
		return fallback()
	}
	builtin := make([]AgentTool, 0, len(tools))
	remote := make([]struct {
		t AgentTool
		s float64
	}, 0)
	for _, t := range tools {
		if !strings.HasPrefix(t.Name, "mcp_") {
			builtin = append(builtin, t)
			names[t.Name] = true
			continue
		}
		vec, e := cachedToolEmbedding(ctx, r, t, embedMcfg)
		if e != nil {
			names[t.Name] = true // 远端工具 embedding 失败：保守保留，避免误杀
			continue
		}
		remote = append(remote, struct {
			t AgentTool
			s float64
		}{t, cosine(qVec, vec)})
	}
	if len(remote) == 0 {
		return fallback()
	}
	sort.SliceStable(remote, func(i, j int) bool { return remote[i].s > remote[j].s })
	out := make([]AgentTool, 0, len(builtin)+topK)
	out = append(out, builtin...)
	if len(remote) <= topK {
		for _, x := range remote {
			out = append(out, x.t)
			names[x.t.Name] = true
		}
		return out, names
	}
	// 兜底：topK 内最高相似度过低，说明 query 与任何远程工具都不相关，回落全量
	if remote[topK-1].s < 0.2 {
		ilog.Infof("tool routing: topK similarity too low, fallback to all %d tools", len(tools))
		return fallback()
	}
	for _, x := range remote[:topK] {
		out = append(out, x.t)
		names[x.t.Name] = true
	}
	ilog.Infof("tool routing: %d remote tools -> %d selected (topK=%d)", len(remote), topK, topK)
	return out, names
}

func cachedToolEmbedding(ctx context.Context, r *AgentRuntime, t AgentTool, embedMcfg *ModelConfig) ([]float64, error) {
	key := t.Name + "::" + t.Description
	toolEmbedCache.mu.Lock()
	if v, ok := toolEmbedCache.m[key]; ok {
		toolEmbedCache.mu.Unlock()
		return v, nil
	}
	toolEmbedCache.mu.Unlock()
	vec, err := r.embedding.EmbedQuery(ctx, t.Name+" "+t.Description, embedMcfg)
	if err != nil {
		return nil, err
	}
	toolEmbedCache.mu.Lock()
	toolEmbedCache.m[key] = vec
	toolEmbedCache.mu.Unlock()
	return vec, nil
}

func cosine(a, b []float64) float64 {
	if len(a) == 0 || len(b) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, na, nb float64
	for i := range a {
		dot += a[i] * b[i]
		na += a[i] * a[i]
		nb += b[i] * b[i]
	}
	if na == 0 || nb == 0 {
		return 0
	}
	return dot / (math.Sqrt(na) * math.Sqrt(nb))
}

// RegisterTools 注册请求级统一工具 Registry，并返回兼容旧运行时的 AgentTool 列表。
// Registry 同时可以通过 ToEinoTools 导出 Eino tool.BaseTool，供 ADK Builder 使用。
// newToolRegistry 构造与运行时一致的工具注册表（策略相同），供注册与计数复用。
func newToolRegistry() *toolkit.Registry {
	return toolkit.NewRegistry(toolkit.DefaultPolicy{ResolveScope: func(ctx context.Context) (bool, bool, error) {
		scope, err := agentscope.RequireScope(ctx)
		return scope.ReadOnly, scope.CanApprove, err
	}})
}

// CountAgentTools 统计智能体的「生效工具数」：工具库内置工具 + 已启用 MCP 服务器缓存的远端工具数。
//
// 这里刻意不拉远端工具清单：RegisterTools 里每个 MCP 服务器都要真实发一次 ListTools 网络请求，
// 列表页一页 20 个智能体直接调用会被拖垮。远端部分以 agent_mcp_servers.tools_count 缓存为准，
// 该缓存由「测试连接」写入、连接信息变更时清零。
func (r *AgentRuntime) CountAgentTools(ctx context.Context, st *store.Store, agentID int64) int {
	if st == nil || agentID <= 0 {
		return 0
	}
	agent, err := st.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		return 0
	}
	libIDs := parseToolLibIDs(agent.ToolLibIDs)
	builtinCount := 0
	if libIDs == nil {
		// 未配置：统计全部启用的内置工具数
		if all, err := st.ListAllEnabledBuiltinTools(ctx); err == nil {
			builtinCount = len(all)
		}
	} else {
		builtinCount = len(libIDs)
	}
	remote := 0
	if n, err := st.SumAgentMCPToolCount(ctx, agentID); err == nil {
		remote = n
	}
	return builtinCount + remote
}

func (r *AgentRuntime) loadAgentConfig(ctx context.Context, st *store.Store, agentID int64) *model.Agent {
	if agentID <= 0 || st == nil {
		return nil
	}
	agent, err := st.GetAgent(ctx, agentID)
	if err != nil || agent == nil {
		return nil
	}
	// 生效快照优先：内置工具的挂载清单受发布约束，
	// 未发布的挂载/卸载不应影响线上行为。
	if snap := model.EffectiveSnapshotFromContext(ctx); snap != nil {
		if raw, err := json.Marshal(snap.ToolLibIDs); err == nil {
			agent.ToolLibIDs = string(raw)
		}
	}
	return agent
}

// builtinToolHandlers 内置工具的执行实现映射。
// 工具定义（名称/描述/参数/元数据）从 tool_libraries 表读取，
// 实际执行逻辑通过 name 在此处查找对应的 handler。
func (r *AgentRuntime) builtinToolHandlers(ctx context.Context, st *store.Store, embedMcfg *ModelConfig) map[string]toolkit.Handler {
	knowledgeRetriever := knowledge.NewRetriever(st,
		func(c context.Context, q string) ([]float64, error) {
			return r.embedding.EmbedQuery(c, q, embedMcfg)
		},
		// 查询改写：用对话模型把用户问题改写成更利于向量召回的精准 query。
		knowledge.WithRewriteQuery(func(c context.Context, q string) (string, error) {
			chatMcfg := modelConfigFromContext(c)
			if chatMcfg == nil || r.chat == nil {
				return q, nil
			}
			out, err := r.chat.Chat(c, []ChatMessage{
				{Role: "system", Content: docSearchRewriteSystem},
				{Role: "user", Content: q},
			}, chatMcfg)
			if err != nil {
				ilog.Warnf("doc_search 查询改写失败，回落原 query: %v", err)
				return q, nil
			}
			return strings.TrimSpace(out), nil
		}),
		// 聚合压缩：用对话模型把命中片段提炼成带出处的要点，缓解「内容模糊」。
		knowledge.WithAggregate(func(c context.Context, q string, docs []*schema.Document) (string, error) {
			chatMcfg := modelConfigFromContext(c)
			if chatMcfg == nil || r.chat == nil {
				return "", fmt.Errorf("未配置对话模型，无法聚合知识片段")
			}
			var b strings.Builder
			b.WriteString("用户问题：\n")
			b.WriteString(q)
			b.WriteString("\n\n知识片段：\n")
			for i, d := range docs {
				fileName, _ := d.MetaData["file_name"].(string)
				fmt.Fprintf(&b, "[%d] 来源：%s %s\n%s\n\n", i+1, fileName, knowledge.DescribeChunkMeta(d.MetaData["metadata"]), d.Content)
			}
			out, err := r.chat.Chat(c, []ChatMessage{
				{Role: "system", Content: docSearchAggregateSystem},
				{Role: "user", Content: b.String()},
			}, chatMcfg)
			if err != nil {
				return "", err
			}
			return strings.TrimSpace(out), nil
		}),
	)
	handlers := map[string]toolkit.Handler{
		"doc_search": func(ctx context.Context, args map[string]any) (any, error) {
			return knowledgeRetriever.SearchText(ctx, knowledge.SearchInput{Query: getString(args, "query", ""), TopK: 5, Threshold: 0.45})
		},
		"search_camera": func(ctx context.Context, args map[string]any) (any, error) {
			query := getString(args, "query", "")
			if query == "" {
				return nil, fmt.Errorf("search_camera 需要提供 query 参数")
			}
			return r.searchCameraEvents(ctx, query, 5, 0.45)
		},
		"search_videos": func(ctx context.Context, args map[string]any) (any, error) {
			query := getString(args, "query", "")
			if query == "" {
				return nil, fmt.Errorf("search_videos 需要提供 query 参数")
			}
			return r.searchCameraEvents(ctx, query, 5, 0.45)
		},
		"get_time": func(context.Context, map[string]any) (any, error) {
			return time.Now().Format("2006-01-02 15:04:05"), nil
		},
		"generate_report": func(ctx context.Context, args map[string]any) (any, error) {
			return fmt.Sprintf("报告「%s」已生成", getString(args, "title", "分析报告")), nil
		},
		"write_local_file": func(ctx context.Context, args map[string]any) (any, error) {
			return r.writeLocalFile(ctx, args)
		},
	}
	// 合并运维类工具
	for name, h := range r.opsBuiltinTools(ctx, st) {
		handlers[name] = h
	}
	return handlers
}

// toolLibraryToSpec 将 ToolLibrary 模型转换为 toolkit.Spec。
// 找不到对应内置 handler 的工具会被跳过（返回 nil）。
func toolLibraryToSpec(tl *model.ToolLibrary, handler toolkit.Handler) *toolkit.Spec {
	if tl == nil || handler == nil {
		return nil
	}
	meta := toolkit.Metadata{
		ExposeToAgent: true,
		ExposeToMCP:   true,
		Source:        toolkit.SourceBuiltin,
	}
	// 解析 metadata JSON
	if tl.Metadata != "" {
		var m model.ToolLibraryMetadata
		if json.Unmarshal([]byte(tl.Metadata), &m) == nil {
			meta.ReadOnly = m.ReadOnly
			meta.SideEffect = m.SideEffect
			meta.ApprovalRequired = m.ApprovalRequired
			meta.ResourceTypes = m.ResourceTypes
		}
	}
	// 解析 parameters JSON（简化：只做展示用，运行时参数校验由 handler 自行处理）
	params := make(map[string]*schema.ParameterInfo)
	if tl.Parameters != "" {
		var raw map[string]map[string]any
		if json.Unmarshal([]byte(tl.Parameters), &raw) == nil {
			for name, p := range raw {
				info := &schema.ParameterInfo{}
				if t, ok := p["type"].(string); ok {
					switch t {
					case "string":
						info.Type = schema.String
					case "number":
						info.Type = schema.Number
					case "integer":
						info.Type = schema.Integer
					case "boolean":
						info.Type = schema.Boolean
					case "array":
						info.Type = schema.Array
					case "object":
						info.Type = schema.Object
					}
				}
				if desc, ok := p["desc"].(string); ok {
					info.Desc = desc
				}
				if req, ok := p["required"].(bool); ok {
					info.Required = req
				}
				params[name] = info
			}
		}
	}
	return &toolkit.Spec{
		Name:        tl.Name,
		Description: tl.Description,
		Parameters:  params,
		Metadata:    meta,
		Handler:     handler,
	}
}

// parseToolLibIDs 解析 Agent.ToolLibIDs（JSON 数组）。
// 返回 nil 表示未配置（使用全部内置工具），返回空 slice 表示全部禁用。
func parseToolLibIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
}

// registerToolLibraryTools 从工具库加载并注册 Agent 挂载的内置工具。
// Agent 未配置 tool_lib_ids 时加载全部启用的内置工具。
func (r *AgentRuntime) registerToolLibraryTools(ctx context.Context, st *store.Store, agentID int64, registry *toolkit.Registry, embedMcfg *ModelConfig) {
	if st == nil {
		return
	}
	agent := r.loadAgentConfig(ctx, st, agentID)
	libIDs := parseToolLibIDs(agent.ToolLibIDs)

	var libItems []*model.ToolLibrary
	var err error
	if libIDs == nil {
		// 未配置：加载全部内置工具（默认全部启用）
		libItems, err = st.ListAllEnabledBuiltinTools(ctx)
	} else if len(libIDs) > 0 {
		libItems, err = st.ListToolLibraryByIDs(ctx, libIDs)
	}
	if err != nil || len(libItems) == 0 {
		return
	}
	handlers := r.builtinToolHandlers(ctx, st, embedMcfg)
	for _, tl := range libItems {
		if tl.ToolType != "builtin" {
			continue // 目前只支持 builtin 类型，HTTP 工具后续扩展
		}
		handler, ok := handlers[tl.Name]
		if !ok {
			ilog.Warnf("tool library %s: no builtin handler found, skipped", tl.Name)
			continue
		}
		spec := toolLibraryToSpec(tl, handler)
		if spec == nil {
			continue
		}
		if err := registry.Register(spec); err != nil {
			ilog.Warnf("register tool %s: %v", tl.Name, err)
		}
	}
}

// AllToolLibraryTools 返回工具库中全部启用的内置工具（带完整定义），供管理界面展示。
func (r *AgentRuntime) AllToolLibraryTools(ctx context.Context, st *store.Store, embedMcfg *ModelConfig) []*toolkit.Spec {
	if st == nil {
		return nil
	}
	libItems, err := st.ListAllEnabledBuiltinTools(ctx)
	if err != nil || len(libItems) == 0 {
		return nil
	}
	handlers := r.builtinToolHandlers(ctx, st, embedMcfg)
	result := make([]*toolkit.Spec, 0, len(libItems))
	for _, tl := range libItems {
		handler, _ := handlers[tl.Name]
		spec := toolLibraryToSpec(tl, handler)
		if spec != nil {
			result = append(result, spec)
		}
	}
	return result
}

// RegisterTools 注册该智能体可用的全部工具：工具库内置工具 + 已启用 MCP 服务器的远端工具（实时拉取）。
func (r *AgentRuntime) RegisterTools(ctx context.Context, st *store.Store, agentID int64, embedMcfg *ModelConfig) (*toolkit.Registry, []AgentTool) {
	registry := newToolRegistry()
	r.registerToolLibraryTools(ctx, st, agentID, registry, embedMcfg)

	register := func(spec *toolkit.Spec) {
		if err := registry.Register(spec); err != nil {
			ilog.Warnf("register tool %s: %v", spec.Name, err)
		}
	}

	if agentID > 0 && st != nil {
		client := mcpclient.NewClient()
		mcpServers, err := st.ListAgentMCPServers(ctx, agentID)
		if err != nil {
			ilog.Warnf("load agent mcp servers: %v", err)
		} else {
			for _, srv := range mcpServers {
				if !srv.Enabled {
					continue
				}
				remoteTools, listErr := client.ListTools(srv)
				if listErr != nil {
					ilog.Warnf("mcp server %s list tools failed: %v", srv.Name, listErr)
					continue
				}
				for _, remote := range remoteTools {
					remote, srv := remote, srv
					inputSchema, schemaErr := toolkit.JSONSchemaFromMap(remote.InputSchema)
					if schemaErr != nil {
						ilog.Warnf("mcp server %s tool %s schema invalid: %v", srv.Name, remote.Name, schemaErr)
						continue
					}
					name := "mcp_" + sanitizeToolName(srv.Name) + "_" + remote.Name
					register(&toolkit.Spec{
						Name: name, Description: fmt.Sprintf("[MCP:%s] %s", srv.Name, remote.Description),
						JSONSchema: inputSchema,
						// 远端未知能力默认按有副作用处理；是否需人工审批由该 MCP 服务器配置决定。
						// 默认（未配置/nil）视为需审批，避免未授权执行外部工具；设为免审批则直接执行。
						Metadata: toolkit.Metadata{SideEffect: true, ApprovalRequired: mcpApprovalRequired(srv.ApprovalRequired), ExposeToAgent: true, Source: toolkit.SourceMCP},
						Handler: func(ctx context.Context, args map[string]any) (any, error) {
							out, callErr := client.CallTool(srv, remote.Name, args)
							if callErr != nil {
								return nil, fmt.Errorf("MCP 工具 %s 调用失败: %w", remote.Name, callErr)
							}
							return truncate(out, obsTruncateLen), nil
						},
					})
				}
			}
		}
	}

	// tool 型技能静态落地（Level 3）：把技能内容里声明的内置工具名集合挂载到 Agent 工具集，
	// 使 SkillKindTool 从「仅有定义」变成「真正启用对应工具」。
	r.applyToolSkills(ctx, st, agentID, registry)
	// 渐进式披露 Level 2：load_skill 内置工具，模型判断技能相关时按需加载全文。
	if agentID > 0 && st != nil {
		register(&toolkit.Spec{
			Name: "load_skill",
			Description: "按名称加载本智能体某个技能的完整内容。系统提示中的技能清单只列了名称/描述/触发条件，" +
				"当用户任务与某技能相关、需要其完整约束或正文时，调用本工具获取全文（prompt 型返回提示词正文，tool 型返回启用的工具集合）。",
			Parameters: map[string]*schema.ParameterInfo{
				"name": {Type: schema.String, Desc: "技能名称", Required: true},
			},
			Metadata: toolkit.Metadata{
				ReadOnly: true, ExposeToAgent: true, ExposeToMCP: false, Source: toolkit.SourceBuiltin,
			},
			Handler: r.skillLoadHandler(st, agentID),
		})
	}

	specs := registry.ListForAgent()
	legacyTools := make([]AgentTool, 0, len(specs))
	for _, spec := range specs {
		spec := spec
		legacyTools = append(legacyTools, AgentTool{
			Name: spec.Name, Description: spec.Description,
			Execute: func(ctx context.Context, args map[string]interface{}) (string, error) {
				return registry.InvokeJSON(ctx, spec.Name, args)
			},
		})
	}
	return registry, legacyTools
}

// mcpApprovalRequired 返回该 MCP 服务器下的工具是否需要人工审批。
// 配置为 nil（未设置）或 true 时视为需要审批（保守默认）；
// 显式 false 表示免审批，运行时由 registry 直接执行，不再挂起等待用户确认。
func mcpApprovalRequired(p *bool) bool {
	return p == nil || *p
}

// writeLocalFile 将生成内容写入平台本地输出目录，并返回可访问的 /output/ 链接。
// 路径限制在该目录内，禁止穿越到平台外，用于把 amap.html 等产物落盘并分享。
func (r *AgentRuntime) writeLocalFile(_ context.Context, args map[string]any) (any, error) {
	rel := strings.TrimSpace(getString(args, "path", ""))
	content := getString(args, "content", "")
	if rel == "" {
		return nil, fmt.Errorf("write_local_file 需要提供 path 参数（相对输出根目录的路径，如 trip2026/amap.html）")
	}
	if content == "" {
		return nil, fmt.Errorf("write_local_file 需要提供 content 参数（文件内容）")
	}
	overwrite := getBool(args, "overwrite", false)

	root := r.OutputDir
	if root == "" {
		root = fsutil.ExportsDir()
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("解析输出根目录失败: %w", err)
	}

	clean := filepath.Clean("/" + rel)        // 得到 /sub/file
	relSafe := strings.TrimPrefix(clean, "/")  // sub/file
	if relSafe == "" || relSafe == "." || strings.HasPrefix(relSafe, "..") {
		return nil, fmt.Errorf("write_local_file 非法路径: %q", rel)
	}
	target := filepath.Join(absRoot, relSafe)
	absTarget, err := filepath.Abs(target)
	if err != nil {
		return nil, fmt.Errorf("解析目标路径失败: %w", err)
	}
	if absTarget != absRoot && !strings.HasPrefix(absTarget, absRoot+string(os.PathSeparator)) {
		return nil, fmt.Errorf("write_local_file 路径越界: %q", rel)
	}

	if _, statErr := os.Stat(absTarget); statErr == nil && !overwrite {
		return nil, fmt.Errorf("文件已存在且 overwrite=false: %s", absTarget)
	}
	if mkErr := os.MkdirAll(filepath.Dir(absTarget), 0o755); mkErr != nil {
		return nil, fmt.Errorf("创建目录失败: %w", mkErr)
	}
	if wrErr := os.WriteFile(absTarget, []byte(content), 0o644); wrErr != nil {
		return nil, fmt.Errorf("写入文件失败: %w", wrErr)
	}
	info, _ := os.Stat(absTarget)
	var size int64
	if info != nil {
		size = info.Size()
	}
	urlPath := "/api/chat/exports/" + strings.ReplaceAll(relSafe, "\\", "/")
	return map[string]any{
		"path":  absTarget,
		"url":   urlPath,
		"bytes": size,
		"note":  "可通过平台访问域名拼接此 url 直接打开/分享（免登录），例如 https://你的域名" + urlPath,
	}, nil
}

// sanitizeToolName 把 MCP 服务器显示名转成工具名安全片段（去掉空格/特殊字符）。
func sanitizeToolName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else if r == ' ' || r == '-' || r == '.' {
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "srv"
	}
	return b.String()
}

// BuildAgentSystemPrompt 按「渐进式披露」拼接该智能体启用的技能清单（Level 1）：
// 只常驻 name + description + 触发条件（Summary），技能全文按需经 load_skill 工具加载（Level 2）。
//
// 业界（Claude Agent Skills）同款思路：~100 tokens/技能 的摘要常驻，命中时再取 SKILL.md 正文，
// 避免把所有启用的技能全文都塞进 system prompt（prompt 膨胀、稀释注意力、易误触发无关约束）。
//
// prompt 型技能 = 领域知识/话术/约束，摘要常驻、全文按需取；
// tool 型技能 = 工具集合引用，摘要常驻，工具已由 RegisterTools 静态挂载。
func (r *AgentRuntime) BuildAgentSystemPrompt(ctx context.Context, st *store.Store, agentID int64, base string) string {
	if agentID <= 0 || st == nil {
		return base
	}
	skills, err := st.ListAgentSkills(ctx, agentID)
	if err != nil || len(skills) == 0 {
		return base
	}
	// 按 SortOrder 排序，保证技能注入顺序稳定
	sort.SliceStable(skills, func(i, j int) bool {
		return skills[i].SortOrder < skills[j].SortOrder
	})
	var extra strings.Builder
	for _, sk := range skills {
		if !sk.Enabled {
			continue
		}
		switch sk.Kind {
		case model.SkillKindPrompt:
			if strings.TrimSpace(sk.Content) == "" {
				continue
			}
			// Level 1：摘要常驻（回退 description / content 截断）
			extra.WriteString("\n\n[技能·")
			extra.WriteString(sk.Name)
			extra.WriteString("] ")
			extra.WriteString(skillOneLineSummary(sk))
			extra.WriteString("\n  完整内容：调用 load_skill(\"")
			extra.WriteString(sk.Name)
			extra.WriteString("\") 获取后严格遵循。")
		case model.SkillKindTool:
			if strings.TrimSpace(sk.Content) == "" {
				continue
			}
			toolNames := toolSkillToolNames(sk.Content)
			if len(toolNames) == 0 {
				continue
			}
			extra.WriteString("\n\n[技能·")
			extra.WriteString(sk.Name)
			extra.WriteString("] ")
			extra.WriteString(skillOneLineSummary(sk))
			extra.WriteString("\n  启用的工具：")
			extra.WriteString(strings.Join(toolNames, "、"))
		}
	}
	if extra.Len() == 0 {
		return base
	}
	return base + "\n\n以下是本智能体启用的技能清单（仅摘要；完整内容在相关任务出现时调用 load_skill 获取）：" + extra.String()
}

// skillOneLineSummary 生成技能常驻摘要：Summary > Description > Content 截断。
func skillOneLineSummary(sk *model.AgentSkill) string {
	if s := strings.TrimSpace(sk.Summary); s != "" {
		return s
	}
	if s := strings.TrimSpace(sk.Description); s != "" {
		return s
	}
	s := strings.TrimSpace(sk.Content)
	if len(s) > 80 {
		return s[:80] + "…"
	}
	return s
}

// toolSkillToolNames 解析 tool 型技能内容（JSON 数组：内置工具名集合）。
// 内容非法时返回 nil，调用方跳过该技能。
func toolSkillToolNames(content string) []string {
	var names []string
	if json.Unmarshal([]byte(strings.TrimSpace(content)), &names) != nil {
		return nil
	}
	out := make([]string, 0, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			out = append(out, n)
		}
	}
	return out
}

// applyToolSkills 落地 tool 型技能（Level 3）：把技能内容声明的内置工具名集合挂载到 Agent 工具集。
// 使 SkillKindTool 从「仅有定义」变成「真正启用对应工具」——命中即 ExposeToAgent，
// 未注册的工具名（拼写错误/未挂载）被忽略并告警提示。
func (r *AgentRuntime) applyToolSkills(ctx context.Context, st *store.Store, agentID int64, registry *toolkit.Registry) {
	if st == nil || agentID <= 0 || registry == nil {
		return
	}
	skills, err := st.ListAgentSkills(ctx, agentID)
	if err != nil {
		return
	}
	for _, sk := range skills {
		if !sk.Enabled || sk.Kind != model.SkillKindTool {
			continue
		}
		names := toolSkillToolNames(sk.Content)
		if len(names) == 0 {
			continue
		}
		want := make(map[string]bool, len(names))
		for _, n := range names {
			want[n] = true
		}
		if hit := registry.EnableForAgent(want); hit < len(names) {
			ilog.Warnf("agent %d: tool skill %s declares %d tools, only %d registered",
				agentID, sk.Name, len(names), hit)
		}
	}
}

// skillLoadHandler 构造 load_skill 工具的 Handler（渐进式披露 Level 2）：
// 按技能名返回全文——prompt 型返回提示词正文，tool 型返回启用的工具集合 JSON。
func (r *AgentRuntime) skillLoadHandler(st *store.Store, agentID int64) toolkit.Handler {
	return func(ctx context.Context, args map[string]any) (any, error) {
		name := getString(args, "name", "")
		if name == "" {
			return nil, fmt.Errorf("load_skill 需要提供 name 参数（技能名称）")
		}
		if st == nil || agentID <= 0 {
			return nil, fmt.Errorf("load_skill 当前智能体上下文不可用")
		}
		skills, err := st.ListAgentSkills(ctx, agentID)
		if err != nil {
			return nil, fmt.Errorf("加载技能列表失败: %w", err)
		}
		for _, sk := range skills {
			if !sk.Enabled || sk.Name != name {
				continue
			}
			switch sk.Kind {
			case model.SkillKindTool:
				return "技能「" + name + "」启用的工具集合：" + sk.Content, nil
			default:
				return "技能「" + name + "」完整内容：\n" + sk.Content, nil
			}
		}
		var names []string
		for _, sk := range skills {
			if sk.Enabled {
				names = append(names, sk.Name)
			}
		}
		if len(names) == 0 {
			return nil, fmt.Errorf("未找到技能「%s」，该智能体当前没有启用的技能", name)
		}
		return nil, fmt.Errorf("未找到启用的技能「%s」，当前可用技能：%s", name, strings.Join(names, "、"))
	}
}

// searchCameraEvents 执行真实的摄像头事件混合检索：NL 解析 → 向量化 → 混合搜索 → 压缩结果文本。
// 结果文本经过 truncate，避免长列表在 Agent 多轮循环里被反复回传烧 token。
func (r *AgentRuntime) searchCameraEvents(ctx context.Context, query string, topK int, threshold float64) (string, error) {
	if r.search == nil {
		return "", fmt.Errorf("摄像头检索服务未初始化（search 为 nil）")
	}
	// 对话模型配置：用于自然语言解析
	mcfg := modelConfigFromContext(ctx)
	if mcfg == nil || mcfg.apiKey() == "" {
		return "", fmt.Errorf("请先在「系统设置 → 模型配置」中配置并激活对话模型")
	}
	// 向量模型配置：必须与对话模型分开，用对话模型名请求 /embeddings 会被网关拒绝
	emcfg := embedModelConfigFromContext(ctx)
	if emcfg == nil || emcfg.apiKey() == "" {
		return "", fmt.Errorf("请先在「系统设置 → 模型配置」中配置并激活向量模型")
	}

	// 1) 自然语言解析为结构化条件 + 向量检索意图
	condition, searchQuery, err := r.search.ParseNaturalLanguage(ctx, query, r.chat, mcfg)
	if err != nil {
		return "", fmt.Errorf("解析查询失败: %w", err)
	}

	// 2) 查询向量化（用向量模型配置）
	emb, err := r.embedding.EmbedQuery(ctx, searchQuery, emcfg)
	if err != nil {
		return "", fmt.Errorf("向量化失败: %w", err)
	}

	// 3) 服务端资源边界：只允许检索 Agent 显式绑定的摄像头事件；无绑定 fail closed。
	if r.store == nil {
		return "", fmt.Errorf("资源授权服务未初始化")
	}
	eventIDs, err := r.store.ListBoundResourceIDs(ctx, agentIDFromContext(ctx), model.ResourceTypeCameraEvent)
	if err != nil {
		return "", fmt.Errorf("加载摄像头事件授权失败: %w", err)
	}
	if len(eventIDs) == 0 {
		return "当前智能体未绑定任何摄像头事件，无法执行检索。", nil
	}
	condition.EventIDs = eventIDs

	// 4) 混合检索
	results, err := r.search.HybridSearch(ctx, emb, condition, topK, threshold)
	if err != nil {
		return "", fmt.Errorf("检索失败: %w", err)
	}

	if len(results) == 0 {
		return fmt.Sprintf("未找到与「%s」匹配的摄像头事件（topK=%d, threshold=%.2f）", query, topK, threshold), nil
	}

	// 4) 压缩为简短文本：每条只保留关键信息，整体截断，防止长结果回传烧 token
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条相关摄像头事件：\n", len(results))
	for i, res := range results {
		fmt.Fprintf(&b, "[%d] %s | %s | 相似度 %.0f%%",
			i+1,
			truncate(res.CameraName, 32),
			res.EventTime.Format("2006-01-02 15:04"),
			res.Score*100)
		// 关键标签
		var tags []string
		if res.HasPerson {
			tags = append(tags, "人:"+truncate(res.PersonDesc, 40))
		}
		if res.HasVehicle {
			tags = append(tags, "车:"+truncate(res.VehicleDesc, 40))
		}
		if res.HasPackage {
			tags = append(tags, "包裹:"+truncate(res.PackageDesc, 40))
		}
		if res.HasPet {
			tags = append(tags, "宠物:"+truncate(res.PetDesc, 40))
		}
		if res.Action != "" {
			tags = append(tags, "动作:"+res.Action)
		}
		if len(tags) > 0 {
			b.WriteString(" | ")
			b.WriteString(strings.Join(tags, "; "))
		}
		b.WriteString("\n")
	}
	return truncate(b.String(), obsTruncateLen), nil
}

func getString(args map[string]interface{}, key, def string) string {
	if v, ok := args[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return def
}

// 说明：本文件原先的 writeLLMLog 已移除。
// LLM 调用观测改由两处统一负责，避免重复记账：
//   1. ChatService.writeChatLog —— 覆盖所有经 ChatService 的调用（含标题生成、记忆摘要等辅助调用）
//   2. handler 层用量回执 —— Eino Agent 主链路（不经过 ChatService）的每次模型调用
