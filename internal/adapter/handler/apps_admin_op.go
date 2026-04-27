package handler

import "github.com/hanmahong5-arch/lurus-platform/internal/module/ops"

// AppsAdminHandler additionally satisfies ops.DelegateOp so it can
// be registered in the platform-wide ops catalogue alongside other
// privileged operations. Only the delete-OIDC-app path qualifies as
// a delegate op (the rotate-secret endpoint is direct admin auth,
// not QR-confirmed); the metadata below describes that op.
//
// Compile-time assertion: any drift between the four metadata
// methods and the ops.Op / ops.DelegateOp shape fails the build
// here rather than surfacing as "missing from catalog" at runtime.
var _ ops.DelegateOp = (*AppsAdminHandler)(nil)

// Type identifies this executor's op in the QR delegate dispatch
// table. Must match the SupportedOps entry exactly so the registry
// and the handler cannot disagree.
func (h *AppsAdminHandler) Type() string { return qrDelegateOpDeleteOIDCApp }

// Description is the one-line English summary surfaced in admin
// UIs and audit logs. Lutu APP renders its own localised label
// keyed off Type().
func (h *AppsAdminHandler) Description() string {
	return "Delete an OIDC app from Zitadel and remove its K8s Secret keys"
}

// RiskLevel marks this op as destructive — the Zitadel app deletion
// + Secret key removal are not transactionally reversible (the new
// app would have a fresh client_id/secret pair).
func (h *AppsAdminHandler) RiskLevel() ops.RiskLevel { return ops.RiskDestructive }

// IsDestructive shorthand for RiskLevel() == RiskDestructive.
func (h *AppsAdminHandler) IsDestructive() bool { return true }
