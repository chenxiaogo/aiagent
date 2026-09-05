package toolkit

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestRegistryPolicy(t *testing.T) {
	registry := NewRegistry(DefaultPolicy{ResolveScope: func(context.Context) (bool, bool, error) {
		return true, false, nil
	}})
	if err := registry.Register(&Spec{
		Name: "read", Description: "read",
		Parameters: map[string]*schema.ParameterInfo{},
		Metadata:   Metadata{ReadOnly: true, ExposeToAgent: true, Source: SourceBuiltin},
		Handler:    func(context.Context, map[string]any) (any, error) { return "ok", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if err := registry.Register(&Spec{
		Name: "write", Description: "write",
		Parameters: map[string]*schema.ParameterInfo{},
		Metadata:   Metadata{SideEffect: true, ApprovalRequired: true, ExposeToAgent: true, Source: SourceBuiltin},
		Handler:    func(context.Context, map[string]any) (any, error) { return "bad", nil },
	}); err != nil {
		t.Fatal(err)
	}
	if result, err := registry.InvokeJSON(context.Background(), "read", nil); err != nil || result != "ok" {
		t.Fatalf("read result=%q err=%v", result, err)
	}
	if _, err := registry.Invoke(context.Background(), "write", nil); err == nil {
		t.Fatal("expected write tool to be denied in read-only scope")
	}
	if got := len(registry.ToEinoTools()); got != 2 {
		t.Fatalf("expected 2 Eino tools, got %d", got)
	}
}
