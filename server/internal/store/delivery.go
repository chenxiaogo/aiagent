package store

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// ---------- 客户 Tenant ----------

// ListTenants 列出全部客户（按名称升序）。
func (s *Store) ListTenants(ctx context.Context) ([]*model.Tenant, error) {
	var list []*model.Tenant
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// EnsureTenantByName 按名称取客户，不存在则创建。
// P0 没有独立的客户管理页面，凭据创建时直接按名称归属，避免留下无法创建的孤儿字段。
func (s *Store) EnsureTenantByName(ctx context.Context, name string) (*model.Tenant, error) {
	name = trimSpace(name)
	if name == "" {
		name = "默认客户"
	}
	var t model.Tenant
	err := s.db.WithContext(ctx).Where("name = ?", name).First(&t).Error
	if err == nil {
		return &t, nil
	}
	if err != gorm.ErrRecordNotFound {
		return nil, err
	}
	t = model.Tenant{
		Name:      name,
		Status:    1,
		QuotaRPM:  60,
		QuotaTPD:  10000,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&t).Error; err != nil {
		// 并发创建时可能撞唯一索引，回退为再查一次
		var again model.Tenant
		if e2 := s.db.WithContext(ctx).Where("name = ?", name).First(&again).Error; e2 == nil {
			return &again, nil
		}
		return nil, err
	}
	return &t, nil
}

// ---------- 客户 ↔ 系统用户 ----------
//
// 客户主体就是平台用户：授权「谁能调用这个智能体」= 从用户列表里挑人。
// 早先版本靠手填客户名称字符串隐式建客户，导致客户与用户是两套互不相干的实体，
// 现在统一到 UserID 上，手填路径仅作兼容保留。

// TenantItem 客户列表项：客户记录 + 关联系统用户的当前信息 + 授权计数。
type TenantItem struct {
	ID          int64  `json:"id"`
	UserID      int64  `json:"userId"`
	Username    string `json:"username"`
	Name        string `json:"name"`
	Contact     string `json:"contact"`
	Status      int    `json:"status"`
	QuotaRPM    int    `json:"quotaRpm"`
	QuotaTPD    int    `json:"quotaTpd"`
	Nickname    string `json:"nickname"`
	Email       string `json:"email"`
	UserStatus  int    `json:"userStatus"` // 0 = 用户已停用或已删除
	ClientCount int64  `json:"clientCount"`
	SubCount    int64  `json:"subCount"`
	CreatedAt   string `json:"createdAt"`
}

// UserCandidate 可作为客户的系统用户。
type UserCandidate struct {
	ID         int64  `json:"id"`
	Username   string `json:"username"`
	Nickname   string `json:"nickname"`
	Email      string `json:"email"`
	Status     int    `json:"status"`
	IsAdmin    bool   `json:"isAdmin"`
	RoleName   string `json:"roleName"`
	TenantID   int64  `json:"tenantId"` // >0 表示已经是客户
	TenantName string `json:"tenantName"`
}

// GetTenant 客户详情。
func (s *Store) GetTenant(ctx context.Context, id int64) (*model.Tenant, bool) {
	var t model.Tenant
	if err := s.db.WithContext(ctx).First(&t, id).Error; err != nil {
		return nil, false
	}
	return &t, true
}

// GetTenantByName 按名称取客户。
func (s *Store) GetTenantByName(ctx context.Context, name string) (*model.Tenant, bool) {
	var t model.Tenant
	if err := s.db.WithContext(ctx).Where("name = ?", name).First(&t).Error; err != nil {
		return nil, false
	}
	return &t, true
}

// GetTenantByUserID 按系统用户取客户。
func (s *Store) GetTenantByUserID(ctx context.Context, userID int64) (*model.Tenant, bool) {
	var t model.Tenant
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&t).Error; err != nil {
		return nil, false
	}
	return &t, true
}

// BindTenantUser 把客户绑定到指定系统用户（用于历史客户补关联）。
func (s *Store) BindTenantUser(ctx context.Context, tenantID, userID int64) error {
	u, ok := s.GetUserByID(ctx, userID)
	if !ok {
		return fmt.Errorf("用户不存在")
	}
	if exist, ok := s.GetTenantByUserID(ctx, userID); ok && exist.ID != tenantID {
		return fmt.Errorf("用户 %s 已绑定客户「%s」", u.Username, exist.Name)
	}
	return s.db.WithContext(ctx).Model(&model.Tenant{}).Where("id = ?", tenantID).
		Updates(map[string]interface{}{
			"user_id":    userID,
			"username":   u.Username,
			"updated_at": time.Now(),
		}).Error
}

// EnsureTenantForUser 按系统用户取客户，不存在则以该用户创建。
// 客户名优先取昵称、回落用户名；与既有客户重名时补用户名后缀，避免撞唯一索引。
func (s *Store) EnsureTenantForUser(ctx context.Context, userID int64) (*model.Tenant, error) {
	if userID <= 0 {
		return nil, fmt.Errorf("userId 非法")
	}
	var u model.User
	if err := s.db.WithContext(ctx).First(&u, userID).Error; err != nil {
		return nil, fmt.Errorf("用户不存在: %w", err)
	}
	// 该用户已绑过客户则直接复用，保证一人一客户
	var t model.Tenant
	if err := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&t).Error; err == nil {
		return &t, nil
	}

	name := strings.TrimSpace(u.Nickname)
	if name == "" {
		name = u.Username
	}
	if exist, _ := s.GetTenantByName(ctx, name); exist != nil && exist.UserID != u.ID {
		name = fmt.Sprintf("%s(%s)", name, u.Username)
	}
	t = model.Tenant{
		UserID:    u.ID,
		Username:  u.Username,
		Name:      name,
		Contact:   u.Email,
		Status:    1,
		QuotaRPM:  60,
		QuotaTPD:  10000,
		CreatedAt: time.Now(),
		UpdatedAt: time.Now(),
	}
	if err := s.db.WithContext(ctx).Create(&t).Error; err != nil {
		// 并发下可能撞唯一索引，回退再查一次
		var again model.Tenant
		if e2 := s.db.WithContext(ctx).Where("user_id = ?", userID).First(&again).Error; e2 == nil {
			return &again, nil
		}
		return nil, err
	}
	return &t, nil
}

