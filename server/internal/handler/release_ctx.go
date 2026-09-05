package handler

import (
	"context"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/ilog"
)

// ---------- 运行态：生效快照注入 ----------
//
// 智能体配置分「编辑态」与「运行态」两套：
//   - 编辑态：agents 表及其子表，管理接口直接读写，改动立即入库；
//   - 运行态：agent_releases 里已发布的不可变快照，只有点「发布新版本」才会更新。
//
// 运行链路（对话 / 检索 / 工具注册）必须先注入生效快照，
// 这样未发布的改动就不会影响线上行为；管理链路不注入，编辑页看到的永远是最新配置。

// effectiveSnapshot 取该智能体当前生效的发布快照并注入 context。
// 返回快照本身供调用方覆盖运行时参数（提示词 / 最大步骤 / 记忆开关等）。
// 从未发布过时返回 nil 快照且不注入 —— 此时为草稿预览模式，行为等同改动前。
func effectiveSnapshot(ctx context.Context, st *store.Store, agentID int64) (context.Context, *model.AgentReleaseSnapshot) {
	if agentID <= 0 || st == nil {
		return ctx, nil
	}
	snap, err := st.LoadEffectiveSnapshot(ctx, agentID)
	if err != nil {
		// 快照读取失败时保守回落到草稿，避免整个对话不可用
		ilog.Warnf("agent %d: load effective snapshot failed, fallback to draft: %v", agentID, err)
		return ctx, nil
	}
	return model.WithEffectiveSnapshot(ctx, snap), snap
}

// withEffectiveSnapshot 只注入快照、不需要快照对象时的便捷写法（检索类接口用）。
func withEffectiveSnapshot(ctx context.Context, st *store.Store, agentID int64) context.Context {
	ctx, _ = effectiveSnapshot(ctx, st, agentID)
	return ctx
}
