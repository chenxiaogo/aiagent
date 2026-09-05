package store

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// ---------- 智能体多模型绑定 AgentModel ----------

// ListAgentModels 列出智能体的模型绑定。
//
// 生效快照感知：context 里带快照时（运行链路）返回快照冻结的绑定，
// 不带快照时（管理界面）返回最新编辑态。改模型因此必须发布才对线上生效。
func (s *Store) ListAgentModels(ctx context.Context, agentID int64) ([]*model.AgentModel, error) {
	if snap := model.EffectiveSnapshotFromContext(ctx); snap != nil {
		// 兼容未升级的老快照：早期版本没有 ModelBindings 字段，解码后为 nil。
		// 这种极端情况下回落查库，避免模型绑定瞬间全空导致对话崩坏；
		// 正常发布的快照一定带完整绑定，不会走到这里。
		if len(snap.ModelBindings) == 0 {
			return s.listDraftAgentModels(ctx, agentID)
		}
		return s.snapshotModelBindings(agentID, snap.ModelBindings), nil
	}
	return s.listDraftAgentModels(ctx, agentID)
}

// listDraftAgentModels 返回最新编辑态的模型绑定（管理界面 / 老快照兜底用）。
func (s *Store) listDraftAgentModels(ctx context.Context, agentID int64) ([]*model.AgentModel, error) {
	var list []*model.AgentModel
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("role ASC, priority ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// snapshotModelBindings 把快照冻结的绑定还原成模型对象列表（按用途、优先级排序）。
func (s *Store) snapshotModelBindings(agentID int64, bindings []model.SnapshotModelBinding) []*model.AgentModel {
	list := make([]*model.AgentModel, 0, len(bindings))
	for i, b := range bindings {
		list = append(list, &model.AgentModel{
			ID: int64(i + 1), AgentID: agentID, ModelID: b.ModelID, Role: b.Role,
			IsPrimary: b.IsPrimary, Priority: b.Priority, Params: b.Params, Enabled: true,
		})
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Role != list[j].Role {
			return list[i].Role < list[j].Role
		}
		return list[i].Priority < list[j].Priority
	})
	return list
}

// GetAgentModel 按 ID 取绑定。
func (s *Store) GetAgentModel(ctx context.Context, id int64) (*model.AgentModel, error) {
	var item model.AgentModel
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateAgentModel 新增绑定。
func (s *Store) CreateAgentModel(ctx context.Context, item *model.AgentModel) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

// UpdateAgentModel 更新绑定。
func (s *Store) UpdateAgentModel(ctx context.Context, item *model.AgentModel) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

// DeleteAgentModel 删除绑定。
func (s *Store) DeleteAgentModel(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.AgentModel{}, id).Error
}

// ReplaceAgentModels 整表保存智能体的模型绑定，并把各用途的主模型回写到 agents 表。
//
// 用「先删后插」而不是逐行 diff：前端是一次性提交整个列表，整表替换语义最清晰，
// 且天然处理了删除行、调整顺序、改变主模型等情况。
func (s *Store) ReplaceAgentModels(ctx context.Context, agent *model.Agent, items []*model.AgentModel) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ?", agent.ID).Delete(&model.AgentModel{}).Error; err != nil {
			return err
		}
		now := time.Now()
		var chatID, embedID int64
		for _, it := range items {
			it.AgentID = agent.ID
			it.ID = 0
			it.CreatedAt, it.UpdatedAt = now, now
			if err := tx.Create(it).Error; err != nil {
				return err
			}
			if !it.Enabled {
				continue
			}
			switch it.Role {
			case model.ModelRoleChat:
				if it.IsPrimary || chatID == 0 {
					chatID = it.ModelID
				}
			case model.ModelRoleEmbedding:
				if it.IsPrimary || embedID == 0 {
					embedID = it.ModelID
				}
			}
		}
		updates := map[string]interface{}{"updated_at": now}
		if chatID > 0 {
			updates["chat_model_id"] = chatID
		}
		if embedID > 0 {
			updates["embed_model_id"] = embedID
		}
		return tx.Model(&model.Agent{}).Where("id = ?", agent.ID).Updates(updates).Error
	})
}

// EnsureAgentModelBaseline 幂等迁移：把历史 agent.chat_model_id / embed_model_id
// 转成 agent_models 里的一行（chat / embedding，主模型，优先级 1）。
func (s *Store) EnsureAgentModelBaseline(ctx context.Context) {
	var agents []*model.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		ilog.Warnf("ensure agent model baseline: list agents: %v", err)
		return
	}
	for _, a := range agents {
		var n int64
		if err := s.db.WithContext(ctx).Model(&model.AgentModel{}).
			Where("agent_id = ?", a.ID).Count(&n).Error; err != nil {
			ilog.Warnf("ensure agent model baseline: count agent %d: %v", a.ID, err)
			continue
		}
		if n > 0 {
			continue
		}
		now := time.Now()
		if a.ChatModelID > 0 {
			_ = s.db.WithContext(ctx).Create(&model.AgentModel{
				AgentID: a.ID, ModelID: a.ChatModelID, Role: model.ModelRoleChat,
				IsPrimary: true, Priority: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
			}).Error
		}
		if a.EmbedModelID > 0 {
			_ = s.db.WithContext(ctx).Create(&model.AgentModel{
				AgentID: a.ID, ModelID: a.EmbedModelID, Role: model.ModelRoleEmbedding,
				IsPrimary: true, Priority: 1, Enabled: true, CreatedAt: now, UpdatedAt: now,
			}).Error
		}
	}
}
