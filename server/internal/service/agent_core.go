package service

import (
	"fmt"
	"strings"
	"time"
)

// ---------- Tool Policy（工具策略） ----------

// ToolPolicy 工具策略评估结果
type ToolPolicy struct {
	Allow              bool   `json:"allow"`
	Kind               string `json:"kind"`
	Reason             string `json:"reason"`
	ApprovalRequired   bool   `json:"approvalRequired"`
	Interrupt          *Interrupt `json:"interrupt,omitempty"`
}

// Interrupt 中断请求
type Interrupt struct {
	Type    string `json:"type"`    // request_approval / ask_user / request_secret
	Reason  string `json:"reason"`
	Message string `json:"message"`
}

// ReadonlyTools 只读工具白名单
var ReadonlyTools = map[string]bool{
	"search_camera":   true,
	"search_videos":   true,
	"search_docs":     true,
	"get_time":        true,
	"get_stats":       true,
	"list_agents":     true,
	"list_videos":     true,
	"list_cameras":    true,
	"generate_report": false, // 副作用
}

// SideEffectTools 副作用工具
var SideEffectTools = map[string]bool{
	"execute_command":  true,
	"write_file":       true,
	"delete_file":      true,
	"upload_video":     true,
	"process_video":    true,
	"generate_report":  true,
	"send_notification": true,
}

// EvaluateToolPolicy 评估工具调用策略。
func EvaluateToolPolicy(toolName string, readOnly bool, canApprove bool) ToolPolicy {
	name := strings.TrimSpace(toolName)
	if name == "" {
		return ToolPolicy{Allow: false, Kind: "invalid_tool", Reason: "toolName is required"}
	}

	// 只读策略检查
	if readOnly && !ReadonlyTools[name] {
		return ToolPolicy{
			Allow:  false,
			Kind:   "read_only_policy",
			Reason: fmt.Sprintf("Tool %s is not allowed in readOnly mode", name),
		}
	}

	// 副作用工具需要审批
	if SideEffectTools[name] {
		if !canApprove {
			return ToolPolicy{
				Allow:            false,
				Kind:             "approval_required",
				Reason:           fmt.Sprintf("Tool %s requires approval", name),
				ApprovalRequired: true,
				Interrupt: &Interrupt{
					Type:    "request_approval",
					Reason:  fmt.Sprintf("Tool %s has side effects", name),
					Message: fmt.Sprintf("确认执行 %s？", name),
				},
			}
		}
		return ToolPolicy{
			Allow:            true,
			ApprovalRequired: true,
			Kind:             "side_effect",
			Reason:           fmt.Sprintf("Tool %s approved with side effects", name),
		}
	}

	return ToolPolicy{Allow: true, Kind: "allowed"}
}

// ---------- Observation Interpreter（观察解释器） ----------

// Observation 工具调用观察
type Observation struct {
	ID         string   `json:"id"`
	Turn       int      `json:"turn"`
	Kind       string   `json:"kind"`
	ToolName   string   `json:"toolName"`
	Text       string   `json:"text"`
	OK         bool     `json:"ok"`
	IsError    bool     `json:"isError"`
	ExitCode   int      `json:"exitCode"`
	Evidence   []string `json:"evidence"`
	ObservedAt string   `json:"observedAt"`
}

// Interpretation 观察解释结果
type Interpretation struct {
	Facts        []Fact        `json:"facts"`
	Failures     []Failure     `json:"failures"`
	SideEffects  []SideEffect  `json:"sideEffects"`
	Verification *Verification `json:"verification,omitempty"`
}

// Fact 事实
type Fact struct {
	ID         string `json:"id"`
	Turn       int    `json:"turn"`
	Kind       string `json:"kind"`
	ToolName   string `json:"toolName"`
	Text       string `json:"text"`
	Confidence string `json:"confidence"` // observed / inferred / verified
	FactType   string `json:"factType"`
}

