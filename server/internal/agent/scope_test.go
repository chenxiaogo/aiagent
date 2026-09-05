package agent

import (
	"context"
	"testing"
)

func TestRequireScope(t *testing.T) {
	if _, err := RequireScope(context.Background()); err == nil {
		t.Fatal("expected missing scope error")
	}
	want := Scope{TenantID: 1, UserID: 2, AgentID: 3, SessionID: 4, ReadOnly: true}
	got, err := RequireScope(WithScope(context.Background(), want))
	if err != nil {
		t.Fatalf("RequireScope: %v", err)
	}
	if got.AgentID != want.AgentID || got.UserID != want.UserID || !got.ReadOnly {
		t.Fatalf("unexpected scope: %+v", got)
	}
}
