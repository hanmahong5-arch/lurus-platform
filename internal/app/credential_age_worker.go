package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/hanmahong5-arch/lurus-platform/internal/pkg/metrics"
)

// Default tuning for CredentialAgeWorker. Operators override the
// enable flag via CRON_CRED_AGE_ENABLED; soft/hard limits are not
// env-tunable on purpose — they encode policy ("90d soft / 180d hard")
// that should change in code with reviewer attention, not silently in
// a Helm values file.
const (
	// DefaultCredentialAgeInterval is how often the worker re-samples
	// the rotation timestamps. One day matches the metric resolution
	// (age in days) — a tighter cadence would just write the same
	// gauge value over and over.
	DefaultCredentialAgeInterval = 24 * time.Hour
	// DefaultCredentialSoftLimit is when operators get a WARN log
	// asking them to plan rotation. 90 days lines up with the
	// industry-standard PAT/admin-token rotation cadence and is well
	// inside the practical lifetime of a service-account secret
	// before audit fatigue sets in.
	DefaultCredentialSoftLimit = 90 * 24 * time.Hour
	// DefaultCredentialHardLimit is when operators get an ERROR log.
	// 180 days is twice the soft window — the alert escalation that
	// flags "this is now an active risk, schedule rotation today".
	DefaultCredentialHardLimit = 180 * 24 * time.Hour
	// credentialRotatedKeyPrefix is the stable Redis key namespace
	// for the last-rotated timestamp. Stable because the same key is
	// also written by the operator runbook (redis-cli SET …) when no
	// CLI flag is available yet.
	credentialRotatedKeyPrefix = "cred:rotated:"
)

// TrackedCredential names a single credential watched by the worker
// along with its rotation policy. Adding a new tracked credential is
// a one-line code change here plus a corresponding `MarkRotated` call
// in the bootstrap path that issues the secret.
type TrackedCredential struct {
	// Name is the metric label and Redis key suffix. Must be a stable
	// short identifier (snake_case); changing it orphans the existing
	// timestamp, so prefer adding a second name and migrating with a
	// one-shot script over renaming in place.
	Name string
	// SoftLimit is the age at which the worker logs WARN. Past this,
	// rotation should be scheduled for the next sprint.
	SoftLimit time.Duration
	// HardLimit is the age at which the worker logs ERROR. Past this,
	// rotation is overdue and should happen today.
	HardLimit time.Duration
}

// DefaultTrackedCredentials is the set of credentials the worker
// watches when wired with default config. Both are platform-level
// privileged tokens whose theft would compromise authentication
// (Zitadel PAT) or LLM gateway billing (NewAPI admin token), so
// rotation discipline is a load-bearing security control. The names
// match the env vars in `internal/pkg/config/config.go`:
// ZITADEL_SERVICE_ACCOUNT_PAT and NEWAPI_ADMIN_ACCESS_TOKEN.
var DefaultTrackedCredentials = []TrackedCredential{
	{
		Name:      "zitadel_pat",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	},
	{
		Name:      "newapi_admin_token",
		SoftLimit: DefaultCredentialSoftLimit,
		HardLimit: DefaultCredentialHardLimit,
	},
}

// credAgeRedis is the narrow Redis surface CredentialAgeWorker needs.
// Declared as an interface so unit tests can substitute miniredis-backed
// clients (or a mock) without dragging the full *redis.Client.
type credAgeRedis interface {
	Get(ctx context.Context, key string) *redis.StringCmd
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
}

// CredentialAgeWorker periodically samples the age of each tracked
// credential and reports it as a Prometheus gauge so an operator
// alert can fire when a credential crosses the soft/hard rotation
// limit. This is the *framework* layer of the rotation story —
// detecting drift. Actual rotation (calling Zitadel / NewAPI APIs +
// rewriting the K8s Secret + rolling the deployment) is a separate
// future ticket; until that ships, operators rotate manually using
// docs/runbooks/credential-rotation.md and stamp the result via
// MarkRotated.
//
// Disabled by default (Enabled=false). Production turns it on via
// CRON_CRED_AGE_ENABLED=true after the runbook is in place — flipping
// the flag without the runbook would just generate alerts no one
// knows how to act on.
type CredentialAgeWorker struct {
	rdb         credAgeRedis
	interval    time.Duration
	enabled     bool
	credentials []TrackedCredential
}

// NewCredentialAgeWorker wires the worker with default credentials
// and a 24h interval. Pass enabled=cfg.CronCredAgeEnabled at startup
// so the env flag controls behaviour without requiring the caller to
// know about TrackedCredential layout.
func NewCredentialAgeWorker(rdb *redis.Client, enabled bool) *CredentialAgeWorker {
	// Defensive copy of the package-level slice so callers that
	// mutate DefaultTrackedCredentials post-construction (e.g. tests)
	// don't poison the running worker's view.
	creds := make([]TrackedCredential, len(DefaultTrackedCredentials))
	copy(creds, DefaultTrackedCredentials)
	return &CredentialAgeWorker{
		rdb:         rdb,
		interval:    DefaultCredentialAgeInterval,
		enabled:     enabled,
		credentials: creds,
	}
}

// Name implements lifecycle.Task — keeps log identifiers consistent
// with the rest of the cron family.
func (w *CredentialAgeWorker) Name() string { return "credential_age_worker" }

