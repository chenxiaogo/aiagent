package toolkit

import (
	"context"
	"strings"
	"testing"
)

// 三档权限模式对副作用工具的裁决矩阵。
// 红线（blocked）无论何种模式都必须拒绝；其余按模式与风险等级决定。
//
//	manual      普通: 询问   高风险: 询问
//	delegated   普通: 执行   高风险: 拒绝
//	full_access 普通: 执行   高风险: 执行
func TestApprovalModeMatrix(t *testing.T) {
	cases := []struct {
		name      string
		mode      ApprovalMode
		risk      string
		blocked   bool
		wantRun   bool // 是否期望真正执行 handler
		wantAsk   bool // 是否期望向用户发起确认
		wantError bool // 是否期望直接拒绝
	}{
		{name: "manual+medium 需确认", mode: ModeManual, risk: "medium", wantAsk: true, wantRun: true},
		{name: "manual+high 需确认", mode: ModeManual, risk: RiskHigh, wantAsk: true, wantRun: true},
		{name: "delegated+medium 自动执行", mode: ModeDelegated, risk: "medium", wantRun: true},
		{name: "delegated+high 拒绝", mode: ModeDelegated, risk: RiskHigh, wantError: true},
		{name: "full_access+high 自动执行", mode: ModeFullAccess, risk: RiskHigh, wantRun: true},
		{name: "full_access+medium 自动执行", mode: ModeFullAccess, risk: "medium", wantRun: true},
		{name: "红线+manual 拒绝", mode: ModeManual, risk: "critical", blocked: true, wantError: true},
		{name: "红线+delegated 拒绝", mode: ModeDelegated, risk: "critical", blocked: true, wantError: true},
		{name: "红线+full_access 拒绝", mode: ModeFullAccess, risk: "critical", blocked: true, wantError: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executed := false
			asked := false

			registry := NewRegistry(DefaultPolicy{
				// 模拟交互式会话：未获预授权
				ResolveScope: func(context.Context) (bool, bool, error) { return false, false, nil },
			})
			if err := registry.Register(&Spec{
				Name: "exec_command",
				Metadata: Metadata{
					SideEffect: true, ApprovalRequired: true, ExposeToAgent: true,
				},
				Handler: func(context.Context, map[string]any) (any, error) {
					executed = true
					return "done", nil
				},
			}); err != nil {
				t.Fatalf("注册工具失败: %v", err)
			}

			ctx := context.Background()
			ctx = WithApprovalMode(ctx, tc.mode)
			ctx = WithRiskAssessor(ctx, func(string, map[string]any) (string, string, bool) {
				if tc.blocked {
					return "critical", "灾难性命令", true
				}
				return tc.risk, "需要确认", false
			})
			ctx = WithApprover(ctx, stubApprover{
				onRequest: func() {
					asked = true
				},
			})

			_, err := registry.Invoke(ctx, "exec_command", map[string]any{"command": "ls"})

			if tc.wantError && err == nil {
				t.Fatalf("期望拒绝，实际执行成功")
			}
			if !tc.wantError && err != nil {
				t.Fatalf("期望可执行，实际报错: %v", err)
			}
			if executed != tc.wantRun {
				t.Fatalf("handler 执行状态: 期望 %v，实际 %v", tc.wantRun, executed)
			}
			if asked != tc.wantAsk {
				t.Fatalf("是否询问用户: 期望 %v，实际 %v", tc.wantAsk, asked)
			}
		})
	}
}

// TestDelegatedHighRiskRejectionExplainsMode 委托模式拒绝高风险时，
// 错误信息要告诉调用方（模型）该怎么升级权限，而不是只说「拒绝」。
func TestDelegatedHighRiskRejectionExplainsMode(t *testing.T) {
	registry := NewRegistry(DefaultPolicy{
		ResolveScope: func(context.Context) (bool, bool, error) { return false, false, nil },
	})
	_ = registry.Register(&Spec{
		Name:     "exec_command",
		Metadata: Metadata{SideEffect: true, ApprovalRequired: true, ExposeToAgent: true},
		Handler:  func(context.Context, map[string]any) (any, error) { return "done", nil },
	})

	ctx := WithApprovalMode(context.Background(), ModeDelegated)
	ctx = WithRiskAssessor(ctx, func(string, map[string]any) (string, string, bool) {
		return RiskHigh, "高风险：关机", false
	})
	ctx = WithApprover(ctx, stubApprover{})

	_, err := registry.Invoke(ctx, "exec_command", map[string]any{"command": "reboot"})
	if err == nil {
		t.Fatal("委托模式应拒绝高风险操作")
	}
	if !strings.Contains(err.Error(), "完全权限") {
		t.Fatalf("拒绝原因应提示升级到完全权限，实际: %v", err)
	}
}

// TestNoApproverInManualMode 人工审批模式但没有交互通道（如后台任务）时必须拒绝，不能静默执行。
func TestNoApproverInManualMode(t *testing.T) {
	registry := NewRegistry(DefaultPolicy{
		ResolveScope: func(context.Context) (bool, bool, error) { return false, false, nil },
	})
	_ = registry.Register(&Spec{
		Name:     "write_file",
		Metadata: Metadata{SideEffect: true, ApprovalRequired: true, ExposeToAgent: true},
		Handler:  func(context.Context, map[string]any) (any, error) { return "done", nil },
	})

	ctx := WithApprovalMode(context.Background(), ModeManual)
	ctx = WithRiskAssessor(ctx, func(string, map[string]any) (string, string, bool) {
		return "medium", "写入文件", false
	})
	// 不注入 Approver

	if _, err := registry.Invoke(ctx, "write_file", map[string]any{"path": "/tmp/a"}); err == nil {
		t.Fatal("缺少交互通道时应拒绝执行")
	}
}

func TestNormalizeApprovalMode(t *testing.T) {
	cases := map[string]ApprovalMode{
		"manual":      ModeManual,
		"delegated":   ModeDelegated,
		"full_access": ModeFullAccess,
		"":            ModeManual,
		"unknown":     ModeManual,
		"FULL_ACCESS": ModeManual, // 大小写敏感，未知值一律降级到最保守
	}
	for in, want := range cases {
		if got := NormalizeApprovalMode(in); got != want {
			t.Errorf("NormalizeApprovalMode(%q) = %s, want %s", in, got, want)
		}
	}
}

// stubApprover 记录是否发起过确认，并始终批准（用于验证「有没有问过用户」）。
type stubApprover struct {
	onRequest func()
}

func (s stubApprover) RequestApproval(context.Context, ApprovalRequest) (bool, bool, string, error) {
	if s.onRequest != nil {
		s.onRequest()
	}
	return true, false, "", nil
}
