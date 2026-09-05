package memory

import (
	"context"

	"github.com/cloudwego/eino/schema"
)

// Scope 是会话记忆的不可覆盖边界，由认证与会话层构造。
type Scope struct {
	TenantID  int64
	UserID    int64
	AgentID   int64
	SessionID int64
}

type RetrieveRequest struct {
	BeforeMessageID int64
	Query           string
	Limit           int
}

type Context struct {
	History        []*schema.Message
	RuntimeContext string
}

type MemorizeRequest struct {
	UserMessage        string
	AssistantMessage   string
	AssistantMessageID int64
}

// Provider 参考 aggo Retrieve/Memorize 生命周期，但授权 Scope 与模型可见输入完全分离。
type Provider interface {
	Retrieve(ctx context.Context, scope Scope, req RetrieveRequest) (*Context, error)
	Memorize(ctx context.Context, scope Scope, req MemorizeRequest)
}
