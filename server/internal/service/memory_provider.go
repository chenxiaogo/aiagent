package service

import (
	"context"

	memoryapi "aiagent/internal/memory"
	"github.com/cloudwego/eino/schema"
)

// MemoryProviderAdapter 将现有摘要/长期记忆实现适配为统一的 Eino 会话记忆 Provider。
type MemoryProviderAdapter struct {
	service    *MemoryService
	chatModel  *ModelConfig
	embedModel *ModelConfig
	// memoryParams 智能体级记忆参数 JSON（空=沿用全局默认）
	memoryParams string
}

func NewMemoryProviderAdapter(service *MemoryService, chatModel, embedModel *ModelConfig, memoryParams string) *MemoryProviderAdapter {
	return &MemoryProviderAdapter{service: service, chatModel: chatModel, embedModel: embedModel, memoryParams: memoryParams}
}

func (a *MemoryProviderAdapter) Retrieve(ctx context.Context, scope memoryapi.Scope, req memoryapi.RetrieveRequest) (*memoryapi.Context, error) {
	loaded, err := a.service.Retrieve(ctx, MemoryScope{
		TenantID: scope.TenantID, UserID: scope.UserID,
		AgentID: scope.AgentID, SessionID: scope.SessionID,
		MemoryParams: a.memoryParams,
	}, req.BeforeMessageID, req.Query, a.embedModel)
	if err != nil {
		return nil, err
	}
	history := make([]*schema.Message, 0, len(loaded.History))
	for _, message := range loaded.History {
		switch message.Role {
		case "system":
			history = append(history, schema.SystemMessage(message.Content))
		case "assistant":
			history = append(history, schema.AssistantMessage(message.Content, nil))
		default:
			history = append(history, schema.UserMessage(message.Content))
		}
	}
	return &memoryapi.Context{History: history, RuntimeContext: loaded.RuntimeContext}, nil
}

func (a *MemoryProviderAdapter) Memorize(_ context.Context, scope memoryapi.Scope, req memoryapi.MemorizeRequest) {
	a.service.MemorizeAsync(MemoryScope{
		TenantID: scope.TenantID, UserID: scope.UserID,
		AgentID: scope.AgentID, SessionID: scope.SessionID,
		MemoryParams: a.memoryParams,
	}, req.UserMessage, req.AssistantMessageID, a.chatModel, a.embedModel)
}

var _ memoryapi.Provider = (*MemoryProviderAdapter)(nil)
