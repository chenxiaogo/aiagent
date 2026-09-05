package store

import (
	"context"
	"strings"
	"time"

	"gorm.io/gorm"

	"aiagent/internal/model"
)

// ---------- 产品 Product ----------

// GetProductByAgentID 按智能体取产品。
func (s *Store) GetProductByAgentID(ctx context.Context, agentID int64) (*model.Product, error) {
	var p model.Product
	if err := s.db.WithContext(ctx).Where("agent_id = ?", agentID).First(&p).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// EnsureProductForAgent 取智能体对应产品，不存在则按智能体信息创建。
// 产品是售卖单元，与 Agent 一对一；首次进入「发布与交付」时自动补齐。
func (s *Store) EnsureProductForAgent(ctx context.Context, agent *model.Agent) (*model.Product, error) {
	if p, err := s.GetProductByAgentID(ctx, agent.ID); err == nil {
		return p, nil
	} else if err != gorm.ErrRecordNotFound {
		return nil, err
	}

	now := time.Now()
	p := &model.Product{
		AgentID:          agent.ID,
		AgentSlug:        agent.Slug,
		Name:             agent.Name,
		Summary:          agent.Description,
		Category:         model.NormalizeAgentCategory(agent.Category),
		DeliveryModes:    strings.Join(model.DefaultDeliveryModes(), ","),
		DefaultReleaseID: agent.CurrentReleaseID,
		Status:           model.ProductStatusDraft,
		OwnerName:        agent.OwnerName,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if len(p.Summary) > 500 {
		p.Summary = p.Summary[:500]
	}
	if err := s.db.WithContext(ctx).Create(p).Error; err != nil {
		// 并发下可能撞唯一索引，回退再查一次
		if again, e2 := s.GetProductByAgentID(ctx, agent.ID); e2 == nil {
			return again, nil
		}
		return nil, err
	}
	return p, nil
}

// UpdateProduct 更新产品。
func (s *Store) UpdateProduct(ctx context.Context, p *model.Product) error {
	p.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(p).Error
}

// UpdateProductByAgent 按智能体 ID 局部更新产品字段（回滚版本时同步默认版本用）。
func (s *Store) UpdateProductByAgent(ctx context.Context, agentID int64, updates map[string]interface{}) error {
	if len(updates) == 0 {
		return nil
	}
	return s.db.WithContext(ctx).Model(&model.Product{}).
		Where("agent_id = ?", agentID).Updates(updates).Error
}

// ListProducts 列出全部产品。
func (s *Store) ListProducts(ctx context.Context) ([]*model.Product, error) {
	var list []*model.Product
	if err := s.db.WithContext(ctx).Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ---------- 套餐 Plan ----------

// ListPlans 列出产品的套餐（按排序升序）。
func (s *Store) ListPlans(ctx context.Context, productID int64) ([]*model.Plan, error) {
	var list []*model.Plan
	if err := s.db.WithContext(ctx).
		Where("product_id = ?", productID).
		Order("sort_order ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetPlan 按 ID 取套餐。
func (s *Store) GetPlan(ctx context.Context, id int64) (*model.Plan, error) {
	var p model.Plan
	if err := s.db.WithContext(ctx).First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePlan 创建套餐。
func (s *Store) CreatePlan(ctx context.Context, p *model.Plan) error {
	now := time.Now()
	p.CreatedAt, p.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(p).Error
}

// UpdatePlan 更新套餐。
func (s *Store) UpdatePlan(ctx context.Context, p *model.Plan) error {
	p.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(p).Error
}

// DeletePlan 删除套餐。
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.Plan{}, id).Error
}

// ---------- 订阅 Subscription（客户授权） ----------

// ListAgentSubscriptions 列出某智能体产品的全部客户授权。
func (s *Store) ListAgentSubscriptions(ctx context.Context, agentID int64) ([]*model.Subscription, error) {
	var list []*model.Subscription
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ListTenantSubscriptions 列出某客户订阅的全部产品。
func (s *Store) ListTenantSubscriptions(ctx context.Context, tenantID int64) ([]*model.Subscription, error) {
	var list []*model.Subscription
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetSubscription 按 ID 取订阅。
func (s *Store) GetSubscription(ctx context.Context, id int64) (*model.Subscription, error) {
	var sub model.Subscription
	if err := s.db.WithContext(ctx).First(&sub, id).Error; err != nil {
		return nil, err
	}
	return &sub, nil
}

// CreateSubscription 创建订阅（授权客户使用某产品）。
func (s *Store) CreateSubscription(ctx context.Context, sub *model.Subscription) error {
	now := time.Now()
	sub.CreatedAt, sub.UpdatedAt = now, now
	if sub.Status == "" {
		sub.Status = model.SubscriptionStatusActive
	}
	if sub.StartedAt == nil {
		sub.StartedAt = &now
	}
	return s.db.WithContext(ctx).Create(sub).Error
}

// UpdateSubscription 更新订阅（含改钉版本、续期、取消）。
func (s *Store) UpdateSubscription(ctx context.Context, sub *model.Subscription) error {
	sub.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(sub).Error
}

// DeleteSubscription 删除订阅。
func (s *Store) DeleteSubscription(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.Subscription{}, id).Error
}

// ---------- 应用 App ----------

// ListTenantApps 列出客户的应用。
func (s *Store) ListTenantApps(ctx context.Context, tenantID int64) ([]*model.App, error) {
	var list []*model.App
	if err := s.db.WithContext(ctx).
		Where("tenant_id = ?", tenantID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// CreateApp 创建应用。
func (s *Store) CreateApp(ctx context.Context, app *model.App) error {
	now := time.Now()
	app.CreatedAt, app.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(app).Error
}

// DeleteApp 删除应用。
func (s *Store) DeleteApp(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.App{}, id).Error
}
