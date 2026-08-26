// Package fonbnk is the client for Fonbnk's server-to-server Merchant API,
// covering both on-ramp (fiat in, crypto out) and off-ramp (crypto in, fiat
// out).
//
// # Which API
//
// Fonbnk publishes two generations and its docs site serves both. This package
// targets server-to-server v2 under /api/v2. The older v1.5 surface
// (/api/onramp/order/create) is on-ramp only and is not used. v2 models both
// directions with one symmetric deposit/payout order, so a single set of types
// covers everything.
//
// # Files
//
// fonbnk.go holds the http.RoundTripper that signs every request. client.go
// holds the shared call/error plumbing. quote.go, order.go, discovery.go and
// webhook.go wrap the endpoint groups. types.go defines the wire shapes and
// the enum constants.
//
// # Signing
//
// Three headers per request: x-client-id, x-timestamp (Unix milliseconds) and
// x-signature, which is Base64(HMAC-SHA256(Base64Decode(secret),
// "{timestamp}:{endpoint}")). The signed endpoint is the path plus the query
// string exactly as sent, so query strings are rendered once with
// url.Values.Encode and never rebuilt — a re-serialised query is the usual
// cause of a 401. The timestamp is valid for sixty seconds, so a retry
// re-signs rather than replaying. The body is not signed.
//
// # The permission gate
//
// Order creation and everything downstream of it — confirm, cancel,
// intermediate-action — require the create-users permission on the merchant
// account. Quoting and discovery do not. An integration can therefore price
// orders end to end and only discover the wall at the first real order, which
// is why that 403 is mapped to its own code, CodeMerchantNotPermitted, rather
// than folded into a generic auth failure.
//
// # Order lifecycle
//
// An order runs deposit, then payout, then refund if the payout cannot be
// delivered. Only payout_successful and refund_successful are final — see
// IsTerminal. deposit_canceled and deposit_expired look terminal and are not:
// Fonbnk still accepts a late payment and runs the payout, so a consumer must
// not release funds or reverse its own records on those alone. Statuses can
// also be skipped, so branch on the status received rather than the one
// expected next.
//
// # Rates
//
// Cashout.RateAfterFees ("exchangeRateAfterFees" on the wire) is
// amountBeforeFees divided by amountAfterFeesUsd. It mixes a pre-fee local
// amount with a post-fee USD one and moves opposite to the user's outcome — on
// an off-ramp it rises as the payout falls. It is a per-leg diagnostic, not a
// price. Quote.EffectiveRate is the figure to compare across providers: what
// actually leaves us over what actually arrives.
//
// Fees are banded and often flat, so the effective rate is strongly
// amount-dependent at small amounts. Quote the real amount rather than caching
// a rate per corridor. FeeSetting.Max is a MaxBound because Fonbnk sends the
// string "Infinity" for the top band of every fee table.
//
// # STK push
//
// On a mobile-money deposit the user gets a carrier prompt.
// TriggerIntermediateAction sends a fresh one, or submits the OTP that unlocks
// it. Calling it once intermediateActionAttempts has reached
// intermediateActionMaxAttempts does not fail harmlessly — it moves the order
// to deposit_expired. Gate every call on
// TransferInstructions.CanRetryIntermediateAction.
//
// # Webhooks
//
// Deliveries are signed with a nested plain SHA-256 rather than an HMAC:
// hex(SHA256(rawBody || hex(SHA256(secret)))). Order matters and the raw bytes
// must be hashed, so the handler has to capture the body before any middleware
// parses it. Endpoints must answer 2xx within twenty seconds, must not
// redirect, and must be idempotent — a delivery can repeat or arrive out of
// order. The payload is a summary; call GetOrder for fees, transfer
// instructions or status history.
//
// # Sandbox
//
// Sandbox and production are separate systems with separate credentials and
// orders. Contract addresses, bank names and carrier lists there are fixtures
// and must never be hard-coded — read them from GetAvailableCurrencies or from
// the quote. In sandbox only, the quote's fieldsToCreateOrder carries
// depositSandboxForcedFlow and payoutSandboxForcedFlow to force an outcome;
// which values a leg accepts varies, so read the field's own options.
//
// Design notes and the corridor decisions behind this package are in the
// knowledge vault at Microvault/fonbnk-onramp-offramp-implementation.md.
package fonbnk
