// Package classic holds the platform's non-Soroban Stellar operations:
// creating accounts, establishing trustlines, and moving USDC. Every
// operation runs against the treasury account, which is the source and
// fee payer for the work this package does.
//
// Service is the single interface. CreateSponsoredAccount and
// EstablishSponsoredTrustline manage child accounts; SponsoredPaymentTransaction
// and SendUSDC move funds; CheckUSDCTrustline reads trustline state. Build one
// with NewService, or NewServiceWithClient to supply your own RPCClient (the
// seam the tests use).
//
// # Sponsorship
//
// Child accounts hold zero XLM. The treasury sponsors their base reserve and
// any per-entry reserves through a BeginSponsoringFutureReserves /
// EndSponsoringFutureReserves envelope, so a sponsored transaction carries two
// signatures: the treasury as source and fee payer, and the child authorizing
// the operations on itself. CreateSponsoredAccount and EstablishSponsoredTrustline
// both follow this shape.
//
// # Child accounts and trustlines
//
// Children are tracking and audit markers, not custody accounts. They are
// created without a USDC trustline because they never receive USDC — borrowed
// funds land in the treasury and payouts leave it via SendUSDC. Add a trustline
// to a child only when one genuinely needs to hold the asset, using
// EstablishSponsoredTrustline. SponsoredPaymentTransaction sources from a child
// and therefore requires that trustline to exist first.
//
// # Submission and confirmation
//
// Mutating operations follow the same path: build the transaction, sign it,
// submit it, then poll. A successful submit returns a PENDING status, which only
// means stellar-core accepted the envelope — it is not confirmation. Each
// operation hands the returned hash to rpc.PollTransaction and treats anything
// other than a successful on-ledger result as a failure. Trustline reads
// (CheckUSDCTrustline) query ledger entries directly and do not submit a
// transaction.
package classic
