package agent

import (
	"context"
	"fmt"
)

// Scope 是服务端构造的可信运行边界，不得由模型参数或工具输入覆盖。
type Scope struct {
	TenantID  int64
	UserID    int64
	AgentID   int64
	SessionID int64

	KnowledgeBaseIDs []int64
	VideoSourceIDs   []int64
	CameraEventIDs   []int64

	// 会话作用域：运维工作台按「主机 / 主机组」开会话，取值同 model.SessionScope*。
	// 操作主机的工具优先以它为授权范围，其次才看智能体的资源绑定。
	HostScopeType string
	HostScopeID   int64

	ReadOnly   bool
	CanApprove bool
	IsAdmin    bool
	Source     string
}

type scopeContextKey struct{}

func WithScope(ctx context.Context, scope Scope) context.Context {
	return context.WithValue(ctx, scopeContextKey{}, scope)
}

func ScopeFromContext(ctx context.Context) (Scope, bool) {
	scope, ok := ctx.Value(scopeContextKey{}).(Scope)
	return scope, ok
}

func RequireScope(ctx context.Context) (Scope, error) {
	scope, ok := ScopeFromContext(ctx)
	if !ok || scope.AgentID <= 0 {
		return Scope{}, fmt.Errorf("缺少可信 Agent 运行作用域")
	}
	return scope, nil
}

// CallPurpose 一次模型调用的用途，供调用观测（CallLog）归类。
type CallPurpose string

const (
	// CallPurposeAgent 智能体主链路：用户对话真正触发的模型调用。
	CallPurposeAgent CallPurpose = "agent"
	// CallPurposeAux 辅助调用：标题生成、记忆摘要、视频/文档分析等后台调用。
	CallPurposeAux CallPurpose = "aux"
)

type callPurposeContextKey struct{}

// WithCallPurpose 声明本次模型调用的用途。
func WithCallPurpose(ctx context.Context, purpose CallPurpose) context.Context {
	return context.WithValue(ctx, callPurposeContextKey{}, purpose)
}

// CallPurposeFrom 取调用用途。
// 未显式声明时按辅助调用处理：宁可把主链路误归为辅助（只是显示位置不同），
// 也不能让大量后台调用混进主链路列表里刷屏。
func CallPurposeFrom(ctx context.Context) CallPurpose {
	if ctx == nil {
		return CallPurposeAux
	}
	if p, ok := ctx.Value(callPurposeContextKey{}).(CallPurpose); ok && p != "" {
		return p
	}
	return CallPurposeAux
}
