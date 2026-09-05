package app

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	"aiagent/internal/model"
	"aiagent/internal/store"
	"aiagent/pkg/app/config"
	"aiagent/pkg/app/route"
	"aiagent/pkg/database"
	"aiagent/pkg/ilog"
	"aiagent/pkg/shutdown"

	"gorm.io/gorm"
)

// NewServer 组装并启动应用。
func NewServer(ctx context.Context) error {
	if err := config.EnsureConfigFile(); err != nil {
		return fmt.Errorf("ensure config file: %w", err)
	}

	conf, err := config.TryLoadFromDisk("")
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := setupLogger(conf); err != nil {
		return fmt.Errorf("logger setup: %w", err)
	}
	defer ilog.Sync()

	// 模型服务已在平台「模型配置」中启用（数据库 model_configs），
	// 未配置时仅提示一次，避免误导用户去配已停用的 QWEN_API_KEY
	if conf.Qwen.APIKey == "" {
		ilog.Warn("模型未配置：请在平台「模型配置」中启用对话/向量/视觉模型")
	}

	// 初始化数据库（pgvector）
	db, err := database.New(&conf.Database)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}

	// 创建向量索引（已有表的索引）
	db.Exec("CREATE INDEX IF NOT EXISTS idx_document_chunks_embedding ON document_chunks USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)")
	db.Exec("CREATE INDEX IF NOT EXISTS idx_video_scenes_embedding ON video_scenes USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)")

	if err := db.AutoMigrate(
		&model.User{}, &model.KnowledgeBase{}, &model.File{},
		&model.DocumentChunk{}, &model.ChatSession{}, &model.ChatMessage{},
		// 作用域化记忆：会话摘要 / 用户长期档案 / 可检索事件
		&model.SessionMemorySummary{}, &model.UserMemoryProfile{}, &model.UserMemoryEvent{},
		&model.Agent{}, &model.AgentPresetQuestion{},
		&model.AgentMCPServer{}, &model.AgentSkill{},
		&model.VideoDatasource{}, &model.VideoScene{},
		&model.ModelConfig{}, &model.PromptConfig{}, &model.Report{},
		&model.CameraEvent{},
		// 对外交付（Agent-as-a-Service）：客户 / 版本 / 凭据 / 计量 / 产品套餐订阅
		&model.Tenant{}, &model.AgentRelease{}, &model.AgentClient{},
		&model.UsageRecord{},
		&model.Product{}, &model.Plan{}, &model.Subscription{}, &model.App{},
		// 智能体多模型绑定
		&model.AgentModel{},
		// 智能体资源绑定（运行数据隔离，防对外交付数据泄漏）
		&model.AgentResource{},
		// 能力市场与治理：MCP 注册表 / 技能库 / 工具库 / Agent 模板 / 模型路由 / 调用观测
		&model.MCPRegistry{},
		&model.SkillLibrary{},
		&model.ToolLibrary{},
		&model.AgentTemplate{},
		&model.ModelRoutingRule{},
		&model.CallLog{},
		// 运维主机管理
		&model.HostGroup{}, &model.Host{}, &model.HostCommandRecord{}, &model.HostAuditLog{},
		// 权限管理（RBAC）：角色 / 权限点 / 菜单 / 菜单按钮 / 受管接口 / casbin 策略
		&model.Role{}, &model.Permission{}, &model.RolePermission{}, &model.UserRole{},
		&model.Menu{}, &model.MenuBtn{}, &model.RoleMenu{}, &model.RoleMenuBtn{},
		&model.Api{}, &model.CasbinRule{},
	); err != nil {
		return fmt.Errorf("auto migrate: %w", err)
	}
	ilog.Info("database migrated: " + conf.Database.Driver)

	// 价格列名修正：早期 AutoMigrate 依 GORM 默认 snake 命名生成了
	// price_in_per1_k / price_out_per1_k，而业务代码使用 price_in_per1k /
	// price_out_per1k。此处幂等地将旧列重命名为与新列名一致的形态。
	migrateModelPriceColumns(db)

	// 创建新表的向量索引（AutoMigrate 之后，ivfflat 需要至少几千条数据才有效）
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_camera_events_embedding ON camera_events USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)").Error; err != nil {
		ilog.Warnf("camera_events vector index: %v (will be effective after more data)", err)
	}
	if err := db.Exec("CREATE INDEX IF NOT EXISTS idx_user_memory_events_embedding ON user_memory_events USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100)").Error; err != nil {
		ilog.Warnf("user_memory_events vector index: %v (will be effective after more data)", err)
	}
	ilog.Info("vector index ensured")

	// 初始化数据仓库
	dataStore := store.New(db)

	// 创建路由
	r, err := route.NewRoute(ctx, conf, dataStore)
	if err != nil {
		return fmt.Errorf("create route: %w", err)
	}

	// 静态文件（前端构建产物）
	attachStatic(r.Engine(), conf)
	// 平台本地产物目录静态访问（/output/*）
	attachOutputStatic(r.Engine())

	server := &http.Server{
		Addr:         conf.ListenAddr(),
		Handler:      r.Handler(),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 300 * time.Second, // SSE 长连接需要较长超时
		IdleTimeout:  120 * time.Second,
	}

	hook := shutdown.NewHook().
		WithSignals(os.Interrupt, os.Kill).
		WithTimeout(30 * time.Second)
	hook.AddCloseFunc(func(c context.Context) {
		ilog.Info("shutting down http server...")
		_ = server.Shutdown(c)
	})

	ilog.Infof("aiagent listening on %s", conf.ListenAddr())
	if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server listen: %w", err)
	}
	return nil
}

// migrateModelPriceColumns 幂等重命名 ModelConfig 的旧价格列。
// 旧列由 GORM 默认 snake 命名生成（PriceInPer1K -> price_in_per1_k），
// 新列名显式固定为 price_in_per1k / price_out_per1k。仅当旧列存在、
// 新列尚不存在时执行重命名，保证可重复运行。
func migrateModelPriceColumns(db *gorm.DB) {
	type T = model.ModelConfig
	renames := []struct {
		old, new string
	}{
		{"price_in_per1_k", "price_in_per1k"},
		{"price_out_per1_k", "price_out_per1k"},
	}
	for _, r := range renames {
		if db.Migrator().HasColumn(&T{}, r.old) && !db.Migrator().HasColumn(&T{}, r.new) {
			if err := db.Migrator().RenameColumn(&T{}, r.old, r.new).Error; err != nil {
				ilog.Warnf("rename column %s -> %s: %v", r.old, r.new, err)
			} else {
				ilog.Infof("migrated column %s -> %s", r.old, r.new)
			}
		}
	}
}

func setupLogger(conf *config.Config) error {	options := []ilog.OptionFunc{
		ilog.WithAppName(conf.App.Name),
		ilog.WithLogLevel(conf.Logger.Level),
		ilog.WithWriteToConsole(true),
	}
	if conf.Logger.FilePath != "" {
		options = append(options, ilog.WithLogPath(conf.Logger.FilePath), ilog.WithWriteToFile(true))
	}
	return ilog.NewLogger(options...)
}
