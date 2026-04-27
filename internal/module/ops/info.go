package ops

// Info is a value type that satisfies Op without an executor
// attached. It's how non-DelegateOps — direct admin actions like
// rotate_secret, future declarative reconcilers — appear in the
// platform's privileged-op catalogue alongside QR-confirmed ops.
//
// Use cases:
//
//   - A direct admin endpoint (no QR scan needed) but operators want
//     it visible in /admin/v1/ops for completeness and audit
//     dashboard filtering. Register an ops.Info pointing at the same
//     Type the handler uses internally.
//
//   - A declarative reconciler (Phase 5+: NewAPI / Memorus YAML)
//     that runs without per-confirm dispatch. Register an ops.Info
//     so the catalog enumerates "yes, this platform reconciles
//     newapi config" without requiring the reconciler to bolt on
//     four metadata methods.
//
// Why this is not a builder / functional-options struct: the field
// set is tiny and unlikely to grow (Op metadata is purposefully
// minimal). A flat struct keeps registration call-sites trivially
// readable in cmd/core/main.go.
type Info struct {
	// OpType is the canonical op identifier (e.g. "rotate_secret").
	// Must match the Type used in any audit log lines / handler
	// registry the catalog is supposed to describe.
	OpType string
	// OpDescription is the one-line English summary.
	OpDescription string
	// OpRisk drives UI colour and audit filtering. Required.
	OpRisk RiskLevel
	// OpDestructive is true iff the op cannot be casually reversed.
	// Convention: true iff OpRisk == RiskDestructive, but kept as a
	// separate field so a deployer can override (e.g. an op that
	// LOOKS warn-coloured but is actually irreversible).
	OpDestructive bool
}

// Op interface implementation.
func (i Info) Type() string             { return i.OpType }
func (i Info) Description() string      { return i.OpDescription }
func (i Info) RiskLevel() RiskLevel     { return i.OpRisk }
func (i Info) IsDestructive() bool      { return i.OpDestructive }

// Compile-time assertion that Info satisfies Op (NOT DelegateOp —
// that's intentional; direct ops register here, QR-delegate ops
// register through their executor).
var _ Op = Info{}
