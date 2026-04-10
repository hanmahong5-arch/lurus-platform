// Package grpc implements the gRPC server for lurus-platform.
// It runs alongside the existing Gin HTTP server on a separate port (18105).
package grpc

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	identityv1 "github.com/hanmahong5-arch/lurus-platform/proto/gen/go/identity/v1"
	"github.com/hanmahong5-arch/lurus-platform/internal/app"
	"github.com/hanmahong5-arch/lurus-platform/internal/domain/entity"
)

// Server implements the identity.v1.IdentityService gRPC server.
type Server struct {
	identityv1.UnimplementedIdentityServiceServer
	accounts     *app.AccountService
	entitlements *app.EntitlementService
	overview     *app.OverviewService
	vip          *app.VIPService
	wallet       *app.WalletService
	referral     *app.ReferralService
	internalKey  string
}

// Deps holds all dependencies for the gRPC server.
type Deps struct {
	Accounts     *app.AccountService
	Entitlements *app.EntitlementService
	Overview     *app.OverviewService
	VIP          *app.VIPService
	Wallet       *app.WalletService
	Referral     *app.ReferralService
	InternalKey  string
}

// NewServer creates a new gRPC identity server.
func NewServer(deps Deps) *Server {
	return &Server{
		accounts:     deps.Accounts,
		entitlements: deps.Entitlements,
		overview:     deps.Overview,
		vip:          deps.VIP,
		wallet:       deps.Wallet,
		referral:     deps.Referral,
		internalKey:  deps.InternalKey,
	}
}

// ListenAndServe starts the gRPC server on the given port.
// It blocks until ctx is cancelled, then gracefully stops.
func (s *Server) ListenAndServe(ctx context.Context, port int) error {
	lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}

	srv := grpc.NewServer(
		grpc.StatsHandler(otelgrpc.NewServerHandler()),
		grpc.ChainUnaryInterceptor(s.authInterceptor),
	)
	identityv1.RegisterIdentityServiceServer(srv, s)

	go func() {
		<-ctx.Done()
		slog.Info("grpc server shutting down")
		srv.GracefulStop()
	}()

	slog.Info("grpc server starting", "port", port)
	if err := srv.Serve(lis); err != nil {
		return fmt.Errorf("grpc serve: %w", err)
	}
	return nil
}

// authInterceptor validates the INTERNAL_API_KEY bearer token from gRPC metadata.
func (s *Server) authInterceptor(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, status.Error(codes.Unauthenticated, "missing metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return nil, status.Error(codes.Unauthenticated, "invalid internal API key")
	}
	// Use constant-time comparison to prevent timing side-channel attacks.
	const prefix = "Bearer "
	token := vals[0]
	if !strings.HasPrefix(token, prefix) || subtle.ConstantTimeCompare([]byte(token[len(prefix):]), []byte(s.internalKey)) != 1 {
		return nil, status.Error(codes.Unauthenticated, "invalid internal API key")
	}
	return handler(ctx, req)
}

// GetAccountByZitadelSub implements IdentityServiceServer.
func (s *Server) GetAccountByZitadelSub(ctx context.Context, req *identityv1.GetAccountByZitadelSubRequest) (*identityv1.Account, error) {
	a, err := s.accounts.GetByZitadelSub(ctx, req.ZitadelSub)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "lookup failed: %v", err)
	}
	if a == nil {
		return nil, status.Error(codes.NotFound, "account not found")
	}
	return accountToProto(a), nil
}

// UpsertAccount implements IdentityServiceServer.
func (s *Server) UpsertAccount(ctx context.Context, req *identityv1.UpsertAccountRequest) (*identityv1.Account, error) {
	a, err := s.accounts.UpsertByZitadelSub(ctx, req.ZitadelSub, req.Email, req.DisplayName, req.AvatarUrl)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "upsert failed: %v", err)
	}

	// Link referrer on first account creation
	if req.ReferrerAffCode != "" && a.ReferrerID == nil {
		referrer, rerr := s.accounts.GetByAffCode(ctx, req.ReferrerAffCode)
		if rerr == nil && referrer != nil && referrer.ID != a.ID {
			referrerID := referrer.ID
			a.ReferrerID = &referrerID
			if uerr := s.accounts.Update(ctx, a); uerr == nil {
				_ = s.referral.OnSignup(ctx, a.ID, referrer.ID)
			}
		}
	}

	return accountToProto(a), nil
}

