# Notifications

How a loan or account event becomes a message on a borrower's handset, and who
owns the words.

This sits on top of the SMS transport described in
[README.md § SMS](./README.md#sms). That section covers reaching a gateway; this
one covers deciding what to send, in which language, and validating it before
anyone's phone sees it.

Code: [`pkg/notifications`](../../pkg/notifications).

---

## The short version

1. Something happens. A loan is approved, a PIN is locked.
2. The caller invokes a port on `contracts.LoanNotifier` or
   `contracts.AccountNotifier`, passing a notification struct.
3. The notifier resolves the recipient's **language**, picks the **template**
   for that event, renders it, and hands the string to a **transport**.

Three concerns kept apart: what to say, how it reads, how it is delivered.

| Layer | Type | Swappable for |
|---|---|---|
| Transport | `Notifier` (`SMSNotifier`, `NoOpNotifier`) | A different channel, or nothing at all |
| Composition | `SMSLoanNotifier`, `SMSAccountNotifier` |, |
| Copy | `LoanTemplates`, `AccountTemplates` | The builder's own wording |

Callers depend on the `contracts` interface, so transport and wording both
change without touching them.

---

## Templates are functions, not format strings

Each template field is a renderer. A func from the notification to the message
text:

```go
type LoanMessage func(n contracts.LoanNotification) string

Approved: func(n contracts.LoanNotification) string {
    return fmt.Sprintf("Your loan of %s %.2f is approved.", n.DisplayCurrency, n.DisplayAmount)
},
```

**The compiler checks the fields a message reads and the verbs it formats them
with.** A positional `Sprintf` template cannot do that: miscount the verbs and
you ship `%!s(MISSING)` to a borrower, with nothing catching it before send.
`text/template` was considered and rejected. It resolves field names by
reflection at execute time, moving the failure from send-time to
first-render-time without ever reaching the compiler.

Loan events cover approval, disbursement, off-ramp failure, the repayment
lifecycle (initiated, reference, more-info, window-expiring, expired, received,
overdue) and cash pickup. Account events cover registration, PIN attempts and
lockout.

---

## Who owns the copy

**The platform ships brand-free defaults.** They name no company, no support URL
and no USSD code, in English, Swahili and French. A second builder importing
this platform must not inherit another company's brand.

**The builder overrides what it cares about.** Pass `WithLoanTemplates` /
`WithAccountTemplates` for one language, or `WithLoanTemplateSet` /
`WithAccountTemplateSet` for all of them:

```go
loanNotifier, err := notifications.NewSMSLoanNotifier(
    notifier,
    notifications.WithLoanTemplateSet(creditnotifications.LoanOverrides(cfg.Mobile.USSDDialString)),
    notifications.WithLoanLanguageResolver(langResolver),
)
```

**A nil field keeps the default.** Overriding one message does not mean
rewriting all of them. The merge is per field, so the credit module replaces
only the four loan messages that name a brand or a dial string and inherits the
rest.

### Values that vary per environment

The service code a borrower dials is not brand-scoped, it is *deployment*-scoped
It differs between the Africa's Talking sandbox and deployed testnet, so it
cannot live in compiled copy.

Func fields make the answer trivial: the override constructor takes the value
and captures it in a closure. `USSD_DIAL_STRING` reaches the templates that way
rather than through a config type threaded into this package.

---

## Validation at construction, not at send

**The constructors return an error.** Before returning a notifier they render
every merged template against `SentinelLoanNotification` or
`SentinelAccountNotification` and reject any that is:

- unset after the merge,
- renders empty, or
- contains a rune outside GSM 03.38.

Broken copy fails at process start rather than on a recipient's handset.

The sentinels are deliberately **worst case**: every field carries a long,
realistic value, so validation measures the longest message a template can
produce rather than the shortest.

### Why one rune matters

| Content | Septets per segment |
|---|---|
| GSM 03.38 only | 160 (single) / 153 (concatenated) |
| One rune outside it | 70. The whole message becomes UCS-2 |

A single curly quote or en dash therefore more than doubles the cost of every
message using that template. Extension-table runes (`^{}\[~]|€`) are escaped and
cost two septets each.

`GSM7Len` and `Segments` are exported so builders can run the same check over
their own templates in their own tests.

---

## Language resolution

`LanguageResolver` maps a user to a language code; the notifier picks the
template set for it and falls back to the default set when there is none. It is
supplied as an option at construction rather than through a setter, so no
half-built notifier exists.

That ordering matters when wiring: the resolver usually needs a user service, so
notifier construction has to come after it in `main.go`.

---

## Failure behaviour

Notification sends are **best effort** everywhere they are used. Nothing
downstream reads the result, and no money movement depends on one landing.

Two consequences worth knowing, both owned by the callers rather than this
package:

- **Sends run off the critical path.** A slow gateway between a vault borrow and
  an off-ramp initiate could stale the FX rate quoted before approval, so the
  loan adapter dispatches notifications asynchronously on a detached context.
- **Some markers are written before the send.** Where a message must go out
  exactly once over a multi-day window, such as the repayment pay-instructions
  SMS, the marker is stamped first, so a failing provider is not retried on every
  poll. The cost is that a failure there is terminal until an operator clears
  the column, which is why those paths alert rather than log. See
  [on-ramp/moneygram.md](../onramp/moneygram.md#telling-the-borrower-how-to-pay).

---

## Related

- [Mobile overview](./README.md): the USSD and SMS transports underneath.
- [On-ramp / MoneyGram cash-in](../onramp/moneygram.md), the rail with the
  strictest delivery requirement.
