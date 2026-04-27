package handler

import "github.com/hanmahong5-arch/lurus-platform/internal/module/ops"

// AccountDeleteExecutor additionally satisfies ops.DelegateOp so the
// GDPR-grade account purge is enumerated by the catalog endpoint
// alongside delete_oidc_app and any future destructive operations.
//
// Compile-time assertion: drift between the metadata methods below
// and ops.Op / ops.DelegateOp surfaces as a build error rather than
// a silent "missing from catalog" runtime gap.
var _ ops.DelegateOp = (*AccountDeleteExecutor)(nil)

// Type identifies this executor's op in the QR delegate dispatch
// table. Must match the SupportedOps() entry exactly.
func (e *AccountDeleteExecutor) Type() string { return qrDelegateOpDeleteAccount }

// Description is the one-line English summary surfaced in admin
// UIs and audit logs. The cascade detail (subs/wallet/zitadel) is
// captured here so audit grep can correlate "completed" lines with
// what actually ran.
func (e *AccountDeleteExecutor) Description() string {
	return "GDPR-grade account purge: cancel subscriptions, zero wallet, deactivate Zitadel user, mark account deleted"
}

// RiskLevel marks this op as destructive — the underlying Zitadel
// deactivation and account status flip are not casually reversible
// and the cascade touches every domain (subs/wallet/identity).
func (e *AccountDeleteExecutor) RiskLevel() ops.RiskLevel { return ops.RiskDestructive }

// IsDestructive shorthand for RiskLevel() == RiskDestructive.
func (e *AccountDeleteExecutor) IsDestructive() bool { return true }