// ListTenantItems 客户列表，补齐关联用户信息与授权计数。
func (s *Store) ListTenantItems(ctx context.Context) ([]*TenantItem, error) {
	var tenants []*model.Tenant
	if err := s.db.WithContext(ctx).Order("id ASC").Find(&tenants).Error; err != nil {
		return nil, err
	}
	users := s.userMap(ctx)
	out := make([]*TenantItem, 0, len(tenants))
	for _, t := range tenants {
		item := &TenantItem{
			ID: t.ID, UserID: t.UserID, Username: t.Username, Name: t.Name,
			Contact: t.Contact, Status: t.Status, QuotaRPM: t.QuotaRPM, QuotaTPD: t.QuotaTPD,
			CreatedAt: t.CreatedAt.Format("2006-01-02 15:04:05"),
		}
		if u, ok := users[t.UserID]; ok {
			item.Nickname, item.Email, item.UserStatus = u.Nickname, u.Email, u.Status
			if item.Username == "" {
				item.Username = u.Username
			}
		}
		s.db.WithContext(ctx).Model(&model.AgentClient{}).Where("tenant_id = ?", t.ID).Count(&item.ClientCount)
		s.db.WithContext(ctx).Model(&model.Subscription{}).Where("tenant_id = ?", t.ID).Count(&item.SubCount)
		out = append(out, item)
	}
	return out, nil
}

// ListTenantCandidates 可作为客户的系统用户，并标注哪些已经是客户。
func (s *Store) ListTenantCandidates(ctx context.Context) ([]*UserCandidate, error) {
	users := s.GetAllUsers(ctx)
	var tenants []model.Tenant
	s.db.WithContext(ctx).Find(&tenants)

	byUser := make(map[int64]model.Tenant, len(tenants))
	for _, t := range tenants {
		if t.UserID > 0 {
			byUser[t.UserID] = t
		}
	}
	out := make([]*UserCandidate, 0, len(users))
	for _, u := range users {
		c := &UserCandidate{
			ID: u.ID, Username: u.Username, Nickname: u.Nickname,
			Email: u.Email, Status: u.Status, IsAdmin: u.IsAdmin,
		}
		if t, ok := byUser[u.ID]; ok {
			c.TenantID, c.TenantName = t.ID, t.Name
		}
		if r, ok := s.GetRole(ctx, u.RoleID); ok {
			c.RoleName = r.Name
		}
		out = append(out, c)
	}
	return out, nil
}

// DeleteTenant 删除客户。其下仍有凭据或订阅时拒绝，避免留下孤儿授权。
func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	var cc, sc int64
	s.db.WithContext(ctx).Model(&model.AgentClient{}).Where("tenant_id = ?", id).Count(&cc)
	s.db.WithContext(ctx).Model(&model.Subscription{}).Where("tenant_id = ?", id).Count(&sc)
	if cc+sc > 0 {
		return fmt.Errorf("该客户下仍有 %d 个凭据、%d 条订阅，请先移除后再删除", cc, sc)
	}
	return s.db.WithContext(ctx).Delete(&model.Tenant{}, id).Error
}

// MigrateTenantUsers 为历史客户回填关联用户（幂等）。
//
// 早期客户靠手填名称隐式创建，与系统用户完全无关。这里按客户名去匹配用户昵称 / 用户名，
// 能对上的补上 UserID；对不上的保持 0，界面上标为「未关联用户」，需要人工绑定。
// 一个用户只允许绑一个客户，已被占用的跳过。
func (s *Store) MigrateTenantUsers(ctx context.Context) {
	var tenants []model.Tenant
	if err := s.db.WithContext(ctx).Where("user_id = ?", 0).Find(&tenants).Error; err != nil || len(tenants) == 0 {
		return
	}
	var users []model.User
	s.db.WithContext(ctx).Find(&users)

	byNick := make(map[string]*model.User, len(users))
	byName := make(map[string]*model.User, len(users))
	for i := range users {
		if n := strings.TrimSpace(users[i].Nickname); n != "" {
			byNick[n] = &users[i]
		}
		byName[users[i].Username] = &users[i]
	}

	linked := 0
	for _, t := range tenants {
		u := byNick[t.Name]
		if u == nil {
			u = byName[t.Name]
		}
		if u == nil {
			continue
		}
		var dup int64
		s.db.WithContext(ctx).Model(&model.Tenant{}).Where("user_id = ?", u.ID).Count(&dup)
		if dup > 0 {
			continue
		}
		s.db.WithContext(ctx).Model(&model.Tenant{}).Where("id = ?", t.ID).
			Updates(map[string]interface{}{"user_id": u.ID, "username": u.Username})
		linked++
	}
	if linked > 0 {
		ilog.Infof("tenant migration: %d 个历史客户已关联到系统用户", linked)
	}
}

// userMap 取全部用户，按 ID 索引。
func (s *Store) userMap(ctx context.Context) map[int64]*model.User {
	var users []*model.User
	s.db.WithContext(ctx).Find(&users)
	m := make(map[int64]*model.User, len(users))
	for _, u := range users {
		m[u.ID] = u
	}
	return m
}

// ---------- 发布版本 AgentRelease ----------

