package ussd

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// ============================================================================
// Session Types
// ============================================================================

// Session represents a USSD session
type Session struct {
	SessionID     string                 `json:"session_id"`
	PhoneNumber   string                 `json:"phone_number"`
	UserID        string                 `json:"user_id,omitempty"`
	CurrentMenu   string                 `json:"current_menu"`
	PreviousMenus []string               `json:"previous_menus"`
	Language      string                 `json:"language"`
	Data          map[string]interface{} `json:"data"`
	CreatedAt     time.Time              `json:"created_at"`
	UpdatedAt     time.Time              `json:"updated_at"`
}

// SessionManager manages USSD sessions
type SessionManager struct {
	cache           *redis.Client
	sessionDuration time.Duration
}

// ============================================================================
// Menu Types
// ============================================================================

// MenuType represents the type of menu response
type MenuType string

const (
	MenuTypeContinue MenuType = "CON" // Continue session
	MenuTypeEnd      MenuType = "END" // End session
)

// Menu represents a USSD menu
type Menu struct {
	ID           string
	Title        map[string]string // Language -> Title
	Options      []MenuOption
	Handler      MenuHandler
	ParentMenu   string
	RequiresAuth bool
}

// MenuOption represents a menu option
type MenuOption struct {
	Key        string
	Label      map[string]string // Language -> Label
	TargetMenu string
	Handler    MenuHandler
}

// MenuHandler is a function that handles menu logic
type MenuHandler func(ctx *MenuContext) (*MenuResponse, error)

// MenuContext provides context for menu handlers
type MenuContext struct {
	Session *Session
	Input   string
	Manager *SessionManager
}

// MenuResponse represents a menu response
type MenuResponse struct {
	Type    MenuType
	Message string
}

// MenuRegistry manages all USSD menus
type MenuRegistry struct {
	menus map[string]*Menu
}

// MenuBuilder helps build menus programmatically
type MenuBuilder struct {
	menu *Menu
}

// ============================================================================
// Handler Types
// ============================================================================

// USSDHandler handles USSD requests
type USSDHandler struct {
	sessionManager *SessionManager
	menuRegistry   *MenuRegistry
	userService    UserService
	loanService    LoanService
}

// ============================================================================
// Provider Types
// ============================================================================

// USSDRequest represents a USSD request from the provider
type USSDRequest struct {
	SessionID    string
	PhoneNumber  string
	Input        string
	ServiceCode  string
	NetworkCode  string
	ProviderData map[string]string // For provider-specific data
}

// USSDResponse represents a USSD response to be sent back
type USSDResponse struct {
	Type    string // "CON" for continue, "END" for terminate
	Message string
}

// USSDProvider is an interface for handling USSD requests from different providers
type USSDProvider interface {
	// ParseRequest parses the provider-specific HTTP request into a standardized USSDRequest
	ParseRequest(ctx context.Context, data map[string]string) (*USSDRequest, error)

	// FormatResponse formats the USSDResponse into the provider-specific response format
	FormatResponse(ctx context.Context, response *USSDResponse) (interface{}, error)

	// GetProviderName returns the name of the USSD provider
	GetProviderName() string

	// ValidateRequest validates the incoming request from the provider
	ValidateRequest(ctx context.Context, data map[string]string) error
}

// USSDService manages USSD providers and routes requests
type USSDService struct {
	providers map[string]USSDProvider
	handler   *USSDHandler
}

// ============================================================================
// Service Interface Types
// ============================================================================

// UserService defines the interface for user-related operations
type UserService interface {
	GetUserWithAccounts(ctx context.Context, userIDOrPhone string) (interface{}, []interface{}, error)
	RegisterUser(ctx context.Context, req *RegisterUserRequest) (interface{}, []interface{}, error)
}

// LoanService defines the interface for loan-related operations
type LoanService interface {
	GetUserLoans(ctx context.Context, userID string) ([]interface{}, error)
	RequestLoan(ctx context.Context, req *LoanRequest) (interface{}, error)
	CheckLoanEligibility(ctx context.Context, userID string, amount int64, duration int) (*LoanApproval, error)
}

// RegisterUserRequest represents a user registration request
type RegisterUserRequest struct {
	MobileNumber      string
	MobileCountryCode string
	FullName          string
	NationalID        string
	PreferredLanguage string
}

// LoanRequest represents a loan request
type LoanRequest struct {
	UserID          string
	AccountID       string
	PrincipalAmount int64
	PrincipalAsset  string
	DurationDays    int
	RepaymentSched  string
}

// LoanApproval represents a loan approval decision
type LoanApproval struct {
	Approved     bool
	Reason       string
	InterestRate float64
}

// ============================================================================
// Menu Preset Types
// ============================================================================

// MenuPreset defines a set of menus for a specific use case or provider
type MenuPreset interface {
	// Initialize loads menus into the registry
	Initialize(registry *MenuRegistry)

	// GetName returns the preset name
	GetName() string
}

// StandardLoanMenuPreset provides standard loan application menus
type StandardLoanMenuPreset struct{}

// SimplifiedMenuPreset provides a simplified menu structure
type SimplifiedMenuPreset struct{}

// CustomMenuPreset allows for completely custom menu structures
type CustomMenuPreset struct {
	name  string
	menus []*Menu
}

// ============================================================================
// Localizer Types
// ============================================================================

// Localizer provides localization services
type Localizer interface {
	// Get retrieves a localized message
	Get(language, key string) string

	// GetWithDefault retrieves a localized message with a default fallback
	GetWithDefault(language, key, defaultMsg string) string

	// HasKey checks if a key exists
	HasKey(key string) bool

	// AddTranslation adds a translation
	AddTranslation(key, language, message string)
}

// InMemoryLocalizer stores translations in memory
type InMemoryLocalizer struct {
	translations map[string]map[string]string // key -> language -> message
	defaultLang  string
}

// TranslationBuilder helps build translations
type TranslationBuilder struct {
	localizer *InMemoryLocalizer
}
