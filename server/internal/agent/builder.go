package agent

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
	einomodel "github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
)

// Builder 参考 aggo AgentBuilder，只负责 Eino ADK 配置组装，不重新实现 ReAct 执行循环。
type Builder struct {
	name        string
	description string
	instruction string
	model       einomodel.ToolCallingChatModel
	tools       []tool.BaseTool
	maxSteps    int
	sequential  bool
}

func NewBuilder(model einomodel.ToolCallingChatModel) *Builder {
	return &Builder{model: model, maxSteps: 8, sequential: true}
}

func (b *Builder) WithName(name string) *Builder               { b.name = name; return b }
func (b *Builder) WithDescription(description string) *Builder { b.description = description; return b }
func (b *Builder) WithInstruction(instruction string) *Builder { b.instruction = instruction; return b }
func (b *Builder) WithTools(tools ...tool.BaseTool) *Builder {
	b.tools = append(b.tools, tools...)
	return b
}
func (b *Builder) WithMaxSteps(maxSteps int) *Builder {
	if maxSteps > 0 {
		b.maxSteps = maxSteps
	}
	return b
}
func (b *Builder) ExecuteToolsSequentially(value bool) *Builder { b.sequential = value; return b }

func (b *Builder) Build(ctx context.Context) (*adk.ChatModelAgent, error) {
	if b.model == nil {
		return nil, fmt.Errorf("Eino ToolCallingChatModel 不能为空")
	}
	name := b.name
	if name == "" {
		name = "aiagent"
	}
	description := b.description
	if description == "" {
		description = "受资源边界和工具策略保护的智能体"
	}
	return adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name: name, Description: description, Instruction: b.instruction,
		Model: b.model,
		ToolsConfig: adk.ToolsConfig{ToolsNodeConfig: compose.ToolsNodeConfig{
			Tools: b.tools, ExecuteSequentially: b.sequential,
		}},
		MaxIterations: b.maxSteps,
	})
}
