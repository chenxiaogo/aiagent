package toolkit

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/cloudwego/eino/components/tool"
	toolutils "github.com/cloudwego/eino/components/tool/utils"
	"github.com/cloudwego/eino/schema"
	"github.com/eino-contrib/jsonschema"

	"aiagent/pkg/ilog"
)

type Source string

const (
	SourceBuiltin Source = "builtin"
	SourceMCP     Source = "mcp"
)

// Metadata 是 Agent、Eino 和 MCP 共用的唯一工具治理元数据。
type Metadata struct {
	ReadOnly         bool
	SideEffect       bool
	ApprovalRequired bool
	ReturnDirectly   bool
	ExposeToAgent    bool
	ExposeToMCP      bool
	Source           Source
	ResourceTypes    []string
}

type Handler func(ctx context.Context, args map[string]any) (any, error)

type Spec struct {
	Name        string
	Description string
	Parameters  map[string]*schema.ParameterInfo
	JSONSchema  *jsonschema.Schema
	Metadata    Metadata
	Handler     Handler
}

type Decision struct {
	Allow            bool
	Kind             string
	Reason           string
	ApprovalRequired bool
}

type Policy interface {
	Evaluate(ctx context.Context, spec *Spec) Decision
}

type ScopeResolver func(ctx context.Context) (readOnly bool, canApprove bool, err error)

type DefaultPolicy struct {
	ResolveScope ScopeResolver
}

func (p DefaultPolicy) Evaluate(ctx context.Context, spec *Spec) Decision {
	readOnly, canApprove := false, false
	if p.ResolveScope != nil {
		var err error
		readOnly, canApprove, err = p.ResolveScope(ctx)
		if err != nil {
			return Decision{Allow: false, Kind: "missing_scope", Reason: err.Error()}
		}
	}
	if readOnly && !spec.Metadata.ReadOnly {
		return Decision{Allow: false, Kind: "read_only_policy", Reason: "只读作用域禁止执行该工具"}
	}
	if spec.Metadata.SideEffect && spec.Metadata.ApprovalRequired && !canApprove {
		return Decision{Allow: false, Kind: "approval_required", Reason: "工具具有副作用且尚未获得审批", ApprovalRequired: true}
	}
	return Decision{Allow: true, Kind: "allowed"}
}

type Registry struct {
	mu     sync.RWMutex
	specs  map[string]*Spec
	policy Policy
}

func NewRegistry(policy Policy) *Registry {
	return &Registry{specs: make(map[string]*Spec), policy: policy}
}

