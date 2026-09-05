package model

import (
	"fmt"
	"time"

	"github.com/pgvector/pgvector-go"
)

// 文件类型
const (
	FileTypePDF  = "pdf"
	FileTypeTXT  = "txt"
	FileTypeMD   = "md"
	FileTypeDOCX = "docx"
	FileTypeHTML = "html"
	FileTypeCSV  = "csv"
	FileTypeJSON = "json"
	FileTypeCode = "code"
)

// 文件状态
const (
	FileStatusPending    = "pending"    // 待处理
	FileStatusProcessing = "processing" // 处理中
	FileStatusReady      = "ready"      // 已就绪
	FileStatusFailed     = "failed"     // 处理失败
)

// 权限点常量（RBAC）。
// 迁移自 scheduler-platform（gocron）：沿用其 task:/node:/user: 等权限码，
// 语义按下表映射到 aiagent 的模块，后续可逐步替换为 aiagent 原生命名。
//
//	task:*   → 智能体（Agent，平台核心可调度资源，对应 gocron 的 Task）
//	node:*   → 运维主机（Host，被管理的远端机器，与 gocron 的 Node 语义一致）
//	exec/log → 运行观测（调用日志 / 执行记录）
//	user/role → 系统设置下的用户与角色管理
const (
	PermDashboardView = "dashboard:view" // 查看首页看板
	PermTaskView      = "task:view"      // 查看任务（智能体）
	PermTaskCreate    = "task:create"    // 创建任务（智能体）
	PermTaskUpdate    = "task:update"    // 更新任务（智能体）
	PermTaskDelete    = "task:delete"    // 删除任务（智能体）
	PermTaskRun       = "task:run"       // 手动执行/启停（智能体）
	PermExecView      = "exec:view"      // 查看执行记录
	PermLogView       = "log:view"       // 查看执行日志
	PermNodeView      = "node:view"      // 查看节点（运维主机）
	PermNodeManage    = "node:manage"    // 节点管理（运维主机增删改）
	PermUserManage    = "user:manage"    // 用户管理
	PermRoleManage    = "role:manage"    // 角色管理
	PermNotifyView    = "notify:view"    // 查看通知配置
	PermNotifyManage  = "notify:manage"  // 管理通知配置
	PermSystemAdmin   = "system:admin"   // 系统管理员（全部权限）

	// 以下为 aiagent 模块新增，命名风格与上述保持一致
	PermMarketView   = "market:view"   // 查看能力市场（MCP/技能库/工具库/提示词库/模型目录）
	PermMarketManage = "market:manage" // 管理能力市场
	PermHostExec     = "host:exec"     // 在主机上执行命令（终端/脚本）
	PermHostFile     = "host:file"     // 主机文件管理（上传/下载/删除/重命名）
	PermOpsView      = "ops:view"      // 查看运行观测（调用日志）
)

// 消息角色
const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleSystem    = "system"
)

