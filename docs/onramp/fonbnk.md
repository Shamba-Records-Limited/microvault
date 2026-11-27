# Fonbnk On-Ramp (package only)

How the `fonbnk` package buys USDC on Stellar with mobile money collected by
STK push, and why the platform does not use it yet.

Code: [`pkg/payment/fonbnk/onramp.go`](../../pkg/payment/fonbnk/onramp.go)
(`StellarOnRamp`, `QuoteStellarOnRamp`, `CreateStellarOnRamp`) and
[`kyc.go`](../../pkg/payment/fonbnk/kyc.go) (`GetUserKYCState`,
`SubmitUserKYC`), over the v2 server-to-server API the off-ramp already uses
([fonbnk.md](../offramp/fonbnk.md)).

---

## What it does

One order collects KES from the payer's M-PESA and pays USDC to a Stellar
account with a memo.

```go
r := fonbnk.StellarOnRamp{
    UserEmail:      email,          // required on every order
    UserIP:         clientIP,       // required on every order
    CountryIsoCode: "KE",
    Currency:       "KES",
    CarrierCode:    "ke_safaricom",
    PayerPhone:     "254711111111",
    USDCAmount:     20,             // payout amount; the deposit is derived
    Destination:    treasuryAddress,
    Memo:           "2802699348",   // Stellar memo id
    OrderParams:    ref,            // echoed back, searchable
}
state, _ := fb.GetUserKYCState(ctx, fonbnk.KYCStateQuery{UserEmail: email, CountryIsoCode: "KE", Order: amounts})
// state.Satisfied() must be true before the order, see below
quote, _ := fb.QuoteStellarOnRamp(ctx, r)
order, _ := fb.CreateStellarOnRamp(ctx, quote, r)
order, _ = fb.RetrySTKPush(ctx, order.ID) // sends the prompt when the instructions allow
order, _ = fb.ConfirmOrder(ctx, order.ID, nil)
```

The quote pins `transferType: stk_push` on a `mobile_money` deposit; Kenyan
mobile money runs as STK push today. The order carries `phoneNumber`,
`blockchainWalletAddress` and `blockchainMemo`. Settlement is
`payout_successful` (`IsOnRampSettled`), reported by the `order-status-change`
webhook; `GetOrderByParams` reads it back by our reference.

## Validation before the call

`StellarOnRamp.Validate` refuses, without calling Fonbnk:

| Problem | Code |
|---|---|
| No user email | `missing_payer_email` |
| No user IP, or not a valid address | `missing_payer_ip` |
| No payer phone | `missing_phone_number` |
| Country, currency or carrier missing | `missing_dependency` |
| Amount not positive | `invalid_amount` |
| Destination is not a `G…` account | `invalid_address` |
| Memo is not a numeric memo id | `invalid_memo` |

## Limitations

- **Every order needs the end user's email and IP address.** Fonbnk resolves
  the user from `userEmail`, and `userIp` must be a valid address. The USSD
  channel holds neither.
- **On-ramp KYC always applies.** Off-ramp KYC is a per-country switch; on-ramp
  KYC is not. Above the country's thresholds the user must pass basic or
  advanced KYC (`GetUserKYCState`, then `SubmitUserKYC` or the hosted
  `kycUrls` link, which signs the user in with a code sent to their email).
  Three submissions on file is a lifetime cap only Fonbnk support can lift.
- **The create-users permission.** Order creation, confirm, cancel,
  intermediate actions and both KYC endpoints answer
  `403 This feature is not available for this merchant` until Fonbnk support
  enables it (`CodeMerchantNotPermitted`). Quotes and discovery are not gated,
  so the wall appears only at the first order.
- **`GetUserKYCState` writes.** An unseen email is registered as a Fonbnk user
  by that read.
- **Not wired to any channel.** No repayment rail, worker or webhook routing
  uses it. A channel that collects the payer's email and runs in a browser or
  app (so a real client IP exists) can drive it. Until then it is callable from
  the package only.
- **Settlement proof.** The order reports the payout leg's transaction hash in
  `payout.transaction.meta`; the treasury payment should still be verified
  on-chain before crediting anything.

## Related

- [On-ramp overview](./README.md)
- [Fonbnk off-ramp](../offramp/fonbnk.md): the same client's payout direction,
  signing and webhook statuses.
