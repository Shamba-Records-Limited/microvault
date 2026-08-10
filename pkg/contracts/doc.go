// Package contracts holds the interface seams between the platform's modules,
// together with the data types those interfaces exchange. It is a dependency-free
// boundary layer: producers and consumers both import contracts and depend on its
// abstractions rather than on each other, which keeps implementations swappable
// and avoids import cycles.
//
// # The interfaces
//
// Lender (lending.go) is the loan port — eligibility assessment, origination, and
// retrieval. The package that originates loans implements it; handlers and jobs
// depend only on the interface.
//
// LoanNotifier (lending.go) and AccountNotifier (pin.go) are the notification
// ports. They are implemented by the notification layer, which may deliver over
// SMS, push, or any other transport, and are consumed by the services that need
// to tell a user about loan lifecycle events (approved, disbursed, off-ramp
// failed, cash-pickup ready, repayment received) or account and PIN events
// (registration, wrong-PIN attempts, lockout, PIN change and reset).
//
// # The data types
//
// EligibilityRequest and EligibilityResult frame an eligibility check;
// CreateLoanRequest and LoanRecord cover origination. A LoanRecord tracks both
// sides of a loan: the on-chain vault disbursement and the off-ramp settlement,
// including the settlement method, disbursement status, and provider reference.
// LoanNotification and AccountNotification carry the fields each notification
// method needs — not every field applies to every method. Monetary amounts are
// in stroops (USDC times ten million); display amounts and currencies travel
// alongside them on the notification types. Sentinel errors live in
// lending_errors.go.
package contracts
