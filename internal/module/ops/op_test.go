package ops_test

import (
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// stubOp is the minimum-viable Op for registry tests. Tests build
// these inline so each case is self-describing and tests don't
// share mutable fixtures.
type stubOp struct {
	t           string
	desc        string
	risk        ops.RiskLevel
	destructive bool
}

func (s stubOp) Type() string             { return s.t }
func (s stubOp) Description() string      { return s.desc }
func (s stubOp) RiskLevel() ops.RiskLevel { return s.risk }
func (s stubOp) IsDestructive() bool      { return s.destructive }

// stubDelegateOp is stubOp + the SupportedOps method that promotes
// it to a DelegateOp. Used to verify ListDelegate filtering.
type stubDelegateOp struct {
	stubOp
	supported []string
}

func (d stubDelegateOp) SupportedOps() []string { return d.supported }

// ── RiskLevel ───────────────────────────────────────────────────────────────

func TestRiskLevel_Valid(t *testing.T) {
	for _, level := range []ops.RiskLevel{ops.RiskInfo, ops.RiskWarn, ops.RiskDestructive} {
		if !level.Valid() {
			t.Errorf("expected %q to be valid", level)
		}
	}
	for _, bad := range []ops.RiskLevel{"", "high", "fatal", "INFO"} {
		if bad.Valid() {
			t.Errorf("expected %q to be invalid", bad)
		}
	}
}

// ── Registry: Register / MustRegister ───────────────────────────────────────

func TestRegistry_Register_Happy(t *testing.T) {
	r := ops.NewRegistry()
	op := stubOp{t: "delete_account", desc: "purge", risk: ops.RiskDestructive, destructive: true}
	if err := r.Register(op); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d; want 1", r.Len())
	}
}

func TestRegistry_Register_RejectsNil(t *testing.T) {
	r := ops.NewRegistry()
	err := r.Register(nil)
	if !errors.Is(err, ops.ErrInvalidOp) {
		t.Fatalf("err = %v; want ErrInvalidOp", err)
	}
}

func TestRegistry_Register_RejectsEmptyType(t *testing.T) {
	r := ops.NewRegistry()
	err := r.Register(stubOp{t: "", risk: ops.RiskInfo})
	if !errors.Is(err, ops.ErrInvalidOp) {
		t.Fatalf("err = %v; want ErrInvalidOp", err)
	}
}

func TestRegistry_Register_RejectsBadRiskLevel(t *testing.T) {
	r := ops.NewRegistry()
	err := r.Register(stubOp{t: "x", risk: ops.RiskLevel("FATAL")})
	if !errors.Is(err, ops.ErrInvalidOp) {
		t.Fatalf("err = %v; want ErrInvalidOp", err)
	}
}

func TestRegistry_Register_DuplicateTypeFails(t *testing.T) {
	r := ops.NewRegistry()
	first := stubOp{t: "delete_account", risk: ops.RiskDestructive, destructive: true}
	if err := r.Register(first); err != nil {
		t.Fatalf("first Register: %v", err)
	}
	err := r.Register(stubOp{t: "delete_account", risk: ops.RiskWarn})
	if !errors.Is(err, ops.ErrDuplicateOp) {
		t.Fatalf("err = %v; want ErrDuplicateOp", err)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d after duplicate; want 1 (collision must not overwrite)", r.Len())
	}
}

func TestRegistry_MustRegister_PanicsOnDuplicate(t *testing.T) {
	r := ops.NewRegistry()
	r.MustRegister(stubOp{t: "x", risk: ops.RiskInfo})
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic on duplicate MustRegister")
		}
	}()
	r.MustRegister(stubOp{t: "x", risk: ops.RiskInfo})
}

// ── Registry: Lookup ────────────────────────────────────────────────────────

func TestRegistry_Lookup_Found(t *testing.T) {
	r := ops.NewRegistry()
	want := stubOp{t: "delete_account", desc: "purge", risk: ops.RiskDestructive, destructive: true}
	r.MustRegister(want)
	got, ok := r.Lookup("delete_account")
	if !ok {
		t.Fatal("Lookup returned ok=false for registered op")
	}
	if got.Type() != "delete_account" || got.Description() != "purge" {
		t.Errorf("Lookup returned wrong op: type=%q desc=%q", got.Type(), got.Description())
	}
}

func TestRegistry_Lookup_Missing(t *testing.T) {
	r := ops.NewRegistry()
	got, ok := r.Lookup("nope")
	if ok {
		t.Errorf("Lookup returned ok=true for missing op (got %v)", got)
	}
}

// ── Registry: List ──────────────────────────────────────────────────────────

func TestRegistry_List_SortedByType(t *testing.T) {
	r := ops.NewRegistry()
	// Register out of order on purpose.
	r.MustRegister(stubOp{t: "delete_oidc_app", risk: ops.RiskDestructive, destructive: true})
	r.MustRegister(stubOp{t: "delete_account", risk: ops.RiskDestructive, destructive: true})
	r.MustRegister(stubOp{t: "audit_log_view", risk: ops.RiskInfo})

	got := r.List()
	if len(got) != 3 {
		t.Fatalf("List len = %d; want 3", len(got))
	}
	wantOrder := []string{"audit_log_view", "delete_account", "delete_oidc_app"}
	for i, op := range got {
		if op.Type() != wantOrder[i] {
			t.Errorf("List[%d].Type = %q; want %q", i, op.Type(), wantOrder[i])
		}
	}
}

func TestRegistry_List_Empty(t *testing.T) {
	r := ops.NewRegistry()
	if got := r.List(); len(got) != 0 {
		t.Errorf("expected empty slice, got %v", got)
	}
}

// ── Registry: ListDelegate (filters to DelegateOp subset) ───────────────────

func TestRegistry_ListDelegate_OnlyDelegateOps(t *testing.T) {
	r := ops.NewRegistry()
	// One DelegateOp.
	r.MustRegister(stubDelegateOp{
		stubOp:    stubOp{t: "delete_account", risk: ops.RiskDestructive, destructive: true},
		supported: []string{"delete_account"},
	})
	// One plain Op (e.g., a future declarative op).
	r.MustRegister(stubOp{t: "reconcile_apps", risk: ops.RiskWarn})

	delegates := r.ListDelegate()
	if len(delegates) != 1 {
		t.Fatalf("ListDelegate len = %d; want 1", len(delegates))
	}
	if delegates[0].Type() != "delete_account" {
		t.Errorf("ListDelegate[0].Type = %q; want delete_account", delegates[0].Type())
	}
	// Sanity: full List still has both.
	if r.Len() != 2 {
		t.Errorf("Len = %d; want 2", r.Len())
	}
}

func TestRegistry_ListDelegate_StableOrder(t *testing.T) {
	r := ops.NewRegistry()
	r.MustRegister(stubDelegateOp{stubOp: stubOp{t: "z_op", risk: ops.RiskWarn}, supported: []string{"z_op"}})
	r.MustRegister(stubDelegateOp{stubOp: stubOp{t: "a_op", risk: ops.RiskWarn}, supported: []string{"a_op"}})

	got := r.ListDelegate()
	if got[0].Type() != "a_op" || got[1].Type() != "z_op" {
		t.Errorf("ListDelegate not sorted: %s, %s", got[0].Type(), got[1].Type())
	}
}