// Failure 失败
type Failure struct {
	ID       string `json:"id"`
	Status   string `json:"status"` // unrecovered / recovered
	Error    string `json:"error"`
	ExitCode int    `json:"exitCode"`
}

// SideEffect 副作用
type SideEffect struct {
	ID                  string `json:"id"`
	ToolName            string `json:"toolName"`
	Description         string `json:"description"`
	RequiresVerification bool   `json:"requiresVerification"`
	Verified            bool   `json:"verified"`
}

// InterpretObservation 解释工具调用观察结果。
func InterpretObservation(obs Observation) *Interpretation {
	interp := &Interpretation{}

	if obs.OK && !obs.IsError {
		interp.Facts = append(interp.Facts, Fact{
			ID:         obs.ID,
			Turn:       obs.Turn,
			Kind:       obs.Kind,
			ToolName:   obs.ToolName,
			Text:       truncate(obs.Text, 1000),
			Confidence: "observed",
			FactType:   "tool_result",
		})
	} else {
		interp.Failures = append(interp.Failures, Failure{
			ID:       obs.ID,
			Status:   "unrecovered",
			Error:    obs.Text,
			ExitCode: obs.ExitCode,
		})
	}

	// 副作用工具标记
	if SideEffectTools[obs.ToolName] {
		interp.SideEffects = append(interp.SideEffects, SideEffect{
			ID:                   obs.ID,
			ToolName:             obs.ToolName,
			Description:          fmt.Sprintf("%s executed: %s", obs.ToolName, truncate(obs.Text, 200)),
			RequiresVerification: true,
		})
	}

	return interp
}

// ---------- Verifiers（验证器） ----------

// Verification 验证结果
type Verification struct {
	Required bool   `json:"required"`
	Status   string `json:"status"` // passed / failed / unsupported / missing
	OK       bool   `json:"ok"`
	Reasons  []string `json:"reasons"`
}

// VerifierRequirement 验证需求
type VerifierRequirement struct {
	ID       string `json:"id"`
	Source   string `json:"source"` // output_contract / side_effect
	Type     string `json:"type"`   // command / http / file_exists / outcome
	Required bool   `json:"required"`
	Summary  string `json:"summary"`
}

// EvaluateVerifierPlan 评估验证计划。
func EvaluateVerifierPlan(requirements []VerifierRequirement, verification *Verification) *Verification {
	required := len(requirements) > 0

	if !required {
		return &Verification{Required: false, Status: "not_required", OK: true}
	}
	if verification == nil {
		missing := make([]string, 0, len(requirements))
		for _, r := range requirements {
			missing = append(missing, r.ID)
		}
		return &Verification{Required: true, Status: "missing", OK: false, Reasons: missing}
	}
	if verification.OK {
		return &Verification{Required: true, Status: "passed", OK: true, Reasons: verification.Reasons}
	}
	return &Verification{Required: true, Status: "failed", OK: false, Reasons: verification.Reasons}
}

// ---------- Budget Management（预算管理） ----------

// Budget 预算
type Budget struct {
	MaxToolCalls  int           `json:"maxToolCalls"`
	MaxCommands   int           `json:"maxCommands"`
	MaxRuntime    time.Duration `json:"maxRuntime"`
	ToolCalls     int           `json:"toolCalls"`
	Commands      int           `json:"commands"`
	StartTime     time.Time     `json:"startTime"`
}

// CheckBudget 检查预算是否耗尽。
func (b *Budget) CheckBudget() (bool, string) {
	if b.ToolCalls >= b.MaxToolCalls {
		return false, fmt.Sprintf("tool calls limit reached (%d/%d)", b.ToolCalls, b.MaxToolCalls)
	}
	if b.Commands >= b.MaxCommands {
		return false, fmt.Sprintf("commands limit reached (%d/%d)", b.Commands, b.MaxCommands)
	}
	if time.Since(b.StartTime) > b.MaxRuntime {
		return false, fmt.Sprintf("runtime limit exceeded (%v)", b.MaxRuntime)
	}
	return true, ""
}