// User 用户
type User struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	Username  string    `json:"username" gorm:"uniqueIndex;size:64"`
	Password  string    `json:"-" gorm:"size:255"`
	Nickname  string    `json:"nickname" gorm:"size:64"`
	Email     string    `json:"email" gorm:"size:128"`
	IsAdmin   bool      `json:"isAdmin"`
	RoleID    int64     `json:"roleId" gorm:"index"` // 所属角色，0 表示未分配
	Status    int       `json:"status" gorm:"index;default:1"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Role 角色。
type Role struct {
	ID          int64        `json:"id" gorm:"primaryKey"`
	Code        string       `json:"code" gorm:"uniqueIndex;size:64"`
	Name        string       `json:"name" gorm:"size:64"`
	Description string       `json:"description" gorm:"size:255"`
	BuiltIn     bool         `json:"builtIn"` // 内置角色不可删除
	PermIDs     []int64      `json:"permIds" gorm:"-"`
	Permissions []Permission `json:"permissions" gorm:"many2many:role_permissions;"`
	// MenuIDs 角色绑定的菜单ID列表（非 DB 列，用于授权展示）
	MenuIDs []int64           `json:"menuIds" gorm:"-"`
	BtnMap  map[int64][]int64 `json:"btnMap" gorm:"-"` // menuId -> btnIds，角色-菜单-按钮授权
	ApiIDs  []int64           `json:"apiIds" gorm:"-"` // 角色已授权的 API ID（casbin 策略反查）
	CreatedAt time.Time       `json:"createdAt"`
	UpdatedAt time.Time       `json:"updatedAt"`
}

// Permission 权限点。
type Permission struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Code        string    `json:"code" gorm:"uniqueIndex;size:64"`
	Name        string    `json:"name" gorm:"size:64"`
	Type        string    `json:"type" gorm:"size:16;index"` // menu / button / api / role
	Description string    `json:"description" gorm:"size:255"`
	CreatedAt   time.Time `json:"createdAt"`
}

// UserRole 用户-角色关联（保留，兼容多角色扩展）。
type UserRole struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	UserID    int64     `json:"userId" gorm:"uniqueIndex:idx_user_role;index"`
	RoleID    int64     `json:"roleId" gorm:"uniqueIndex:idx_user_role;index"`
	CreatedAt time.Time `json:"createdAt"`
}

// RolePermission 角色-权限点关联。
type RolePermission struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	RoleID       int64     `json:"roleId" gorm:"uniqueIndex:idx_role_perm;index"`
	PermissionID int64     `json:"permissionId" gorm:"uniqueIndex:idx_role_perm;index"`
	CreatedAt    time.Time `json:"createdAt"`
}

// KnowledgeBase 知识库（单类型：一个知识库代表一种内容类型，如 video/file/audio/text/camera）
type KnowledgeBase struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;index"`
	Description string    `json:"description" gorm:"size:512"`
	Icon        string    `json:"icon" gorm:"size:64"`
	Type        string    `json:"type" gorm:"size:32;index;default:general"` // 内容类型：video/file/audio/text/camera/general
	Tags        string    `json:"tags" gorm:"size:512"`                      // 标签，逗号分隔（轻量方案：不走关联表，便于 LIKE 筛选）
	Meta        string    `json:"meta" gorm:"type:text"`                     // 元信息 JSON：归属部门 / 语言 / 负责人等自定义键值
	OwnerID     int64     `json:"ownerId" gorm:"index"`
	OwnerName   string    `json:"ownerName" gorm:"size:64"`
	AgentID     int64     `json:"agentId" gorm:"index"` // 归属智能体：数据按智能体隔离（视频检索型 Agent 详情的「知识库」Tab）
	FileCount   int       `json:"fileCount" gorm:"default:0"`
	ChunkCount  int       `json:"chunkCount" gorm:"default:0"`
	Status      int       `json:"status" gorm:"default:1"` // 1=启用 0=禁用
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// File 文件（云存储文件索引）
type File struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	KnowledgeID  int64     `json:"knowledgeId" gorm:"index"`
	AgentID      int64     `json:"agentId" gorm:"index"` // 冗余归属智能体，便于按 Agent 过滤文件
	FileName     string    `json:"fileName" gorm:"size:255"`
	FilePath     string    `json:"filePath" gorm:"size:512"`
	FileType     string    `json:"fileType" gorm:"size:16;index"`
	FileSize     int64     `json:"fileSize"`
	FileHash     string    `json:"fileHash" gorm:"size:64;index"`
	Tags         string    `json:"tags" gorm:"size:512"`                     // 文件级标签，逗号分隔
	Meta         string    `json:"meta" gorm:"type:text"`                    // 解析元信息 JSON：页数 / 行数 / 工作表 / 解析器
	StorageType  string    `json:"storageType" gorm:"size:32;default:local"` // local/cos/oss/s3
	StoragePath  string    `json:"storagePath" gorm:"size:512"`              // 云存储路径
	Status       string    `json:"status" gorm:"size:16;index;default:pending"`
	ChunkCount   int       `json:"chunkCount" gorm:"default:0"`
	ErrorMessage string    `json:"errorMessage" gorm:"type:text"`
	UploaderID   int64     `json:"uploaderId" gorm:"index"`
	UploaderName string    `json:"uploaderName" gorm:"size:64"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// DocumentChunk 文档分块（含向量嵌入）
type DocumentChunk struct {
	ID          int64           `json:"id" gorm:"primaryKey"`
	FileID      int64           `json:"fileId" gorm:"index"`
	KnowledgeID int64           `json:"knowledgeId" gorm:"index"`
	ChunkIndex  int             `json:"chunkIndex"`
	Content     string          `json:"content" gorm:"type:text"`
	ContentLen  int             `json:"contentLen"`
	Embedding   pgvector.Vector `json:"-" gorm:"type:vector(1024)"` // 向量维度 1024（text-embedding-v3）
	TokenCount  int             `json:"tokenCount"`
	Metadata    string          `json:"metadata" gorm:"type:text"` // JSON 元数据
	CreatedAt   time.Time       `json:"createdAt"`
}

// ChatSession 聊天会话
type ChatSession struct {
	ID            int64     `json:"id" gorm:"primaryKey"`
	Title         string    `json:"title" gorm:"size:255"`
	AgentID       int64     `json:"agentId" gorm:"index"`
	AgentName     string    `json:"agentName" gorm:"size:128"`
	KnowledgeID   int64     `json:"knowledgeId" gorm:"index"`
	KnowledgeName string    `json:"knowledgeName" gorm:"size:128"`
	UserID        int64     `json:"userId" gorm:"index"`
	Username      string    `json:"username" gorm:"size:64"`
	MessageCount  int       `json:"messageCount" gorm:"default:0"`
	IsPinned      bool      `json:"isPinned" gorm:"default:false"`
	// 会话作用域：运维工作台按「主机 / 主机组」开会话，让每轮对话有明确的操作对象。
	// global: 不绑定具体机器；host: 绑定单台主机；host_group: 绑定整个主机组。
	ScopeType     string    `json:"scopeType" gorm:"size:16;default:global;index"`
	ScopeID       int64     `json:"scopeId" gorm:"index"`
	ScopeName     string    `json:"scopeName" gorm:"size:128"` // 冗余存名称，列表页免联表
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// 会话作用域类型。
const (
	SessionScopeGlobal    = "global"
	SessionScopeHost      = "host"
	SessionScopeHostGroup = "host_group"
)

// ChatMessage 聊天消息
type ChatMessage struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	SessionID   int64     `json:"sessionId" gorm:"index"`
	Role        string    `json:"role" gorm:"size:16"` // user / assistant / system
	Content     string    `json:"content" gorm:"type:text"`
	ContentType string    `json:"contentType" gorm:"size:16;default:text"` // text / sql / markdown / error
	TokenCount  int       `json:"tokenCount"`
	Sources     string    `json:"sources" gorm:"type:text"` // JSON 来源引用
	CreatedAt   time.Time `json:"createdAt"`
}

// SearchResult 搜索结果（非 DB 模型，API 返回用）
type SearchResult struct {
	ChunkID    int64   `json:"chunkId"`
	FileID     int64   `json:"fileId"`
	FileName   string  `json:"fileName"`
	Content    string  `json:"content"`
	Score      float64 `json:"score"`
	ChunkIndex int     `json:"chunkIndex"`
	Metadata   string  `json:"metadata"` // 分块元数据 JSON（页码 / 行号 / 工作表名），命中后可回溯出处
}

// ChatRequest 聊天请求
type ChatRequest struct {
	SessionID   int64  `json:"sessionId"`
	AgentID     int64  `json:"agentId"`
	KnowledgeID int64  `json:"knowledgeId"`
	Message     string `json:"message"`
	ModelID     int64  `json:"modelId"` // 可选：临时指定已绑定的对话模型
}

// ChatStreamChunk 流式响应块
type ChatStreamChunk struct {
	Type    string         `json:"type"`              // text / search / error / done
	Content string         `json:"content"`           // 文本内容
	Sources []SearchResult `json:"sources,omitempty"` // 搜索来源
	Error   string         `json:"error,omitempty"`
}

// EmbeddingRequest 向量化请求
type EmbeddingRequest struct {
	Texts []string `json:"texts"`
}

// EmbeddingResponse 向量化响应
type EmbeddingResponse struct {
	Embeddings [][]float64 `json:"embeddings"`
	Tokens     int         `json:"tokens"`
}

// ---------- 智能体 Agent ----------

// Agent 状态
const (
	AgentStatusDraft     = "draft"     // 草稿
	AgentStatusPublished = "published" // 已发布
	AgentStatusOffline   = "offline"   // 已下线
)

// Agent 运行时类型。
const (
	AgentRuntimeLegacy = "legacy"
	AgentRuntimeEinoV2 = "eino_v2"
)

// Agent 分类（决定工作区形态与菜单分组，前端按此渲染对应能力）
const (
	AgentCategoryVideo   = "video"   // 视频检索型：视频场景语义检索
	AgentCategoryCamera  = "camera"  // 摄像头检索型：摄像头事件混合检索
	AgentCategoryDoc     = "doc"     // 文档检索型：文档向量+全文检索
	AgentCategoryReport  = "report"  // 报告生成型：基于数据与检索结果产出分析报告
	AgentCategoryOps     = "ops"     // 运维型：主机命令执行与文件操作
	AgentCategoryGeneral = "general" // 通用对话型：纯对话
)

// IsValidAgentCategory 判断分类是否合法。
func IsValidAgentCategory(c string) bool {
	switch c {
	case AgentCategoryVideo, AgentCategoryCamera, AgentCategoryDoc, AgentCategoryReport, AgentCategoryOps, AgentCategoryGeneral:
		return true
	}
	return false
}

// NormalizeAgentCategory 归一化分类：空或非法值统一归为 general。
func NormalizeAgentCategory(c string) string {
	if IsValidAgentCategory(c) {
		return c
	}
	return AgentCategoryGeneral
}

// Agent 智能体
type Agent struct {
	ID               int64      `json:"id" gorm:"primaryKey"`
	Name             string     `json:"name" gorm:"size:255;not null;index"`
	Description      string     `json:"description" gorm:"type:text"`
	Avatar           string     `json:"avatar" gorm:"size:512"`
	Status           string     `json:"status" gorm:"size:32;index;default:draft"`
	APIKey           string     `json:"apiKey,omitempty" gorm:"size:128;index"`
	APIKeyEnabled    bool       `json:"apiKeyEnabled" gorm:"default:false"`
	Prompt           string     `json:"prompt" gorm:"type:text"`
	Category         string     `json:"category" gorm:"size:100;index"`
	Tags             string     `json:"tags" gorm:"size:512"` // 逗号分隔
	ChatModelID      int64      `json:"chatModelId"`          // 关联对话模型配置
	EmbedModelID     int64      `json:"embedModelId"`         // 关联向量模型配置
	RuntimeType      string     `json:"runtimeType" gorm:"size:32;default:eino_v2"`
	MaxSteps         int        `json:"maxSteps" gorm:"default:8"`
	MemoryEnabled bool   `json:"memoryEnabled" gorm:"default:true"`
	MemoryParams  string `json:"memoryParams" gorm:"type:text;default:''"` // JSON：会话记忆参数（摘要阈值/长期记忆策略等），空=沿用全局默认
	ToolLibIDs    string `json:"toolLibIds" gorm:"type:text;default:''"`   // JSON 数组，挂载的工具库 ID，空=全部内置工具
	OwnerID       int64  `json:"ownerId" gorm:"index"`
	OwnerName        string     `json:"ownerName" gorm:"size:64"`
	VideoCount       int        `json:"videoCount" gorm:"default:0"`
	SessionCount     int        `json:"sessionCount" gorm:"default:0"`
	MCPCount         int        `json:"mcpCount" gorm:"-"`                               // 运行时聚合，不落库
	SkillCount       int        `json:"skillCount" gorm:"-"`                             // 运行时聚合，不落库
	ModelCount       int        `json:"modelCount" gorm:"-"`                             // 运行时聚合：agent_models 中已启用的模型绑定数
	ToolCount        int        `json:"toolCount" gorm:"-"`                              // 运行时聚合：内置工具 + 已启用 MCP 缓存的远端工具
	Slug             string     `json:"slug" gorm:"size:128;index"`                      // 对外路径片段，如 video-assistant-3
	Visibility       string     `json:"visibility" gorm:"size:32;default:private;index"` // 见 Visibility* 常量
	SortOrder        int        `json:"sortOrder" gorm:"default:0;index"`                // 智能体管理列表的手动排序位置，越小越靠前
	CurrentReleaseID int64      `json:"currentReleaseId" gorm:"index"`                   // 当前生效版本（latest）
	PublishedAt      *time.Time `json:"publishedAt"`                                     // 最近一次发布时间
	CreatedAt        time.Time  `json:"createdAt"`
	UpdatedAt        time.Time  `json:"updatedAt"`
}

// AgentMCPServer 智能体接入的外部 MCP 服务器（MCP Client 方向）。
// 与 internal/mcp 的 Server（对外提供能力，MCP Server 方向）相反：
// 这里配置的是「本平台的 Agent 去连接谁」，从而使用外部工具。
type AgentMCPServer struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AgentID   int64     `json:"agentId" gorm:"index;not null"`
	Name      string    `json:"name" gorm:"size:128;not null"`     // 显示名
	Transport string    `json:"transport" gorm:"size:32;not null"` // sse / streamable_http
	URL       string    `json:"url" gorm:"size:512;not null"`      // MCP 端点地址
	Headers   string    `json:"headers" gorm:"type:text"`          // JSON 对象字符串，附加请求头（如鉴权）
	Enabled   bool      `json:"enabled" gorm:"default:true;index"`
	// ToolsCount 最近一次成功拉取到的远端工具数。列表页统计「工具数」时直接读它，
	// 避免为了一个数字去实时连接所有 MCP 服务器；连接串变更后会被清零，等下次测试/刷新重写。
	ToolsCount    int        `json:"toolsCount" gorm:"default:0"`
	ToolsSyncedAt *time.Time `json:"toolsSyncedAt"`
	// ApprovalRequired 该 MCP 服务器下的工具是否需要人工审批。
	// nil 或 true 表示需要（保守默认，避免未授权执行外部工具）；
	// false 表示免审批，运行时直接执行，适合只读/可信的外部服务（如高德地图）。
	ApprovalRequired *bool `json:"approvalRequired" gorm:"default:true"`
	// RegistryID 来源：由平台 MCP 注册表导入时记录原条目 ID，
	// 用于重复导入时幂等更新（而非新增重复配置）；手工新增的配置为 nil。
	RegistryID *int64 `json:"registryId" gorm:"index"`
	CreatedAt     time.Time  `json:"createdAt"`
	UpdatedAt     time.Time  `json:"updatedAt"`
}

// AgentMCPServer 传输方式
const (
	MCPTransportSSE            = "sse"             // GET /sse + POST /messages
	MCPTransportStreamableHTTP = "streamable_http" // POST /mcp JSON-RPC over HTTP
)

// AgentSkill 智能体技能。
// Kind 决定 Content 的解析方式：
//   - prompt：内容即提示词片段，运行时按需加载（渐进式披露：摘要常驻，全文经 load_skill 取）
//   - tool：内容为 JSON 数组，描述要启用的内置工具名集合，运行时静态挂载到工具集
//
// 渐进式披露：Summary 记录「触发条件/摘要」，常驻 system prompt（Level 1）；
// Content 全文仅在模型调用 load_skill(name) 后注入（Level 2）。
type AgentSkill struct {
	ID      int64 `json:"id" gorm:"primaryKey"`
	AgentID int64 `json:"agentId" gorm:"index;not null"`
	// SkillLibID 指向市场技能库（model.SkillLibrary）：>0 表示复用市场资产，0 表示智能体私有技能。
	// 复用时 Name/Description/Summary/Kind/Content 是引用时刻的快照，市场后续改动不会自动同步。
	SkillLibID  int64     `json:"skillLibId" gorm:"index;default:0"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"size:512"`
	// Summary 触发条件/摘要，常驻 system prompt（Level 1）。为空时回退用 Description / Content 截断。
	Summary     string    `json:"summary" gorm:"size:512"`
	Kind        string    `json:"kind" gorm:"size:32;not null;default:prompt"` // prompt / tool
	Content     string    `json:"content" gorm:"type:text"`
	Enabled     bool      `json:"enabled" gorm:"default:true;index"`
	SortOrder   int       `json:"sortOrder" gorm:"default:0"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AgentSkill 技能类型
