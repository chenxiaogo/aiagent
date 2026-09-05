// 临时验证脚本：确认 MCP 工具的「免审批」开关是否真能让工具跳过人工确认。
// 复刻 newToolRegistry 的策略构造与 RegisterTools 里 MCP 工具的 metadata，
// 在「交互式对话 + 未预授权（canApprove=false）」场景下对比开关两种取值。
package main

import (
	"context"
	"fmt"

	agentscope "aiagent/internal/agent"
	"aiagent/internal/toolkit"

	"github.com/cloudwego/eino/schema"
)

func main() {
	// 与 service.newToolRegistry 完全一致的策略构造
	newReg := func() *toolkit.Registry {
		return toolkit.NewRegistry(toolkit.DefaultPolicy{ResolveScope: func(ctx context.Context) (bool, bool, error) {
			scope, err := agentscope.RequireScope(ctx)
			return scope.ReadOnly, scope.CanApprove, err
		}})
	}

	// 模拟一次真实的对话调用：非只读作用域，且本次没有预授权
	run := func(label string, canApprove bool) {
		ctx := agentscope.WithScope(context.Background(), agentscope.Scope{
			AgentID: 1, ReadOnly: false, CanApprove: canApprove,
		})
		fmt.Printf("—— %s（canApprove=%v）——\n", label, canApprove)
		for _, required := range []bool{false, true} {
			reg := newReg()
			name := fmt.Sprintf("mcp_demo_%v", required)
			if err := reg.Register(&toolkit.Spec{
				Name:        name,
				Description: "demo",
				Parameters:  map[string]*schema.ParameterInfo{},
				// 与 RegisterTools 中 MCP 工具一致：SideEffect=true，ApprovalRequired 由该 MCP 配置决定
				Metadata: toolkit.Metadata{
					SideEffect: true, ApprovalRequired: required,
					ExposeToAgent: true, Source: toolkit.SourceMCP,
				},
				Handler: func(context.Context, map[string]any) (any, error) { return "工具已执行", nil },
			}); err != nil {
				fmt.Printf("  注册失败: %v\n", err)
				continue
			}
			out, err := reg.Invoke(ctx, name, map[string]any{})
			if err != nil {
				fmt.Printf("  approvalRequired=%-5v → 未直接执行（进入审批/被拒）: %v\n", required, err)
				continue
			}
			fmt.Printf("  approvalRequired=%-5v → 直接执行: %v\n", required, out)
		}
	}

	run("交互式对话，未预授权", false)
	run("已预授权的会话", true)
}
