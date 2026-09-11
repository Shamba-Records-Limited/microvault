// Package compliance defines the provider-neutral contract for screening a
// blockchain address before it is trusted with money — the vault allowlist's
// upstream decision, not the allowlist itself (see pkg/services/vaultwatch
// and the vault contract's own allow_depositor/disallow_depositor for the
// enforcement side).
//
// pkg/compliance/elliptic implements Screener against Elliptic's AML API.
// There is one provider today; this package stays small rather than
// inventing a registry for it, per pkg/payment/README.md's layering
// principles.
package compliance