const (
	SkillKindPrompt = "prompt" // 提示词片段
	SkillKindTool   = "tool"   // 工具集合
)

// AgentPresetQuestion 智能体预设问题
type AgentPresetQuestion struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AgentID   int64     `json:"agentId" gorm:"index"`
	Question  string    `json:"question" gorm:"type:text;not null"`
	SortOrder int       `json:"sortOrder" gorm:"default:0"`
	IsActive  bool      `json:"isActive" gorm:"default:true"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ---------- 运维主机管理 ----------

// 主机状态
const (
	HostStatusOnline  = "online"  // 在线
	HostStatusOffline = "offline" // 离线
	HostStatusPending = "pending" // 待检测
	HostStatusFailed  = "failed"  // 连接失败
)

// 认证方式
const (
	HostAuthPassword = "password" // 密码认证
	HostAuthKey      = "key"      // 私钥认证
)

// 主机角色（环境 / 用途分类，参考 1Shell host.role 思路）。
// 用于权限视图隔离与运维分类，不影响连接能力。
const (
	HostRoleProd    = "prod"    // 生产
	HostRoleTest    = "test"    // 测试
	HostRoleDev     = "dev"     // 开发
	HostRoleBastion = "bastion" // 堡垒机
	HostRoleOther   = "other"   // 其他
)

// HostRoles 合法角色集合。
var HostRoles = map[string]struct{}{
	HostRoleProd:    {},
	HostRoleTest:    {},
	HostRoleDev:     {},
	HostRoleBastion: {},
	HostRoleOther:   {},
}

// HostRoleText 角色中文名。
func HostRoleText(r string) string {
	switch r {
	case HostRoleProd:
		return "生产"
	case HostRoleTest:
		return "测试"
	case HostRoleDev:
		return "开发"
	case HostRoleBastion:
		return "堡垒机"
	case HostRoleOther:
		return "其他"
	}
	return "未分类"
}

// IsValidHostRole 判断角色是否合法。
func IsValidHostRole(r string) bool {
	if r == "" {
		return true // 允许不填
	}
	_, ok := HostRoles[r]
	return ok
}

// Host 运维主机资产。
// 归属于某个主机组，Agent 通过绑定主机组获得操作权限。
type Host struct {
	ID          int64      `json:"id" gorm:"primaryKey"`
	Name        string     `json:"name" gorm:"size:128;not null"`   // 显示名
	Hostname    string     `json:"hostname" gorm:"size:256;not null"` // IP 或域名
	Port        int        `json:"port" gorm:"default:22"`            // SSH 端口
	Username    string     `json:"username" gorm:"size:64;not null"` // 登录用户名
	AuthType    string     `json:"authType" gorm:"size:16;default:password"`
	Password    string     `json:"-" gorm:"size:256"`   // 密码（加密存储）
	PrivateKey  string     `json:"-" gorm:"type:text"`  // 私钥（加密存储）
	Passphrase  string     `json:"-" gorm:"size:256"`  // 私钥口令（加密存储）
	OS          string     `json:"os" gorm:"size:32"`  // linux / windows
	Role        string     `json:"role" gorm:"size:16;default:other"` // 环境/用途分类
	Status      string     `json:"status" gorm:"size:16;default:pending;index"`
	GroupID     int64      `json:"groupId" gorm:"index;default:0"` // 所属主机组
	Tags        string     `json:"tags" gorm:"size:256"`
	Description string     `json:"description" gorm:"size:512"`
	OwnerID     int64      `json:"ownerId" gorm:"index;not null"`
	OwnerName   string     `json:"ownerName" gorm:"size:64"`
	LastCheckAt *time.Time `json:"lastCheckAt"`
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
}

// ---------- 主机操作审计（参考 1Shell auditService） ----------

// 审计动作类型。
const (
	HostAuditGroupCreate = "host_group_create"
	HostAuditGroupUpdate = "host_group_update"
	HostAuditGroupDelete = "host_group_delete"
	HostAuditHostCreate  = "host_create"
	HostAuditHostUpdate  = "host_update"
	HostAuditHostDelete  = "host_delete"
	HostAuditHostTerminal = "host_terminal_open"
	HostAuditHostExec     = "host_exec"
	// 文件管理（列目录 / 上传 / 下载 / 新建目录 / 删除 / 重命名）
	HostAuditFileUpload   = "host_file_upload"
	HostAuditFileDownload = "host_file_download"
	HostAuditFileMkdir    = "host_file_mkdir"
	HostAuditFileDelete   = "host_file_delete"
	HostAuditFileRename   = "host_file_rename"
)

// HostAuditLog 主机/主机组变更审计记录。
type HostAuditLog struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Action      string    `json:"action" gorm:"size:32;index"`   // 动作类型
	TargetType  string    `json:"targetType" gorm:"size:16"`     // host / group
	TargetID    int64     `json:"targetId" gorm:"index"`
	TargetName  string    `json:"targetName" gorm:"size:128"`
	OperatorID  int64     `json:"operatorId" gorm:"index"`
	OperatorName string   `json:"operatorName" gorm:"size:64"`
	Detail      string    `json:"detail" gorm:"type:text"`       // 变更摘要
	ClientIP    string    `json:"clientIp" gorm:"size:64"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