// RecordToolCall 记录工具调用。
func (b *Budget) RecordToolCall() { b.ToolCalls++ }
func (b *Budget) RecordCommand()  { b.Commands++ }

// ---------- Outcome Evaluation（结果评估） ----------

// AgentOutcome Agent 运行结果
type AgentOutcome struct {
	TaskStatus string   `json:"taskStatus"` // verified / failed / blocked / unverified
	Reasons    []string `json:"reasons"`
	Phases     []Phase  `json:"phases"`
}

// Phase 阶段
type Phase struct {
	ID       string `json:"id"`
	Status   string `json:"status"`
	Required bool   `json:"required"`
}

// EvaluateAgentOutcome 评估 Agent 运行结果。
func EvaluateAgentOutcome(phases []Phase, verification *Verification, budget *Budget, hasUnresolvedFailure bool) *AgentOutcome {
	outcome := &AgentOutcome{TaskStatus: "unverified"}

	// 检查必需阶段是否失败
	for _, p := range phases {
		if p.Required && p.Status == "failed" {
			outcome.TaskStatus = "failed"
			outcome.Reasons = append(outcome.Reasons, fmt.Sprintf("required_phase_failed:%s", p.ID))
			return outcome
		}
	}

	// 检查预算
	if ok, reason := budget.CheckBudget(); !ok {
		outcome.TaskStatus = "blocked"
		outcome.Reasons = append(outcome.Reasons, reason)
		return outcome
	}

	// 检查未解决的失败
	if hasUnresolvedFailure {
		outcome.TaskStatus = "failed"
		outcome.Reasons = append(outcome.Reasons, "unresolved_tool_failure")
		return outcome
	}

	// 检查验证器
	if verification != nil && verification.Required {
		switch verification.Status {
		case "passed":
			outcome.TaskStatus = "verified"
			outcome.Reasons = append(outcome.Reasons, "verify_passed")
		case "failed":
			outcome.TaskStatus = "failed"
			outcome.Reasons = append(outcome.Reasons, "verify_failed")
		default:
			outcome.TaskStatus = "unverified"
			outcome.Reasons = append(outcome.Reasons, "verify_required")
		}
		return outcome
	}

	// 检查必需阶段是否完成
	for _, p := range phases {
		if p.Required && p.Status != "done" {
			outcome.TaskStatus = "unverified"
			outcome.Reasons = append(outcome.Reasons, fmt.Sprintf("required_phase_incomplete:%s", p.ID))
			return outcome
		}
	}

	outcome.TaskStatus = "verified"
	outcome.Reasons = append(outcome.Reasons, "all_phases_done")
	return outcome
}

// ---------- Decision Policy（决策策略） ----------

// DecisionReview 决策审查
type DecisionReview struct {
	Allow    bool     `json:"allow"`
	Reason   string   `json:"reason"`
	Warnings []string `json:"warnings"`
}

// ReviewDecision 审查 Agent 决策。
func ReviewDecision(action string, toolName string, hasSideEffects bool, budget *Budget) DecisionReview {
	review := DecisionReview{Allow: true}

	// 预算接近上限警告
	if budget.ToolCalls >= budget.MaxToolCalls-3 {
		review.Warnings = append(review.Warnings, fmt.Sprintf("tool calls near limit: %d/%d", budget.ToolCalls, budget.MaxToolCalls))
	}

	// 危险操作拒绝
	if toolName == "execute_command" && strings.Contains(action, "rm -rf") {
		return DecisionReview{Allow: false, Reason: "dangerous command detected: rm -rf"}
	}

	if hasSideEffects {
		review.Warnings = append(review.Warnings, "side effect tool will be used")
	}

	return review
}

// ---------- 工具函数 ----------

func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n]) + "..."
}