// Package slogctx provides a context-enriching slog.Handler that injects
// trace_id, span_id, account_id, and request_id from context.Context into
// every JSON log entry. This enables log-trace correlation in Loki/Grafana.
package slogctx

import (
	"context"
	"fmt"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

type ctxKey string

const (
	keyAccountID ctxKey = "log_account_id"
	keyRequestID ctxKey = "log_request_id"
)

// WithAccountID attaches account_id to context for structured log extraction.
func WithAccountID(ctx context.Context, accountID int64) context.Context {
	return context.WithValue(ctx, keyAccountID, accountID)
}

// WithRequestID attaches request_id to context for structured log extraction.
func WithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, keyRequestID, requestID)
}

// Handler wraps an inner slog.Handler and enriches every log record with
// trace_id, span_id, account_id, and request_id extracted from context.
type Handler struct {
	inner slog.Handler
}

// New creates a context-enriching Handler that delegates to inner.
func New(inner slog.Handler) *Handler {
	return &Handler{inner: inner}
}

func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if ctx != nil {
		// Inject OTel trace context.
		sc := trace.SpanContextFromContext(ctx)
		if sc.HasTraceID() {
			r.AddAttrs(slog.String("trace_id", sc.TraceID().String()))
		}
		if sc.HasSpanID() {
			r.AddAttrs(slog.String("span_id", sc.SpanID().String()))
		}
		// Inject account_id.
		if aid := ctx.Value(keyAccountID); aid != nil {
			r.AddAttrs(slog.String("account_id", fmt.Sprintf("%v", aid)))
		}
		// Inject request_id.
		if rid := ctx.Value(keyRequestID); rid != nil {
			r.AddAttrs(slog.String("request_id", fmt.Sprintf("%v", rid)))
		}
	}
	return h.inner.Handle(ctx, r)
}

func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{inner: h.inner.WithAttrs(attrs)}
}

func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{inner: h.inner.WithGroup(name)}
}
