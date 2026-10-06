# Microvault Documentation

Developer documentation for Microvault, grouped by area.

| Area | Read this when you want to… |
|---|---|
| **[Soroban Contracts](./soroban/README.md)** | Understand the on-chain Vault and governance contracts: deposit/withdraw, borrow/repay, the compliance allowlist and exit grace period, views, events, error codes, and admin workflows. |
| **[Stellar Go Client](./stellar/client.md)** | Call Stellar from Go: the sponsorship model, child-account lifecycle, moving USDC, the Vault client, transaction confirmation, errors, and configuration. |
| **[Off-Ramp](./offramp/README.md)** | Turn a USDC loan into real money through a payment provider: YellowCard and Fonbnk mobile money (settlement modes, the direct→fiat pivot, order lifecycles, webhook state machines) and MoneyGram cash pickup (the SEP-24 interactive flow, poller, treasury send, and refunds). |
| **[On-Ramp](./onramp/README.md)** | Turn real money back into USDC: MoneyGram cash-in repayment (the SEP-24 deposit, the column-driven poller, deposit memos, and the treasury→vault leg). |
| **[Payment Relay](./relay/README.md)** | Decide which provider handles a payout: the effective-rate comparison, the `ENABLE_PAYMENT_PROVIDER_RELAY_SWITCH` kill switch, the margin guard, and how to add a provider of your own. |
| **[Mobile](./mobile/README.md)** | Wire telecom gateways into the platform: USSD menu flows, SMS send and delivery reports, how to add a provider for either channel, and how the credit module supplies the USSD loan/rate ports. |
| **[M-Pesa](./mpesa/README.md)** | Work with the Daraja integration: STK push repayment, C2B paybill collection, Hakikisha, Account Balance, the confirm-before-credit staging model, and what's package-capability-only vs. actually wired. |
| **[Compliance](./compliance/README.md)** | Understand KYB/AML on the vault's institutional deposit path: Elliptic wallet screening, the on-chain allowlist, the exit grace period and freeze, the lifecycle service, the on-chain writer and rescreen tickers, the vault watcher, and the admin queue. |
| **[Airtel Money](./airtel/README.md)** | Work with the Airtel Money cash-in rail: USSD push repayment, the enquiry-driven poller, the summary reconciler, callback security, and what is gated until Airtel approves the developer account. |
| **[Observability](./observability/README.md)** | Raise an ops alert from code, understand the page and digest tiers, change the OpenObserve alert trees, test the pipeline with `make alert-test`, reach OpenObserve on testnet, and run the operator commands. |
