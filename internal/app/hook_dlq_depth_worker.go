package app

import (
	"context"
	"log/slog"
	"time"
)

// HookDLQDepthSampler is the read side of the hook DLQ store. The
// worker only needs the depth count, not full row access.
type HookDLQDepthSampler interface {
	PendingDepth(ctx context.Context) (int64, error)
}

// HookDLQDepthSink updates the prometheus gauge. Method name matches
// module.HookMetricsSink so the same adapter struct satisfies both.
type HookDLQDepthSink interface {
	SetDLQDepth(depth int64)
}

// HookDLQDepthWorker periodically samples module.hook_failures pending
// count and refreshes the `hook_dlq_pending` gauge so alerts fire on
// fresh data even when no admin is browsing the DLQ. Without this the
// gauge would only update during operator-initiated List calls.
//
// Idempotent: safe to start multiple instances (each just samples the
// same DB and writes the same gauge). Cheap query (single COUNT with
// partial-index lookup) so a 30s tick is fine.
type HookDLQDepthWorker struct {
	sampler  HookDLQDepthSampler
	sink     HookDLQDepthSink
	interval time.Duration
}

// NewHookDLQDepthWorker constructs the worker. interval ≤ 0 falls back
// to 30 seconds. sampler or sink == nil → worker becomes a no-op so
// callers can wire unconditionally and disable by passing nil.
func NewHookDLQDepthWorker(sampler HookDLQDepthSampler, sink HookDLQDepthSink, interval time.Duration) *HookDLQDepthWorker {
	if interval <= 0 {
		interval = 30 * time.Second
	}
	return &HookDLQDepthWorker{
		sampler:  sampler,
		sink:     sink,
		interval: interval,
	}
}

// Run samples the depth on each tick until ctx cancellation. First
// sample fires immediately on start so the gauge isn't 0-by-default
// for the first interval after process boot.
//
// Sampling errors are logged at WARN — they don't abort the loop. A
// transient DB blip shouldn't kill the worker for the rest of the
// process lifetime.
func (w *HookDLQDepthWorker) Run(ctx context.Context) error {
	if w == nil || w.sampler == nil || w.sink == nil {
		// nil-safe no-op: just block on ctx so the errgroup doesn't
		// see an immediate return that could be confused with success.
		<-ctx.Done()
		return nil
	}

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

func (w *HookDLQDepthWorker) sampleOnce(ctx context.Context) {
	depth, err := w.sampler.PendingDepth(ctx)
	if err != nil {
		slog.WarnContext(ctx, "hook_dlq_depth_worker: sample failed", "err", err)
		return
	}
	w.sink.SetDLQDepth(depth)
}
