package app

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/hanmahong5-arch/lurus-platform/modules/notification/internal/domain/entity"
)

const (
	// digestDay is the day of week to send weekly digests (Monday).
	digestDay = time.Monday
	// digestHour is the hour (CST) to send digests.
	digestHour = 9
)

// DigestWorker sends weekly usage digest emails to active users.
type DigestWorker struct {
	notifSvc *NotificationService
	fetcher  DigestDataFetcher
}

// DigestDataFetcher retrieves per-user weekly usage data from platform internal API.
type DigestDataFetcher interface {
	// FetchActiveAccountIDs returns account IDs that were active in the past week.
	FetchActiveAccountIDs(ctx context.Context) ([]int64, error)
	// FetchWeeklyData returns usage summary data for a single account.
	FetchWeeklyData(ctx context.Context, accountID int64) (*WeeklyDigestData, error)
}

// WeeklyDigestData holds the aggregated data for a weekly digest email.
type WeeklyDigestData struct {
	AccountID     int64
	Email         string
	DisplayName   string
	APICallCount  int64
	TokensUsed    int64
	TopModel      string
	BalanceChange float64
	// Lucrum-specific
	BacktestCount    int64
	BestStrategy     string
	CheckinStreak    int
}

// NewDigestWorker creates a DigestWorker.
func NewDigestWorker(notifSvc *NotificationService, fetcher DigestDataFetcher) *DigestWorker {
	return &DigestWorker{notifSvc: notifSvc, fetcher: fetcher}
}

// Start launches the digest worker that checks once per hour if it's time to send.
func (w *DigestWorker) Start(ctx context.Context) {
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				// Check if it's Monday 09:00 CST.
				cst := time.FixedZone("CST", 8*60*60)
				nowCST := now.In(cst)
				if nowCST.Weekday() == digestDay && nowCST.Hour() == digestHour {
					w.sendDigests(ctx)
				}
			}
		}
	}()
}

func (w *DigestWorker) sendDigests(ctx context.Context) {
	if w.fetcher == nil {
		slog.Debug("digest worker: no data fetcher configured, skipping")
		return
	}

	accountIDs, err := w.fetcher.FetchActiveAccountIDs(ctx)
	if err != nil {
		slog.Error("digest worker: failed to fetch active accounts", "err", err)
		return
	}

	slog.Info("digest worker: starting weekly digest", "accounts", len(accountIDs))
	sent := 0
	for _, accountID := range accountIDs {
		data, err := w.fetcher.FetchWeeklyData(ctx, accountID)
		if err != nil {
			slog.Warn("digest worker: failed to fetch data",
				"account_id", accountID, "err", err)
			continue
		}

		if data.Email == "" {
			continue
		}

		vars := map[string]string{
			"display_name":   data.DisplayName,
			"api_calls":      fmt.Sprintf("%d", data.APICallCount),
			"tokens_used":    fmt.Sprintf("%d", data.TokensUsed),
			"top_model":      data.TopModel,
			"balance_change": fmt.Sprintf("%.2f", data.BalanceChange),
		}

		week := time.Now().AddDate(0, 0, -7).Format("2006-01-02")
		if err := w.notifSvc.Send(ctx, SendRequest{
			AccountID: accountID,
			EventType: "system.weekly_digest",
			EventID:   fmt.Sprintf("digest_%d_%s", accountID, week),
			Channels:  []entity.Channel{entity.ChannelEmail},
			Vars:      vars,
			EmailAddr: data.Email,
		}); err != nil {
			slog.Warn("digest worker: failed to send digest",
				"account_id", accountID, "err", err)
			continue
		}
		sent++
	}

	slog.Info("digest worker: weekly digest completed", "sent", sent, "total", len(accountIDs))
}
