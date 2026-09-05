package model

import (
	"time"

	"github.com/pgvector/pgvector-go"
)

const (
	MemoryEventTypeFact       = "fact"
	MemoryEventTypePreference = "preference"
	MemoryEventTypeMilestone  = "milestone"
	MemoryEventTypeConstraint = "constraint"
)

// SessionMemorySummary 保存会话增量摘要及摘要游标。
// user_id + agent_id + session_id 共同构成记忆边界，禁止跨 Agent 复用会话记忆。
type SessionMemorySummary struct {
	ID            int64     `json:"id" gorm:"primaryKey"`
	TenantID      int64     `json:"tenantId" gorm:"not null;default:0;uniqueIndex:uk_memory_summary_scope,priority:1"`
	UserID        int64     `json:"userId" gorm:"not null;uniqueIndex:uk_memory_summary_scope,priority:2;index"`
	AgentID       int64     `json:"agentId" gorm:"not null;default:0;uniqueIndex:uk_memory_summary_scope,priority:3;index"`
	SessionID     int64     `json:"sessionId" gorm:"not null;uniqueIndex:uk_memory_summary_scope,priority:4;index"`
	Summary       string    `json:"summary" gorm:"type:text"`
	LastMessageID int64     `json:"lastMessageId" gorm:"not null;default:0"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// UserMemoryProfile 保存用户在指定 Agent 范围内的稳定长期记忆。
type UserMemoryProfile struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	TenantID  int64     `json:"tenantId" gorm:"not null;default:0;uniqueIndex:uk_memory_profile_scope,priority:1"`
	UserID    int64     `json:"userId" gorm:"not null;uniqueIndex:uk_memory_profile_scope,priority:2;index"`
	AgentID   int64     `json:"agentId" gorm:"not null;default:0;uniqueIndex:uk_memory_profile_scope,priority:3;index"`
	Content   string    `json:"content" gorm:"type:text"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// UserMemoryEvent 保存可检索的长期事实/偏好/里程碑。
type UserMemoryEvent struct {
	ID              int64           `json:"id" gorm:"primaryKey"`
	TenantID        int64           `json:"tenantId" gorm:"not null;default:0;index:idx_memory_event_scope,priority:1"`
	UserID          int64           `json:"userId" gorm:"not null;index:idx_memory_event_scope,priority:2"`
	AgentID         int64           `json:"agentId" gorm:"not null;default:0;index:idx_memory_event_scope,priority:3"`
	SourceSessionID int64           `json:"sourceSessionId" gorm:"index"`
	EventType       string          `json:"eventType" gorm:"size:32;index"`
	EventDate       time.Time       `json:"eventDate" gorm:"index"`
	Keywords        string          `json:"keywords" gorm:"size:512"`
	Summary         string          `json:"summary" gorm:"type:text;not null"`
	Confidence      float64         `json:"confidence" gorm:"default:0.8"`
	Embedding       pgvector.Vector `json:"-" gorm:"type:vector(1024)"`
	EmbeddingModel  string          `json:"embeddingModel" gorm:"size:255"`
	CreatedAt       time.Time       `json:"createdAt"`
}
