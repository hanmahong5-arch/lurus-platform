package ops_test

import (
	"errors"
	"testing"

	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

func TestInfo_SatisfiesOp(t *testing.T) {
	var op ops.Op = ops.Info{
		OpType:        "rotate_secret",
		OpDescription: "Rotate a secret",
		OpRisk:        ops.RiskWarn,
		OpDestructive: false,
	}
	if op.Type() != "rotate_secret" {
		t.Errorf("Type = %q", op.Type())
	}
	if op.RiskLevel() != ops.RiskWarn {
		t.Errorf("RiskLevel = %q", op.RiskLevel())
	}
	if op.IsDestructive() {
		t.Error("IsDestructive should be false")
	}
}

func TestInfo_NotADelegateOp(t *testing.T) {
	// Info intentionally does NOT satisfy DelegateOp — that
	// distinction is what makes the catalog endpoint set
	// `delegate: false` for direct admin ops.
	var op ops.Op = ops.Info{OpType: "x", OpRisk: ops.RiskInfo}
	if _, ok := op.(ops.DelegateOp); ok {
		t.Error("ops.Info should NOT satisfy DelegateOp")
	}
}

func TestInfo_RegistersInRegistry(t *testing.T) {
	r := ops.NewRegistry()
	if err := r.Register(ops.Info{
		OpType:        "rotate_secret",
		OpDescription: "rotate",
		OpRisk:        ops.RiskWarn,
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	op, ok := r.Lookup("rotate_secret")
	if !ok {
		t.Fatal("Lookup returned ok=false")
	}
	if op.Description() != "rotate" {
		t.Errorf("Description = %q", op.Description())
	}
}

func TestInfo_RejectsBadRiskLevel(t *testing.T) {
	r := ops.NewRegistry()
	err := r.Register(ops.Info{OpType: "x", OpRisk: ops.RiskLevel("BAD")})
	if !errors.Is(err, ops.ErrInvalidOp) {
		t.Fatalf("err = %v; want ErrInvalidOp", err)
	}
}