// ListAgentReleases 列出智能体的全部版本（新在前）。
func (s *Store) ListAgentReleases(ctx context.Context, agentID int64) ([]*model.AgentRelease, error) {
	var list []*model.AgentRelease
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetAgentRelease 按 ID 获取版本。
func (s *Store) GetAgentRelease(ctx context.Context, id int64) (*model.AgentRelease, error) {
	var item model.AgentRelease
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetAgentReleaseByVersion 按版本号获取。
func (s *Store) GetAgentReleaseByVersion(ctx context.Context, agentID int64, version string) (*model.AgentRelease, error) {
	var item model.AgentRelease
	if err := s.db.WithContext(ctx).
		Where("agent_id = ? AND version = ?", agentID, version).
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// NextAgentVersion 生成下一个版本号（v1 / v2 / ...）。
func (s *Store) NextAgentVersion(ctx context.Context, agentID int64) (string, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.AgentRelease{}).
		Where("agent_id = ?", agentID).Count(&n).Error; err != nil {
		return "", err
	}
	return fmt.Sprintf("v%d", n+1), nil
}

// CreateAgentRelease 写入新版本。
func (s *Store) CreateAgentRelease(ctx context.Context, item *model.AgentRelease) error {
	return s.db.WithContext(ctx).Create(item).Error
}

// SetDefaultRelease 把某版本设为默认（latest），同时把该智能体其它版本取消默认。
func (s *Store) SetDefaultRelease(ctx context.Context, agentID, releaseID int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.AgentRelease{}).
			Where("agent_id = ? AND id <> ?", agentID, releaseID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AgentRelease{}).
			Where("id = ?", releaseID).
			Updates(map[string]interface{}{"is_default": true, "status": model.ReleaseStatusPublished}).Error; err != nil {
			return err
		}
		return tx.Model(&model.Agent{}).Where("id = ?", agentID).
			Updates(map[string]interface{}{"current_release_id": releaseID, "published_at": time.Now()}).Error
	})
}

