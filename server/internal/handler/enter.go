package handler

import (
	"context"
	"time"

	"aiagent/internal/approval"
	"aiagent/internal/knowledge"
	"aiagent/internal/service"
	"aiagent/internal/store"
	"aiagent/pkg/app/config"
	"aiagent/pkg/auth"
)

// ResolveEmbedModelConfig 委托给 service 包，保证 Handler 与后台重建任务用的是同一套解析逻辑。
func ResolveEmbedModelConfig(s *store.Store, ctx context.Context) *service.ModelConfig {
	return service.ResolveEmbedModelConfig(s, ctx)
}

// ResolveVisionModelConfig 委托给 service 包，解析视觉理解模型（视频帧 / 摄像头截图）。
func ResolveVisionModelConfig(s *store.Store, ctx context.Context) *service.ModelConfig {
	return service.ResolveVisionModelConfig(s, ctx)
}

// Logic 聚合所有业务 Handler。
type Logic struct {
	Auth         *AuthHandler
	Chat         *ChatHandler
	File         *FileHandler
	Knowledge    *KnowledgeHandler
	Agent        *AgentHandler
	Video        *VideoHandler
	ModelConfig  *ModelConfigHandler
	PromptConfig *PromptConfigHandler
	Report       *ReportHandler
	MCP          *MCPHandler
	Analysis     *AnalysisHandler
	Camera       *CameraHandler
	// 对外交付：版本发布 / 客户授权 / 访问凭据 / 用量
	AgentDelivery *AgentDeliveryHandler
	// 客户管理（客户主体即平台用户，授权智能体给谁调用）
	Tenant *TenantHandler
	// 索引重建：知识库文档 / 视频场景 / 摄像头事件的向量重建
	Reindex *ReindexHandler
	// 能力市场与治理（MCP 注册表 / 技能库 / Agent 模板 / 模型目录 / 路由 / 调用观测）
	Market *MarketHandler
	// 智能体多模型绑定
	AgentModel *AgentModelHandler
	// 智能体运行数据绑定（知识库 / 视频源 / 摄像头事件隔离）
	AgentResource *AgentResourceHandler
	// 对外 MCP 端点（客户 API Key 鉴权）
	AgentMCP *AgentMCPHandler
	// 运维主机管理
	Host *HostHandler
	// 权限管理（RBAC）：用户 / 角色 / 菜单 / 受管接口
	User *UserHandler
	Role *RoleHandler
	Menu *MenuHandler
	Api  *ApiHandler
	// 人工确认中心：Agent 执行危险操作（主机命令/文件写入）时等待用户在聊天框确认
	Approvals *approval.Broker
	// JWT 供路由层挂载鉴权中间件
	JWT *auth.JWT
}

// NewLogic 组装全部 Handler。
func NewLogic(ctx context.Context, conf *config.Config, store *store.Store) *Logic {
	svc := service.New(conf)
	// 组装需要 store 的服务
	svc.VideoProcess = service.NewVideoProcessService(store, svc.FFmpeg, svc.Embedding, svc.Chat)
	svc.CameraEvent = service.NewCameraEventService(store, svc.FFmpeg, svc.Chat, svc.Embedding)
	svc.CameraSearch = service.NewCameraSearchService(store)
	svc.AgentRuntime.SetSearch(svc.CameraSearch) // 注入摄像头检索服务到 Agent 运行时，供 search_camera/search_videos 工具使用
	svc.AgentRuntime.SetStore(store)             // 注入数据仓库，供 LLM 调用观测日志（CallLog）落库
	svc.Embedding.SetStore(store)                // 注入数据仓库，供向量调用观测日志（CallLog/embedding）落库
	// 向量观测默认关闭：需要核对向量成本时把 observability.logEmbedding 改成 true
	svc.Embedding.SetLogEnabled(conf.Observability.LogEmbedding)
	svc.Chat.SetStore(store)                     // 注入数据仓库，供对话调用观测日志（CallLog/llm）落库
	// 服务级默认：只显式开启长期记忆抽取，其余字段留 0，由 MemoryService 回退到内置默认值。
	// 每个智能体可在配置页用自己的 memoryParams 覆盖（见 Agent.memoryParams）。
	svc.Memory = service.NewMemoryService(store, svc.Chat, svc.Embedding, service.MemoryConfig{
		LongTermAlways: true,
	})
	svc.Indexer = service.NewIndexerService(store, svc.Embedding, svc.FFmpeg, svc.Chat)

	jwt := auth.New(conf.Security.JWTSecret, conf.Security.TokenExpireHours)
	// 单次确认最长等待 5 分钟，会话级放行有效期 2 小时
	approvals := approval.NewBroker(5*time.Minute, 2*time.Hour)
	return &Logic{
		Auth:          NewAuthHandler(store, jwt),
		Chat:          NewChatHandler(store, svc, approvals),
		File:          NewFileHandler(store, svc),
		Knowledge:     NewKnowledgeHandler(knowledge.NewManager(store)),
		Agent:         NewAgentHandler(store, svc),
		Video:         NewVideoHandler(store, svc),
		ModelConfig:   NewModelConfigHandler(store),
		PromptConfig:  NewPromptConfigHandler(store),
		Report:        NewReportHandler(store),
		MCP:           NewMCPHandler(store),
		Analysis:      NewAnalysisHandler(svc.Analysis),
		Camera:        NewCameraHandler(store, svc),
		AgentDelivery: NewAgentDeliveryHandler(store),
		Tenant:        NewTenantHandler(store),
		Reindex:       NewReindexHandler(store, svc),
		Market:        NewMarketHandler(store),
		AgentModel:    NewAgentModelHandler(store),
		AgentResource: NewAgentResourceHandler(store),
		AgentMCP:      NewAgentMCPHandler(store),
		Host:          NewHostHandler(store),
		User:          NewUserHandler(store),
		Role:          NewRoleHandler(store),
		Menu:          NewMenuHandler(store),
		Api:           NewApiHandler(store),
		JWT:           jwt,
	}
}