// HostGroup 主机组，Agent 通过绑定主机组获得操作权限。
// 与知识库、视频源等资源模型一致，走 AgentResource 绑定体系。
type HostGroup struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"size:512"`
	HostCount   int       `json:"hostCount" gorm:"default:0"`
	OwnerID     int64     `json:"ownerId" gorm:"index;not null"`
	OwnerName   string    `json:"ownerName" gorm:"size:64"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// HostCommandRecord 命令执行审计记录。
type HostCommandRecord struct {
	ID         int64      `json:"id" gorm:"primaryKey"`
	HostID     int64      `json:"hostId" gorm:"index;not null"`
	HostName   string     `json:"hostName" gorm:"size:128"`
	AgentID    int64      `json:"agentId" gorm:"index;default:0"`
	UserID     int64      `json:"userId" gorm:"index;default:0"`
	Command    string     `json:"command" gorm:"type:text;not null"`
	ExitCode   int        `json:"exitCode" gorm:"default:-1"`
	Stdout     string     `json:"stdout" gorm:"type:text"`
	Stderr     string     `json:"stderr" gorm:"type:text"`
	DurationMs int64      `json:"durationMs"`
	Status     string     `json:"status" gorm:"size:16;index"` // running / success / failed / timeout
	CreatedAt  time.Time  `json:"createdAt"`
	FinishedAt *time.Time `json:"finishedAt"`
}