// Run is the lifecycle entry point. When disabled, it logs once and
// blocks on ctx.Done so the errgroup wait isn't immediately
// short-circuited (other workers in the same group still need to be
// awaited cleanly). When enabled, it samples once on boot — so
// freshly-deployed pods don't sit at gauge=0 for the first interval
// — then ticks at w.interval until ctx cancellation.
func (w *CredentialAgeWorker) Run(ctx context.Context) error {
	if w == nil {
		<-ctx.Done()
		return nil
	}
	if !w.enabled {
		slog.Info("credential_age_worker: disabled (set CRON_CRED_AGE_ENABLED=true to enable)")
		<-ctx.Done()
		return nil
	}
	if w.rdb == nil {
		// Misconfiguration — fall back to ctx-blocking no-op rather
		// than panicking. Prevents a partial wiring from killing the
		// whole errgroup.
		slog.Warn("credential_age_worker: redis client is nil, worker is a no-op")
		<-ctx.Done()
		return nil
	}

	slog.Info("credential_age_worker.started",
		"interval", w.interval.String(),
		"credentials", len(w.credentials))

	w.sampleOnce(ctx)
	t := time.NewTicker(w.interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
			w.sampleOnce(ctx)
		}
	}
}

// sampleOnce reads each tracked credential's last-rotated timestamp
// from Redis, computes the age in days, and emits the gauge. A
// missing timestamp is treated as "operator forgot to MarkRotated on
// issue" — we log WARN and skip the gauge update for that credential
// (as opposed to writing 0, which would mask an actual fresh-credential
// case). Errors on individual credentials don't abort the loop —
// every credential is independent.
func (w *CredentialAgeWorker) sampleOnce(ctx context.Context) {
	now := time.Now()
	for _, c := range w.credentials {
		key := credentialRotatedKeyPrefix + c.Name
		rotatedAt, err := w.rdb.Get(ctx, key).Time()
		if err != nil {
			if errors.Is(err, redis.Nil) {
				// First-ever sample for this credential — operator
				// hasn't stamped a rotation yet. Don't crash, don't
				// fabricate a fake "now" stamp (that would silently
				// hide a forgotten rotation), just warn the operator
				// once per tick. The runbook tells them to call
				// MarkRotated.
				slog.Warn("credential_age_worker.no_rotation_timestamp",
					"name", c.Name,
					"hint", "call MarkRotated when the credential is issued; see docs/runbooks/credential-rotation.md")
				continue
			}
			slog.Warn("credential_age_worker.read_failed",
				"name", c.Name,
				"err", err)
			continue
		}

		age := now.Sub(rotatedAt)
		if age < 0 {
			// Future timestamp — clock skew or bad operator input.
			// Treat as zero so the gauge stays sane; the operator
			// gets a WARN to investigate.
			slog.Warn("credential_age_worker.future_timestamp",
				"name", c.Name,
				"rotated_at", rotatedAt.Format(time.RFC3339))
			age = 0
		}
		days := age.Hours() / 24
		metrics.RecordCredentialAgeDays(c.Name, days)

		switch {
		case age >= c.HardLimit:
			slog.Error("credential_age_worker.hard_limit_exceeded",
				"name", c.Name,
				"age_days", fmt.Sprintf("%.1f", days),
				"hard_limit_days", c.HardLimit.Hours()/24,
				"action", "rotate today; see docs/runbooks/credential-rotation.md")
		case age >= c.SoftLimit:
			slog.Warn("credential_age_worker.soft_limit_exceeded",
				"name", c.Name,
				"age_days", fmt.Sprintf("%.1f", days),
				"soft_limit_days", c.SoftLimit.Hours()/24,
				"action", "schedule rotation in next sprint")
		default:
			slog.Debug("credential_age_worker.sampled",
				"name", c.Name,
				"age_days", fmt.Sprintf("%.1f", days))
		}
	}
}

// MarkRotated stamps the credential as rotated NOW. Called by the
// operator (manual rotation runbook) or by the future rotation worker
// when it ships. Returns an error only when Redis itself errors —
// callers should log and continue rather than aborting the rotation
// (the worst case is a stale gauge, not a broken rotation).
//
// The name argument is validated against the empty string but not
// against w.credentials — that lets the same Redis key namespace be
// shared with credentials the operator has tracked via runbook before
// they're added to DefaultTrackedCredentials.
func (w *CredentialAgeWorker) MarkRotated(ctx context.Context, name string) error {
	if w == nil || w.rdb == nil {
		return errors.New("credential_age_worker: not wired")
	}
	if strings.TrimSpace(name) == "" {
		return errors.New("credential_age_worker: name required")
	}
	key := credentialRotatedKeyPrefix + name
	// No TTL: rotation timestamps are persistent state; expiring them
	// would silently re-introduce the "no rotation timestamp" warn
	// path on a healthy credential.
	if err := w.rdb.Set(ctx, key, time.Now().UTC().Format(time.RFC3339Nano), 0).Err(); err != nil {
		return fmt.Errorf("credential_age_worker: redis SET %s: %w", key, err)
	}
	slog.Info("credential_age_worker.marked_rotated", "name", name)
	return nil
}
