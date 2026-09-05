package tracex

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type contextKey string

const (
	traceIDKey contextKey = "trace_id"
	spanIDKey  contextKey = "span_id"
)

// TraceIDFromContext 从 context 提取 trace ID。
func TraceIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(traceIDKey).(string); ok {
		return v
	}
	return ""
}

// SpanIDFromContext 从 context 提取 span ID。
func SpanIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	if v, ok := ctx.Value(spanIDKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithTraceID 注入 trace ID。
func ContextWithTraceID(ctx context.Context, tid string) context.Context {
	return context.WithValue(ctx, traceIDKey, tid)
}

// ContextWithSpanID 注入 span ID。
func ContextWithSpanID(ctx context.Context, sid string) context.Context {
	return context.WithValue(ctx, spanIDKey, sid)
}

// NewTraceID 生成 32 位 hex trace ID。
func NewTraceID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// NewSpanID 生成 16 位 hex span ID。
func NewSpanID() string {
	b := make([]byte, 8)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// FromRequest 从 gin.Context 提取 context（已注入 trace/span）。
func FromRequest(c *gin.Context) context.Context {
	return c.Request.Context()
}

// TraceIDShort 返回 trace ID 的前 8 位（用于日志前缀）。
func TraceIDShort(ctx context.Context) string {
	tid := TraceIDFromContext(ctx)
	if len(tid) > 8 {
		return tid[:8]
	}
	return tid
}

// Middleware 链路追踪中间件：从请求头提取或生成 trace/span ID 注入 context。
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		tid := extractTraceID(c.Request)
		if tid == "" {
			tid = NewTraceID()
		}
		sid := NewSpanID()

		ctx = ContextWithTraceID(ctx, tid)
		ctx = ContextWithSpanID(ctx, sid)
		c.Request = c.Request.WithContext(ctx)

		c.Header("X-Trace-Id", tid)
		c.Next()
	}
}

func extractTraceID(r *http.Request) string {
	// 尝试从 X-Trace-Id 或 traceparent 头提取
	if tid := r.Header.Get("X-Trace-Id"); tid != "" {
		return tid
	}
	if tp := r.Header.Get("traceparent"); tp != "" {
		// traceparent: 00-{traceId}-{spanId}-{flags}
		parts := strings.Split(tp, "-")
		if len(parts) >= 2 && len(parts[1]) == 32 {
			return parts[1]
		}
	}
	return ""
}

// NewContext 生成根链路 context（供后台任务使用）。
func NewContext(ctx context.Context) context.Context {
	tid := NewTraceID()
	sid := NewSpanID()
	ctx = ContextWithTraceID(ctx, tid)
	ctx = ContextWithSpanID(ctx, sid)
	return ctx
}

func init() {
	rand.Read(make([]byte, 16)) // warm up rand
	_ = time.Now
}