// GetEntitlements implements IdentityServiceServer.
func (s *Server) GetEntitlements(ctx context.Context, req *identityv1.GetEntitlementsRequest) (*identityv1.GetEntitlementsResponse, error) {
	em, err := s.entitlements.Get(ctx, req.AccountId, req.ProductId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "entitlements failed: %v", err)
	}
	if em == nil {
		em = map[string]string{"plan_code": "free"}
	}
	return &identityv1.GetEntitlementsResponse{Entitlements: em}, nil
}

// GetAccountOverview implements IdentityServiceServer.
func (s *Server) GetAccountOverview(ctx context.Context, req *identityv1.GetAccountOverviewRequest) (*identityv1.AccountOverview, error) {
	ov, err := s.overview.Get(ctx, req.AccountId, req.ProductId)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "overview failed: %v", err)
	}
	return overviewToProto(ov), nil
}

// ReportUsage implements IdentityServiceServer.
func (s *Server) ReportUsage(ctx context.Context, req *identityv1.ReportUsageRequest) (*identityv1.ReportUsageResponse, error) {
	_ = s.vip.RecalculateFromWallet(ctx, req.AccountId)
	return &identityv1.ReportUsageResponse{Accepted: true}, nil
}

// WalletDebit implements IdentityServiceServer.
func (s *Server) WalletDebit(ctx context.Context, req *identityv1.WalletOperationRequest) (*identityv1.WalletOperationResponse, error) {
	tx, err := s.wallet.Debit(ctx, req.AccountId, req.Amount, req.Type, req.Description, "internal_debit", "", req.ProductId)
	if err != nil {
		slog.Warn("grpc/wallet-debit: failed", "account_id", req.AccountId, "amount", req.Amount, "type", req.Type, "product_id", req.ProductId, "err", err)
		return nil, status.Errorf(codes.InvalidArgument, "insufficient_balance")
	}
	slog.Info("grpc/wallet-debit", "account_id", req.AccountId, "amount", req.Amount, "type", req.Type, "product_id", req.ProductId, "balance_after", tx.BalanceAfter)
	return &identityv1.WalletOperationResponse{Success: true, BalanceAfter: tx.BalanceAfter}, nil
}

// WalletCredit implements IdentityServiceServer.
func (s *Server) WalletCredit(ctx context.Context, req *identityv1.WalletOperationRequest) (*identityv1.WalletOperationResponse, error) {
	tx, err := s.wallet.Credit(ctx, req.AccountId, req.Amount, req.Type, req.Description, "internal_credit", "", req.ProductId)
	if err != nil {
		slog.Error("grpc/wallet-credit: failed", "account_id", req.AccountId, "amount", req.Amount, "type", req.Type, "err", err)
		return nil, status.Errorf(codes.Internal, "credit failed: %v", err)
	}
	slog.Info("grpc/wallet-credit", "account_id", req.AccountId, "amount", req.Amount, "type", req.Type, "balance_after", tx.BalanceAfter)
	return &identityv1.WalletOperationResponse{Success: true, BalanceAfter: tx.BalanceAfter}, nil
}

// WalletPreAuthorize implements IdentityServiceServer.
func (s *Server) WalletPreAuthorize(ctx context.Context, req *identityv1.WalletPreAuthorizeRequest) (*identityv1.WalletPreAuthorizeResponse, error) {
	ttl := time.Duration(req.TtlSeconds) * time.Second
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	pa, err := s.wallet.PreAuthorize(ctx, req.AccountId, req.Amount, req.ProductId, req.ReferenceId, req.Description, ttl)
	if err != nil {
		slog.Warn("grpc/wallet-preauthorize: failed", "account_id", req.AccountId, "amount", req.Amount, "err", err)
		return nil, status.Errorf(codes.InvalidArgument, "pre-authorize failed: %v", err)
	}
	slog.Info("grpc/wallet-preauthorize", "account_id", req.AccountId, "preauth_id", pa.ID, "amount", req.Amount)
	return &identityv1.WalletPreAuthorizeResponse{
		PreauthId: pa.ID,
		Amount:    pa.Amount,
		Status:    pa.Status,
		ExpiresAt: timestamppb.New(pa.ExpiresAt),
	}, nil
}

