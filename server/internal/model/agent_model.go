package model

import "time"

// ---------- 智能体多模型绑定 ----------
//
// 一个智能体可以绑定多个模型：
//   - 按「用途 role」路由：对话走 chat、向量走 embedding、截图/帧理解走 vision、重排走 rerank；
//   - 同一用途内按 priority 形成回退链：主模型超时/限流/报错时自动切下一个。
//
// 保留 agent.chat_model_id / embed_model_id 作为主模型的冗余字段（既有服务仍在读），
// 保存模型列表时会把各用途的 primary 回写过去，保证老链路不受影响。

// 模型用途
const (
	ModelRoleChat      = "chat"      // 对话
	ModelRoleEmbedding = "embedding" // 向量化
	ModelRoleVision    = "vision"    // 视觉理解（视频帧 / 截图）
	ModelRoleRerank    = "rerank"    // 结果重排
	ModelRoleFallback  = "fallback"  // 兜底（任意用途失败后的最后选择）
)

// IsValidModelRole 判断用途是否合法。
func IsValidModelRole(r string) bool {
	switch r {
	case ModelRoleChat, ModelRoleEmbedding, ModelRoleVision, ModelRoleRerank, ModelRoleFallback:
		return true
	}
	return false
}

// ModelRoleText 用途中文名。
func ModelRoleText(r string) string {
	switch r {
	case ModelRoleChat:
		return "对话"
	case ModelRoleEmbedding:
		return "向量化"
	case ModelRoleVision:
		return "视觉理解"
	case ModelRoleRerank:
		return "结果重排"
	case ModelRoleFallback:
		return "兜底"
	}
	return r
}

// AgentModel 智能体与模型的绑定关系。
// 唯一约束：同一智能体下 (model_id, role) 不重复。
type AgentModel struct {
	ID        int64     `json:"id" gorm:"primaryKey"`
	AgentID   int64     `json:"agentId" gorm:"uniqueIndex:idx_agent_model_role,priority:1;not null"`
	ModelID   int64     `json:"modelId" gorm:"uniqueIndex:idx_agent_model_role,priority:2;not null"`
	Role      string    `json:"role" gorm:"uniqueIndex:idx_agent_model_role,priority:3;size:32;not null;default:chat"`
	IsPrimary bool      `json:"isPrimary" gorm:"default:false"`
	Priority  int       `json:"priority" gorm:"default:10"` // 回退顺序，数字小者优先
	Params    string    `json:"params" gorm:"type:text"`    // JSON 覆写：见 AgentModelParams
	Enabled   bool      `json:"enabled" gorm:"default:true"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AgentModelParams 针对该绑定的参数覆写，留空表示沿用模型配置本身的值。
type AgentModelParams struct {
	Temperature *float64 `json:"temperature,omitempty"`
	MaxTokens   *int     `json:"maxTokens,omitempty"`
	TimeoutMs   *int     `json:"timeoutMs,omitempty"`
}
