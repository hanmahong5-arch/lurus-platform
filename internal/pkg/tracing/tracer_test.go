package tracing

import (
	"context"
	"testing"
)

func TestInit_EmptyEndpoint_Noop(t *testing.T) {
	shutdown, err := Init(context.Background(), "test-service", "")
	if err != nil {
		t.Fatalf("Init with empty endpoint returned error: %v", err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}
	if err := shutdown(context.Background()); err != nil {
		t.Errorf("shutdown() = %v, want nil", err)
	}
}

func TestTracer_ReturnsNonNil(t *testing.T) {
	// Ensure noop provider is set first.
	_, _ = Init(context.Background(), "test", "")
	tr := Tracer("my-component")
	if tr == nil {
		t.Error("Tracer() returned nil")
	}
}
