package approval

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestBrokerApproveThenRemember 验证：用户确认并勾选「本次会话不再询问」后，
// 同一会话的同一种工具后续不再打扰用户。
func TestBrokerApproveThenRemember(t *testing.T) {
	b := NewBroker(5*time.Minute, time.Hour)
	ctx := context.Background()

	var mu sync.Mutex
	emitted := 0
	emit := func(string, map[string]any) { mu.Lock(); emitted++; mu.Unlock() }

	var res Result
	var err error
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, err = b.RequestApproval(ctx, Request{SessionID: 7, UserID: 1, ToolName: "exec_command"}, emit)
	}()

	// 等待事件推送后提交「允许并记住」
	waitPending(t, b)
	if err := b.Resolve(pendingID(t, b), 1, Result{Approved: true, Remember: true}); err != nil {
		t.Fatalf("提交决策失败: %v", err)
	}
	wg.Wait()

	if err != nil || !res.Approved {
		t.Fatalf("期望批准，实际 approved=%v err=%v", res.Approved, err)
	}
	if emitted != 1 {
		t.Fatalf("应只推送一次确认请求，实际 %d 次", emitted)
	}

	// 第二次：会话级放行，直接通过且不再推事件
	second, err := b.RequestApproval(ctx, Request{SessionID: 7, UserID: 1, ToolName: "exec_command"}, emit)
	if err != nil || !second.Approved {
		t.Fatalf("会话级放行失败: approved=%v err=%v", second.Approved, err)
	}
	if emitted != 1 {
		t.Fatalf("会话级放行不应再推事件，实际共 %d 次", emitted)
	}
}

// TestBrokerReject 验证用户拒绝后结果能正确回传。
func TestBrokerReject(t *testing.T) {
	b := NewBroker(5*time.Minute, time.Hour)
	emit := func(string, map[string]any) {}

	var res Result
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		res, _ = b.RequestApproval(context.Background(), Request{SessionID: 8, UserID: 2, ToolName: "write_file"}, emit)
	}()

	waitPending(t, b)
	if err := b.Resolve(pendingID(t, b), 2, Result{Approved: false, Comment: "不希望改动线上配置"}); err != nil {
		t.Fatalf("提交决策失败: %v", err)
	}
	wg.Wait()

	if res.Approved {
		t.Fatal("期望拒绝，实际被批准")
	}
	if res.Comment != "不希望改动线上配置" {
		t.Fatalf("拒绝原因未回传: %q", res.Comment)
	}
	// 拒绝不应产生会话级放行
	if b.HasGrant(8, "write_file") {
		t.Fatal("拒绝后不应产生会话级放行")
	}
}

// TestBrokerTimeout 验证长时间不确认会自动取消，避免 Agent 永久挂起。
func TestBrokerTimeout(t *testing.T) {
	b := NewBroker(80*time.Millisecond, time.Hour)
	res, err := b.RequestApproval(context.Background(),
		Request{SessionID: 9, UserID: 3, ToolName: "exec_command"},
		func(string, map[string]any) {})
	if err != nil {
		t.Fatalf("超时不应返回错误: %v", err)
	}
	if res.Approved {
		t.Fatal("超时后应视为取消，不应批准")
	}
}

// TestBrokerNoEmitter 验证没有交互通道（外部 API 调用）时直接失败，而不是静默通过。
func TestBrokerNoEmitter(t *testing.T) {
	b := NewBroker(time.Minute, time.Hour)
	if _, err := b.RequestApproval(context.Background(), Request{SessionID: 10, ToolName: "exec_command"}, nil); err == nil {
		t.Fatal("无交互通道时应返回错误")
	}
}

// TestBrokerResolveWrongUser 验证其它用户不能替发起者做决策。
func TestBrokerResolveWrongUser(t *testing.T) {
	b := NewBroker(time.Minute, time.Hour)
	emit := func(string, map[string]any) {}
	go func() {
		_, _ = b.RequestApproval(context.Background(), Request{SessionID: 11, UserID: 100, ToolName: "exec_command"}, emit)
	}()
	waitPending(t, b)

	if err := b.Resolve(pendingID(t, b), 999, Result{Approved: true}); err == nil {
		t.Fatal("非发起者不应能处理审批请求")
	}
	if err := b.Resolve(pendingID(t, b), 100, Result{Approved: true}); err != nil {
		t.Fatalf("发起者应能处理自己的审批请求: %v", err)
	}
}

// waitPending 等待请求进入待确认状态。
func waitPending(t *testing.T, b *Broker) {
	t.Helper()
	for i := 0; i < 200; i++ {
		b.mu.Lock()
		n := len(b.pending)
		b.mu.Unlock()
		if n > 0 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("等待审批请求超时")
}

// pendingID 取出当前唯一待确认请求的 ID。
func pendingID(t *testing.T, b *Broker) string {
	t.Helper()
	b.mu.Lock()
	defer b.mu.Unlock()
	for id := range b.pending {
		return id
	}
	t.Fatal("没有待确认的审批请求")
	return ""
}
