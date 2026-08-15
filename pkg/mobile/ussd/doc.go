// Package ussd is the interactive USSD application: the menu-driven flow a
// mobile subscriber walks through to register, request a loan, pick a payout
// method, repay, and manage their PIN.
//
// The package is layered. USSDService is the outermost entry point — it holds
// the registered USSDProvider transports (one per telecom gateway) and
// dispatches an incoming request to USSDHandler. The handler owns the
// application logic: it resolves the Session, walks the MenuRegistry, and
// routes each input to the MenuHandler registered for the current screen.
//
// # Session and menus
//
// SessionManager persists session state in Redis with a short TTL (default 5
// minutes). A Session carries the current menu, the menu history stack, the
// chosen language, and a free-form Data bag for cross-screen state (loan
// amount, payout method, national ID, etc.). Menus are registered once at
// startup; MenuTypeContinue ("CON") keeps the session open, MenuTypeEnd
// ("END") releases it.
//
// MenuRegistry holds the menu graph. Menus are built with MenuBuilder and
// grouped into presets — StandardLoanMenuPreset wires the full registration,
// loan, repayment, PIN, and security-question flow. Each Menu carries
// language-keyed Titles and Options so the same graph renders in any
// supported language.
//
// # Localization
//
// InMemoryLocalizer stores translations keyed by language and message key,
// with fallback to the default language when a translation is missing.
//
// # Network mapping
//
// NetworkMapper resolves a telco's MCC+MNC code (e.g. "63902") to the
// corresponding mobile-money network (M-Pesa, Airtel Money, etc.) and ISO
// country. It is used during registration to pre-fill the user's network and
// country from the carrier-reported code.
//
// # Ports
//
// The handler depends on four narrow interfaces defined in this package —
// UserService, LoanService, RateService, PINService — plus a
// contracts.AccountNotifier for side-effect SMS. Each port is satisfied by an
// adapter in pkg/mobile/ussd/adapters, which translates between the USSD
// request/response shapes and the underlying user, account, loan, off-ramp,
// and Stellar services. The handler never imports those services directly.
//
// # Providers
//
// USSDProvider is the transport contract: ParseRequest turns a gateway's
// HTTP form post into a normalized USSDRequest, FormatResponse turns a
// USSDResponse back into the gateway's expected shape, and ValidateRequest
// gates entry. Concrete transports live under providers/ (today:
// providers/africastalking).
//
// For the architecture diagram, request lifecycle, and file/subpackage map,
// see README.md.
package ussd
