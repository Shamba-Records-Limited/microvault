// Package compliance orchestrates the KYB lifecycle described in
// elliptic-compliance-integration.md §13: a counterparty submits an
// address, KYB approval triggers screening via pkg/compliance.Screener,
// and the verdict is applied to pkg/repository.CounterpartyRepository's
// three tables.
//
// This is core-owned end to end — unlike pkg/services/mgpoller, which
// splits its interfaces because MoneyGram's flow touches credit-owned
// loans, nothing here reaches outside core, so there is no adapter
// indirection to a credit-side implementation.
//
// Scope for this pass (Phase 2 of the source design doc): persistence and
// the lifecycle service. It does not submit the vault contract's
// allow_depositor transaction — see RecordScreening's doc comment on why
// that stays a separate, not-yet-built worker.
package compliance
