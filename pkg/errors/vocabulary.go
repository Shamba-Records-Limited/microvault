package errors

// This file is the shared vocabulary for structured errors built with
// samber/oops. It holds no logic — only the strings that must agree across
// packages for the attributes to be worth anything.
//
// The point is aggregation. An error's domain, code and attribute keys are what
// an on-call engineer filters and groups by in Datadog or Loki, so a code
// spelled `vault_repay_failed` in one package and `repay_vault_failed` in
// another produces two dashboards for one failure. Constants make that a
// compile error rather than a discovery made during an incident.
//
// Rules that keep the vocabulary useful:
//
//   - The message stays static and low-cardinality. Anything variable — a loan
//     ID, an address, an amount — goes in an attribute, or APM groups one
//     error per loan instead of one per failure mode.
//   - A code exists only if someone would filter or alert on it. Codes that
//     merely restate the message earn nothing.
//   - Sentinels are not replaced. oops wraps them, and errors.Is keeps working;
//     see the sentinel files in pkg/stellar/types, pkg/pin and elsewhere.

// Domains name the bounded area an error came from, passed to oops.In. One per
// area rather than one per package: a caller debugging a failed payout cares
// that it was the off-ramp, not which of four files produced it.
const (
	// DomainStellarVault covers Soroban vault contract calls — borrow, repay,
	// repay_for, bump_yield, accrue.
	DomainStellarVault = "stellar-vault"

	// DomainStellarClassic covers non-Soroban Stellar work: accounts,
	// trustlines, payments, and transaction confirmation.
	DomainStellarClassic = "stellar-classic"

	// DomainStellarAnchor covers the SEP-1/9/10/24 anchor protocol client.
	DomainStellarAnchor = "stellar-anchor"

	// DomainMoneyGram covers MoneyGram's REST surface — OAuth and FX rates —
	// as distinct from the SEP protocol work under DomainStellarAnchor.
	DomainMoneyGram = "moneygram"

	// DomainMoneyGramPoller covers both directions of the SEP-24 poller: cash
	// pickup out and borrower repayment in.
	DomainMoneyGramPoller = "moneygram-poller"

	// DomainOffRamp covers payout providers and the refund unwind path.
	DomainOffRamp = "off-ramp"

	// DomainRepaymentCashIn covers the borrower repayment rail on the credit
	// side — quote lock, deposit initiation, and settlement.
	DomainRepaymentCashIn = "repayment-cash-in"

	// DomainUSSD covers the USSD session, its menus and its adapters.
	DomainUSSD = "ussd"

	// DomainIdentity covers registration, PIN and authentication.
	DomainIdentity = "identity"

	// DomainLending covers loan origination and the credit services behind it.
	DomainLending = "lending"

	// DomainPersistence covers repositories and the database layer.
	DomainPersistence = "persistence"
)

