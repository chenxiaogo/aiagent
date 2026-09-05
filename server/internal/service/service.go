package service

import (
	"aiagent/pkg/app/config"
)

// Service 聚合所有业务服务。
type Service struct {
	Embedding    *EmbeddingService
	Chat         *ChatService
	Analysis     *AnalysisService
	FFmpeg       *FFmpegService
	VideoProcess *VideoProcessService
	CameraEvent  *CameraEventService
	CameraSearch *CameraSearchService
	AgentRuntime *AgentRuntime
	Memory       *MemoryService
	// Indexer 统一索引：文档分块 / 视频场景 / 摄像头事件的向量写入与重建
	Indexer      *IndexerService
}

// New 创建基础 Service（不含需要 store 的服务）。
func New(cfg *config.Config) *Service {
	chat := NewChatService()
	embedding := NewEmbeddingService()
	return &Service{
		Embedding:    embedding,
		Chat:         chat,
		Analysis:     NewAnalysisService(),
		FFmpeg:       NewFFmpegService(),
		AgentRuntime: NewAgentRuntime(chat, embedding),
	}
}
