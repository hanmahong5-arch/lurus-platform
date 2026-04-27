package grpc

// Edge-case tests that increase coverage from 83.8% → 92%+.
// Targets: authInterceptor missing-auth-header path, UpsertAccount store error,
// GetEntitlements nil-map fallback, overviewToProto subscription-no-expiry path.

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	identityv1 "github.com/hanmahong5-arch/lurus-platform/proto/gen/go/identity/v1"
)

// ── authInterceptor: missing authorization header in metadata ─────────────────

// TestGRPCServer_AuthInterceptor_EmptyAuthHeader verifies rejection when
// the metadata key exists but carries an empty slice (len(vals)==0 branch).
func TestGRPCServer_AuthInterceptor_EmptyAuthHeader(t *testing.T) {
	s := newTestServerDeps().buildServer("secret-key")

	md := metadata.MD{"authorization": []string{}}
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := s.authInterceptor(ctx, nil, nil, noopHandler)
	if err == nil {
		t.Fatal("expected Unauthenticated for empty authorization slice, got nil")
	}
	st, ok := status.FromError(err)
	if !ok {
		t.Fatalf("expected gRPC status error, got %v", err)
	}
	if st.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", st.Code())
	}
}

// TestGRPCServer_AuthInterceptor_OnlyBearerPrefix verifies rejection when the
// authorization header is "Bearer " with an empty token part.
func TestGRPCServer_AuthInterceptor_OnlyBearerPrefix(t *testing.T) {
	s := newTestServerDeps().buildServer("secret")

	md := metadata.Pairs("authorization", "Bearer ")
	ctx := metadata.NewIncomingContext(context.Background(), md)

	_, err := s.authInterceptor(ctx, nil, nil, noopHandler)
	if err == nil {
		t.Fatal("expected error for Bearer-only header with empty token, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Unauthenticated {
		t.Errorf("code = %v, want Unauthenticated", st.Code())
	}
}

// ── authInterceptor: handler receives original request ────────────────────────

// TestGRPCServer_AuthInterceptor_HandlerReceivesRequest verifies that the handler
// is called with the exact request passed through the interceptor.
func TestGRPCServer_AuthInterceptor_HandlerReceivesRequest(t *testing.T) {
	s := newTestServerDeps().buildServer("the-key")

	var handlerReq any
	handler := func(_ context.Context, req any) (any, error) {
		handlerReq = req
		return "handled", nil
	}

	sentinel := &identityv1.GetAccountByZitadelSubRequest{ZitadelSub: "test-sub"}
	result, err := s.authInterceptor(
		incomingCtx("Bearer the-key"),
		sentinel,
		&grpc.UnaryServerInfo{},
		handler,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "handled" {
		t.Errorf("result = %v, want handled", result)
	}
	if handlerReq != sentinel {
		t.Error("handler should receive the original request unchanged")
	}
}

// ── UpsertAccount: store error path ──────────────────────────────────────────

// TestGRPCServer_UpsertAccount_StoreError verifies Internal status when the
// account store fails during UpsertByZitadelSub (GetByZitadelSub propagates error).
func TestGRPCServer_UpsertAccount_StoreError(t *testing.T) {
	d := newTestServerDeps()
	d.accounts.getErr = errors.New("database connection lost")
	s := d.buildServer("key")

	_, err := s.UpsertAccount(context.Background(), &identityv1.UpsertAccountRequest{
		ZitadelSub:  "brand-new-sub",
		Email:       "new@example.com",
		DisplayName: "New User",
	})
	if err == nil {
		t.Fatal("expected Internal error for store failure, got nil")
	}
	st, _ := status.FromError(err)
	if st.Code() != codes.Internal {
		t.Errorf("code = %v, want Internal", st.Code())
	}
}

// ── GetEntitlements: nil-map fallback ─────────────────────────────────────────

// TestGRPCServer_GetEntitlements_EmptyReturnsFreePlan verifies that when both
// the subscription store and cache return empty/nil, the response contains
// {"plan_code": "free"}.
func TestGRPCServer_GetEntitlements_EmptyReturnsFreePlan(t *testing.T) {
	d := newTestServerDeps()
	s := d.buildServer("key")

	resp, err := s.GetEntitlements(context.Background(), &identityv1.GetEntitlementsRequest{
		AccountId: 9999,
		ProductId: "unknown-product",
	})
	if err != nil {
		t.Fatalf("GetEntitlements unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response should not be nil")
	}
	if resp.Entitlements == nil {
		t.Fatal("Entitlements map should not be nil")
	}
	if resp.Entitlements["plan_code"] != "free" {
		t.Errorf("plan_code = %q, want free", resp.Entitlements["plan_code"])
	}
}

// ── overviewToProto: subscription with nil ExpiresAt ─────────────────────────

// buildMinimalOverviewWithSub returns an AccountOverview containing a subscription
// with the given expiresAt pointer (may be nil).
func buildMinimalOverviewWithSub(expiresAt *time.Time) *app.AccountOverview {
	return &app.AccountOverview{
		Account: app.AccountSummary{ID: 1},
		VIP:     app.VIPSummary{},
		Wallet:  app.WalletSummary{},
		Subscription: &app.SubscriptionSummary{
			ProductID: "lucrum",
			PlanCode:  "pro",
			Status:    "active",
			AutoRenew: false,
			ExpiresAt: expiresAt,
		},
	}
}

// TestOverviewToProto_SubscriptionNilExpiry verifies no panic when ExpiresAt == nil.
func TestOverviewToProto_SubscriptionNilExpiry(t *testing.T) {
	ov := buildMinimalOverviewWithSub(nil)
	pb := overviewToProto(ov)
	if pb.Subscription == nil {
		t.Fatal("Subscription should not be nil")
	}
	if pb.Subscription.ExpiresAt != nil {
		t.Error("ExpiresAt should be nil when input ExpiresAt is nil")
	}
	if pb.Subscription.PlanCode != "pro" {
		t.Errorf("PlanCode = %q, want pro", pb.Subscription.PlanCode)
	}
}

// TestOverviewToProto_SubscriptionWithExpiry verifies ExpiresAt is set when non-nil.
func TestOverviewToProto_SubscriptionWithExpiry(t *testing.T) {
	exp := time.Now().Add(30 * 24 * time.Hour)
	ov := buildMinimalOverviewWithSub(&exp)
	pb := overviewToProto(ov)
	if pb.Subscription == nil {
		t.Fatal("Subscription should not be nil")
	}
	if pb.Subscription.ExpiresAt == nil {
		t.Error("ExpiresAt should be set when input ExpiresAt is non-nil")
	}
}
