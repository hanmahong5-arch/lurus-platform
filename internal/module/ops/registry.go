package ops

import (
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrDuplicateOp is returned by Register when an op with the same
// Type() is already registered. Callers should treat this as a boot
// misconfiguration and panic — the alternative (silent overwrite)
// hides routing bugs that would later surface as "wrong handler ran
// the destructive action".
var ErrDuplicateOp = errors.New("ops: op already registered")

// ErrInvalidOp is returned by Register when the supplied Op fails
// shape checks (nil, empty Type, unknown RiskLevel). Surfaces at
// boot so a misspelled risk level cannot make it to production.
var ErrInvalidOp = errors.New("ops: invalid op")

// Registry holds the platform's privileged-op catalogue. Built once
// at boot, read at request time. Concurrent reads are safe; writes
// (Register / MustRegister) are guarded but expected to happen only
// during startup.
type Registry struct {
	mu  sync.RWMutex
	ops map[string]Op
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{ops: make(map[string]Op)}
}

// Register adds op to the registry. Returns ErrInvalidOp on bad
// shape and ErrDuplicateOp on Type() collision. Both conditions
// should panic at boot — the registry is not the right place to
// recover from misconfiguration.
func (r *Registry) Register(op Op) error {
	if op == nil {
		return fmt.Errorf("%w: nil op", ErrInvalidOp)
	}
	t := op.Type()
	if t == "" {
		return fmt.Errorf("%w: empty Type()", ErrInvalidOp)
	}
	if !op.RiskLevel().Valid() {
		return fmt.Errorf("%w: %q has unknown RiskLevel %q", ErrInvalidOp, t, op.RiskLevel())
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.ops[t]; ok {
		return fmt.Errorf("%w: %q", ErrDuplicateOp, t)
	}
	r.ops[t] = op
	return nil
}

// MustRegister panics on Register error. Boot-time use only — keeps
// cmd/core/main.go free of error-handling boilerplate for what is
// always a deployer mistake when it fails.
func (r *Registry) MustRegister(op Op) {
	if err := r.Register(op); err != nil {
		panic(err)
	}
}

// Lookup returns the Op registered under t. The boolean lets callers
// distinguish "registered but nil" (impossible) from "not
// registered" without sentinel comparisons.
func (r *Registry) Lookup(t string) (Op, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	op, ok := r.ops[t]
	return op, ok
}

// List returns every registered Op sorted by Type() ascending. The
// stable order makes the catalog endpoint's response deterministic,
// which matters for client-side caching and snapshot tests.
func (r *Registry) List() []Op {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Op, 0, len(r.ops))
	for _, op := range r.ops {
		out = append(out, op)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Type() < out[j].Type() })
	return out
}

// ListDelegate returns only the registered ops that satisfy
// DelegateOp — the subset the QR confirm path can dispatch to.
// Sorted by Type() to keep response order stable.
func (r *Registry) ListDelegate() []DelegateOp {
	all := r.List() // already sorted; preserves order
	out := make([]DelegateOp, 0, len(all))
	for _, op := range all {
		if d, ok := op.(DelegateOp); ok {
			out = append(out, d)
		}
	}
	return out
}

// Len returns the number of registered ops. Useful for boot logs
// and tests that assert the expected catalogue size.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.ops)
}