// ArchiveAgentRelease 归档版本（历史版本不再作为默认）。
func (s *Store) ArchiveAgentRelease(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Model(&model.AgentRelease{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{"status": model.ReleaseStatusArchived, "is_default": false}).Error
}

// ---------- 访问凭据 AgentClient ----------

// ListAgentClients 列出智能体的访问凭据。
func (s *Store) ListAgentClients(ctx context.Context, agentID int64) ([]*model.AgentClient, error) {
	var list []*model.AgentClient
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("id DESC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetAgentClient 按 ID 获取凭据。
func (s *Store) GetAgentClient(ctx context.Context, id int64) (*model.AgentClient, error) {
	var item model.AgentClient
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetAgentClientByPlainKey 用明文 Key 反查凭据（对外鉴权入口）。
func (s *Store) GetAgentClientByPlainKey(ctx context.Context, plainKey string) (*model.AgentClient, error) {
	var item model.AgentClient
	if err := s.db.WithContext(ctx).
		Where("key_hash = ?", HashClientKey(plainKey)).
		First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// CreateAgentClient 创建凭据。
func (s *Store) CreateAgentClient(ctx context.Context, item *model.AgentClient) error {
	return s.db.WithContext(ctx).Create(item).Error
}

// UpdateAgentClient 更新凭据。
func (s *Store) UpdateAgentClient(ctx context.Context, item *model.AgentClient) error {
	return s.db.WithContext(ctx).Save(item).Error
}

// DeleteAgentClient 删除凭据。
func (s *Store) DeleteAgentClient(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.AgentClient{}, id).Error
}

// TouchAgentClientUsedAt 刷新最后使用时间（异步，失败不影响主流程）。
func (s *Store) TouchAgentClientUsedAt(id int64) {
	now := time.Now()
	_ = s.db.Model(&model.AgentClient{}).Where("id = ?", id).
		Updates(map[string]interface{}{"last_used_at": now, "updated_at": now}).Error
}

// HashClientKey 计算凭据 Key 的 SHA-256（明文不落库）。
func HashClientKey(plainKey string) string {
	sum := sha256.Sum256([]byte(plainKey))
	return hex.EncodeToString(sum[:])
}

// ---------- 智能体对外路径 ----------

// GetAgentBySlug 按对外 slug 获取智能体。
func (s *Store) GetAgentBySlug(ctx context.Context, slug string) (*model.Agent, error) {
	var item model.Agent
	if err := s.db.WithContext(ctx).Where("slug = ?", slug).First(&item).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// ---------- 发布快照 ----------

// BuildAgentReleaseSnapshot 把智能体当前的编辑态冻结成一份运行快照。
//
// 快照是发布流程的唯一载体：运行时（站内对话 + 对外 MCP）只认它。
// 因此这里必须用「不带生效快照的 context」调用，否则会把自己套进上一次发布的内容里。
func (s *Store) BuildAgentReleaseSnapshot(ctx context.Context, agent *model.Agent) (*model.AgentReleaseSnapshot, error) {
	// 草稿上下文：清掉可能已注入的生效快照，保证读到的是最新编辑态
	draftCtx := model.WithEffectiveSnapshot(context.Background(), nil)

	snap := &model.AgentReleaseSnapshot{
		AgentID:         agent.ID,
		AgentName:       agent.Name,
		AgentDesc:       agent.Description,
		Avatar:          agent.Avatar,
		Category:        model.NormalizeAgentCategory(agent.Category),
		Visibility:      agent.Visibility,
		Prompt:          agent.Prompt,
		ChatModelID:     agent.ChatModelID,
		EmbedModelID:    agent.EmbedModelID,
		RuntimeType:     defaultString(agent.RuntimeType, model.AgentRuntimeEinoV2),
		MaxSteps:        agent.MaxSteps,
		MemoryEnabled:   agent.MemoryEnabled,
		MemoryParams:    agent.MemoryParams,
		ToolLibIDs:      parseSnapshotToolLibIDs(agent.ToolLibIDs),
		ExposedTools:    model.DefaultExposedTools(agent.Category),
		Policy:          model.DefaultPolicy(),
		Skills:          []model.SnapshotSkill{},
		MCPServers:      []model.SnapshotMCPServer{},
		ModelBindings:   []model.SnapshotModelBinding{},
		PresetQuestions: []string{},
	}
	if snap.MaxSteps <= 0 {
		snap.MaxSteps = 8
	}

	if qs, err := s.ListAgentPresetQuestions(draftCtx, agent.ID); err == nil {
		for _, q := range qs {
			if q.IsActive {
				snap.PresetQuestions = append(snap.PresetQuestions, q.Question)
			}
		}
	} else {
		ilog.Warnf("build snapshot: list preset questions: %v", err)
	}

	skills, err := s.ListAgentSkills(draftCtx, agent.ID)
	if err != nil {
		return nil, err
	}
	for _, sk := range skills {
		if !sk.Enabled {
			continue
		}
		snap.Skills = append(snap.Skills, model.SnapshotSkill{
			Name: sk.Name, Kind: sk.Kind, Description: sk.Description,
			Summary: sk.Summary, Content: sk.Content, SortOrder: sk.SortOrder,
		})
	}

	mcps, err := s.ListAgentMCPServers(draftCtx, agent.ID)
	if err != nil {
		return nil, err
	}
	for _, m := range mcps {
		if !m.Enabled {
			continue
		}
		snap.MCPServers = append(snap.MCPServers, model.SnapshotMCPServer{
			Name: m.Name, Transport: m.Transport, URL: m.URL,
			Headers: m.Headers, ApprovalRequired: m.ApprovalRequired,
		})
	}

	// 冻结模型绑定：改模型同样要发布才生效，否则线上会在无人操作的情况下换模型。
	if bindings, err := s.ListAgentModels(draftCtx, agent.ID); err == nil {
		for _, b := range bindings {
			if !b.Enabled {
				continue
			}
			snap.ModelBindings = append(snap.ModelBindings, model.SnapshotModelBinding{
				ModelID: b.ModelID, Role: b.Role, IsPrimary: b.IsPrimary,
				Priority: b.Priority, Params: b.Params,
			})
		}
	} else {
		ilog.Warnf("build snapshot: list agent models: %v", err)
	}

	// 冻结资源绑定：把智能体显式绑定的知识库/视频源/摄像头事件 ID 写入快照，
	// MCP 工具只检索这些被绑定的数据，杜绝跨 Agent 数据泄漏。
	res, err := s.ListAgentResources(draftCtx, agent.ID)
	if err != nil {
		ilog.Warnf("build snapshot: list agent resources: %v", err)
	} else {
		for _, r := range res {
			switch r.ResourceType {
			case model.ResourceTypeKnowledgeBase:
				snap.Resources.KnowledgeBaseIDs = append(snap.Resources.KnowledgeBaseIDs, r.ResourceID)
			case model.ResourceTypeVideoSource:
				snap.Resources.VideoSourceIDs = append(snap.Resources.VideoSourceIDs, r.ResourceID)
			case model.ResourceTypeCameraEvent:
				snap.Resources.CameraEventIDs = append(snap.Resources.CameraEventIDs, r.ResourceID)
			case model.ResourceTypeHostGroup:
				snap.Resources.HostGroupIDs = append(snap.Resources.HostGroupIDs, r.ResourceID)
			}
		}
	}
	return snap, nil
}

// parseSnapshotToolLibIDs 解析 agent.tool_lib_ids（JSON 数组）。
// 解析失败或为空返回 nil（表示沿用全部内置工具），与运行时的 parseToolLibIDs 语义一致。
func parseSnapshotToolLibIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	return ids
}

// ---------- 生效版本：运行时的唯一数据源 ----------

// LoadEffectiveRelease 取该智能体当前生效的发布版本；从未发布过返回 nil。
//
// 优先级与对外交付保持一致：agent.current_release_id → 最近一次发布。
func (s *Store) LoadEffectiveRelease(ctx context.Context, agentID int64) (*model.AgentRelease, error) {
	if agentID <= 0 {
		return nil, nil
	}
	var agent model.Agent
	if err := s.db.WithContext(ctx).Select("id", "current_release_id").First(&agent, agentID).Error; err != nil {
		return nil, err
	}
	if agent.CurrentReleaseID > 0 {
		rel, err := s.GetAgentRelease(ctx, agent.CurrentReleaseID)
		if err == nil && rel != nil && rel.Status != model.ReleaseStatusArchived {
			return rel, nil
		}
	}
	// 兜底：取最近一次发布（current_release_id 缺失或被归档的场景）
	var rel model.AgentRelease
	if err := s.db.WithContext(ctx).
		Where("agent_id = ? AND status = ?", agentID, model.ReleaseStatusPublished).
		Order("id DESC").First(&rel).Error; err != nil {
		return nil, nil // 从未发布
	}
	return &rel, nil
}

// LoadEffectiveSnapshot 取该智能体当前生效的运行快照；从未发布过返回 nil。
//
// 返回 nil 即「草稿模式」：此时该智能体还没走过发布流程，
// 运行时回落到 agents 主表（编辑态），管理界面会给出未发布提示。
func (s *Store) LoadEffectiveSnapshot(ctx context.Context, agentID int64) (*model.AgentReleaseSnapshot, error) {
	rel, err := s.LoadEffectiveRelease(ctx, agentID)
	if err != nil || rel == nil {
		return nil, nil
	}
	snap, err := model.DecodeAgentReleaseSnapshot(rel.Snapshot)
	if err != nil {
		return nil, err
	}
	// 老版本快照缺字段（如 runtimeType）时补齐默认值，避免运行时读到空配置
	snap.Normalize()
	return snap, nil
}

// DraftReleaseDiff 对比「当前编辑态」与「线上生效版本」，产出可展示的变更清单。
// 从未发布过时，base 为 nil，清单即为全部配置的初始清单。
func (s *Store) DraftReleaseDiff(ctx context.Context, agent *model.Agent) ([]model.ReleaseChange, error) {
	next, err := s.BuildAgentReleaseSnapshot(ctx, agent)
	if err != nil {
		return nil, err
	}
	rel, err := s.LoadEffectiveRelease(ctx, agent.ID)
	if err != nil || rel == nil {
		return model.DiffReleaseSnapshots(nil, next), nil
	}
	base, err := model.DecodeAgentReleaseSnapshot(rel.Snapshot)
	if err != nil {
		// 快照损坏：按「全量新增」展示，引导重新发布覆盖
		return model.DiffReleaseSnapshots(nil, next), nil
	}
	base.Normalize()
	return model.DiffReleaseSnapshots(base, next), nil
}

// HasUnpublishedChanges 判断当前配置与已发布版本是否存在差异。
//
// 做法：按当前编辑态重建一次快照，与默认版本的快照逐字节比较。
// 这样不需要在每个配置写入点挂钩子（易漏），结果永远与真实配置一致。
func (s *Store) HasUnpublishedChanges(ctx context.Context, agent *model.Agent) (bool, error) {
	if agent.CurrentReleaseID == 0 {
		return true, nil
	}
	rel, err := s.GetAgentRelease(ctx, agent.CurrentReleaseID)
	if err != nil {
		// 默认版本查不到（被手工清理等），视为有改动，引导重新发布
		return true, nil
	}
	snap, err := s.BuildAgentReleaseSnapshot(ctx, agent)
	if err != nil {
		return false, err
	}
	raw, err := snap.Encode()
	if err != nil {
		return false, err
	}
	return raw != rel.Snapshot, nil
}

// ResolveReleaseForClient 解析某凭据本次调用应该用哪个版本。
// 优先级：凭据钉版本 > 客户订阅钉版本 > 智能体默认版本(latest) > 最近一次发布。
// 通过「订阅钉版本」实现同一 Agent 不同客户跑不同版本的隔离能力。
func (s *Store) ResolveReleaseForClient(ctx context.Context, agent *model.Agent, client *model.AgentClient) (*model.AgentRelease, error) {
	if client.PinnedVersion != "" {
		return s.GetAgentReleaseByVersion(ctx, agent.ID, client.PinnedVersion)
	}
	if client.TenantID > 0 {
		var sub model.Subscription
		err := s.db.WithContext(ctx).
			Where("tenant_id = ? AND agent_id = ? AND status = ?",
				client.TenantID, agent.ID, model.SubscriptionStatusActive).
			Order("id DESC").First(&sub).Error
		if err == nil && sub.PinnedReleaseID > 0 {
			if rel, e2 := s.GetAgentRelease(ctx, sub.PinnedReleaseID); e2 == nil {
				return rel, nil
			}
		}
	}
	if agent.CurrentReleaseID > 0 {
		if rel, err := s.GetAgentRelease(ctx, agent.CurrentReleaseID); err == nil {
			return rel, nil
		}
	}
	// 兜底：取最近发布的一个版本
	var rel model.AgentRelease
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agent.ID).
		Order("id DESC").First(&rel).Error; err != nil {
		return nil, err
	}
	return &rel, nil
}

// ---------- 计量 Usage ----------

// UsageEvent 一次调用的计量事件，异步写入 usage_records。
type UsageEvent struct {
	AgentID    int64
	ClientID   int64
	TenantID   int64
	ReleaseID  int64
	Protocol   string
	Requests   int
	Errors     int
	ToolCalls  int
	TokensIn   int
	TokensOut  int
	LatencyMs  int64
}

// RecordUsage 记录一次调用计量。非阻塞：队列满时丢弃并告警，绝不影响业务链路。
func (s *Store) RecordUsage(e UsageEvent) {
	if s.usageCh == nil {
		return
	}
	e.Protocol = defaultString(e.Protocol, model.ProtocolMCP)
	select {
	case s.usageCh <- e:
	default:
		ilog.Warn("usage queue full, drop usage event")
	}
}

// usageWorker 串行消费计量事件，保证 read-modify-write 无并发竞争。
func (s *Store) usageWorker() {
	for e := range s.usageCh {
		s.applyUsage(e)
	}
}

func (s *Store) applyUsage(e UsageEvent) {
	ctx := context.Background()
	day := time.Now().Format("2006-01-02")

	var rec model.UsageRecord
	err := s.db.WithContext(ctx).
		Where("client_id = ? AND day = ? AND protocol = ?", e.ClientID, day, e.Protocol).
		First(&rec).Error
	if err == gorm.ErrRecordNotFound {
		rec = model.UsageRecord{
			TenantID: e.TenantID, AgentID: e.AgentID, ClientID: e.ClientID,
			ReleaseID: e.ReleaseID, Day: day, Protocol: e.Protocol,
			Requests: e.Requests, Errors: e.Errors, ToolCalls: e.ToolCalls,
			TokensIn: e.TokensIn, TokensOut: e.TokensOut, LatencyMs: e.LatencyMs,
			UpdatedAt: time.Now(),
		}
		if err := s.db.WithContext(ctx).Create(&rec).Error; err != nil {
			ilog.Warnf("create usage record: %v", err)
		}
		return
	}
	if err != nil {
		ilog.Warnf("query usage record: %v", err)
		return
	}
	if err := s.db.WithContext(ctx).Model(&model.UsageRecord{}).Where("id = ?", rec.ID).
		Updates(map[string]interface{}{
			"requests":   gorm.Expr("requests + ?", e.Requests),
			"errors":     gorm.Expr("errors + ?", e.Errors),
			"tool_calls": gorm.Expr("tool_calls + ?", e.ToolCalls),
			"tokens_in":  gorm.Expr("tokens_in + ?", e.TokensIn),
			"tokens_out": gorm.Expr("tokens_out + ?", e.TokensOut),
			"latency_ms": gorm.Expr("latency_ms + ?", e.LatencyMs),
			"updated_at": time.Now(),
		}).Error; err != nil {
		ilog.Warnf("update usage record: %v", err)
	}
}

// UsageSummary 用量汇总（供「发布与交付」页展示）。
type UsageSummary struct {
	TotalRequests int64           `json:"totalRequests"`
	TotalErrors   int64           `json:"totalErrors"`
	TotalToolCall int64           `json:"totalToolCalls"`
	TotalTokens   int64           `json:"totalTokens"`
	AvgLatencyMs  int64           `json:"avgLatencyMs"`
	TodayRequests int64           `json:"todayRequests"`
	ByDay         []UsageDayPoint `json:"byDay"`
}

// UsageDayPoint 单日用量点。
type UsageDayPoint struct {
	Day       string `json:"day"`
	Requests  int64  `json:"requests"`
	Errors    int64  `json:"errors"`
	ToolCalls int64  `json:"toolCalls"`
}

// SumAgentUsage 汇总智能体近 N 天用量。
func (s *Store) SumAgentUsage(ctx context.Context, agentID int64, days int) (*UsageSummary, error) {
	if days <= 0 {
		days = 7
	}
	since := time.Now().AddDate(0, 0, -(days - 1)).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")

	sum := &UsageSummary{ByDay: []UsageDayPoint{}}

	type row struct {
		Requests  int64
		Errors    int64
		ToolCalls int64
		TokensIn  int64
		TokensOut int64
		LatencyMs int64
	}
	var total row
	if err := s.db.WithContext(ctx).Model(&model.UsageRecord{}).
		Select("COALESCE(SUM(requests),0) as requests, COALESCE(SUM(errors),0) as errors, "+
			"COALESCE(SUM(tool_calls),0) as tool_calls, COALESCE(SUM(tokens_in),0) as tokens_in, "+
			"COALESCE(SUM(tokens_out),0) as tokens_out, COALESCE(SUM(latency_ms),0) as latency_ms").
		Where("agent_id = ? AND day >= ?", agentID, since).
		Scan(&total).Error; err != nil {
		return nil, err
	}
	sum.TotalRequests = total.Requests
	sum.TotalErrors = total.Errors
	sum.TotalToolCall = total.ToolCalls
	sum.TotalTokens = total.TokensIn + total.TokensOut
	if total.Requests > 0 {
		sum.AvgLatencyMs = total.LatencyMs / total.Requests
	}

	_ = s.db.WithContext(ctx).Model(&model.UsageRecord{}).
		Select("COALESCE(SUM(requests),0)").
		Where("agent_id = ? AND day = ?", agentID, today).
		Scan(&sum.TodayRequests).Error

	var points []UsageDayPoint
	if err := s.db.WithContext(ctx).Model(&model.UsageRecord{}).
		Select("day, COALESCE(SUM(requests),0) as requests, COALESCE(SUM(errors),0) as errors, COALESCE(SUM(tool_calls),0) as tool_calls").
		Where("agent_id = ? AND day >= ?", agentID, since).
		Group("day").Order("day ASC").
		Scan(&points).Error; err != nil {
		return nil, err
	}
	sum.ByDay = points
	return sum, nil
}

// ---------- 智能体资源绑定 AgentResource ----------

// ListAgentResources 列出智能体全部资源绑定。
func (s *Store) ListAgentResources(ctx context.Context, agentID int64) ([]*model.AgentResource, error) {
	var list []*model.AgentResource
	if err := s.db.WithContext(ctx).
		Where("agent_id = ?", agentID).
		Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// SetAgentResources 以「先删后插」方式重置智能体的资源绑定。
// resourceIDs 为 nil/空表示该类型不绑定任何资源。
func (s *Store) SetAgentResources(ctx context.Context, agentID int64, resourceType string, resourceIDs []int64) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("agent_id = ? AND resource_type = ?", agentID, resourceType).
			Delete(&model.AgentResource{}).Error; err != nil {
			return err
		}
		now := time.Now()
		for _, rid := range resourceIDs {
			if rid <= 0 {
				continue
			}
			if err := tx.Create(&model.AgentResource{
				AgentID: agentID, ResourceType: resourceType, ResourceID: rid, CreatedAt: now,
			}).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// ListBoundResourceIDs 列出智能体某类型的已绑定资源 ID 集合。
//
// 生效快照感知：context 里带快照时（运行链路）返回快照冻结的资源，
// 不带快照时（管理界面）返回最新编辑态。资源绑定改动因此必须发布才对线上生效，
// 否则会出现「界面上还没绑定，客户已经能搜到这份数据」的越权检索。
func (s *Store) ListBoundResourceIDs(ctx context.Context, agentID int64, resourceType string) ([]int64, error) {
	if snap := model.EffectiveSnapshotFromContext(ctx); snap != nil {
		return snap.SnapshotResourceIDs(resourceType), nil
	}
	var ids []int64
	if err := s.db.WithContext(ctx).Model(&model.AgentResource{}).
		Where("agent_id = ? AND resource_type = ?", agentID, resourceType).
		Pluck("resource_id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// ListAgentResourcesForType 列出某类型全部绑定（供迁移脚本按资源归属批量写）。
func (s *Store) ListAgentResourcesForType(ctx context.Context, resourceType string) ([]*model.AgentResource, error) {
	var list []*model.AgentResource
	if err := s.db.WithContext(ctx).
		Where("resource_type = ?", resourceType).
		Order("id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// CountProcessedCameraEvents 统计已处理的摄像头事件数（发布校验用）。
func (s *Store) CountProcessedCameraEvents(ctx context.Context) (int64, error) {
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.CameraEvent{}).
		Where("processed = ?", true).Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// CountFilesInKnowledgeBases 统计指定知识库下已完成索引的文件数（发布校验用）。
// 不使用 KnowledgeBase.FileCount：该字段是冗余计数，上传链路并未维护，始终为 0。
func (s *Store) CountFilesInKnowledgeBases(ctx context.Context, kbIDs []int64) (int64, error) {
	if len(kbIDs) == 0 {
		return 0, nil
	}
	var n int64
	if err := s.db.WithContext(ctx).Model(&model.File{}).
		Where("knowledge_id IN ? AND status = ?", kbIDs, model.FileStatusReady).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// SumClientRequestsToday 统计凭据当日累计请求数，用于 TPD 配额的持久化基线。
// 内存计数重启会丢失，因此每次跨天（或冷启动）时用库里的值做基线。
func (s *Store) SumClientRequestsToday(ctx context.Context, clientID int64) int64 {
	var n int64
	_ = s.db.WithContext(ctx).Model(&model.UsageRecord{}).
		Select("COALESCE(SUM(requests),0)").
		Where("client_id = ? AND day = ?", clientID, time.Now().Format("2006-01-02")).
		Scan(&n).Error
	return n
}

// ClientUsageStat 单个凭据的累计用量（凭据列表页展示）。
type ClientUsageStat struct {
	ClientID  int64 `json:"clientId"`
	Requests  int64 `json:"requests"`
	Errors    int64 `json:"errors"`
	ToolCalls int64 `json:"toolCalls"`
}

// ListAgentClientUsage 统计智能体下各凭据的累计用量。
func (s *Store) ListAgentClientUsage(ctx context.Context, agentID int64) (map[int64]ClientUsageStat, error) {
	var rows []ClientUsageStat
	if err := s.db.WithContext(ctx).Model(&model.UsageRecord{}).
		Select("client_id, COALESCE(SUM(requests),0) as requests, COALESCE(SUM(errors),0) as errors, COALESCE(SUM(tool_calls),0) as tool_calls").
		Where("agent_id = ?", agentID).
		Group("client_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]ClientUsageStat, len(rows))
	for _, r := range rows {
		m[r.ClientID] = r
	}
	return m, nil
}

// ---------- 启动期迁移 ----------

// EnsureAgentDeliveryBaseline 幂等补齐对外交付所需的基础数据：
//  1. 为历史智能体生成 slug / 可见性；
//  2. 为从未发布过的智能体生成 v1 快照并设为默认版本。
//
// 多次执行无副作用，已存在的 slug 与版本不会被覆盖。
func (s *Store) EnsureAgentDeliveryBaseline(ctx context.Context) {
	var agents []*model.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		ilog.Warnf("ensure delivery baseline: list agents: %v", err)
		return
	}
	for _, a := range agents {
		updates := map[string]interface{}{}
		if a.Slug == "" {
			updates["slug"] = model.Slugify(a.Name, a.ID)
		}
		if a.Visibility == "" {
			updates["visibility"] = model.VisibilityPrivate
		}
		if len(updates) > 0 {
			if err := s.db.WithContext(ctx).Model(&model.Agent{}).Where("id = ?", a.ID).Updates(updates).Error; err != nil {
				ilog.Warnf("ensure delivery baseline: update agent %d: %v", a.ID, err)
				continue
			}
			if v, ok := updates["slug"].(string); ok {
				a.Slug = v
			}
			if v, ok := updates["visibility"].(string); ok {
				a.Visibility = v
			}
		}

		var n int64
		if err := s.db.WithContext(ctx).Model(&model.AgentRelease{}).
			Where("agent_id = ?", a.ID).Count(&n).Error; err != nil {
			ilog.Warnf("ensure delivery baseline: count releases of agent %d: %v", a.ID, err)
			continue
		}

		// 资源绑定迁移：引入 agent_resources 表之前，MCP 工具直接检索全平台数据。
		// 为避免历史智能体在新隔离机制下突然搜不到数据，这里在「该 Agent 没有任何绑定」时，
		// 按分类把全量对应资源绑给它（等价于旧行为），后续由管理员在界面上收敛。
		s.migrateAgentResources(ctx, a)

		if n == 0 {
			if err := s.publishRelease(ctx, a, "v1", "初始化发布"); err != nil {
				ilog.Warnf("ensure delivery baseline: publish v1 of agent %d: %v", a.ID, err)
			}
		}
	}
}

// UpgradeAgentReleaseSnapshots 把历史快照升级到当前格式（幂等，仅重写缺失新字段的快照）。
//
// 背景：快照结构随迭代新增了运行参数、模型绑定、工具挂载、MCP 请求头等要素。
// 在此之前运行时直接读 agents 主表，历史 v1 快照里没有这些字段；
// 若直接切到「运行时只读快照」，存量智能体会集体退化。
// 因此启动时按当前编辑态原地重建一次老快照 —— 在此之前线上行为本来就等同草稿，
// 这次重建只是把「隐式生效的草稿」显式固化成版本，不改变任何实际行为。
func (s *Store) UpgradeAgentReleaseSnapshots(ctx context.Context) {
	var agents []*model.Agent
	if err := s.db.WithContext(ctx).Find(&agents).Error; err != nil {
		ilog.Warnf("upgrade release snapshots: list agents: %v", err)
		return
	}
	for _, a := range agents {
		var rels []*model.AgentRelease
		if err := s.db.WithContext(ctx).Where("agent_id = ?", a.ID).Find(&rels).Error; err != nil {
			ilog.Warnf("upgrade release snapshots: list releases of agent %d: %v", a.ID, err)
			continue
		}
		for _, rel := range rels {
			// 已含新字段说明是新版快照，跳过（幂等）
			if strings.Contains(rel.Snapshot, `"runtimeType"`) {
				continue
			}
			snap, err := s.BuildAgentReleaseSnapshot(ctx, a)
			if err != nil {
				ilog.Warnf("upgrade release snapshots: build snapshot of agent %d: %v", a.ID, err)
				continue
			}
			raw, err := snap.Encode()
			if err != nil {
				ilog.Warnf("upgrade release snapshots: encode snapshot of agent %d: %v", a.ID, err)
				continue
			}
			if err := s.db.WithContext(ctx).Model(&model.AgentRelease{}).
				Where("id = ?", rel.ID).Update("snapshot", raw).Error; err != nil {
				ilog.Warnf("upgrade release snapshots: update release %d: %v", rel.ID, err)
			}
		}
	}
}

// migrateAgentResources 为历史智能体补齐资源绑定（幂等：已有任意绑定则跳过）。
func (s *Store) migrateAgentResources(ctx context.Context, a *model.Agent) {
	var cnt int64
	if err := s.db.WithContext(ctx).Model(&model.AgentResource{}).
		Where("agent_id = ?", a.ID).Count(&cnt).Error; err != nil {
		ilog.Warnf("migrate agent resources: count of agent %d: %v", a.ID, err)
		return
	}
	if cnt > 0 {
		return
	}

	cat := model.NormalizeAgentCategory(a.Category)
	bind := func(rtype string, ids []int64) {
		if len(ids) == 0 {
			return
		}
		if err := s.SetAgentResources(ctx, a.ID, rtype, ids); err != nil {
			ilog.Warnf("migrate agent resources: bind %s to agent %d: %v", rtype, a.ID, err)
		}
	}

	switch cat {
	case model.AgentCategoryDoc:
		var ids []int64
		s.db.WithContext(ctx).Model(&model.KnowledgeBase{}).Pluck("id", &ids)
		bind(model.ResourceTypeKnowledgeBase, ids)
	case model.AgentCategoryVideo:
		var ids []int64
		s.db.WithContext(ctx).Model(&model.VideoDatasource{}).Pluck("id", &ids)
		bind(model.ResourceTypeVideoSource, ids)
	case model.AgentCategoryCamera:
		// 摄像头事件无 agent_id 归属，历史数据整体绑给摄像头型 Agent（保持旧的全量检索行为）
		var ids []int64
		s.db.WithContext(ctx).Model(&model.CameraEvent{}).Pluck("id", &ids)
		bind(model.ResourceTypeCameraEvent, ids)
	}
}

// publishRelease 构建快照并落库，同时把该版本设为默认。
func (s *Store) publishRelease(ctx context.Context, agent *model.Agent, version, changelog string) error {
	snap, err := s.BuildAgentReleaseSnapshot(ctx, agent)
	if err != nil {
		return err
	}
	raw, err := snap.Encode()
	if err != nil {
		return err
	}
	now := time.Now()
	rel := &model.AgentRelease{
		AgentID:     agent.ID,
		Version:     version,
		Snapshot:    raw,
		Changelog:   changelog,
		Status:      model.ReleaseStatusPublished,
		IsDefault:   true,
		ToolCount:   len(snap.ExposedTools),
		PublishedBy: agent.OwnerName,
		PublishedAt: now,
		CreatedAt:   now,
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(rel).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.AgentRelease{}).
			Where("agent_id = ? AND id <> ?", agent.ID, rel.ID).
			Update("is_default", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.Agent{}).Where("id = ?", agent.ID).
			Updates(map[string]interface{}{
				"current_release_id": rel.ID,
				"published_at":       now,
				// 发布即上线：状态同步为 published，让「已发布」在列表页与运行时都可判定。
				// 此前发布只写 release 不动 status，导致智能体长期停留在 draft，
				// 发布与未发布在界面上无法区分。
				"status":     model.AgentStatusPublished,
				"updated_at": now,
			}).Error; err != nil {
			return err
		}
		// 产品默认版本跟随最新发布（客户侧若钉了版本则以订阅为准）
		return tx.Model(&model.Product{}).Where("agent_id = ?", agent.ID).
			Updates(map[string]interface{}{"default_release_id": rel.ID, "updated_at": now}).Error
	})
}

// PublishAgentRelease 发布新版本（对外暴露给 Handler）。
func (s *Store) PublishAgentRelease(ctx context.Context, agent *model.Agent, changelog string) (*model.AgentRelease, error) {
	version, err := s.NextAgentVersion(ctx, agent.ID)
	if err != nil {
		return nil, err
	}
	if err := s.publishRelease(ctx, agent, version, changelog); err != nil {
		return nil, err
	}
	return s.GetAgentReleaseByVersion(ctx, agent.ID, version)
}

func trimSpace(s string) string {
	b := []byte(s)
	i, j := 0, len(b)
	for i < j && (b[i] == ' ' || b[i] == '\t' || b[i] == '\n' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\t' || b[j-1] == '\n' || b[j-1] == '\r') {
		j--
	}
	return string(b[i:j])
}

func defaultString(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
