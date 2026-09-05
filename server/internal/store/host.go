package store

import (
	"context"
	"time"

	"aiagent/internal/model"
)

// ---------- 主机组 ----------

func (s *Store) ListHostGroups(ctx context.Context, ownerID int64, keyword string) ([]*model.HostGroup, error) {
	q := s.db.WithContext(ctx).Model(&model.HostGroup{}).
		Where("owner_id = ?", ownerID)
	if keyword != "" {
		q = q.Where("name ILIKE ?", "%"+keyword+"%")
	}
	var list []*model.HostGroup
	err := q.Order("id DESC").Find(&list).Error
	return list, err
}

func (s *Store) GetHostGroup(ctx context.Context, id int64) (*model.HostGroup, error) {
	var item model.HostGroup
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

func (s *Store) CreateHostGroup(ctx context.Context, item *model.HostGroup) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	return s.db.WithContext(ctx).Create(item).Error
}

func (s *Store) UpdateHostGroup(ctx context.Context, item *model.HostGroup) error {
	item.UpdatedAt = time.Now()
	return s.db.WithContext(ctx).Save(item).Error
}

func (s *Store) DeleteHostGroup(ctx context.Context, id int64) error {
	return s.db.WithContext(ctx).Delete(&model.HostGroup{}, id).Error
}

// UpdateHostGroupCount 刷新主机组的主机计数。
func (s *Store) UpdateHostGroupCount(ctx context.Context, groupID int64) error {
	var count int64
	s.db.WithContext(ctx).Model(&model.Host{}).
		Where("group_id = ?", groupID).Count(&count)
	return s.db.WithContext(ctx).Model(&model.HostGroup{}).
		Where("id = ?", groupID).
		Update("host_count", count).Error
}

// ---------- 主机 ----------

type HostQuery struct {
	OwnerID  int64
	GroupID  int64
	Keyword  string
	Status   string
	Role     string
	All      bool // 管理员：忽略 owner 过滤，查看全部主机
	Page     int
	PageSize int
}

func (s *Store) ListHosts(ctx context.Context, q HostQuery) ([]*model.Host, int64, error) {
	db := s.db.WithContext(ctx).Model(&model.Host{})
	// 非管理员按 owner 隔离；管理员（All=true）可查看全部（运维视角）
	if !q.All {
		db = db.Where("owner_id = ?", q.OwnerID)
	}
	if q.GroupID > 0 {
		db = db.Where("group_id = ?", q.GroupID)
	}
	if q.Keyword != "" {
		db = db.Where("name ILIKE ? OR hostname ILIKE ?", "%"+q.Keyword+"%", "%"+q.Keyword+"%")
	}
	if q.Status != "" {
		db = db.Where("status = ?", q.Status)
	}
	if q.Role != "" {
		db = db.Where("role = ?", q.Role)
	}
	var total int64
	if err := db.Count(&total).Error; err != nil {
		return nil, 0, err
	}
	page := q.Page
	if page <= 0 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize <= 0 || pageSize > 100 {
		pageSize = 20
	}
	var list []*model.Host
	err := db.Order("id DESC").
		Offset((page - 1) * pageSize).
		Limit(pageSize).
		Find(&list).Error
	return list, total, err
}

func (s *Store) GetHost(ctx context.Context, id int64) (*model.Host, error) {
	var item model.Host
	if err := s.db.WithContext(ctx).First(&item, id).Error; err != nil {
		return nil, err
	}
	return &item, nil
}

// GetHostsByGroup 获取主机组下的所有主机（Agent 工具执行时用）。
func (s *Store) GetHostsByGroup(ctx context.Context, groupID int64) ([]*model.Host, error) {
	var list []*model.Host
	err := s.db.WithContext(ctx).Model(&model.Host{}).
		Where("group_id = ? AND status = ?", groupID, model.HostStatusOnline).
		Order("id ASC").Find(&list).Error
	return list, err
}

