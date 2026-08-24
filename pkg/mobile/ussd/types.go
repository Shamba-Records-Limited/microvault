package ussd

import (
	"context"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/pin"
	"github.com/redis/go-redis/v9"
)

//
// # Session Types
//

// Session represents a single interactive USSD session with a mobile user.
type Session struct {
	SessionID     string         `json:"session_id"`
	PhoneNumber   string         `json:"phone_number"`
	ServiceCode   string         `json:"service_code"`
	NetworkCode   string         `json:"network_code"`
	UserID        string         `json:"user_id,omitempty"`
	CurrentMenu   string         `json:"current_menu"`
	PreviousMenus []string       `json:"previous_menus"`
	Language      string         `json:"language"`
	Data          map[string]any `json:"data"`
	CreatedAt     time.Time      `json:"created_at"`
	UpdatedAt     time.Time      `json:"updated_at"`
}

// SessionManager handles the storage, retrieval, and lifecycle of USSD sessions.
type SessionManager struct {
	cache           *redis.Client
	sessionDuration time.Duration
}

//
// # Menu Types
//

// Menu is a single screen: a localized title and the options rendered beneath
// it. The registry is a rendering store, not a router — USSDHandler routes on
// the session's current menu with an explicit switch.
type Menu struct {
	ID         string
	Title      map[string]string // Language to Title
	Options    []MenuOption
	ParentMenu string
}

// MenuOption represents a user-selectable choice within a Menu.
type MenuOption struct {
	Key        string
	Label      map[string]string // Language to Label
	TargetMenu string
}

// MenuRegistry stores and provides access to all configured menus in the USSD application.
type MenuRegistry struct {
	menus map[string]*Menu
}

// MenuBuilder provides a fluent interface for programmatically constructing Menu instances.
type MenuBuilder struct {
	menu *Menu
}

//
// # Handler Types
//

// USSDHandler orchestrates the processing of incoming USSD requests, managing sessions and routing to appropriate menus.
type USSDHandler struct {
	sessionManager  *SessionManager
	menuRegistry    *MenuRegistry
	userService     UserService
	loanService     LoanService
	rateService     RateService
	pinService      PINService
	repayPaybill    string
	accountNotifier contracts.AccountNotifier
	loanNotifier    contracts.LoanNotifier
}

//
// # Provider Types
//

// USSDRequest encapsulates the normalized data received from a USSD gateway provider.
type USSDRequest struct {
	SessionID    string
	PhoneNumber  string
	Input        string
	ServiceCode  string
	NetworkCode  string
	ProviderData map[string]string // For provider-specific data
}

// USSDResponse contains the formatted data to be returned to the USSD gateway provider.
type USSDResponse struct {
	Type    string // "CON" for continue, "END" for terminate
	Message string
}

// USSDProvider defines the contract for integrating with different telecommunication USSD gateways.
type USSDProvider interface {
	// ParseRequest parses the provider-specific HTTP request into a standardized USSDRequest.
	ParseRequest(ctx context.Context, data map[string]string) (*USSDRequest, error)

	// FormatResponse formats the USSDResponse into the provider-specific response format.
	FormatResponse(ctx context.Context, response *USSDResponse) (any, error)

	// GetProviderName returns the name of the USSD provider.
	GetProviderName() string

	// ValidateRequest validates the incoming request from the provider.
	ValidateRequest(ctx context.Context, data map[string]string) error
}

// USSDService manages multiple USSD providers and routes incoming requests to the appropriate handler.
type USSDService struct {
	providers map[string]USSDProvider
	handler   *USSDHandler
}

//
// # Service Interface Types
//

// UserService defines the contract for user identity and account management operations required by the USSD flow.
type UserService interface {
	// GetUserWithAccounts retrieves a user and their associated accounts.
	GetUserWithAccounts(ctx context.Context, userIDOrPhone string) (any, []any, error)
	// RegisterUser creates a new user account based on the provided registration request.
	RegisterUser(ctx context.Context, req *RegisterUserRequest) (any, []any, error)
	// NationalIDExists reports whether a user is already registered with the
	// given national ID, so registration can reject a duplicate up front rather
	// than at the final atomic insert (the DB unique constraint is the real
	// guard; this is the fast, friendly path).
	NationalIDExists(ctx context.Context, nationalID string) (bool, error)

	// UpdateBio sets the optional SEP-9 bio fields on an existing user (added
	// post-registration via the account menu for faster cash pickup). Empty
	// fields are left unchanged; birthDate is YYYY-MM-DD.
	UpdateBio(ctx context.Context, userID string, bio BioUpdate) error

	// GetUserIDByNationalID resolves the account behind a national ID. Used by
	// new-SIM recovery to find the existing account when registration hits the
	// duplicate-ID case. Returns "" when no user holds that ID.
	GetUserIDByNationalID(ctx context.Context, nationalID string) (string, error)

	// RebindMobileNumber moves an account to the dialing SIM. Only called after
	// the caller has verified ownership via security questions — it changes the
	// account's identity anchor.
	RebindMobileNumber(ctx context.Context, userID, mobileNumber string) error
}

