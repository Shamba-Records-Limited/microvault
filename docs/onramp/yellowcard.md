# YellowCard On-Ramp (package only)

How the `yellowcard` package collects local currency from a mobile money payer
and settles it as USDC on Stellar, and why the platform does not use it yet.

Code: [`pkg/payment/yellowcard/onramp.go`](../../pkg/payment/yellowcard/onramp.go)
(`StellarReceive`, `SubmitStellarReceive`), over the receive endpoints in
[`collections.go`](../../pkg/payment/yellowcard/collections.go).

---

## What it does

One receive request does both legs. YellowCard prompts the payer's mobile
money wallet, converts what arrives, and sends USDC to a Stellar account with a
memo. YellowCard calls this a direct-settlement receive ("Convert Currencies").

```go
col, err := yc.SubmitStellarReceive(ctx, yellowcard.StellarReceive{
    SequenceID:  ref,            // idempotency key, echoed on webhooks
    CustomerUID: userID,
    ChannelID:   momoChannelID,  // GetChannels, deposit ramp, momo
    NetworkID:   mpesaNetworkID, // GetNetworks
    PayerPhone:  "+254711111111",
    Payer:       payer,          // retail KYC, see below
    LocalAmount: 2500,
    Currency:    yellowcard.CurrencyKES,
    Country:     yellowcard.CountryKenya,
    Destination: treasuryAddress,
    Memo:        "2802699348",   // Stellar memo id
})
```

The request is sent with `forceAccept`, `channelType: momo`,
`directSettlement: true` and `settlementInfo { walletAddress, walletTag,
cryptoCurrency: USDC, cryptoNetwork: XLM }`. Kenya supports receives over
M-PESA and Airtel.

The receive moves through `processing` → `settlement_pending` →
`settlement_processing` → `settlement_complete` (`IsReceiveSettled`), with v2
webhooks `RECEIVE.*` and `CONVERT.*`.

## Validation before the call

`StellarReceive.Request` refuses, without calling YellowCard:

| Problem | Code |
|---|---|
| A retail payer field is empty (`RetailPayerFields`; `MissingRetailFields` lists which) | `missing_payer_kyc` |
| Destination is not a `G…` account | `invalid_address` |
| Memo is not a numeric memo id | `invalid_memo` |
| Amount not positive | `invalid_amount` |
| No payer phone | `missing_phone_number` |
| Sequence id, customer uid, channel, network, currency or country missing | `missing_dependency` |

## Limitations

- **Retail payer KYC needs an email.** YellowCard requires name, country,
  phone, email, address, date of birth, ID type and ID number on a retail
  payer. The USSD channel collects everything except the email. Tier 0 KYC
  needs only name, country and email, but still an email, and caps at USD 200
  lifetime per `customerUID`.
- **Not wired to any channel.** No repayment rail, worker or webhook routing
  uses it. A channel that collects payer email (web, app, agent-assisted
  onboarding) can drive it as a repayment rail. Until then it is callable from
  the package only.
- **Webhooks for receives are not routed.** The YellowCard webhook handler
  treats every event as a payout. A consumer must branch on `RECEIVE.*` and
  `CONVERT.*` before relying on webhooks.
- **Settlement proof.** The receive reports `settlementInfo.cryptoAmount`; the
  on-chain transaction should still be verified on the treasury before
  crediting anything.
- **Redirect channels.** South Africa and Côte d'Ivoire collect through a
  hosted page and need `redirectUrl`; `StellarReceive` targets mobile money
  prompts only.

## Related

- [On-ramp overview](./README.md)
- [YellowCard off-ramp](../offramp/yellowcard.md): the same client's payout
  direction, webhook vocabulary and signing.