// ---------- 视频数据源 VideoDatasource ----------

// 视频处理状态
const (
	VideoStatusPending    = "pending"    // 待处理
	VideoStatusProcessing = "processing" // 处理中
	VideoStatusReady      = "ready"      // 已就绪
	VideoStatusFailed     = "failed"     // 处理失败
)

// VideoDatasource 视频数据源
type VideoDatasource struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	AgentID      int64     `json:"agentId" gorm:"index"`
	KnowledgeID  int64     `json:"knowledgeId" gorm:"index"` // 归属知识库：视频源是知识库下的一个数据源
	Title        string    `json:"title" gorm:"size:255;not null"`
	FileName     string    `json:"fileName" gorm:"size:512"`
	FilePath     string    `json:"filePath" gorm:"size:512"`
	FileSize     int64     `json:"fileSize"`
	FileHash     string    `json:"fileHash" gorm:"size:64;index"`
	Duration     float64   `json:"duration"`                  // 视频时长（秒）
	Resolution   string    `json:"resolution" gorm:"size:32"` // 分辨率 如 1920x1080
	FPS          float64   `json:"fps"`                       // 帧率
	Status       string    `json:"status" gorm:"size:32;index;default:pending"`
	Transcript   string    `json:"transcript" gorm:"type:text"` // 语音转文字全文
	Summary      string    `json:"summary" gorm:"type:text"`    // AI 生成的视频摘要
	ChunkCount   int       `json:"chunkCount" gorm:"default:0"`
	SceneCount   int       `json:"sceneCount" gorm:"default:0"` // 场景/镜头数
	ErrorMessage string    `json:"errorMessage" gorm:"type:text"`
	UploaderID   int64     `json:"uploaderId" gorm:"index"`
	UploaderName string    `json:"uploaderName" gorm:"size:64"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// VideoScene 视频场景分块（带帧截图 + 时间戳 + 描述 + 向量）
type VideoScene struct {
	ID          int64           `json:"id" gorm:"primaryKey"`
	VideoID     int64           `json:"videoId" gorm:"index"`
	AgentID     int64           `json:"agentId" gorm:"index"`
	SceneIndex  int             `json:"sceneIndex"`
	StartTime   float64         `json:"startTime"` // 起始时间（秒）
	EndTime     float64         `json:"endTime"`   // 结束时间（秒）
	Duration    float64         `json:"duration"`
	FramePath   string          `json:"framePath" gorm:"size:512"`    // 关键帧截图路径
	Description string          `json:"description" gorm:"type:text"` // AI 视觉描述
	Transcript  string          `json:"transcript" gorm:"type:text"`  // 该片段字幕
	Embedding   pgvector.Vector `json:"-" gorm:"type:vector(1024)"`
	TokenCount  int             `json:"tokenCount"`
	CreatedAt   time.Time       `json:"createdAt"`
}

// ---------- 模型配置 ModelConfig ----------

// 模型类型
const (
	ModelTypeChat      = "CHAT"      // 对话模型
	ModelTypeEmbedding = "EMBEDDING" // 向量模型
	ModelTypeVision    = "VISION"    // 视觉理解模型（视频帧 / 摄像头截图 / 知识库视频分析）
)

// ModelConfig 模型配置
type ModelConfig struct {
	ID              int64   `json:"id" gorm:"primaryKey"`
	Provider        string  `json:"provider" gorm:"size:255;not null"` // 厂商标识
	BaseURL         string  `json:"baseUrl" gorm:"size:512;not null"`
	APIKey          string  `json:"apiKey,omitempty" gorm:"size:512"`
	ModelName       string  `json:"modelName" gorm:"size:255;not null"`
	Temperature     float64 `json:"temperature" gorm:"default:0.7"`
	MaxTokens       int     `json:"maxTokens" gorm:"default:4096"`
	IsActive        bool    `json:"isActive" gorm:"default:false;index"`
	ModelType       string  `json:"modelType" gorm:"size:32;index;default:CHAT"`
	CompletionsPath string  `json:"completionsPath" gorm:"size:255"` // Chat 模型路径，如 /v1/chat/completions
	EmbeddingsPath  string  `json:"embeddingsPath" gorm:"size:255"`  // Embedding 模型路径
	ProxyEnabled    bool    `json:"proxyEnabled" gorm:"default:false"`
	ProxyHost       string  `json:"proxyHost" gorm:"size:255"`
	ProxyPort       int     `json:"proxyPort"`
	ProxyUsername   string  `json:"proxyUsername" gorm:"size:128"`
	ProxyPassword   string  `json:"proxyPassword" gorm:"size:128"`
	// 价格（模型目录 / 成本核算用），单位：分（人民币），按每 1K token 计
	PriceInPer1K  float64   `json:"priceInPer1k" gorm:"column:price_in_per1k;default:0"`
	PriceOutPer1K float64   `json:"priceOutPer1k" gorm:"column:price_out_per1k;default:0"`
	BillingType   string    `json:"billingType" gorm:"size:32;default:token"` // token / time
	Currency      string    `json:"currency" gorm:"size:16;default:CNY"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	IsDeleted     bool      `json:"-" gorm:"default:false;index"`
}

