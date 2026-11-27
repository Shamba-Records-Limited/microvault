# Chipper On-Ramp (loan repayments)

How a borrower repays through Chipper: one Chipper order collects KES from
their M-Pesa and pays USDC to the treasury, and the loan settles without the
OTC desk. It is the only ramp wired as a repayment rail; YellowCard and Fonbnk
are package only ([yellowcard.md](./yellowcard.md), [fonbnk.md](./fonbnk.md)).

Code: [`pkg/payment/chipper/order.go`](../../pkg/payment/chipper/order.go)
(the order client), [`pkg/webhook/chipper.go`](../../pkg/webhook/chipper.go)
(order events), and the credit module's `ChipperRepayments` adapter, which
opens, drives and settles the order.

---

## Turning it on

`REPAYMENT_RAIL_MPESA=chipper`, with Chipper configured and an `MPESA` entry in
`CHIPPER_METHOD_CODES`. The default, `daraja`, keeps M-Pesa on Daraja. The USSD
menu does not change: the borrower still picks M-Pesa, and config decides who
collects. The paybill line stays on Daraja.

## The flow

1. **Prompt.** The payoff is locked on the loan and a Chipper order is opened:
   `from` is `ke_mpesa` with the borrower's number, `to` is `usdc_stellar` at
   the treasury with the payoff as `to.amount` (rounded up to six decimals) and
   the borrower's child-account memo as `to.tag`. Chipper prompts the borrower
   and adds its fee to what they pay, so the treasury receives the full payoff.
   The `externalReference` is `rp-<loan reference>-<unix time>`, one per
   attempt, and is written to the loan before the order is created, so an
   order can never take the borrower's money untracked. A refusal closes the
   attempt as `failed`; a timeout leaves it open, and if the reference later
   finds no order it is closed then.
2. **Wait.** `order.*` webhooks drive the loan at once; a poller (every 30 s)
   is the backstop. Both read the order back from Chipper; the webhook body is
   never trusted. An order still `created` or `awaiting_funds` after 15 minutes is expired
   (mobile money orders carry no expiry of their own).
3. **Receive.** On `completed`, the USDC that arrived is established:
   - with a Stellar hash on the order's payout, from the ledger: the treasury
     payment in that transaction, USDC from the configured issuer. The hash is
     stored with a unique index, so one payment can never settle two loans;
   - without one, from the amount Chipper reports, with a
     `Chipper repayment settled without on-chain proof` alert.
4. **Settle.** The loan moves to `funds_received`, the borrower is told, and the
   treasury repays the vault (`repay_for`) exactly as a MoneyGram cash-in does.

## Outcomes

| Order | Loan |
|---|---|
| `completed`, full amount | `funds_received`, then `settled` once the vault leg lands |
| `completed`, more than 2% short | `partial_funds_received`, vault leg withheld, ops alerted |
| `completed`, hash pays the treasury nothing | Held, ops paged |
| `failed` | Repayment `failed`; the borrower can try again |
| `expired`, or unpaid past 15 min | Repayment `expired` |

A failed vault leg retries with backoff and pages ops on the fifth failure; an
unknown outcome parks the loan until ops verify it on-chain.

## Webhooks

The order's own legs also fire `payout.*` and `collection.*`. A payout to a
crypto destination is left to the order events rather than applied as a loan
disbursement, and a mobile money collection is ignored by the deposit-release
path.

## Limitations

- **The USSD screen shows our quote, not Chipper's charge.** The confirmation
  shows the payoff at the platform rate; Chipper's prompt asks for its own rate
  plus its fee, slightly more.
- **No refund call.** Chipper has no refund: a short or disputed repayment is
  returned with a normal payout, by hand.
- **`reversed` after `completed`** is not handled automatically; by then the
  vault is repaid.
- **One replica.** A loan is driven by one caller at a time within a
  process; running several credit replicas needs a database lock instead.
- **Kenya only.** Only `ke_mpesa` is mapped.
