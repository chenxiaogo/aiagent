package model

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ---------- 对外交付（Agent-as-a-Service） ----------
//
// 核心设计：编辑态与运行态分离。
//   - 编辑态：agents 表及其子表（技能 / 预设问题 / MCP 配置），管理员随意改；
//   - 运行态：agent_releases 里的不可变快照，外部客户调用时只读快照。
//
// 因此管理员改配置不会影响线上客户行为，直到「发布新版本」才生效，且可一键回滚。

// 智能体可见性
const (
	VisibilityPrivate = "private" // 仅授权客户可调用
	VisibilityOrg     = "org"     // 所有登录用户可调用
	VisibilityPublic  = "public"  // 公开调用（仍受凭据与配额限制）
)

// 发布版本状态
const (
	ReleaseStatusPublished = "published" // 已发布（对外可调用）
	ReleaseStatusArchived  = "archived"  // 已归档（历史版本，不再作为默认）
)

// 访问凭据状态
const (
	ClientStatusActive  = "active"
	ClientStatusRevoked = "revoked"
	ClientStatusExpired = "expired"
)

// 调用协议（同时作为凭据作用域 scope 的取值与计量维度）
const (
	ProtocolMCP     = "mcp"      // MCP 客户端接入
	ProtocolChatAPI = "chat_api" // OpenAI 兼容接口
	ProtocolPortal  = "portal"   // 网页门户 / 嵌入
)

// 默认作用域：新建凭据时未指定则给全部
var DefaultClientScopes = []string{ProtocolMCP, ProtocolChatAPI, ProtocolPortal}