// GetHostsByGroups 批量获取多个主机组的在线主机。
func (s *Store) GetHostsByGroups(ctx context.Context, groupIDs []int64) ([]*model.Host, error) {
	if len(groupIDs) == 0 {
		return nil, nil
	}
	var list []*model.Host
	err := s.db.WithContext(ctx).Model(&model.Host{}).
		Where("group_id IN ? AND status = ?", groupIDs, model.HostStatusOnline).
		Order("id ASC").Find(&list).Error
	return list, err
}

func (s *Store) CreateHost(ctx context.Context, item *model.Host) error {
	now := time.Now()
	item.CreatedAt, item.UpdatedAt = now, now
	if err := s.db.WithContext(ctx).Create(item).Error; err != nil {
		return err
	}
	if item.GroupID > 0 {
		s.UpdateHostGroupCount(ctx, item.GroupID)
	}
	return nil
}

func (s *Store) UpdateHost(ctx context.Context, item *model.Host) error {
	// 读取旧数据用于判断 group 是否变化
	old, err := s.GetHost(ctx, item.ID)
	if err != nil {
		return err
	}
	item.UpdatedAt = time.Now()
	if err := s.db.WithContext(ctx).Save(item).Error; err != nil {
		return err
	}
	if old.GroupID != item.GroupID {
		if old.GroupID > 0 {
			s.UpdateHostGroupCount(ctx, old.GroupID)
		}
		if item.GroupID > 0 {
			s.UpdateHostGroupCount(ctx, item.GroupID)
		}
	}
	return nil
}

func (s *Store) UpdateHostStatus(ctx context.Context, id int64, status string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&model.Host{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":        status,
			"updated_at":    now,
			"last_check_at": now,
		}).Error
}

func (s *Store) DeleteHost(ctx context.Context, id int64) error {
	host, err := s.GetHost(ctx, id)
	if err != nil {
		return err
	}
	if err := s.db.WithContext(ctx).Delete(&model.Host{}, id).Error; err != nil {
		return err
	}
	if host.GroupID > 0 {
		s.UpdateHostGroupCount(ctx, host.GroupID)
	}
	return nil
}

// ---------- 命令执行记录 ----------

func (s *Store) CreateHostCommandRecord(ctx context.Context, record *model.HostCommandRecord) error {
	record.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(record).Error
}

func (s *Store) FinishHostCommandRecord(ctx context.Context, id int64, exitCode int, stdout, stderr string, durationMs int64, status string) error {
	now := time.Now()
	return s.db.WithContext(ctx).Model(&model.HostCommandRecord{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"exit_code":   exitCode,
			"stdout":      stdout,
			"stderr":      stderr,
			"duration_ms": durationMs,
			"status":      status,
			"finished_at": now,
		}).Error
}

func (s *Store) ListHostCommandRecords(ctx context.Context, hostID int64, limit int) ([]*model.HostCommandRecord, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	var list []*model.HostCommandRecord
	err := s.db.WithContext(ctx).Model(&model.HostCommandRecord{}).
		Where("host_id = ?", hostID).
		Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ---------- 主机操作审计 ----------

// CreateHostAudit 写入一条主机变更审计。
func (s *Store) CreateHostAudit(ctx context.Context, log *model.HostAuditLog) error {
	log.CreatedAt = time.Now()
	return s.db.WithContext(ctx).Create(log).Error
}

// ListHostAudits 查询审计记录（按时间倒序）。
// targetType / targetID / action 可选过滤；limit 默认 50。
func (s *Store) ListHostAudits(ctx context.Context, targetType string, targetID int64, action string, limit int) ([]*model.HostAuditLog, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	db := s.db.WithContext(ctx).Model(&model.HostAuditLog{})
	if targetType != "" {
		db = db.Where("target_type = ?", targetType)
	}
	if targetID > 0 {
		db = db.Where("target_id = ?", targetID)
	}
	if action != "" {
		db = db.Where("action = ?", action)
	}
	var list []*model.HostAuditLog
	err := db.Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}
