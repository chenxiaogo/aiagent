package toolkit

import (
	"context"
	"encoding/json"
	"strings"
)

// ApprovalMode 会话的权限模式，决定「副作用工具」在什么条件下可以执行。
//
// 语义（与红线规则正交：红线无论何种模式都直接拒绝）：
//   - manual：人工审批。普通副作用操作、高风险操作都要用户在聊天框确认后执行。
//   - delegated：委托审批。用户一次性委托本次会话，常规副作用自动执行；
//     高风险操作仍然 fail-closed（拒绝并提示需要完全权限）。
//   - full_access：完全权限。高风险操作也自动执行，仅红线保持阻断。
type ApprovalMode string

const (
	ModeManual     ApprovalMode = "manual"
	ModeDelegated  ApprovalMode = "delegated"
	ModeFullAccess ApprovalMode = "full_access"
)

// RiskHigh 高风险：对应 1Shell 里 approvalRequired 那一档（关机、kill -9、写系统路径等）。
const RiskHigh = "high"

// NormalizeApprovalMode 归一化权限模式，未知输入一律按最保守的人工审批处理。
func NormalizeApprovalMode(value string) ApprovalMode {
	switch ApprovalMode(value) {
	case ModeDelegated:
		return ModeDelegated
	case ModeFullAccess:
		return ModeFullAccess
	default:
		return ModeManual
	}
}

type ctxKeyApprovalMode struct{}

// WithApprovalMode 把会话权限模式注入 context。
func WithApprovalMode(ctx context.Context, mode ApprovalMode) context.Context {
	return context.WithValue(ctx, ctxKeyApprovalMode{}, mode)
}

// ApprovalModeFrom 取出会话权限模式，缺省为人工审批。
func ApprovalModeFrom(ctx context.Context) ApprovalMode {
	if ctx == nil {
		return ModeManual
	}
	if mode, ok := ctx.Value(ctxKeyApprovalMode{}).(ApprovalMode); ok && mode != "" {
		return mode
	}
	return ModeManual
}

// ApprovalRequest 需要用户确认的工具调用描述。
// 字段名保持通用，接入层（聊天 / 外部 API）负责填充具体的会话与用户上下文。
type ApprovalRequest struct {
	ToolName  string
	Summary   string // 一句话说明要做什么，例如待执行的命令
	Detail    string // 完整参数（JSON，已脱敏）
	Risk      string // medium / high
	Reason    string // 为什么需要确认
	SessionID int64
	UserID    int64
	AgentID   int64
}

// Approver 人工确认通道：工具需要用户批准时挂起等待用户决策。
// 由接入层实现并注入 context，工具层不感知具体交互方式（聊天框 / 终端 / API）。
type Approver interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (approved bool, remember bool, comment string, err error)
}

// RiskAssessor 工具调用风险评估：返回风险等级、原因，以及是否属于红线（blocked 时直接拒绝，不再询问）。
type RiskAssessor func(toolName string, args map[string]any) (risk string, reason string, blocked bool)

type ctxKeyApprover struct{}
type ctxKeyRiskAssessor struct{}

// WithApprover 把人工确认通道注入 context。
func WithApprover(ctx context.Context, a Approver) context.Context {
	if a == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyApprover{}, a)
}

// ApproverFrom 取出 context 中的人工确认通道。
func ApproverFrom(ctx context.Context) (Approver, bool) {
	if ctx == nil {
		return nil, false
	}
	a, ok := ctx.Value(ctxKeyApprover{}).(Approver)
	return a, ok && a != nil
}

// WithRiskAssessor 把风险评估函数注入 context。
func WithRiskAssessor(ctx context.Context, assess RiskAssessor) context.Context {
	if assess == nil {
		return ctx
	}
	return context.WithValue(ctx, ctxKeyRiskAssessor{}, assess)
}

// RiskAssessorFrom 取出 context 中的风险评估函数。
func RiskAssessorFrom(ctx context.Context) (RiskAssessor, bool) {
	if ctx == nil {
		return nil, false
	}
	a, ok := ctx.Value(ctxKeyRiskAssessor{}).(RiskAssessor)
	return a, ok && a != nil
}

// SummarizeToolCall 生成给人看的一句话摘要，供确认卡片展示。
func SummarizeToolCall(toolName string, args map[string]any) string {
	switch toolName {
	case "exec_command":
		if cmd := stringArg(args, "command"); cmd != "" {
			return cmd
		}
	case "write_file":
		if path := stringArg(args, "path"); path != "" {
			return "写入文件 " + path
		}
	case "read_file", "list_dir":
		if path := stringArg(args, "path"); path != "" {
			return toolName + " " + path
		}
	}
	return toolName
}

// MarshalArgsForDisplay 序列化工具参数用于前端展示，敏感字段打码。
func MarshalArgsForDisplay(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	safe := make(map[string]any, len(args))
	for k, v := range args {
		safe[k] = redactValue(k, v)
	}
	data, err := json.Marshal(safe)
	if err != nil {
		return "{}"
	}
	return string(data)
}

// redactValue 对密钥类字段打码，避免凭据随确认卡片回传到前端。
func redactValue(key string, value any) any {
	lower := strings.ToLower(key)
	for _, sensitive := range []string{"password", "passwd", "secret", "token", "apikey", "api_key", "privatekey", "private_key", "passphrase"} {
		if strings.Contains(lower, sensitive) {
			return "***"
		}
	}
	// 超长内容截断，避免确认卡片被大段文本撑爆
	if s, ok := value.(string); ok && len(s) > 2000 {
		return s[:2000] + "…（已截断）"
	}
	return value
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	v, ok := args[key]
	if !ok || v == nil {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return ""
	}
	return s
}
