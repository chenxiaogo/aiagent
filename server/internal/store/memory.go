package store

import (
	"context"
	"time"

	"github.com/pgvector/pgvector-go"
	"gorm.io/gorm/clause"

	"aiagent/internal/model"
	"aiagent/pkg/ilog"
)

// MemoryScope 是记忆查询的强制边界。当前后台用户 TenantID 为 0，交付链路可传真实租户 ID。
type MemoryScope struct {
	TenantID  int64
	UserID    int64
	AgentID   int64
	SessionID int64
}

func (s *Store) GetSessionMemorySummary(ctx context.Context, scope MemoryScope) (*model.SessionMemorySummary, error) {
	var summary model.SessionMemorySummary
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND agent_id = ? AND session_id = ?", scope.TenantID, scope.UserID, scope.AgentID, scope.SessionID).
		First(&summary).Error
	if err != nil {
		return nil, err
	}
	return &summary, nil
}

func (s *Store) UpsertSessionMemorySummary(ctx context.Context, summary *model.SessionMemorySummary) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}, {Name: "agent_id"}, {Name: "session_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"summary":         summary.Summary,
			"last_message_id": summary.LastMessageID,
			"updated_at":      time.Now(),
		}),
	}).Create(summary).Error
}

func (s *Store) GetUserMemoryProfile(ctx context.Context, scope MemoryScope) (*model.UserMemoryProfile, error) {
	var profile model.UserMemoryProfile
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND agent_id = ?", scope.TenantID, scope.UserID, scope.AgentID).
		First(&profile).Error
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Store) UpsertUserMemoryProfile(ctx context.Context, profile *model.UserMemoryProfile) error {
	return s.db.WithContext(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "tenant_id"}, {Name: "user_id"}, {Name: "agent_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"content":    profile.Content,
			"updated_at": time.Now(),
		}),
	}).Create(profile).Error
}

func (s *Store) ListRecentUserMemoryEvents(ctx context.Context, scope MemoryScope, limit int) ([]*model.UserMemoryEvent, error) {
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	var events []*model.UserMemoryEvent
	err := s.db.WithContext(ctx).
		Where("tenant_id = ? AND user_id = ? AND agent_id = ?", scope.TenantID, scope.UserID, scope.AgentID).
		Order("event_date DESC, id DESC").Limit(limit).Find(&events).Error
	return events, err
}

func (s *Store) CreateUserMemoryEventIfAbsent(ctx context.Context, event *model.UserMemoryEvent) error {
	var count int64
	if err := s.db.WithContext(ctx).Model(&model.UserMemoryEvent{}).
		Where("tenant_id = ? AND user_id = ? AND agent_id = ? AND summary = ?", event.TenantID, event.UserID, event.AgentID, event.Summary).
		Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	return s.db.WithContext(ctx).Create(event).Error
}

// SearchUserMemoryEvents 在强制作用域内执行 pgvector 余弦相似度检索。
func (s *Store) SearchUserMemoryEvents(ctx context.Context, scope MemoryScope, embedding []float64, limit int, threshold float64) ([]*model.UserMemoryEvent, error) {
	if len(embedding) == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 5
	}
	vec := pgvector.NewVector(toFloat32Slice(embedding))
	var events []*model.UserMemoryEvent
	err := s.db.WithContext(ctx).Raw(`
		SELECT * FROM user_memory_events
		WHERE tenant_id = ? AND user_id = ? AND agent_id = ?
		  AND embedding IS NOT NULL
		  AND 1 - (embedding <=> ?::vector) >= ?
		ORDER BY embedding <=> ?::vector
		LIMIT ?`, scope.TenantID, scope.UserID, scope.AgentID, vec, threshold, vec, limit).Scan(&events).Error
	return events, err
}

func toFloat32Slice(values []float64) []float32 {
	out := make([]float32, len(values))
	for i, value := range values {
		out[i] = float32(value)
	}
	return out
}

// ListMessagesForMemory 返回摘要游标之后、当前消息之前的会话消息。
func (s *Store) ListMessagesForMemory(ctx context.Context, scope MemoryScope, afterID, beforeID int64, limit int) ([]*model.ChatMessage, error) {
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	// Select("chat_messages.*") 不能省：两表都有 id / created_at 等同名列，
	// 裸 SELECT * 时 chat_sessions 的列排在后面会覆盖 chat_messages 的，
	// 于是 msg.ID 拿到的是会话 ID。这个 ID 会被当摘要游标写进
	// session_memory_summaries.last_message_id，导致下次
	// 「id > last_message_id」的范围出错——要么反复压旧消息，要么漏掉新消息。
	query := s.db.WithContext(ctx).Model(&model.ChatMessage{}).
		Select("chat_messages.*").
		Joins("JOIN chat_sessions ON chat_sessions.id = chat_messages.session_id").
		Where("chat_messages.session_id = ? AND chat_sessions.user_id = ? AND chat_sessions.agent_id = ?", scope.SessionID, scope.UserID, scope.AgentID)
	if afterID > 0 {
		query = query.Where("chat_messages.id > ?", afterID)
	}
	if beforeID > 0 {
		query = query.Where("chat_messages.id < ?", beforeID)
	}
	var messages []*model.ChatMessage
	if err := query.Order("chat_messages.id DESC").Limit(limit).Find(&messages).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}
	return messages, nil
}

// RepairMemorySummaryCursor 修复被污染的会话摘要游标（幂等）。
//
// 背景：ListMessagesForMemory 曾用裸 SELECT * 做 JOIN，chat_sessions 的 id / created_at
// 覆盖了 chat_messages 的同名列，于是写入 last_message_id 的其实是「会话 ID」。
// 该游标决定「id > last_message_id」从哪条消息开始压缩，一旦错：
//   - 偏小 → 每轮都把已压缩过的旧消息再压一遍，摘要不断重复；
//   - 偏大 → 新消息永远进不了压缩范围，摘要不再更新。
//
// 判定：游标指向的消息不存在，或不属于该会话 —— 即认为已被污染，重置为 0 重新累积。
func (s *Store) RepairMemorySummaryCursor(ctx context.Context) {
	var rows []struct {
		ID     int64
		LastID int64
	}
	if err := s.db.WithContext(ctx).Raw(`
		SELECT s.id AS id, s.last_message_id AS last_id
		FROM session_memory_summaries s
		WHERE s.last_message_id > 0
		  AND NOT EXISTS (
			SELECT 1 FROM chat_messages m
			WHERE m.id = s.last_message_id AND m.session_id = s.session_id
		  )`).Scan(&rows).Error; err != nil {
		ilog.Warnf("repair memory summary cursor scan failed: %v", err)
		return
	}
	if len(rows) == 0 {
		return
	}
	for _, r := range rows {
		s.db.WithContext(ctx).Model(&model.SessionMemorySummary{}).
			Where("id = ?", r.ID).Update("last_message_id", 0)
	}
	ilog.Infof("memory repair: %d 条会话摘要游标异常已重置", len(rows))
}

func (s *Store) GetScopedChatSession(ctx context.Context, scope MemoryScope) (*model.ChatSession, error) {
	var session model.ChatSession
	err := s.db.WithContext(ctx).
		Where("id = ? AND user_id = ? AND agent_id = ?", scope.SessionID, scope.UserID, scope.AgentID).
		First(&session).Error
	if err != nil {
		return nil, err
	}
	return &session, nil
}
