package model

import "context"

// ---------- 生效快照注入 ----------
//
// 编辑态（agents 表及其子表）与运行态（agent_releases 快照）分离后，
// 运行时必须知道「本次调用该用哪个版本」。这里用 context 传递，好处是：
//   - 管理接口不注入 → 读草稿，编辑页看到的永远是最新配置；
//   - 运行链路（对话 / 检索 / MCP）注入 → 只读已发布快照，未发布的改动不生效。
//
// 放在 model 包是为了让 store / service / handler / knowledge 都能引用而不产生循环依赖。

type effectiveSnapshotKey struct{}

// WithEffectiveSnapshot 把本次调用应生效的发布快照注入 context。
// snap 为 nil 时不做注入，调用方会回落为草稿模式。
func WithEffectiveSnapshot(ctx context.Context, snap *AgentReleaseSnapshot) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if snap == nil {
		return ctx
	}
	return context.WithValue(ctx, effectiveSnapshotKey{}, snap)
}

// EffectiveSnapshotFromContext 取出生效快照；未注入返回 nil（草稿模式）。
func EffectiveSnapshotFromContext(ctx context.Context) *AgentReleaseSnapshot {
	if ctx == nil {
		return nil
	}
	snap, _ := ctx.Value(effectiveSnapshotKey{}).(*AgentReleaseSnapshot)
	return snap
}
