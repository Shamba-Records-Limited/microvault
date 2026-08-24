# Microvault Documentation

Developer documentation for Microvault, grouped by area.

| Area | Read this when you want to… |
|---|---|
| **[Soroban Contracts](./soroban/README.md)** | Understand the on-chain Vault and governance contracts: deposit/withdraw, borrow/repay, views, events, error codes, and admin workflows. |
| **[Stellar Go Client](./stellar/client.md)** | Call Stellar from Go: the sponsorship model, child-account lifecycle, moving USDC, the Vault client, transaction confirmation, errors, and configuration. |
| **[Off-Ramp](./offramp/README.md)** | Turn a USDC loan into real money through a payment provider: YellowCard mobile money (settlement modes, the direct→fiat pivot, the webhook state machine) and MoneyGram cash pickup (the SEP-24 interactive flow, poller, treasury send, and refunds). |
| **[Mobile](./mobile/README.md)** | Wire telecom gateways into the platform: USSD menu flows, SMS send and delivery reports, how to add a provider for either channel, and how the credit module supplies the USSD loan/rate ports. |
