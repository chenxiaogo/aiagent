package approval

import (
	"context"
	"fmt"
	"sync"
	"time"

	"aiagent/pkg/ilog"
)

// Request 一次待用户确认的工具调用。
type Request struct {
	ID        string    `json:"id"`
	SessionID int64     `json:"sessionId"`
	AgentID   int64     `json:"agentId"`
	UserID    int64     `json:"userId"`
	ToolName  string    `json:"toolName"`
	Summary   string    `json:"summary"`  // 一句话说明要做什么（命令 / 路径）
	Detail    string    `json:"detail"`   // 完整参数（JSON 字符串）
	Risk      string    `json:"risk"`     // medium / high
	Reason    string    `json:"reason"`   // 为什么需要确认
	CreatedAt time.Time `json:"createdAt"`
}

// Result 用户的确认结果。
type Result struct {
	Approved bool   `json:"approved"`
	Remember bool   `json:"remember"` // 本次会话内同类操作不再询问
	Comment  string `json:"comment"`
}

// Emitter 向当前会话前端推送事件（与聊天流式事件共用通道）。
type Emitter func(eventType string, payload map[string]any)

// Broker 审批中心：承接「工具需要用户确认」的挂起等待与决策回传。
//
// 交互链路：
//  1. Agent 调用副作用工具 → 策略判定需要审批 → Broker.RequestApproval
//  2. 通过 Emitter 向聊天流推 approval_request，前端渲染确认卡片
//  3. 用户在聊天框点「允许 / 拒绝」→ HTTP 提交 → Broker.Resolve
//  4. 阻塞的工具调用拿到结果，继续执行或返回拒绝原因给模型
type Broker struct {
	mu      sync.Mutex
	pending map[string]*pendingEntry
	// 会话级放行：sessionKey(会话:工具) -> 过期时间，用户勾选「本次会话不再询问」后写入
	grants   map[string]time.Time
	seq      uint64
	waitTTL  time.Duration // 单次确认的最长等待时间
	grantTTL time.Duration // 会话级放行的有效期
}

type pendingEntry struct {
	request Request
	done    chan Result
}

// NewBroker 创建审批中心。waitTTL 为单次确认超时，grantTTL 为会话级放行有效期。
func NewBroker(waitTTL, grantTTL time.Duration) *Broker {
	if waitTTL <= 0 {
		waitTTL = 5 * time.Minute
	}
	if grantTTL <= 0 {
		grantTTL = 2 * time.Hour
	}
	return &Broker{
		pending:  make(map[string]*pendingEntry),
		grants:   make(map[string]time.Time),
		waitTTL:  waitTTL,
		grantTTL: grantTTL,
	}
}

// RequestApproval 请求用户确认，阻塞直到用户决策、ctx 取消或超时。
// emit 为 nil 表示没有交互式通道（如外部 API 调用），此时直接拒绝。
func (b *Broker) RequestApproval(ctx context.Context, req Request, emit Emitter) (Result, error) {
	if req.ToolName == "" {
		return Result{}, fmt.Errorf("工具名为空")
	}
	// 会话级放行：用户此前勾选过「本次会话不再询问」
	if req.SessionID > 0 && b.HasGrant(req.SessionID, req.ToolName) {
		ilog.Infof("approval auto-granted by session: session=%d tool=%s", req.SessionID, req.ToolName)
		return Result{Approved: true}, nil
	}
	if emit == nil {
		return Result{}, fmt.Errorf("当前会话不支持交互确认")
	}

	req.ID = b.nextID(req.SessionID)
	req.CreatedAt = time.Now()
	entry := &pendingEntry{request: req, done: make(chan Result, 1)}

	b.mu.Lock()
	b.pending[req.ID] = entry
	b.mu.Unlock()
	defer b.remove(req.ID)

	emit("approval_request", map[string]any{
		"id":        req.ID,
		"toolName":  req.ToolName,
		"summary":   req.Summary,
		"detail":    req.Detail,
		"risk":      req.Risk,
		"reason":    req.Reason,
		"sessionId": req.SessionID,
		"timeout":   int(b.waitTTL.Seconds()),
	})
	ilog.Infof("approval requested: id=%s session=%d tool=%s risk=%s", req.ID, req.SessionID, req.ToolName, req.Risk)

	timer := time.NewTimer(b.waitTTL)
	defer timer.Stop()

	select {
	case res := <-entry.done:
		if res.Approved && res.Remember && req.SessionID > 0 {
			b.GrantSession(req.SessionID, req.ToolName)
		}
		ilog.Infof("approval resolved: id=%s approved=%v remember=%v", req.ID, res.Approved, res.Remember)
		return res, nil
	case <-ctx.Done():
		return Result{Approved: false, Comment: "会话已结束，操作取消"}, ctx.Err()
	case <-timer.C:
		return Result{Approved: false, Comment: fmt.Sprintf("超过 %v 未确认，已自动取消", b.waitTTL)}, nil
	}
}

// Resolve 提交用户决策。userID 用于校验决策者就是发起者，防止越权确认。
func (b *Broker) Resolve(id string, userID int64, res Result) error {
	b.mu.Lock()
	entry, ok := b.pending[id]
	b.mu.Unlock()
	if !ok {
		return fmt.Errorf("审批请求不存在或已处理")
	}
	// 会话内发起者才有权决策；userID<=0 表示内部调用不做校验
	if userID > 0 && entry.request.UserID > 0 && entry.request.UserID != userID {
		return fmt.Errorf("无权处理该审批请求")
	}
	select {
	case entry.done <- res:
		return nil
	default:
		return fmt.Errorf("审批请求已处理")
	}
}

// Get 查询待确认请求（用于校验与展示）。
func (b *Broker) Get(id string) (Request, bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	entry, ok := b.pending[id]
	if !ok {
		return Request{}, false
	}
	return entry.request, true
}

// HasGrant 判断该会话是否已放行某工具。
func (b *Broker) HasGrant(sessionID int64, toolName string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	expire, ok := b.grants[grantKey(sessionID, toolName)]
	if !ok {
		return false
	}
	if time.Now().After(expire) {
		delete(b.grants, grantKey(sessionID, toolName))
		return false
	}
	return true
}

// GrantSession 放行某会话的某工具，直到会话放行有效期结束。
func (b *Broker) GrantSession(sessionID int64, toolName string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.grants[grantKey(sessionID, toolName)] = time.Now().Add(b.grantTTL)
}

// RevokeSession 撤销某会话的全部放行（如切换主机组、用户主动收紧权限时）。
func (b *Broker) RevokeSession(sessionID int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	prefix := fmt.Sprintf("%d:", sessionID)
	for k := range b.grants {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			delete(b.grants, k)
		}
	}
}

func (b *Broker) nextID(sessionID int64) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	return fmt.Sprintf("apr-%d-%d-%d", sessionID, time.Now().UnixNano(), b.seq)
}

func (b *Broker) remove(id string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.pending, id)
}

func grantKey(sessionID int64, toolName string) string {
	return fmt.Sprintf("%d:%s", sessionID, toolName)
}
