// Package mgpoller drives both directions of the MoneyGram SEP-24 rail:
// withdrawal (cash out at an agent) and deposit (cash in, settling a loan).
//
// MoneyGram publishes no webhooks, so status only moves when we ask. Both
// directions share Runner: wake on a cadence, fetch a batch, drive each record,
// never let one record's failure abort the batch.
//
// # Why the two directions are not mirror images
//
// A withdrawal is something we started and MoneyGram must finish. It resolves
// in minutes and is polled hard on a fixed ticker until it settles.
//
// A deposit is something the borrower must finish, and nothing on our side
// compels them to. The window runs to a day or more, most of it spent doing
// nothing. Cadence therefore lives in a repayment_next_poll_at column rather
// than in the ticker: in-memory timers are lost on restart, and a 30-second
// tick for three days is tens of thousands of pointless requests. The runner
// interval decides how often we ask which rows are due; the column decides
// which come back.
//
// # The deadline is MoneyGram's
//
// A deposit lapses on the transaction's user_action_required_by whatever
// REPAYMENT_WINDOW says. It has been observed at roughly 24 hours against a
// default of 96, so acting on ours would keep a dead deposit live for three
// days and tell the borrower they still had time. The reminder lead is capped
// at a fraction of that real window, measured from started_at — measuring from
// the time remaining would make the threshold chase itself and only ever be met
// at expiry.
//
// # Ordering on the deposit side
//
// The borrower is told the moment funds reach the treasury, before the vault
// leg is attempted: from their side the repayment is done, and the
// treasury-to-vault transfer is our leg. A failure there never reaches them,
// the row stays at funds_received, and the retry never stops — their USDC is
// already on the treasury, so there is no state in which abandoning it is
// correct. The attempt ceiling decides when a human is told, not when to give
// up.
//
// # Telling a borrower how to pay
//
// Two artifacts can carry the instructions and MoneyGram issues them at
// different points. external_transaction_id is the reference quoted at the
// counter, but SEP-24 defines it as the external transaction that "started the
// deposit" — on a cash-in it does not exist until the borrower has already
// paid. more_info_url is the field the spec designates for telling a user how
// to start one, and is populated from the first poll.
//
// The reference is preferred because a code a borrower can read out beats a
// link they must open on a feature phone; the page is the fallback, because a
// borrower holding neither cannot pay at all. Either way the marker is written
// before the send, so a failing provider is not retried every tick for the rest
// of the window. The cost is that a failure there is terminal until an operator
// clears repayment_reference_sent, which is why it alerts rather than logs.
//
// # Refunds
//
// A refund is settled from what landed on-ledger, never from the anchor's own
// arithmetic: MoneyGram reports amount_refunded already net of its withdrawal
// fee and then restates that fee in amount_fee, so deriving the total from
// those fields subtracts it twice. Settlement runs in stages across ticks so
// every step is retried until it succeeds, and nothing depends on a single
// observation.
//
// Status strings for the lending module's loan model are duplicated here as
// constants rather than imported: importing the lending module would invert the
// layering.
package mgpoller
