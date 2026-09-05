package service

import (
	"context"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/app/config"
)

// runtimeModelConfig 把数据库模型配置转换为运行时配置，并保留 Provider（区分请求格式用）。
func runtimeModelConfig(m *model.ModelConfig) *ModelConfig {
	return &ModelConfig{
		BaseURL:     m.BaseURL,
		APIKey:      m.APIKey,
		ModelName:   m.ModelName,
		MaxTokens:   m.MaxTokens,
		Temperature: m.Temperature,
		Provider:    m.Provider,
	}
}

// ResolveEmbedModelConfig 解析当前生效的向量模型配置。
//
// 除对话链路外，视频搜索 / 文档向量化 / 摄像头检索等处也曾硬编码传 nil 给 Embed，
// 导致必然报「请先在模型配置中配置并激活一个向量模型」。统一走这里解析，避免再漏传。
//
// 取用顺序：已激活的向量模型 → 回退复用对话模型（多数中转服务两者同域）→ config.yaml 兜底。
//
// 放在 service 包而不是 handler 包，是因为索引重建等后台任务也要用同一套解析逻辑，
// 两处各写一份迟早会不一致。
func ResolveEmbedModelConfig(s *store.Store, ctx context.Context) *ModelConfig {
	if mcfg, err := s.GetActiveModelConfig(ctx, model.ModelTypeEmbedding); err == nil && mcfg.APIKey != "" {
		return runtimeModelConfig(mcfg)
	}
	// 回退：复用对话模型（Base URL + Key），模型名仍用对话模型名以兼容同域网关
	if chatMcfg, err := s.GetActiveModelConfig(ctx, model.ModelTypeChat); err == nil && chatMcfg.APIKey != "" {
		return runtimeModelConfig(chatMcfg)
	}
	cfg := config.GetCurrentConfig()
	return DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.EmbedModel)
}

// ResolveVisionModelConfig 解析当前生效的视觉理解模型（视频帧 / 摄像头截图 / 知识库视频分析）。
//
// 取用顺序：已激活的视觉模型 → 回退复用对话模型（多数中转服务同一模型支持多模态）→ config.yaml 兜底。
// 注意：只有 provider=google / baseUrl 指向 generativelanguage 的配置才会走 Gemini 原生格式，
// 其余（qwen-vl、本地多模态网关等）走 OpenAI 兼容格式。
func ResolveVisionModelConfig(s *store.Store, ctx context.Context) *ModelConfig {
	if mcfg, err := s.GetActiveModelConfig(ctx, model.ModelTypeVision); err == nil && mcfg.APIKey != "" {
		return runtimeModelConfig(mcfg)
	}
	if chatMcfg, err := s.GetActiveModelConfig(ctx, model.ModelTypeChat); err == nil && chatMcfg.APIKey != "" {
		return runtimeModelConfig(chatMcfg)
	}
	cfg := config.GetCurrentConfig()
	return DefaultModelConfig(cfg.Qwen.APIKey, cfg.Qwen.BaseURL, cfg.Qwen.ChatModel)
}