// Codes are machine-readable failure identifiers, passed to oops.Code. They are
// deliberately coarse: a code answers "what kind of thing went wrong", and the
// attributes answer "to what".
const (
	// CodeMissingDependency is a constructor handed a nil collaborator. Always
	// a boot-time failure, never a runtime one.
	CodeMissingDependency = "missing_dependency"

	// CodeInvalidAmount is a money value that is non-positive or outside the
	// range a provider accepts.
	CodeInvalidAmount = "invalid_amount"

	// CodeInvalidAddress is a string that is not a usable Stellar account or
	// contract address.
	CodeInvalidAddress = "invalid_address"

	// CodeBelowAnchorMinimum is an amount under a provider's floor — for
	// MoneyGram deposits, 15 USDC. Distinct from CodeInvalidAmount because the
	// amount is well-formed and the limit is someone else's.
	CodeBelowAnchorMinimum = "below_anchor_minimum"

	// CodeAboveAnchorMaximum is an amount over a provider's ceiling — for
	// MoneyGram deposits, 950 USDC; for withdrawals, 2,500. Same shape as
	// CodeBelowAnchorMinimum and separate from it because the two are actioned
	// differently: below the floor the borrower waits or uses another rail,
	// above the ceiling they must split the payment.
	CodeAboveAnchorMaximum = "above_anchor_maximum"

	// CodeSimulationRejected is a Soroban simulation returning a contract-level
	// error. The call was well-formed and the contract refused it.
	CodeSimulationRejected = "simulation_rejected"

	// CodeSimulationFailed is a simulation that could not be performed at all.
	// Transport, not contract.
	CodeSimulationFailed = "simulation_failed"

	// CodeBuildFailed and CodeSubmitFailed bracket transaction construction and
	// submission.
	CodeBuildFailed  = "build_failed"
	CodeSubmitFailed = "submit_failed"

	// CodeVaultRepayFailed is the treasury-to-vault leg failing. Worth its own
	// code because it is the one failure where a borrower has already paid.
	CodeVaultRepayFailed = "vault_repay_failed"

	// CodeStateWriteFailed is a database write inside a state transition.
	CodeStateWriteFailed = "state_write_failed"

	// CodeLoanLoadFailed is a loan row that could not be read.
	CodeLoanLoadFailed = "loan_load_failed"

	// CodeNotFound is a record that does not exist, as distinct from one that
	// could not be read.
	CodeNotFound = "not_found"

	// CodeUnauthorized is a provider or contract rejecting our credentials.
	CodeUnauthorized = "unauthorized"

	// CodePermissionDenied is the caller lacking rights to an operation, as
	// distinct from bad credentials.
	CodePermissionDenied = "permission_denied"

	// CodeAccountLocked is a PIN lockout. User-facing, so it carries a Public
	// message.
	CodeAccountLocked = "account_locked"

	// CodeHTTPError is a non-2xx from a provider; CodeTransportFailed is a
	// request that never completed.
	CodeHTTPError       = "http_error"
	CodeTransportFailed = "transport_failed"

	// CodeEncodeFailed and CodeDecodeFailed are serialisation at a boundary.
	CodeEncodeFailed = "encode_failed"
	CodeDecodeFailed = "decode_failed"

	// CodeIncompleteResponse is a provider returning success but omitting
	// fields the protocol requires.
	CodeIncompleteResponse = "incomplete_response"

	// CodeRateUnavailable is an FX rate that could not be sourced from any leg
	// of the cascade.
	CodeRateUnavailable = "rate_unavailable"

	// CodeQuoteExpired is a locked quote used after its window closed.
	CodeQuoteExpired = "quote_expired"

	// CodeDuplicateRequest is an idempotency key already seen.
	CodeDuplicateRequest = "duplicate_request"

	// CodeInsufficientLiquidity is the vault unable to fund a borrow.
	CodeInsufficientLiquidity = "insufficient_liquidity"

	// Missing-input codes. Each names a field whose absence stops an operation
	// before it starts, and each is separate because they are actioned
	// differently: a missing JWT is a wiring fault, a missing borrower address
	// is a data fault, a missing phone number is a registration gap.
	CodeMissingJWT          = "missing_jwt"
	CodeMissingAmount       = "missing_amount"
	CodeMissingAccount      = "missing_account"
	CodeMissingAccountIndex = "missing_account_index"
	CodeMissingBorrowerAddr = "missing_borrower_address"
	CodeMissingPhoneNumber  = "missing_phone_number"

	// CodeNilTransaction is a polled anchor transaction that came back nil.
	CodeNilTransaction = "nil_transaction"

	// CodeAnchorNotWired is a repayment attempted before the SEP-24 client and
	// treasury address were configured. Boot-time misconfiguration surfacing
	// at runtime, which is why it is not CodeMissingDependency.
	CodeAnchorNotWired = "anchor_not_wired"

	// CodeDepositInitFailed is the anchor refusing to open a cash deposit.
	CodeDepositInitFailed = "deposit_init_failed"

	// CodeQuoteFailed is a payoff that could not be computed — vault index or
	// FX unavailable. The quote hard-fails rather than serving a stale figure.
	CodeQuoteFailed = "quote_failed"

	// CodeSendFailed is an outbound notification that could not be delivered.
	CodeSendFailed = "send_failed"
)

// Attribute keys are passed to oops .With. Consistency matters more here than
// completeness: these become the filter keys, and a loan recorded under both
// `loan_id` and `loanID` cannot be searched as one field.
const (
	AttrLoanID           = "loan_id"
	AttrUserID           = "user_id"
	AttrAccountIndex     = "account_index"
	AttrBorrower         = "borrower"
	AttrRecipient        = "recipient"
	AttrAddress          = "address"
	AttrAmountStroops    = "amount_stroops"
	AttrAmountLocal      = "amount_local"
	AttrCurrency         = "currency"
	AttrTxHash           = "tx_hash"
	AttrMoneyGramTxID    = "mg_tx_id"
	AttrSequenceID       = "sequence_id"
	AttrCollectionID     = "collection_id"
	AttrProvider         = "provider"
	AttrContractFunction = "contract_function"
	AttrOperation        = "operation"
	AttrDirection        = "direction"
	AttrDependency       = "dependency"
	AttrStatusCode       = "status_code"
	AttrNotification     = "notification"
)