// WalletSettlePreAuth implements IdentityServiceServer.
func (s *Server) WalletSettlePreAuth(ctx context.Context, req *identityv1.WalletSettlePreAuthRequest) (*identityv1.WalletPreAuthStatusResponse, error) {
	pa, err := s.wallet.SettlePreAuth(ctx, req.PreauthId, req.ActualAmount)
	if err != nil {
		slog.Warn("grpc/wallet-settle: failed", "preauth_id", req.PreauthId, "actual_amount", req.ActualAmount, "err", err)
		return nil, status.Errorf(codes.Internal, "settle failed: %v", err)
	}
	actualAmount := 0.0
	if pa.ActualAmount != nil {
		actualAmount = *pa.ActualAmount
	}
	slog.Info("grpc/wallet-settle", "preauth_id", pa.ID, "status", pa.Status, "actual_amount", actualAmount)
	return &identityv1.WalletPreAuthStatusResponse{
		PreauthId:    pa.ID,
		Status:       pa.Status,
		HeldAmount:   pa.Amount,
		ActualAmount: actualAmount,
	}, nil
}

// WalletReleasePreAuth implements IdentityServiceServer.
func (s *Server) WalletReleasePreAuth(ctx context.Context, req *identityv1.WalletReleasePreAuthRequest) (*identityv1.WalletPreAuthStatusResponse, error) {
	pa, err := s.wallet.ReleasePreAuth(ctx, req.PreauthId)
	if err != nil {
		slog.Warn("grpc/wallet-release: failed", "preauth_id", req.PreauthId, "err", err)
		return nil, status.Errorf(codes.Internal, "release failed: %v", err)
	}
	slog.Info("grpc/wallet-release", "preauth_id", pa.ID, "status", pa.Status)
	return &identityv1.WalletPreAuthStatusResponse{
		PreauthId:  pa.ID,
		Status:     pa.Status,
		HeldAmount: pa.Amount,
	}, nil
}

// accountToProto converts a domain Account to a proto Account.
func accountToProto(a *entity.Account) *identityv1.Account {
	return &identityv1.Account{
		Id:            a.ID,
		LurusId:       a.LurusID,
		ZitadelSub:    a.ZitadelSub,
		DisplayName:   a.DisplayName,
		AvatarUrl:     a.AvatarURL,
		Email:         a.Email,
		EmailVerified: a.EmailVerified,
		Phone:         a.Phone,
		PhoneVerified: a.PhoneVerified,
		Status:        int32(a.Status),
		Locale:        a.Locale,
		AffCode:       a.AffCode,
		CreatedAt:     timestamppb.New(a.CreatedAt),
		UpdatedAt:     timestamppb.New(a.UpdatedAt),
	}
}

// overviewToProto converts a domain AccountOverview to a proto AccountOverview.
func overviewToProto(ov *app.AccountOverview) *identityv1.AccountOverview {
	pb := &identityv1.AccountOverview{
		Account: &identityv1.AccountSummary{
			Id:          ov.Account.ID,
			LurusId:     ov.Account.LurusID,
			DisplayName: ov.Account.DisplayName,
			AvatarUrl:   ov.Account.AvatarURL,
		},
		Vip: &identityv1.VIPSummary{
			Level:     int32(ov.VIP.Level),
			LevelName: ov.VIP.LevelName,
			LevelEn:   ov.VIP.LevelEN,
			Points:    ov.VIP.Points,
		},
		Wallet: &identityv1.WalletSummary{
			Balance:      ov.Wallet.Balance,
			Frozen:       ov.Wallet.Frozen,
			DiscountRate: ov.Wallet.DiscountRate,
			DiscountTier: ov.Wallet.DiscountTier,
		},
		TopupUrl: ov.TopupURL,
	}

	if ov.VIP.LevelExpiresAt != nil {
		pb.Vip.LevelExpiresAt = timestamppb.New(*ov.VIP.LevelExpiresAt)
	}

	if ov.Subscription != nil {
		pb.Subscription = &identityv1.SubscriptionSummary{
			ProductId: ov.Subscription.ProductID,
			PlanCode:  ov.Subscription.PlanCode,
			Status:    ov.Subscription.Status,
			AutoRenew: ov.Subscription.AutoRenew,
		}
		if ov.Subscription.ExpiresAt != nil {
			pb.Subscription.ExpiresAt = timestamppb.New(*ov.Subscription.ExpiresAt)
		}
	}

	return pb
}