func (r *Registry) Register(spec *Spec) error {
	if spec == nil || strings.TrimSpace(spec.Name) == "" {
		return fmt.Errorf("tool name is required")
	}
	if spec.Handler == nil {
		return fmt.Errorf("tool %s handler is required", spec.Name)
	}
	if spec.Metadata.ReadOnly && spec.Metadata.SideEffect {
		return fmt.Errorf("tool %s cannot be both read-only and side-effecting", spec.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.specs[spec.Name]; exists {
		return fmt.Errorf("tool %s already registered", spec.Name)
	}
	copySpec := *spec
	r.specs[spec.Name] = &copySpec
	return nil
}

func (r *Registry) Get(name string) (*Spec, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	spec, ok := r.specs[name]
	return spec, ok
}

func (r *Registry) List() []*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Spec, 0, len(r.specs))
	for _, spec := range r.specs {
		list = append(list, spec)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

func (r *Registry) ListForAgent() []*Spec {
	r.mu.RLock()
	defer r.mu.RUnlock()
	list := make([]*Spec, 0, len(r.specs))
	for _, spec := range r.specs {
		if spec.Metadata.ExposeToAgent {
			list = append(list, spec)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// EnableForAgent 把 names 中已注册的工具标记为对 Agent 暴露（ExposeToAgent=true）。
// 用于 tool 型技能静态启用：技能内容 = 内置工具名 JSON 数组，命中即挂载。
// 返回实际命中的工具数；未注册的工具名被忽略（不报错）。
func (r *Registry) EnableForAgent(names map[string]bool) int {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for name := range names {
		if spec, ok := r.specs[name]; ok && !spec.Metadata.ExposeToAgent {
			spec.Metadata.ExposeToAgent = true
			n++
		}
	}
	return n
}

// Select 返回只含 names 中工具的新 Registry（浅拷贝 Spec），用于工具路由后缩减注入模型的工具集。
func (r *Registry) Select(names map[string]bool) *Registry {
	nr := NewRegistry(r.policy)
	r.mu.RLock()
	for _, spec := range r.specs {
		if names[spec.Name] {
			cp := *spec
			nr.specs[spec.Name] = &cp
		}
	}
	r.mu.RUnlock()
	return nr
}

func (r *Registry) Invoke(ctx context.Context, name string, args map[string]any) (any, error) {
	spec, ok := r.Get(name)
	if !ok {
		return nil, fmt.Errorf("未知工具: %s", name)
	}
	if r.policy != nil {
		decision := r.policy.Evaluate(ctx, spec)
		if !decision.Allow {
			// 需要审批的工具不直接拒绝，而是挂起等待用户确认（有交互通道时）
			if decision.ApprovalRequired {
				return r.invokeWithApproval(ctx, spec, args)
			}
			return nil, fmt.Errorf("工具策略拒绝 [%s]: %s", decision.Kind, decision.Reason)
		}
	}
	return spec.Handler(ctx, args)
}

// invokeWithApproval 处理「有副作用且尚未获授权」的工具调用：
// 先做红线判定（命中则直接拒绝，不询问），再通过 context 中的人工确认通道等待用户决策。
func (r *Registry) invokeWithApproval(ctx context.Context, spec *Spec, args map[string]any) (any, error) {
	risk, reason := "", "该操作会改变外部状态，需要你确认后执行"
	if assess, ok := RiskAssessorFrom(ctx); ok {
		r, why, blocked := assess(spec.Name, args)
		risk, reason = r, why
		if blocked {
			// 红线：不可逆的灾难性操作，不询问，直接拒绝
			return nil, fmt.Errorf("%s", why)
		}
		if why != "" {
			reason = why
		}
	}

	// 先按会话权限模式裁决：委托模式下高风险操作 fail-closed，
	// 完全权限模式下高风险操作放行，人工审批模式则一律走确认。
	mode := ApprovalModeFrom(ctx)
	switch {
	case mode == ModeFullAccess:
		return spec.Handler(ctx, args)
	case mode == ModeDelegated:
		if risk == RiskHigh {
			return nil, fmt.Errorf("当前是「委托审批」模式，%s 属于高风险操作，已被拒绝；如需执行请切换到「完全权限」模式后重试", spec.Name)
		}
		return spec.Handler(ctx, args)
	}

	approver, ok := ApproverFrom(ctx)
	if !ok {
		return nil, fmt.Errorf("该操作需要用户确认，但当前会话没有交互通道，已拒绝执行")
	}

	approved, remember, comment, err := approver.RequestApproval(ctx, ApprovalRequest{
		ToolName: spec.Name,
		Summary:  SummarizeToolCall(spec.Name, args),
		Detail:   MarshalArgsForDisplay(args),
		Risk:     risk,
		Reason:   reason,
	})
	if err != nil {
		return nil, fmt.Errorf("等待用户确认失败: %w", err)
	}
	if !approved {
		msg := "用户拒绝执行该操作"
		if comment != "" {
			msg += "：" + comment
		}
		// 拒绝不视为工具故障：把结果返回给模型，让它换一种方式回答而不是直接报错中断
		return map[string]any{"approved": false, "rejected": true, "message": msg}, nil
	}
	_ = remember // 是否「本次会话不再询问」由审批中心记录，工具层无需处理
	return spec.Handler(ctx, args)
}

func (r *Registry) ToEinoTools() []tool.BaseTool {
	specs := r.ListForAgent()
	tools := make([]tool.BaseTool, 0, len(specs))
	for _, spec := range specs {
		params := schema.NewParamsOneOfByParams(spec.Parameters)
		if spec.JSONSchema != nil {
			params = schema.NewParamsOneOfByJSONSchema(spec.JSONSchema)
		}
		info := &schema.ToolInfo{
			Name:        spec.Name,
			Desc:        spec.Description,
			ParamsOneOf: params,
			Extra: map[string]any{
				"read_only":         spec.Metadata.ReadOnly,
				"side_effect":       spec.Metadata.SideEffect,
				"approval_required": spec.Metadata.ApprovalRequired,
				"source":            spec.Metadata.Source,
				"resource_types":    spec.Metadata.ResourceTypes,
			},
		}
		tools = append(tools, toolutils.NewTool(info, func(ctx context.Context, input map[string]any) (any, error) {
			result, err := r.Invoke(ctx, spec.Name, input)
			if err == nil {
				return result, nil
			}
			// 工具出错时不要把错误抛给框架：Eino 会直接终止整个 Agent 循环，
			// 用户看到的就只是一句「Agent 执行失败」，既没有回答也不知道为什么。
			// 把失败原因作为工具结果回给模型，让它重试、换个工具或如实转述原因。
			// 只有中断类错误（停止生成、超时、断连）才继续向上抛，交给上层按中断处理。
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			ilog.Errorf("tool %s failed: %v", spec.Name, err)
			return map[string]any{
				"succeeded": false,
				"error":     err.Error(),
			}, nil
		}))
	}
	return tools
}

// InvokeJSON 供旧运行时和 MCP 统一复用同一 Registry 执行入口。
// JSONSchemaFromMap 将 MCP inputSchema 转为 Eino 使用的 JSON Schema。
func JSONSchemaFromMap(input map[string]any) (*jsonschema.Schema, error) {
	if len(input) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	var schemaValue jsonschema.Schema
	if err := json.Unmarshal(data, &schemaValue); err != nil {
		return nil, err
	}
	return &schemaValue, nil
}

func (r *Registry) InvokeJSON(ctx context.Context, name string, args map[string]any) (string, error) {
	result, err := r.Invoke(ctx, name, args)
	if err != nil {
		return "", err
	}
	if text, ok := result.(string); ok {
		return text, nil
	}
	data, err := json.Marshal(result)
	if err != nil {
		return "", err
	}
	return string(data), nil
}
