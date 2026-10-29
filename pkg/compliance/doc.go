// Package compliance defines the provider-neutral contract for screening a
// blockchain address before it is trusted with money — the vault allowlist's
// upstream decision, not the allowlist itself (see pkg/services/vaultwatch
// and the vault contract's own allow_depositor/disallow_depositor for the
// enforcement side).
//
// Providers live in subpackages (elliptic, merkle, didit); Combine runs
// several and keeps the strictest verdict, and pkg/compliance/screeners
// builds the configured one.
package compliance
