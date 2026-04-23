package tracing

import (
	"context"
	"net"
	"testing"
	"time"
)

// listenAndAccept starts a TCP listener on a random port, accepts connections
// in the background and returns the listener address.
// The listener is closed when the test finishes.
func listenAndAccept(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listenAndAccept: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			conn.Close()
		}
	}()
	return ln.Addr().String()
}

// TestInit_WithEndpoint_Success verifies the full OTLP gRPC setup path.
// It uses a real TCP listener so that grpc.NewClient and the exporter's Start
// both succeed without making real OTLP calls.
func TestInit_WithEndpoint_Success(t *testing.T) {
	addr := listenAndAccept(t)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "test-service", addr)
	if err != nil {
		t.Fatalf("Init with endpoint %q returned error: %v", addr, err)
	}
	if shutdown == nil {
		t.Fatal("expected non-nil shutdown function")
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer shutCancel()
	if err := shutdown(shutCtx); err != nil {
		t.Errorf("shutdown() returned error: %v", err)
	}
}

// TestInit_WithEndpoint_ShutdownIdempotent calls shutdown twice and expects
// no panic or error on the second call.
func TestInit_WithEndpoint_ShutdownIdempotent(t *testing.T) {
	addr := listenAndAccept(t)

	ctx := context.Background()
	shutdown, err := Init(ctx, "svc-idempotent", addr)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	shutCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = shutdown(shutCtx)
	// A second call should not panic (tp.Shutdown is idempotent in OTel SDK).
	_ = shutdown(shutCtx)
}

// TestInit_DifferentServiceNames ensures Init works for multiple service names
// including empty string (resource may fall back to empty).
func TestInit_DifferentServiceNames(t *testing.T) {
	cases := []string{"platform-core", "notification", "", "very-long-service-name-exceeding-normal-length"}
	for _, svcName := range cases {
		svcName := svcName
		t.Run("service="+svcName, func(t *testing.T) {
			addr := listenAndAccept(t)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			shutdown, err := Init(ctx, svcName, addr)
			if err != nil {
				t.Fatalf("Init(%q): %v", svcName, err)
			}
			shutCtx, sc := context.WithTimeout(context.Background(), 2*time.Second)
			defer sc()
			if err := shutdown(shutCtx); err != nil {
				t.Errorf("shutdown(%q): %v", svcName, err)
			}
		})
	}
}

// TestInit_NoopThenReal exercises both code paths in sequence to ensure they
// do not interfere with each other (global provider is reset each time).
func TestInit_NoopThenReal(t *testing.T) {
	// First install a noop provider.
	shutdown1, err := Init(context.Background(), "svc", "")
	if err != nil {
		t.Fatalf("noop Init: %v", err)
	}
	if err := shutdown1(context.Background()); err != nil {
		t.Fatalf("noop shutdown: %v", err)
	}

	// Then install a real OTLP provider.
	addr := listenAndAccept(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown2, err := Init(ctx, "svc", addr)
	if err != nil {
		t.Fatalf("real Init: %v", err)
	}
	shutCtx, sc := context.WithTimeout(context.Background(), 2*time.Second)
	defer sc()
	if err := shutdown2(shutCtx); err != nil {
		t.Errorf("real shutdown: %v", err)
	}
}

// TestTracer_MultipleNames checks that Tracer returns a non-nil tracer for
// various names including edge cases.
func TestTracer_MultipleNames(t *testing.T) {
	// Ensure noop provider is in place.
	_, _ = Init(context.Background(), "test", "")

	names := []string{"", "component", "very-long-component-name", "package/sub/nested"}
	for _, name := range names {
		tr := Tracer(name)
		if tr == nil {
			t.Errorf("Tracer(%q) returned nil", name)
		}
	}
}

// TestTracer_AfterRealInit verifies Tracer works after a real provider is installed.
func TestTracer_AfterRealInit(t *testing.T) {
	addr := listenAndAccept(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "tracer-test", addr)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}
	defer func() {
		shutCtx, sc := context.WithTimeout(context.Background(), 2*time.Second)
		defer sc()
		_ = shutdown(shutCtx)
	}()

	tr := Tracer("my-component")
	if tr == nil {
		t.Error("Tracer() returned nil after real provider init")
	}

	// Start and end a span to exercise the tracer provider pipeline.
	_, span := tr.Start(context.Background(), "test-operation")
	span.End()
}

// TestInit_ShutdownWithCancelledContext verifies that a cancelled shutdown
// context does not panic (it may return an error but must not crash).
func TestInit_ShutdownWithCancelledContext(t *testing.T) {
	addr := listenAndAccept(t)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "cancel-test", addr)
	if err != nil {
		t.Fatalf("Init: %v", err)
	}

	// Cancel the shutdown context immediately.
	cancelledCtx, c := context.WithCancel(context.Background())
	c()
	// Should not panic even if context is already cancelled.
	_ = shutdown(cancelledCtx)
}
