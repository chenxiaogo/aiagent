package model

import (
	"encoding/json"
	"time"
)

// ---------- 能力市场（平台级可复用资产） ----------
//
// 与「智能体私有配置」（agents / agent_mcp_servers / agent_skills）解耦：
//   - 市场资产是平台沉淀的、可被多个智能体引用的公共能力；
//   - 智能体引用市场资产的 ID，而非各自维护一份拷贝，便于统一治理与版本演进。

// 资产可见性
const (
	AssetVisibilityPublic  = "public"  // 全平台可见可用
	AssetVisibilityPrivate = "private" // 仅创建者/内部可用
)

// MCPRegistry 平台级 MCP 服务注册表（可复用目录）。
// 与 AgentMCPServer（智能体私有 MCP 连接）不同：注册表是「资产目录」，
// 智能体通过引用 registry_id 挂载，避免每个 Agent 重复填连接信息。
type MCPRegistry struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;not null;uniqueIndex"`
	Description string    `json:"description" gorm:"size:512"`
	Transport   string    `json:"transport" gorm:"size:32;not null;default:sse"` // sse / streamable_http
	URL         string    `json:"url" gorm:"size:512;not null"`
	Headers     string    `json:"headers" gorm:"type:text"` // JSON 对象字符串，附加请求头
	Category    string    `json:"category" gorm:"size:64;index"`
	Version     string    `json:"version" gorm:"size:32"`
	OwnerName   string    `json:"ownerName" gorm:"size:64"`
	Visibility  string    `json:"visibility" gorm:"size:32;default:public;index"`
	Status      int       `json:"status" gorm:"default:1;index"` // 1=启用 0=停用
	// ApprovalRequired 该 MCP 服务的工具是否需要人工审批。
	// nil 或 true 表示需要（保守默认）；false 表示免审批、直接执行。
	// 智能体从注册表导入时会继承该设置，无需每个智能体再单独配一遍。
	ApprovalRequired *bool `json:"approvalRequired" gorm:"default:true"`
	RefCount    int       `json:"refCount" gorm:"-"`              // 被多少智能体引用（运行时聚合）
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// SkillLibrary 平台级技能库（可复用技能）。
// 与 AgentSkill（智能体私有技能）不同：技能库是公共资产，智能体通过引用 skill_lib_id 挂载。
type SkillLibrary struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"size:512"`
	// Summary 触发条件/摘要，市场技能被智能体引用后作为 AgentSkill.Summary 快照。
	Summary     string    `json:"summary" gorm:"size:512"`
	Kind        string    `json:"kind" gorm:"size:32;not null;default:prompt"` // prompt / tool
	Category    string    `json:"category" gorm:"size:64;index"`
	Content     string    `json:"content" gorm:"type:text"`
	OwnerName   string    `json:"ownerName" gorm:"size:64"`
	Visibility  string    `json:"visibility" gorm:"size:32;default:public;index"`
	Status      int       `json:"status" gorm:"default:1;index"`
	RefCount    int       `json:"refCount" gorm:"-"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ToolLibrary 平台级工具库（可复用工具定义）。
// 与 MCP 远端工具不同：工具库是平台内置/注册的本地工具定义，
// Agent 通过 tool_lib_ids 引用挂载，统一管理、可复用。
type ToolLibrary struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;not null;uniqueIndex"`
	Description string    `json:"description" gorm:"size:512"`
	Category    string    `json:"category" gorm:"size:64;index"`
	// Parameters JSON 字符串，工具入参定义（键值对，值为 {type, desc, required}）
	Parameters string `json:"parameters" gorm:"type:text"`
	// ToolType 工具类型：builtin（内置代码实现）/ http（HTTP 调用）
	ToolType string `json:"toolType" gorm:"size:32;not null;default:builtin"`
	// Config 工具配置（JSON，HTTP 工具存 URL/method/headers 等，builtin 工具可为空）
	Config string `json:"config" gorm:"type:text"`
	// Metadata 治理元数据（JSON）：readOnly / sideEffect / approvalRequired / resourceTypes 等
	Metadata   string    `json:"metadata" gorm:"type:text"`
	OwnerName  string    `json:"ownerName" gorm:"size:64"`
	Visibility string    `json:"visibility" gorm:"size:32;default:public;index"`
	Status     int       `json:"status" gorm:"default:1;index"` // 1=启用 0=停用
	RefCount   int       `json:"refCount" gorm:"-"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// ToolLibraryMetadata 工具库治理元数据（与 toolkit.Metadata 对齐）。
type ToolLibraryMetadata struct {
	ReadOnly         bool     `json:"readOnly"`
	SideEffect       bool     `json:"sideEffect"`
	ApprovalRequired bool     `json:"approvalRequired"`
	ResourceTypes    []string `json:"resourceTypes"`
}

// AgentTemplate 智能体模板（预置配置快照，可一键创建 Agent）。
// 快照包含分类、提示词、是否含知识库/视频/摄像头形态等，新建 Agent 时套用。
type AgentTemplate struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	Name        string    `json:"name" gorm:"size:128;not null"`
	Description string    `json:"description" gorm:"type:text"`
	Category    string    `json:"category" gorm:"size:64;index"`
	Icon        string    `json:"icon" gorm:"size:64"`
	// Snapshot 预置的 Agent 配置（JSON）：prompt / tags / 默认工具集等
	Snapshot    string    `json:"snapshot" gorm:"type:text"`
	OwnerName   string    `json:"ownerName" gorm:"size:64"`
	Visibility  string    `json:"visibility" gorm:"size:32;default:public;index"`
	UsageCount  int       `json:"usageCount" gorm:"default:0"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AgentTemplateSnapshot 模板里的预置配置。
