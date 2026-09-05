package agent

import (
	"context"
	"fmt"
	"time"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"

	"aiagent/internal/toolkit"
)

// RunUsage 一次 Agent 执行的用量统计，供调用观测（CallLog）落库。
type RunUsage struct {
	PromptTokens int   // 各轮 prompt token 累加（多轮会重复计入上下文，即真实消耗）
	OutputTokens int
	LLMRounds    int   // 模型被调用的轮数
	ToolCalls    int   // 工具被执行的次数
	LatencyMs    int64
	Err          error // 本次执行的最终错误，用于标记观测记录的成功/失败
}

// Runtime 是 Eino ADK 的轻量执行封装。现有兼容 Runtime 可与其并行灰度。
type Runtime struct {
	model     einomodel.ToolCallingChatModel
	registry  *toolkit.Registry
	name      string
	maxSteps  int
	usageSink func(RunUsage) // 用量回执：由接入层注入，用于写调用观测日志
}

func NewRuntime(model einomodel.ToolCallingChatModel, registry *toolkit.Registry) *Runtime {
	return &Runtime{model: model, registry: registry, name: "aiagent-eino", maxSteps: 8}
}

// WithUsageSink 注册用量回执，执行结束（无论成功、失败还是被中断）都会回调一次。
func (r *Runtime) WithUsageSink(fn func(RunUsage)) *Runtime {
	if fn != nil {
		r.usageSink = fn
	}
	return r
}

func (r *Runtime) WithName(name string) *Runtime {
	if name != "" {
		r.name = name
	}
	return r
}

func (r *Runtime) WithMaxSteps(maxSteps int) *Runtime {
	if maxSteps > 0 {
		r.maxSteps = maxSteps
	}
	return r
}

// AgentEvent 是 Agent 执行过程事件，供流式通道（WebSocket）向前端推送中间过程。
type AgentEvent struct {
	Type    string `json:"type"`    // thinking / tool_call / tool_result / text
	Name    string `json:"name"`    // 工具名
	Input   string `json:"input"`   // 工具入参（JSON 字符串）
	Output  string `json:"output"`  // 工具输出
	Content string `json:"content"` // 文本内容
}

// RunWithEvents 执行完整 ReAct 循环，并通过 onEvent 推送中间过程。
// onEvent 为 nil 时与 Run 等价。ctx 取消时立即停止迭代：
// 已产生部分答案则按中断返回，否则返回 ctx 错误。
func (r *Runtime) RunWithEvents(ctx context.Context, instruction string, messages []*schema.Message, onEvent func(AgentEvent)) (answer string, err error) {
	if r.registry == nil {
		return "", fmt.Errorf("tool registry 不能为空")
	}
	// 用量统计：正常结束、出错、被 stop 中断都要回传给上层落库，
	// 否则平台内部对话在「调用观测」里一条记录都不会有。
	start := time.Now()
	usage := RunUsage{}
	defer func() {
		usage.LatencyMs = time.Since(start).Milliseconds()
		usage.Err = err
		if r.usageSink != nil {
			r.usageSink(usage)
		}
	}()

	built, err := NewBuilder(r.model).
		WithName(r.name).
		WithInstruction(instruction).
		WithTools(r.registry.ToEinoTools()...).
		WithMaxSteps(r.maxSteps).
		ExecuteToolsSequentially(true).
		Build(ctx)
	if err != nil {
		return "", err
	}
	runner := adk.NewRunner(ctx, adk.RunnerConfig{Agent: built, EnableStreaming: false})
	iterator := runner.Run(ctx, messages)

	emit := func(event AgentEvent) {
		if onEvent != nil {
			onEvent(event)
		}
	}
	emit(AgentEvent{Type: "thinking", Content: "正在分析..."})

	answer = ""
	for {
		// 停止信号：ctx 由上游（WS 的 stop 指令）取消
		if err := ctx.Err(); err != nil {
			if answer != "" {
				return answer, nil
			}
			return "", err
		}
		event, ok := iterator.Next()
		if !ok {
			break
		}
		if event == nil {
			continue
		}
		if event.Err != nil {
			return answer, event.Err
		}
		if event.Output == nil || event.Output.MessageOutput == nil {
			continue
		}
		message, getErr := event.Output.MessageOutput.GetMessage()
		if getErr != nil {
			return "", getErr
		}
		if message == nil {
			continue
		}
		// 累加真实 token 用量（模型不返回 usage 时为 0，由上层按字符估算兜底）
		if message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
			usage.PromptTokens += message.ResponseMeta.Usage.PromptTokens
			usage.OutputTokens += message.ResponseMeta.Usage.CompletionTokens
		}
		switch {
		case len(message.ToolCalls) > 0:
			usage.LLMRounds++
			usage.ToolCalls += len(message.ToolCalls)
			for _, tc := range message.ToolCalls {
				emit(AgentEvent{Type: "tool_call", Name: tc.Function.Name, Input: tc.Function.Arguments})
			}
		case message.Role == schema.Tool:
			emit(AgentEvent{Type: "tool_result", Name: message.ToolName, Output: message.Content})
		case message.Role == schema.Assistant && message.Content != "":
			usage.LLMRounds++
			answer = message.Content
			emit(AgentEvent{Type: "text", Content: message.Content})
		}
	}
	if answer == "" {
		return "", fmt.Errorf("Eino Agent 未生成最终回答")
	}
	return answer, nil
}

// Run 使用 Eino 原生 function calling 和 ToolsNode 执行完整 ReAct 循环。
func (r *Runtime) Run(ctx context.Context, instruction string, messages []*schema.Message) (string, error) {
	return r.RunWithEvents(ctx, instruction, messages, nil)
}
