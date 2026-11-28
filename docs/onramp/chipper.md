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
collects. The paybill line stays on Daraja, and disappears when Daraja is not
configured; Chipper can be a deployment's only M-Pesa path.

## The flow

1. **Prompt.** The payoff is locked on the loan and a Chipper order is opened:
   `from` is `ke_mpesa` with the borrower's number, `to` is `usdc_stellar` at
   the treasury with the payoff as `to.amount` (rounded up to six decimals) and
   the borrower's child-account memo as `to.tag`. Chipper prompts the borrower
   and adds its fee to what they pay, so the treasury receives the full payoff.
   The USSD confirmation shows that charge (the order's `instructions.amount`),
   so the screen and the M-Pesa prompt agree.
   The `externalReference` is `rp-<loan reference>-<unix time>-<8 random hex>`,
   unique per attempt so a retry within the same second is never replayed as
   the earlier order, and is written to the loan before the order is created, so an
   order can never take the borrower's money untracked. A refusal closes the
   attempt as `failed`; a timeout leaves it open, and if the reference later
   finds no order it is closed then.
2. **Wait.** `order.*` webhooks drive the loan at once; a poller (every 30 s)
   is the backstop. Both read the order back from Chipper; the webhook body is
   never trusted. An order still `created` or `awaiting_funds` after 15 minutes is expired
   (mobile money orders carry no expiry of their own).
3. **Receive.** On `completed`, the USDC that arrived is established from
   the ledger whenever the treasury payment can be found:
   - by the Stellar hash on the order's payout, when Chipper reports one (the
     sandbox does not);
   - otherwise by the loan's memo: the USDC contract's transfer events into
     the treasury since the attempt was locked are read through Stellar RPC,
     and the earliest transaction carrying the memo and paying at least the
     payoff less the 2% tolerance is taken. A memo is no secret, so the floor
     keeps dust sent under a borrower's memo from standing in for the real
     payment.

   Either way the amount is what the transaction's transfer events credited
   the treasury, so a path payment counts as well as a plain one. The hash is stored with
   a unique index, so one payment can never settle two loans. If nothing turns
   up within 5 minutes of the order completing, the amount Chipper reports is
   used instead, with a `Chipper repayment settled without on-chain proof`
   alert. RPC keeps events for about a week, so the search runs while the
   order settles.
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

## After settlement

A settled repayment can still be challenged on its M-Pesa leg. Chipper can
open a dispute on the collection, holding the amount on our balance, or
reverse the collection; it has no `collection.reversed` event.

- The order's `collectionId` is stored on the loan when it settles. A
  `dispute.*` event names only the collection, so that is how it finds its
  loan. A dispute that matches no loan is still raised, since the money is
  held either way.
- Every six hours the collection of each Chipper repayment from the last 30
  days is read back. A change to `reversed` or `disputed` is raised once; the
  last status seen is kept on the loan.
- The loan stays repaid: the vault was repaid when the money arrived, so any
  loss is the treasury's. Ops answer disputes in the Chipper dashboard.

## Webhooks

The order's own legs also fire `payout.*` and `collection.*`. A payout to a
crypto destination is left to the order events rather than applied as a loan
disbursement, and a mobile money collection is ignored by the deposit-release
path.

## Limitations

- **No refund call.** Chipper has no refund: a short or disputed repayment is
  returned with a normal payout, by hand.
- **`reversed` after `completed`** is not handled automatically; by then the
  vault is repaid.
- **One replica.** A loan is driven by one caller at a time within a
  process; running several credit replicas needs a database lock instead.
- **Kenya only.** Only `ke_mpesa` is mapped.
