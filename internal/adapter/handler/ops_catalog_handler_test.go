package handler_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/hanmahong5-arch/lurus-platform/internal/adapter/handler"
	"github.com/hanmahong5-arch/lurus-platform/internal/module/ops"
)

// catalogStub is the minimum-viable Op for catalog handler tests.
// Lives next to the test (not exported) because the ops package's
// own tests already cover Op interface conformance.
type catalogStub struct {
	t           string
	desc        string
	risk        ops.RiskLevel
	destructive bool
	supported   []string // non-nil → also satisfies DelegateOp
}

func (c catalogStub) Type() string             { return c.t }
func (c catalogStub) Description() string      { return c.desc }
func (c catalogStub) RiskLevel() ops.RiskLevel { return c.risk }
func (c catalogStub) IsDestructive() bool      { return c.destructive }

// catalogDelegateStub embeds catalogStub and adds SupportedOps so it
// passes the type assertion to ops.DelegateOp.
type catalogDelegateStub struct {
	catalogStub
}

func (d catalogDelegateStub) SupportedOps() []string { return d.supported }

func runOpsCatalog(t *testing.T, h *handler.OpsCatalogHandler) (*httptest.ResponseRecorder, []map[string]any) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/admin/v1/ops", nil)
	h.List(c)

	var body struct {
		Ops []map[string]any `json:"ops"`
	}
	if w.Body.Len() > 0 {
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode response: %v — raw=%s", err, w.Body.String())
		}
	}
	return w, body.Ops
}

func TestOpsCatalog_NilRegistry_503(t *testing.T) {
	h := handler.NewOpsCatalogHandler(nil)
	w, _ := runOpsCatalog(t, h)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d; want 503", w.Code)
	}
}

func TestOpsCatalog_EmptyRegistry_ReturnsEmptyArray(t *testing.T) {
	r := ops.NewRegistry()
	h := handler.NewOpsCatalogHandler(r)

	w, list := runOpsCatalog(t, h)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s); want 200", w.Code, w.Body.String())
	}
	if list == nil {
		t.Fatal("ops field missing from response")
	}
	if len(list) != 0 {
		t.Errorf("ops len = %d; want 0", len(list))
	}
}

func TestOpsCatalog_PopulatedRegistry_ReturnsSortedEntries(t *testing.T) {
	r := ops.NewRegistry()
	// Register out of alphabetical order to verify the catalog
	// preserves Registry.List()'s sort.
	r.MustRegister(catalogDelegateStub{
		catalogStub: catalogStub{
			t: "delete_oidc_app", desc: "Delete an OIDC app", risk: ops.RiskDestructive, destructive: true,
		},
	})
	r.MustRegister(catalogDelegateStub{
		catalogStub: catalogStub{
			t: "delete_account", desc: "GDPR purge", risk: ops.RiskDestructive, destructive: true,
		},
	})
	r.MustRegister(catalogStub{
		t: "audit_view", desc: "Read-only audit log", risk: ops.RiskInfo,
	})

	h := handler.NewOpsCatalogHandler(r)
	w, list := runOpsCatalog(t, h)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d (body=%s); want 200", w.Code, w.Body.String())
	}
	if len(list) != 3 {
		t.Fatalf("ops len = %d; want 3 — body=%s", len(list), w.Body.String())
	}

	// Sorted ascending by type.
	wantOrder := []string{"audit_view", "delete_account", "delete_oidc_app"}
	for i, entry := range list {
		got, _ := entry["type"].(string)
		if got != wantOrder[i] {
			t.Errorf("ops[%d].type = %q; want %q", i, got, wantOrder[i])
		}
	}

	// audit_view is non-destructive + non-delegate.
	if list[0]["destructive"] != false {
		t.Errorf("ops[0].destructive = %v; want false", list[0]["destructive"])
	}
	if list[0]["delegate"] != false {
		t.Errorf("ops[0].delegate = %v; want false", list[0]["delegate"])
	}
	if list[0]["risk_level"] != "info" {
		t.Errorf("ops[0].risk_level = %v; want info", list[0]["risk_level"])
	}

	// delete_account is destructive + delegate.
	if list[1]["destructive"] != true {
		t.Errorf("ops[1].destructive = %v; want true", list[1]["destructive"])
	}
	if list[1]["delegate"] != true {
		t.Errorf("ops[1].delegate = %v; want true (DelegateOp)", list[1]["delegate"])
	}
	if list[1]["risk_level"] != "destructive" {
		t.Errorf("ops[1].risk_level = %v; want destructive", list[1]["risk_level"])
	}
	if list[1]["description"] != "GDPR purge" {
		t.Errorf("ops[1].description = %v; want 'GDPR purge'", list[1]["description"])
	}
}

// Production executors must satisfy ops.DelegateOp via the metadata
// methods added in apps_admin_op.go / account_delete_op.go. This
// test guards the wiring contract — if either type stops being a
// DelegateOp at compile time, the var _ assertions catch it; this
// test additionally guards that the *values* (Type / RiskLevel) are
// what production wiring expects so a typo in the metadata can't
// silently land the wrong colour in the APP.
func TestProductionExecutors_AreDelegateOps(t *testing.T) {
	// AppsAdminHandler is exposed via NewAppsAdminHandler with three
	// nil deps — it is permitted to be partially-wired since we only
	// touch the Op metadata methods here, none of which deref the
	// concrete deps.
	apps := handler.NewAppsAdminHandler("", nil, nil)
	var asOp ops.DelegateOp = apps
	if asOp.Type() != "delete_oidc_app" {
		t.Errorf("apps.Type = %q; want delete_oidc_app", asOp.Type())
	}
	if asOp.RiskLevel() != ops.RiskDestructive {
		t.Errorf("apps.RiskLevel = %q; want destructive", asOp.RiskLevel())
	}
	if !asOp.IsDestructive() {
		t.Error("apps.IsDestructive = false; want true")
	}
	if got := asOp.SupportedOps(); len(got) != 1 || got[0] != "delete_oidc_app" {
		t.Errorf("apps.SupportedOps = %v; want [delete_oidc_app]", got)
	}

	acct := handler.NewAccountDeleteExecutor(nil, nil, nil, nil)
	var asOp2 ops.DelegateOp = acct
	if asOp2.Type() != "delete_account" {
		t.Errorf("acct.Type = %q; want delete_account", asOp2.Type())
	}
	if asOp2.RiskLevel() != ops.RiskDestructive {
		t.Errorf("acct.RiskLevel = %q; want destructive", asOp2.RiskLevel())
	}
	if !asOp2.IsDestructive() {
		t.Error("acct.IsDestructive = false; want true")
	}
	if got := asOp2.SupportedOps(); len(got) != 1 || got[0] != "delete_account" {
		t.Errorf("acct.SupportedOps = %v; want [delete_account]", got)
	}
}
