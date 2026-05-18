// Package offramp defines the channel-agnostic contract for off-ramping
// funds from crypto to fiat across multiple providers (e.g. MoneyGram cash
// pickup, YellowCard mobile money). It is consumed by USSD adapters,
// background pollers, and future chat-bot channels alike — no channel
// concerns leak into this package.
//
// Provider implementations live in their respective sub-packages
// (pkg/payment/moneygram, pkg/payment/yellowcard) and the thin glue that
// wires a request shape into each provider lives in
// pkg/mobile/ussd/adapters until phase 2 splits capabilities further.
package offramp