// BioUpdate carries the optional SEP-9 fields set from the account menu.
type BioUpdate struct {
	BirthDate  string // YYYY-MM-DD
	Address    string
	City       string
	PostalCode string
}

// RateService provides exchange rate lookups for local currency conversion.
type RateService interface {
	// GetExchangeRate returns the current sell rate for the specified currency.
	GetExchangeRate(ctx context.Context, currency string) (sellRate float64, err error)
}

// LoanService defines the interface for loan-related operations.
// Implementations live in the lending module; the USSD handler depends only on
// this consumer-defined interface.
type LoanService interface {
	// GetUserLoans returns all loans for the given user, formatted for USSD display.
	GetUserLoans(ctx context.Context, userID string) ([]any, error)

	// RequestLoan orchestrates the full loan disbursement cycle.
	RequestLoan(ctx context.Context, req *LoanRequest) (any, error)

	// CheckLoanEligibility checks whether the user qualifies for the requested amount.
	CheckLoanEligibility(ctx context.Context, userID string, amount int64, duration int) (*LoanApproval, error)

	// GetProductConfig returns the active loan product parameters used by the
	// USSD flow (limits, duration, schedule). Returns nil when no product is configured.
	GetProductConfig() *LoanProductConfig

	// GetRepaymentQuote returns the amount currently owed on a loan, in both
	// USDC stroops and local currency cents. The USDC figure is derived from
	// the vault's current borrow_index relative to the index at origination;
	// the local figure applies the latest FX. Returns an error (hard-fail —
	// no stale fallback) when the vault or FX is unavailable.
	GetRepaymentQuote(ctx context.Context, loanID string) (*RepaymentQuote, error)

	// InitiateRepayment locks the payoff in USDC and opens a MoneyGram cash
	// deposit against it. The interactive URL is shortened and delivered by
	// SMS rather than returned here: a USSD screen cannot carry a URL a
	// feature-phone user could act on, and the borrower needs it after the
	// session has ended.
	//
	// The locked figure is what settles the loan however long the borrower
	// takes to reach an agent, so this is the point the quote stops moving.
	InitiateRepayment(ctx context.Context, loanID, phoneNumber string) (*RepaymentInitiation, error)
}

// RepaymentInitiation is what the borrower needs to be told after a cash
// deposit has been opened. The amount is the locked payoff, in USDC, because
// MoneyGram converts the cash at its own counter rate — a local-currency
// figure quoted here is an estimate the agent may contradict.
type RepaymentInitiation struct {
	LoanID            string
	AmountUSDCStroops int64
	ExpiresAt         time.Time
}

// MinMoneyGramDepositStroops is MoneyGram's production on-ramp floor, 15 USDC
// at seven decimals.
//
// It is a hard limit on their side, so a payoff below it cannot use the cash
// rail at all and the repay menu offers mobile money alone. A KES 1,000 loan
// is roughly 7 USDC, which is well inside the excluded range.
const MinMoneyGramDepositStroops int64 = 150_000_000

// RepaymentQuote is the live amount owed on a loan, computed from the vault
// borrow_index + current FX. Stored repayment quotes are advisory only — this
// is the figure to show on USSD screens, in SMS reminders, and to validate
// borrower payments against (with ±2 % tolerance).
type RepaymentQuote struct {
	LoanID             string
	AmountUSDCStroops  int64   // What the borrower owes in USDC, stroops.
	AmountLocalCents   int64   // Same, converted at the FX rate below.
	LocalCurrency      string  // ISO 4217 (e.g. "KES").
	BorrowIndexAtQuote int64   // WAD scale (1e18).
	FXRate             float64 // local-per-USD at quote time.
	QuoteSource        string  // "mg_primary" | "yc_fallback" | "stale_cache".
	AsOf               time.Time
}

// LoanProductConfig provides loan product parameters for the USSD flow.
// It is loaded once at startup from the highest-priority active loan product.
type LoanProductConfig struct {
	ProductID         string // UUID of the loan product record.
	MinAmountCents    int64  // Minimum loan in fiat cents (e.g. 50000 = KES 500).
	MaxAmountCents    int64  // Maximum auto-approved loan in fiat cents (e.g. 300000 = KES 3,000).
	Currency          string // ISO 4217 currency code (e.g. "KES").
	DurationDays      int    // Fixed loan term in days (e.g. 30).
	RepaymentSchedule string // Repayment cadence (e.g. "lump_sum").
	InterestRateBps   int32  // Fallback annual rate in basis points; vault APR takes precedence.
	OriginationFeeBps int32  // One-off fee in bps (1 bps = 0.01 %). Zero when product has none.
}