// UnitPriceText 返回单价的友好展示（分 → 元）。
func (m *ModelConfig) UnitPriceText() string {
	in := m.PriceInPer1K / 100.0
	out := m.PriceOutPer1K / 100.0
	return fmt.Sprintf("¥%.4f/¥%.4f (in/out per 1K)", in, out)
}

// ---------- Prompt 配置 ----------

// Prompt 类型
const (
	PromptTypeIntentRecognition   = "intent-recognition"     // 意图识别
	PromptTypeQueryEnhancement    = "query-enhancement"      // 查询增强
	PromptTypeFeasibilityAssess   = "feasibility-assessment" // 可行性评估
	PromptTypePlanner             = "planner"                // 计划生成
	PromptTypeVideoAnalyze        = "video-analyze"          // 视频分析
	PromptTypeSceneDescribe       = "scene-describe"         // 场景描述
	PromptTypeSummary             = "summary"                // 摘要生成
	PromptTypeReport              = "report"                 // 报告生成
	PromptTypeChartSelector       = "chart-selector"         // 图表选择器
	PromptTypeSemanticConsistency = "semantic-consistency"   // 语义一致性
	PromptTypeAnswer              = "answer"                 // 问答
	PromptTypeJsonFix             = "json-fix"               // JSON 修复
	PromptTypeCameraVision        = "camera-vision-analysis" // 摄像头事件结构化分析
	PromptTypeFrameDescription    = "frame-description"      // 视频帧画面描述
)