// Tenant 客户。
//
// 客户主体是平台的用户（User）：授权智能体给谁调用，就是从用户列表里挑一个人。
// 这里不复制用户信息，只用一个 UserID 指过去，避免两处信息不一致。
// 用户被停用/删除后，其客户记录保留（历史订阅与调用记录要能追溯），
// 但 isUserActive 会回落为 false，授权界面据此提示。
type Tenant struct {
	ID       int64  `json:"id" gorm:"primaryKey"`
	// default:0 不能去掉：tenants 是既有表，加 NOT NULL 列时 PostgreSQL 需要靠默认值
	// 填充存量行，否则 AutoMigrate 直接报 23502 启动失败。
	// 回填真实用户由 store.MigrateTenantUsers 在启动后按名称匹配完成。
	UserID   int64  `json:"userId" gorm:"index;not null;default:0"` // 关联的 platform 用户，0 = 未关联
	Username string `json:"username" gorm:"size:64;index"`
	Name     string `json:"name" gorm:"size:128;not null;uniqueIndex"`
	Code     string `json:"code" gorm:"size:64;index"`

	Contact    string    `json:"contact" gorm:"size:128"`
	Status     int       `json:"status" gorm:"default:1;index"` // 1=启用 0=停用
	QuotaRPM   int       `json:"quotaRpm" gorm:"default:60"`
	QuotaTPD   int       `json:"quotaTpd" gorm:"default:10000"`
	BrandLogo  string    `json:"brandLogo" gorm:"size:512"`
	BrandColor string    `json:"brandColor" gorm:"size:32"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// AgentRelease 智能体发布版本（不可变快照）。
type AgentRelease struct {
	ID          int64     `json:"id" gorm:"primaryKey"`
	AgentID     int64     `json:"agentId" gorm:"index;not null"`
	Version     string    `json:"version" gorm:"size:32;not null"`
	Snapshot    string    `json:"snapshot" gorm:"type:text"`
	Changelog   string    `json:"changelog" gorm:"size:512"`
	Status      string    `json:"status" gorm:"size:32;index;default:published"`
	IsDefault   bool      `json:"isDefault" gorm:"index;default:false"` // latest 指向
	ToolCount   int       `json:"toolCount"`                            // 快照暴露的工具数，列表页展示用
	PublishedBy string    `json:"publishedBy" gorm:"size:64"`
	PublishedAt time.Time `json:"publishedAt"`
	CreatedAt   time.Time `json:"createdAt"`
}

// AgentClient 发给客户的访问凭据。
// KeyHash 为明文 Key 的 SHA-256，明文只在创建时返回一次。
type AgentClient struct {
	ID              int64      `json:"id" gorm:"primaryKey"`
	AgentID         int64      `json:"agentId" gorm:"index;not null"`
	TenantID        int64      `json:"tenantId" gorm:"index"`
	TenantName      string     `json:"tenantName" gorm:"size:128"`
	Name            string     `json:"name" gorm:"size:128;not null"`
	KeyPrefix       string     `json:"keyPrefix" gorm:"size:16;index"`
	KeyHash         string     `json:"-" gorm:"size:64;uniqueIndex"`
	Scopes          string     `json:"scopes" gorm:"size:255"`   // 逗号分隔：mcp,chat_api,portal
	PinnedVersion   string     `json:"pinnedVersion" gorm:"size:32"` // 钉版本，空表示跟随默认版本
	IPAllowList     string     `json:"ipAllowList" gorm:"size:512"`
	OriginAllowList string     `json:"originAllowList" gorm:"size:512"`
	ExpiresAt       *time.Time `json:"expiresAt"`
	QuotaRPM        int        `json:"quotaRpm" gorm:"default:60"`
	QuotaTPD        int        `json:"quotaTpd" gorm:"default:10000"`
	Status          string     `json:"status" gorm:"size:32;index;default:active"`
	LastUsedAt      *time.Time `json:"lastUsedAt"`
	CreatedBy       string     `json:"createdBy" gorm:"size:64"`
	CreatedAt       time.Time  `json:"createdAt"`
	UpdatedAt       time.Time  `json:"updatedAt"`
}

// HasScope 判断凭据是否具备某个作用域。
func (c *AgentClient) HasScope(scope string) bool {
	if c == nil || c.Scopes == "" {
		return false
	}
	for _, s := range strings.Split(c.Scopes, ",") {
		if strings.TrimSpace(s) == scope {
			return true
		}
	}
	return false
}

// IsUsable 凭据是否处于可用状态（未吊销且未过期）。
func (c *AgentClient) IsUsable(now time.Time) bool {
	if c == nil || c.Status != ClientStatusActive {
		return false
	}
	return c.ExpiresAt == nil || c.ExpiresAt.After(now)
}

// 注：客户授权统一用 Subscription（客户 × 产品 × 套餐，可钉版本）表达，
// 不再单独设 AgentAccessGrant，避免两套授权机制并存。

// UsageRecord 计量记录，按「凭据 × 日期 × 协议」聚合。
type UsageRecord struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	TenantID  int64     `json:"tenantId" gorm:"index"`
	AgentID   int64     `json:"agentId" gorm:"index"`
	ClientID  int64     `json:"clientId" gorm:"index"`
	ReleaseID int64     `json:"releaseId" gorm:"index"`
	Day       string    `json:"day" gorm:"size:10;index"` // YYYY-MM-DD
	Protocol  string    `json:"protocol" gorm:"size:32;index"`
	Requests  int       `json:"requests"`
	Errors    int       `json:"errors"`
	ToolCalls int       `json:"toolCalls"`
	TokensIn  int       `json:"tokensIn"`
	TokensOut int       `json:"tokensOut"`
	LatencyMs int64     `json:"latencyMs"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// 智能体资源绑定类型（运行数据按类型隔离，避免对外交付时跨 Agent 数据泄漏）。
const (
	ResourceTypeKnowledgeBase = "knowledge_base" // 知识库（文档检索型）
	ResourceTypeVideoSource   = "video_source"   // 视频数据源（视频检索型）
	ResourceTypeCameraEvent   = "camera_event"   // 摄像头事件（摄像头检索型）
	ResourceTypeHostGroup     = "host_group"     // 主机组（运维型）
)

// AgentResource 智能体与运行数据的绑定关系（多对多）。
// 发布时按此关系把资源 ID 冻结进快照，MCP 工具只检索该智能体显式绑定的数据，
// 从而根治「文档型 Agent 能搜到全平台所有知识库」之类的数据泄漏。
type AgentResource struct {
	ID           int64     `json:"id" gorm:"primaryKey"`
	AgentID      int64     `json:"agentId" gorm:"uniqueIndex:uk_agent_resource;index;not null"`
	ResourceType string    `json:"resourceType" gorm:"uniqueIndex:uk_agent_resource;size:32;not null"`
	ResourceID   int64     `json:"resourceId" gorm:"uniqueIndex:uk_agent_resource;not null;index"`
	CreatedAt    time.Time `json:"createdAt"`
}

// ---------- 发布快照 ----------

// SnapshotResources 冻结在快照里的「资源绑定」，按类型拆好，运行时直接消费。
// 新增可绑定的资源类型时必须同步加到这里，否则该类资源会失去发布约束。
type SnapshotResources struct {
	KnowledgeBaseIDs []int64 `json:"knowledgeBaseIds"`
	VideoSourceIDs   []int64 `json:"videoSourceIds"`
	CameraEventIDs   []int64 `json:"cameraEventIds"`
	HostGroupIDs     []int64 `json:"hostGroupIds"`
}

// AgentReleaseSnapshot 一次发布冻结下来的全部运行要素。
//
// 这里是发布流程的核心：快照里没有的要素，运行时就不会生效。
// 因此新增任何影响智能体行为的配置，都必须同步加进快照并在
// store.BuildAgentReleaseSnapshot 里落盘，否则会出现「改了不生效」或「没发布就生效」。
type AgentReleaseSnapshot struct {
	AgentID       int64  `json:"agentId"`
	AgentName     string `json:"agentName"`
	AgentDesc     string `json:"agentDesc"`
	Avatar        string `json:"avatar"`
	Category      string `json:"category"`
	Visibility    string `json:"visibility"`
	Prompt        string `json:"prompt"`
	ChatModelID   int64  `json:"chatModelId"`
	EmbedModelID  int64  `json:"embedModelId"`
	// 运行参数：决定对话怎么跑，必须与提示词一样受发布约束
	RuntimeType   string `json:"runtimeType"`
	MaxSteps      int    `json:"maxSteps"`
	MemoryEnabled bool   `json:"memoryEnabled"`
	MemoryParams  string `json:"memoryParams"`
	// ToolLibIDs 挂载的工具库（内置工具）ID，空表示沿用「全部内置工具」
	ToolLibIDs    []int64                `json:"toolLibIds"`
	ModelBindings []SnapshotModelBinding `json:"modelBindings"`
	PresetQuestions []string             `json:"presetQuestions"`
	Skills          []SnapshotSkill      `json:"skills"`
	MCPServers      []SnapshotMCPServer  `json:"mcpServers"`
	ExposedTools    []string             `json:"exposedTools"`
	Resources       SnapshotResources    `json:"resources"`
	Policy          SnapshotPolicy       `json:"policy"`
}

// SnapshotSkill 快照中的技能。
type SnapshotSkill struct {
	Name        string `json:"name"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Summary     string `json:"summary"`
	Content     string `json:"content"`
	SortOrder   int    `json:"sortOrder"`
}

// SnapshotMCPServer 快照中的上游 MCP 服务器（智能体作为 MCP Client 去连接谁）。
type SnapshotMCPServer struct {
	Name      string `json:"name"`
	Transport string `json:"transport"`
	URL       string `json:"url"`
	// Headers 附加请求头（JSON 对象字符串，含鉴权），运行时连接上游必须用到
	Headers string `json:"headers"`
	// ApprovalRequired nil 或 true 视为需要人工审批（保守默认）
	ApprovalRequired *bool `json:"approvalRequired"`
}

// SnapshotModelBinding 快照中的模型绑定。
type SnapshotModelBinding struct {
	ModelID   int64  `json:"modelId"`
	Role      string `json:"role"`
	IsPrimary bool   `json:"isPrimary"`
	Priority  int    `json:"priority"`
	Params    string `json:"params"`
}

// SnapshotResourceIDs 按类型取出快照冻结的资源 ID，供运行时做数据隔离。
func (s *AgentReleaseSnapshot) SnapshotResourceIDs(resourceType string) []int64 {
	if s == nil {
		return nil
	}
	switch resourceType {
	case ResourceTypeKnowledgeBase:
		return s.Resources.KnowledgeBaseIDs
	case ResourceTypeVideoSource:
		return s.Resources.VideoSourceIDs
	case ResourceTypeCameraEvent:
		return s.Resources.CameraEventIDs
	case ResourceTypeHostGroup:
		return s.Resources.HostGroupIDs
	}
	return nil
}

// Normalize 补齐老版本快照缺失的字段。
// 快照格式随功能迭代会加字段，历史版本解码后新字段为零值；
// 运行时读到零值会退化（如 runtimeType 为空导致跑错运行时），因此统一兜底。
func (s *AgentReleaseSnapshot) Normalize() {
	if s == nil {
		return
	}
	if s.RuntimeType == "" {
		s.RuntimeType = AgentRuntimeEinoV2
	}
	if s.MaxSteps <= 0 {
		s.MaxSteps = 8
	}
	if s.Visibility == "" {
		s.Visibility = VisibilityPrivate
	}
	if s.Category == "" {
		s.Category = AgentCategoryGeneral
	}
	if s.Skills == nil {
		s.Skills = []SnapshotSkill{}
	}
	if s.MCPServers == nil {
		s.MCPServers = []SnapshotMCPServer{}
	}
	if s.PresetQuestions == nil {
		s.PresetQuestions = []string{}
	}
	if s.ExposedTools == nil {
		s.ExposedTools = []string{}
	}
}

// SnapshotPolicy 运行策略（只读 / 审批 / 预算），P0 全默认，P1 可在基础配置里编辑。
type SnapshotPolicy struct {
	ReadOnly        bool `json:"readOnly"`
	RequireApproval bool `json:"requireApproval"`
	MaxToolCalls    int  `json:"maxToolCalls"`
	MaxRuntimeSec   int  `json:"maxRuntimeSec"`
}

// DefaultPolicy 默认运行策略：对外交付一律先给只读 + 保守预算，避免客户侧触发副作用。
func DefaultPolicy() SnapshotPolicy {
	return SnapshotPolicy{
		ReadOnly:        true,
		RequireApproval: false,
		MaxToolCalls:    16,
		MaxRuntimeSec:   300,
	}
}

// Encode 序列化快照。
func (s *AgentReleaseSnapshot) Encode() (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// DecodeAgentReleaseSnapshot 反序列化快照。
func DecodeAgentReleaseSnapshot(raw string) (*AgentReleaseSnapshot, error) {
	var snap AgentReleaseSnapshot
	if err := json.Unmarshal([]byte(raw), &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ---------- 版本差异（发布前的变更清单） ----------

// 变更类型
const (
	ChangeKindAdded   = "added"   // 新增
	ChangeKindRemoved = "removed" // 移除
	ChangeKindChanged = "changed" // 修改
)

// ReleaseChange 一条配置变更的可读描述。用于「发布新版本」时向操作人展示
// 这次到底改了什么，避免只填一句 changelog 却说不清变更范围。
type ReleaseChange struct {
	Field string `json:"field"`
	Label string `json:"label"`
	Kind  string `json:"kind"` // added / removed / changed
	Old   string `json:"old"`
	New   string `json:"new"`
}

// DiffReleaseSnapshots 对比两份快照，产出变更清单。
// base 为当前线上生效版本（可为 nil，表示从未发布），next 为草稿。
// 只比对影响运行行为的要素，创建时间之类不参与。
func DiffReleaseSnapshots(base, next *AgentReleaseSnapshot) []ReleaseChange {
	if next == nil {
		return nil
	}
	out := make([]ReleaseChange, 0, 8)
	add := func(field, label, kind, old, new string) {
		out = append(out, ReleaseChange{Field: field, Label: label, Kind: kind, Old: old, New: new})
	}

	// 长文本只比较是否变化，不把全文塞进 diff（前端展示不下，也没必要）
	if base == nil {
		if next.Prompt != "" {
			add("prompt", "系统提示词", ChangeKindAdded, "", fmt.Sprintf("%d 字", len(next.Prompt)))
		}
		for _, name := range namedList(next.Skills, func(s SnapshotSkill) string { return s.Name }) {
			add("skills", "技能", ChangeKindAdded, "", name)
		}
		for _, name := range next.MCPServers {
			add("mcpServers", "MCP 服务器", ChangeKindAdded, "", name.Name)
		}
		for _, q := range next.PresetQuestions {
			add("presetQuestions", "预设问题", ChangeKindAdded, "", q)
		}
		for _, id := range next.ToolLibIDs {
			add("toolLibIds", "内置工具", ChangeKindAdded, "", fmt.Sprintf("#%d", id))
		}
		for _, b := range next.ModelBindings {
			add("modelBindings", "模型绑定", ChangeKindAdded, "", ModelRoleText(b.Role)+fmt.Sprintf(" #%d", b.ModelID))
		}
		if n := len(next.Resources.KnowledgeBaseIDs) + len(next.Resources.VideoSourceIDs) + len(next.Resources.CameraEventIDs); n > 0 {
			add("resources", "数据资源", ChangeKindAdded, "", fmt.Sprintf("%d 项", n))
		}
		return out
	}

	if base.Prompt != next.Prompt {
		add("prompt", "系统提示词", ChangeKindChanged, fmt.Sprintf("%d 字", len(base.Prompt)), fmt.Sprintf("%d 字", len(next.Prompt)))
	}
	if base.Category != next.Category {
		add("category", "智能体类型", ChangeKindChanged, base.Category, next.Category)
	}
	if base.RuntimeType != next.RuntimeType {
		add("runtimeType", "运行时", ChangeKindChanged, base.RuntimeType, next.RuntimeType)
	}
	if base.MaxSteps != next.MaxSteps {
		add("maxSteps", "最大步骤", ChangeKindChanged, strconv.Itoa(base.MaxSteps), strconv.Itoa(next.MaxSteps))
	}
	if base.MemoryEnabled != next.MemoryEnabled {
		add("memoryEnabled", "会话记忆", ChangeKindChanged, boolText(base.MemoryEnabled), boolText(next.MemoryEnabled))
	}
	if base.MemoryParams != next.MemoryParams {
		add("memoryParams", "记忆参数", ChangeKindChanged, base.MemoryParams, next.MemoryParams)
	}
	if base.AgentName != next.AgentName {
		add("agentName", "名称", ChangeKindChanged, base.AgentName, next.AgentName)
	}
	if base.AgentDesc != next.AgentDesc {
		add("agentDesc", "描述", ChangeKindChanged, base.AgentDesc, next.AgentDesc)
	}
	if base.Visibility != next.Visibility {
		add("visibility", "可见性", ChangeKindChanged, base.Visibility, next.Visibility)
	}

	diffStringList := func(field, label string, oldList, newList []string) {
		oldSet := make(map[string]bool, len(oldList))
		newSet := make(map[string]bool, len(newList))
		for _, v := range oldList {
			oldSet[v] = true
		}
		for _, v := range newList {
			newSet[v] = true
		}
		for _, v := range newList {
			if !oldSet[v] {
				add(field, label, ChangeKindAdded, "", v)
			}
		}
		for _, v := range oldList {
			if !newSet[v] {
				add(field, label, ChangeKindRemoved, v, "")
			}
		}
	}

	diffStringList("presetQuestions", "预设问题", base.PresetQuestions, next.PresetQuestions)
	diffStringList("exposedTools", "对外暴露工具", base.ExposedTools, next.ExposedTools)

	// 技能：按名字比对，内容变化也标为修改
	oldSkills := make(map[string]SnapshotSkill, len(base.Skills))
	newSkills := make(map[string]SnapshotSkill, len(next.Skills))
	for _, s := range base.Skills {
		oldSkills[s.Name] = s
	}
	for _, s := range next.Skills {
		newSkills[s.Name] = s
	}
	for name, ns := range newSkills {
		os, ok := oldSkills[name]
		if !ok {
			add("skills", "技能", ChangeKindAdded, "", name)
			continue
		}
		if os.Content != ns.Content || os.Kind != ns.Kind || os.Summary != ns.Summary {
			add("skills", "技能", ChangeKindChanged, name, name+"（内容更新）")
		}
	}
	for name := range oldSkills {
		if _, ok := newSkills[name]; !ok {
			add("skills", "技能", ChangeKindRemoved, name, "")
		}
	}

	// MCP 服务器：按名字比对，地址/请求头变化同样影响运行
	oldMCP := make(map[string]SnapshotMCPServer, len(base.MCPServers))
	newMCP := make(map[string]SnapshotMCPServer, len(next.MCPServers))
	for _, m := range base.MCPServers {
		oldMCP[m.Name] = m
	}
	for _, m := range next.MCPServers {
		newMCP[m.Name] = m
	}
	for name, nm := range newMCP {
		om, ok := oldMCP[name]
		if !ok {
			add("mcpServers", "MCP 服务器", ChangeKindAdded, "", name)
			continue
		}
		if om.URL != nm.URL || om.Transport != nm.Transport || om.Headers != nm.Headers {
			add("mcpServers", "MCP 服务器", ChangeKindChanged, name, name+"（连接信息更新）")
		}
	}
	for name := range oldMCP {
		if _, ok := newMCP[name]; !ok {
			add("mcpServers", "MCP 服务器", ChangeKindRemoved, name, "")
		}
	}

	// 工具库挂载
	diffInt64List := func(field, label string, oldList, newList []int64) {
		oldSet := make(map[int64]bool, len(oldList))
		newSet := make(map[int64]bool, len(newList))
		for _, v := range oldList {
			oldSet[v] = true
		}
		for _, v := range newList {
			newSet[v] = true
		}
		for _, v := range newList {
			if !oldSet[v] {
				add(field, label, ChangeKindAdded, "", fmt.Sprintf("#%d", v))
			}
		}
		for _, v := range oldList {
			if !newSet[v] {
				add(field, label, ChangeKindRemoved, fmt.Sprintf("#%d", v), "")
			}
		}
	}
	diffInt64List("toolLibIds", "内置工具", base.ToolLibIDs, next.ToolLibIDs)

	// 模型绑定：按「用途 + 模型」比对
	type bindingKey struct {
		Role    string
		ModelID int64
	}
	oldBind := make(map[bindingKey]SnapshotModelBinding, len(base.ModelBindings))
	newBind := make(map[bindingKey]SnapshotModelBinding, len(next.ModelBindings))
	for _, b := range base.ModelBindings {
		oldBind[bindingKey{b.Role, b.ModelID}] = b
	}
	for _, b := range next.ModelBindings {
		newBind[bindingKey{b.Role, b.ModelID}] = b
	}
	for k, nb := range newBind {
		if _, ok := oldBind[k]; !ok {
			add("modelBindings", "模型绑定", ChangeKindAdded, "", ModelRoleText(k.Role)+fmt.Sprintf(" #%d", k.ModelID))
		} else if ob := oldBind[k]; ob.Priority != nb.Priority || ob.IsPrimary != nb.IsPrimary || ob.Params != nb.Params {
			add("modelBindings", "模型绑定", ChangeKindChanged,
				ModelRoleText(k.Role)+fmt.Sprintf(" #%d", k.ModelID),
				ModelRoleText(k.Role)+fmt.Sprintf(" #%d", k.ModelID))
		}
	}
	for k := range oldBind {
		if _, ok := newBind[k]; !ok {
			add("modelBindings", "模型绑定", ChangeKindRemoved, ModelRoleText(k.Role)+fmt.Sprintf(" #%d", k.ModelID), "")
		}
	}

	// 数据资源
	diffInt64List("kb", "知识库", base.Resources.KnowledgeBaseIDs, next.Resources.KnowledgeBaseIDs)
	diffInt64List("video", "视频数据源", base.Resources.VideoSourceIDs, next.Resources.VideoSourceIDs)
	diffInt64List("camera", "摄像头事件", base.Resources.CameraEventIDs, next.Resources.CameraEventIDs)
	diffInt64List("hostGroup", "主机组", base.Resources.HostGroupIDs, next.Resources.HostGroupIDs)

	return out
}

func namedList[T any](items []T, name func(T) string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, name(it))
	}
	return out
}

func boolText(v bool) string {
	if v {
		return "开启"
	}
	return "关闭"
}

// DefaultExposedTools 按智能体分类给出默认对外暴露的内置工具。
// 后续「MCP 工具」Tab 支持手工勾选后，以快照里的 ExposedTools 为准。
func DefaultExposedTools(category string) []string {
	switch NormalizeAgentCategory(category) {
	case AgentCategoryVideo:
		return []string{"video_search", "video_summary", "list_videos", "agent_info"}
	case AgentCategoryCamera:
		return []string{"camera_search", "agent_info"}
	case AgentCategoryDoc:
		return []string{"doc_search", "agent_info"}
	case AgentCategoryReport:
		return []string{"generate_report", "list_reports", "agent_info"}
	default:
		return []string{"agent_info"}
	}
}

var nonSlugChars = regexp.MustCompile(`[^a-z0-9]+`)

// Slugify 生成对外路径片段。中文名会被过滤为空，此时回退为 agent-<id>，保证全局可读且唯一。
func Slugify(name string, id int64) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = nonSlugChars.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return fmt.Sprintf("agent-%d", id)
	}
	if len(s) > 48 {
		s = strings.Trim(s[:48], "-")
	}
	return fmt.Sprintf("%s-%d", s, id)
}
