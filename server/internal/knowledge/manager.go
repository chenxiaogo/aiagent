package knowledge

import (
	"context"
	"fmt"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
)

type OwnerScope struct {
	UserID   int64
	Username string
	IsAdmin  bool
	// AgentID 请求维度的智能体过滤（0 表示不过滤）。
	// 列表/文件查询按它把结果收敛到某个智能体名下，与 owner 边界是两件事，
	// 所以既在 scope 里带上，也作为显式参数传给各方法。
	AgentID int64
}

// Manager 将知识库管理面与 Agent 检索面分离，并统一执行 owner 边界。
type Manager struct {
	store *store.Store
}

func NewManager(st *store.Store) *Manager { return &Manager{store: st} }

func (m *Manager) List(ctx context.Context, scope OwnerScope, agentID int64) ([]*model.KnowledgeBase, error) {
	return m.store.ListKnowledgeBasesScoped(ctx, scope.UserID, scope.IsAdmin, agentID)
}

func (m *Manager) Get(ctx context.Context, scope OwnerScope, id int64) (*model.KnowledgeBase, error) {
	return m.store.GetKnowledgeBaseScoped(ctx, id, scope.UserID, scope.IsAdmin)
}

func (m *Manager) Create(ctx context.Context, scope OwnerScope, agentID int64, kbType, name, description, icon string) (*model.KnowledgeBase, error) {
	if scope.UserID <= 0 || name == "" {
		return nil, fmt.Errorf("知识库名称和当前用户不能为空")
	}
	if kbType == "" {
		kbType = "general"
	}
	kb := &model.KnowledgeBase{
		Name: name, Description: description, Icon: icon, Type: kbType,
		OwnerID: scope.UserID, OwnerName: scope.Username, AgentID: agentID,
		Status: 1, CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
	if err := m.store.CreateKnowledgeBase(ctx, kb); err != nil {
		return nil, err
	}
	return kb, nil
}

func (m *Manager) Update(ctx context.Context, scope OwnerScope, id int64, apply func(*model.KnowledgeBase)) (*model.KnowledgeBase, error) {
	kb, err := m.Get(ctx, scope, id)
	if err != nil {
		return nil, err
	}
	apply(kb)
	kb.UpdatedAt = time.Now()
	if err := m.store.UpdateKnowledgeBase(ctx, kb); err != nil {
		return nil, err
	}
	return kb, nil
}

func (m *Manager) Delete(ctx context.Context, scope OwnerScope, id int64) error {
	if _, err := m.Get(ctx, scope, id); err != nil {
		return err
	}
	return m.store.DeleteKnowledgeBase(ctx, id)
}

func (m *Manager) ListFiles(ctx context.Context, scope OwnerScope, id int64, agentID int64) ([]*model.File, error) {
	if _, err := m.Get(ctx, scope, id); err != nil {
		return nil, err
	}
	return m.store.ListFiles(ctx, id, agentID)
}