// PromptConfig Prompt 配置
type PromptConfig struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	Name         string    `json:"name" gorm:"size:255;not null"`
	PromptType   string    `json:"promptType" gorm:"size:64;index;not null"`
	AgentID      int64     `json:"agentId" gorm:"index"` // 0 表示全局
	SystemPrompt string    `json:"systemPrompt" gorm:"type:text;not null"`
	Enabled      bool      `json:"enabled" gorm:"default:true;index"`
	Description  string    `json:"description" gorm:"size:512"`
	Priority     int       `json:"priority" gorm:"default:0"` // 数字越大优先级越高
	DisplayOrder int       `json:"displayOrder" gorm:"default:0"`
	CreatorID    int64     `json:"creatorId"`
	CreatorName  string    `json:"creatorName" gorm:"size:64"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ---------- 报告 Report ----------

// 报告状态
const (
	ReportStatusGenerating = "generating"
	ReportStatusReady      = "ready"
	ReportStatusFailed     = "failed"
)

// 报告类型
const (
	ReportTypeAnalysis = "analysis" // 分析报告
	ReportTypeSummary  = "summary"  // 摘要报告
	ReportTypeCustom   = "custom"   // 自定义报告
)

// Report 智能报告
type Report struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	AgentID      int64     `json:"agentId" gorm:"index"`
	SessionID    int64     `json:"sessionId" gorm:"index"`
	Title        string    `json:"title" gorm:"size:255;not null"`
	ReportType   string    `json:"reportType" gorm:"size:32;index;default:analysis"`
	Status       string    `json:"status" gorm:"size:32;index;default:generating"`
	Content      string    `json:"content" gorm:"type:text"`     // Markdown 内容
	HTMLContent  string    `json:"htmlContent" gorm:"type:text"` // HTML 内容（含 ECharts）
	Charts       string    `json:"charts" gorm:"type:text"`      // 图表配置 JSON 数组
	VideoIDs     string    `json:"videoIds" gorm:"size:512"`     // 关联视频 ID，逗号分隔
	CreatorID    int64     `json:"creatorId" gorm:"index"`
	CreatorName  string    `json:"creatorName" gorm:"size:64"`
	ErrorMessage string    `json:"errorMessage" gorm:"type:text"`
	CreatedAt    time.Time `json:"createdAt"`
	UpdatedAt    time.Time `json:"updatedAt"`
}

// ---------- API 响应结构 ----------

// ApiResponse 统一响应
type ApiResponse struct {
	Code    int         `json:"code"`
	Message string      `json:"message"`
	Data    interface{} `json:"data,omitempty"`
}

// PageResult 分页结果
type PageResult struct {
	List     interface{} `json:"list"`
	Total    int64       `json:"total"`
	Page     int         `json:"page"`
	PageSize int         `json:"pageSize"`
}