type AgentTemplateSnapshot struct {
	Prompt       string   `json:"prompt"`
	Tags         string   `json:"tags"`
	ExposedTools []string `json:"exposedTools"`
	PresetQuestions []string `json:"presetQuestions"`
}

// Encode 序列化模板快照。
func (s *AgentTemplateSnapshot) Encode() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeAgentTemplateSnapshot 反序列化模板快照。
func DecodeAgentTemplateSnapshot(raw string) (*AgentTemplateSnapshot, error) {
	var snap AgentTemplateSnapshot
	if raw == "" {
		return &snap, nil
	}
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ---------- 模型目录与路由 ----------

// 模型计费维度
const (
	ModelBillingTypeToken = "token" // 按 token 计费
	ModelBillingTypeTime  = "time"  // 按时长计费（如某些专有服务）
)

// 在 ModelConfig 已有的字段基础上，模型目录扩展「价格」以支撑成本核算：
//   - PriceInPer1K  ：每 1K 输入 token 价格，单位：分（人民币）
//   - PriceOutPer1K ：每 1K 输出 token 价格，单位：分
//   - BillingType   ：计费方式（token / time）
//   - Currency      ：货币（默认 CNY）
// 这些字段直接挂在 ModelConfig 上（见 model.go 的 ModelConfig 扩展），此处仅定义常量。

// ModelRoutingRule 平台级模型路由规则。
// 当一次请求进来时，按「匹配条件」选择目标模型（优先匹配高优先级规则）：
//   - MatchCategory ：智能体分类（空=任意）
//   - MatchKeyword ：用户消息关键词（逗号分隔，空=任意）
//   - Strategy     ：cost（选最便宜）/ smart（按复杂度）/ manual（用指定 model_id）
//   - TargetModelID：manual 策略下指定的模型
//
// 路由规则与 AgentModel 回退链协同：路由规则先决定「用哪个模型族」，
// AgentModel 的 priority 再在族内做主备回退。
type ModelRoutingRule struct {
	ID            int64     `json:"id" gorm:"primaryKey"`
	Name          string    `json:"name" gorm:"size:128;not null"`
	MatchCategory string    `json:"matchCategory" gorm:"size:64;index"`  // 空=任意
	MatchKeyword  string    `json:"matchKeyword" gorm:"size:256"`        // 逗号分隔，空=任意
	Strategy      string    `json:"strategy" gorm:"size:32;not null;default:cost"` // cost / smart / manual
	TargetModelID int64     `json:"targetModelId" gorm:"index"`          // manual 策略指向的模型
	Priority      int       `json:"priority" gorm:"default:10"`          // 数字小者优先
	Enabled       bool      `json:"enabled" gorm:"default:true;index"`
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
}

// 模型路由策略
const (
	RoutingStrategyCost  = "cost"  // 成本优先：同一匹配下选单价最低且可用的模型
	RoutingStrategySmart = "smart" // 智能：按请求特征（如长度/复杂度）选模型
	RoutingStrategyManual = "manual" // 指定模型：直接用 TargetModelID
)

// IsValidRoutingStrategy 校验策略合法性。
func IsValidRoutingStrategy(s string) bool {
	switch s {
	case RoutingStrategyCost, RoutingStrategySmart, RoutingStrategyManual:
		return true
	}
	return false
}

// ---------- 调用观测（LLM / MCP 调用日志） ----------

// CallLog 每次 LLM / MCP 调用的观测记录。
// 与 UsageRecord（按客户×日聚合计量）不同，CallLog 是「逐条明细」，用于排障与成本下钻。
type CallLog struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	TenantID     int64    `json:"tenantId" gorm:"index"`
	AgentID     int64     `json:"agentId" gorm:"index"`
	ClientID    int64     `json:"clientId" gorm:"index"`
	ReleaseID   int64     `json:"releaseId" gorm:"index"`
	CallType    string    `json:"callType" gorm:"size:32;index"` // llm / mcp_tool / embedding
	ModelID     int64     `json:"modelId" gorm:"index"`          // 命中的模型（llm/embedding）
	ModelName   string    `json:"modelName" gorm:"size:128"`
	ToolName    string    `json:"toolName" gorm:"size:128"`      // mcp_tool 时记录工具名
	PromptTokens  int     `json:"promptTokens"`
	OutputTokens int      `json:"outputTokens"`
	TotalTokens   int      `json:"totalTokens"`
	// 成本（分）：按模型单价 × token 估算，查询时回填或落库
	CostCents   int64     `json:"costCents"`
	LatencyMs   int64     `json:"latencyMs"`
	Status      int       `json:"status" gorm:"index"` // 1=成功 0=失败
	ErrorMsg    string    `json:"errorMsg" gorm:"size:512"`
	TraceID     string    `json:"traceId" gorm:"size:64;index"`
	CreatedAt   time.Time `json:"createdAt" gorm:"index"`
}

// 调用类型
const (
	CallTypeLLM       = "llm"
	CallTypeMCPTool   = "mcp_tool"
	CallTypeEmbedding = "embedding"
	// CallTypeLLMAux 辅助调用：会话标题生成、记忆摘要、视频/文档分析等后台调用。
	// 与主链路的 llm 分开记，观测页默认只看主链路，避免这些高频低价值的记录淹没列表。
	CallTypeLLMAux = "llm_aux"
)
