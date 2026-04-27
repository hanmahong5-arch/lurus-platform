// Package ops is the platform's catalogue of privileged operations:
// delete an OIDC app, purge a user account, rotate a master secret,
// approve a large refund, etc. Each Op carries enough metadata for
// admin UIs and the Lutu APP confirm screen to render risk-aware
// prompts without hardcoding op-specific knowledge.
//
// Why a separate package
//
// QRHandler dispatches confirmed delegate sessions to executors via
// the QRDelegateExecutor interface — that's the *execution* contract.
// This package adds the *metadata* contract: what ops exist, what
// risk colour to paint, what description to read aloud. Splitting
// the two lets the catalog endpoint enumerate ops without touching
// the handler package and lets future declarative reconcilers (Phase
// 5+: NewAPI / Memorus YAML) join the same registry.
//
// Lifetime
//
// Registry is built once at boot in cmd/core/main.go, populated with
// every privileged op the deployment exposes, and passed to the
// catalog handler. It is read-only after startup; the package
// deliberately omits a global so tests can build hermetic registries
// and parallel deployments don't share state.
package ops

// RiskLevel classifies how dangerous an op is. Drives UI colour and
// whether the APP requires biometric step-up before confirming. The
// values are intentionally a small fixed set — adding a new tier is
// an explicit contract change so APP/admin UI palettes stay aligned.
type RiskLevel string

const (
	// RiskInfo is for read-only or trivially reversible ops (status
	// queries, low-stakes config reads). Rendered with neutral UI.
	RiskInfo RiskLevel = "info"
	// RiskWarn is for state-changing ops that are reversible or
	// low-blast-radius (e.g. rotate a single secret). Rendered amber
	// in admin/APP UI; biometric step-up is recommended but not
	// strictly required.
	RiskWarn RiskLevel = "warn"
	// RiskDestructive is for ops that destroy data or revoke access
	// in ways that are slow or impossible to undo (delete app,
	// purge account, terminate cluster). Rendered red; APP MUST
	// require biometric step-up before confirm.
	RiskDestructive RiskLevel = "destructive"
)

// Valid reports whether r is a known risk level. Used by the
// registry to refuse misspelled levels at register time rather than
// surfacing them as broken UI rendering at runtime.
func (r RiskLevel) Valid() bool {
	switch r {
	case RiskInfo, RiskWarn, RiskDestructive:
		return true
	default:
		return false
	}
}

// Op is the metadata contract every privileged operation satisfies.
// The interface is metadata-only on purpose — execution surfaces
// (QRDelegateExecutor for APP-confirmed ops, future Reconciler for
// declarative ops) layer on top. A single registry can therefore
// hold both kinds and the catalog endpoint enumerates them
// uniformly.
//
// Implementations should be value-typed where practical so the
// registry can be passed by pointer without surprising mutation.
type Op interface {
	// Type returns the canonical op identifier (e.g. "delete_account").
	// Must match the string used in QR delegate session params for
	// DelegateOps so the registry and the handler dispatch agree.
	Type() string
	// Description is a one-line English summary suitable for admin
	// UIs and audit logs. Kept English so log/Loki greps are
	// stable; Lutu APP maps Type→localised label client-side.
	Description() string
	// RiskLevel drives UI colour and biometric-step-up gating.
	RiskLevel() RiskLevel
	// IsDestructive is a convenience for callers that want a quick
	// boolean. Conventionally true iff RiskLevel() == RiskDestructive.
	IsDestructive() bool
}

// DelegateOp marks an Op that runs on the APP confirm path of a QR
// delegate session. SupportedOps mirrors handler.QRDelegateExecutor
// so a production executor that already implements that interface
// becomes a DelegateOp with just four extra metadata methods —
// metadata is collocated with execution, no risk of drift.
//
// The handler package never imports this package; instead, when
// main.go wires an executor it both calls qr.WithDelegateExecutor
// and registry.Register on the same value. That keeps the handler
// free of an ops dependency while the registry still holds the
// canonical catalogue.
type DelegateOp interface {
	Op
	SupportedOps() []string
}