// PINService defines the interface for PIN management operations used by the
// USSD handler. It is satisfied by [pin.Service].
type PINService interface {
	// SetPIN creates a new PIN for a user (during registration).
	SetPIN(ctx context.Context, userID, pin string) error

	// VerifyPIN checks the supplied PIN against the stored hash.
	// Returns (true, nil) on success, (false, nil) on wrong PIN.
	VerifyPIN(ctx context.Context, userID, pin string) (bool, error)

	// ChangePIN verifies the old PIN and sets a new one.
	ChangePIN(ctx context.Context, userID, oldPin, newPin string) error

	// ResetPIN sets a new PIN after identity verification (security questions).
	ResetPIN(ctx context.Context, userID, newPin string) error

	// IsLocked reports whether the account is locked and when the lockout expires.
	IsLocked(ctx context.Context, userID string) (bool, time.Time, error)

	// HasPIN reports whether the user has a PIN set.
	HasPIN(ctx context.Context, userID string) (bool, error)

	// SetSecurityQuestions stores hashed answers for the given questions.
	SetSecurityQuestions(ctx context.Context, userID string, questions []pin.QuestionAnswer) error

	// VerifySecurityAnswers checks the supplied answers against stored hashes.
	VerifySecurityAnswers(ctx context.Context, userID string, answers []pin.QuestionAnswer) (bool, error)

	// GetUserQuestionIDs returns the predefined question IDs the user has configured.
	GetUserQuestionIDs(ctx context.Context, userID string) ([]int, error)

	// GetRemainingAttempts returns the number of PIN attempts left before lockout.
	GetRemainingAttempts(ctx context.Context, userID string) (int, error)
}

// RegisterUserRequest contains the necessary information to register a new user via USSD.
type RegisterUserRequest struct {
	MobileNumber      string
	MobileCountryCode string
	NetworkCode       string
	FullName          string
	NationalID        string
	PreferredLanguage string

	// PinHash is the pre-hashed PIN (from pin.HashPIN), written atomically with
	// the user so no PIN-less account is ever persisted. Empty only when the
	// deployment has no PIN service configured.
	PinHash  string
	PinSetAt *time.Time

	// Optional SEP-9 bio (MoneyGram cash-pickup prefill). Empty when the user
	// skips the bio step. BirthDate is YYYY-MM-DD.
	BirthDate  string
	Address    string
	City       string
	PostalCode string
}

// LoanRequest represents a loan request from the USSD flow.
type LoanRequest struct {
	UserID          string
	AccountID       string // Database UUID of the user's account record.
	StellarAddress  string // Stellar public key for vault borrow recipient.
	ProductID       string // Loan product ID from LoanProductConfig.
	PhoneNumber     string
	RecipientName   string
	NationalID      string
	CountryCode     string
	NetworkCode     string
	NetworkName     string
	PrincipalAmount int64 // USDC amount in stroops (converted from local currency).
	PrincipalAsset  string
	DurationDays    int
	RepaymentSched  string
	LocalAmount     int64   // Local currency amount in cents (e.g. KES cents).
	LocalCurrency   string  // ISO 4217 currency code (e.g. "KES").
	ConversionRate  float64 // YellowCard buy rate at quote time (e.g. 153.50).

	// Off-ramp routing — selects the provider (mobile money vs cash pickup).
	// Empty defaults to mobile money for back-compat with menus that don't
	// expose the cash-pickup branch yet.
	PayoutMethod string

	// Cash-pickup-only KYC fields (MoneyGram). Ignored by mobile-money flows.
	FirstName          string
	LastName           string
	BirthDate          string // ISO-8601 (YYYY-MM-DD)
	Address            string
	PostalCode         string
	City               string
	AddressCountryCode string
	ChildAccountIndex  uint32 // per-user Stellar derivation index for SEP-10 memo
}

// LoanApproval contains the result of a loan eligibility check, including approval status and terms.
type LoanApproval struct {
	Approved     bool
	Reason       string
	InterestRate float64
}

//
// # Menu Preset Types
//

// MenuPreset defines a reusable collection of menus for a specific workflow or use case.
type MenuPreset interface {
	// Initialize loads menus into the registry.
	Initialize(registry *MenuRegistry)

	// GetName returns the preset name.
	GetName() string
}

// StandardLoanMenuPreset provides the standard set of menus for the default loan application flow.
type StandardLoanMenuPreset struct{}

// SimplifiedMenuPreset provides a streamlined set of menus with fewer steps.
type SimplifiedMenuPreset struct{}

// CustomMenuPreset allows for the definition of arbitrary menu flows.
type CustomMenuPreset struct {
	name  string
	menus []*Menu
}

//
// # Localizer Types
//

// Localizer defines the contract for retrieving translated strings based on language preferences.
type Localizer interface {
	// Get retrieves a localized message.
	Get(language, key string) string

	// GetWithDefault retrieves a localized message with a default fallback.
	GetWithDefault(language, key, defaultMsg string) string

	// HasKey checks if a key exists.
	HasKey(key string) bool

	// AddTranslation adds a translation.
	AddTranslation(key, language, message string)
}

// InMemoryLocalizer implements Localizer using an in-memory map for fast translation lookups.
type InMemoryLocalizer struct {
	translations map[string]map[string]string // key -> language -> message
	defaultLang  string
}

// TranslationBuilder provides a fluent interface for populating an InMemoryLocalizer.
type TranslationBuilder struct {
	localizer *InMemoryLocalizer
}
