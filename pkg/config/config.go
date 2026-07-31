package config

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/stellar/go-stellar-sdk/clients/rpcclient"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/network"
	"github.com/stellar/go-stellar-sdk/strkey"
)

// Config holds all configuration for the application, composed of sub-configs.
type Config struct {
	Postgres  PostgresConfig
	Redis     RedisConfig
	Server    ServerConfig
	Stellar   StellarConfig
	Payments  PaymentsConfig
	Mobile    MobileConfig
	Auth      AuthConfig
	Shortener ShortenerConfig
}

// PostgresConfig holds all database-related configuration.
type PostgresConfig struct {
	Host     string
	User     string
	Password string
	DBName   string
	Port     string
	SSLMode  string
	TimeZone string
}

// DSN returns the connection string for Postgres.
func (c *PostgresConfig) DSN() string {
	return fmt.Sprintf("host=%s user=%s password=%s dbname=%s port=%s sslmode=%s TimeZone=%s",
		c.Host, c.User, c.Password, c.DBName, c.Port, c.SSLMode, c.TimeZone,
	)
}

// RedisConfig holds all Redis-related configuration.
type RedisConfig struct {
	Host           string
	Port           string
	Password       string
	DBNumber       int
	IdempotencyTTL time.Duration
}

// GetIdempotencyTTL returns the configured TTL or default of 24 hours
func (c *RedisConfig) GetIdempotencyTTL() time.Duration {
	if c.IdempotencyTTL == 0 {
		return 24 * time.Hour
	}
	return c.IdempotencyTTL
}

// Addr returns the host:port address for Redis.
func (c *RedisConfig) Addr() string {
	host := strings.TrimPrefix(c.Host, "redis://")
	return fmt.Sprintf("%s:%s", host, c.Port)
}

// ServerConfig holds all server-related configuration.
type ServerConfig struct {
	ServerEnvironment string
	ServerHost        string
	CoreServerPort    string
	CreditServerPort  string
	// PublicBaseURL is the externally-reachable origin used to build SMS
	// short-links (e.g. https://microvault.outray.app). No trailing slash.
	PublicBaseURL string
}

// ShortenerConfig configures the optional dub link shortener used for
// outbound cash-pickup SMS links. When APIKey is empty the shortener is off
// and links fall back to the self-hosted /r/{code} redirect.
type ShortenerConfig struct {
	// APIKey is the dub workspace API key (from DUB_API_KEY). Presence
	// enables the shortener.
	APIKey string
	// BaseURL is the dub API origin (from DUB_API_URL), set to point at a
	// self-hosted instance. Empty uses the SDK default, https://api.dub.co.
	BaseURL string
	// Domain is the short-link domain links are created under (from
	// DUB_DOMAIN). Optional; empty lets the workspace default apply.
	Domain string
	// PreviewTitle overrides the og:title in dub Custom Link Previews (from
	// DUB_PREVIEW_TITLE). Optional; empty derives it from the destination host.
	PreviewTitle string
	// PreviewDescription overrides the og:description (from
	// DUB_PREVIEW_DESCRIPTION). Optional; empty derives it from the
	// destination host and path.
	PreviewDescription string
	// ImagePreviewURL is the og:image shown in dub Custom Link Previews
	// (from DUB_IMAGE_PREVIEW_URL). Optional.
	ImagePreviewURL string
}

// Enabled reports whether the dub shortener should be used.
func (c *ShortenerConfig) Enabled() bool {
	return c.APIKey != ""
}

// CoreAddr returns the host:port address for the core server to listen on.
func (c *ServerConfig) CoreAddr() string {
	return fmt.Sprintf("%s:%s", c.ServerHost, c.CoreServerPort)
}

// CreditAddr returns the host:port address for the credit server to listen on.
func (c *ServerConfig) CreditAddr() string {
	return fmt.Sprintf("%s:%s", c.ServerHost, c.CreditServerPort)
}

// StellarConfig holds all stellar-related configuration.
type StellarConfig struct {
	RpcURL                  string
	AdminPublicKey          string
	AdminSecretKey          string
	TreasurySecretKey       string
	ServerSecretKey         string
	NetworkPassphrase       string
	EnableMultiSig          bool
	MultiSigLowThreshold    uint32
	MultiSigMediumThreshold uint32
	MultiSigHighThreshold   uint32
	USDCIssuer              string
	ContractID              string
}

// NewRpcClient creates a new instance of Stellar RPC Client to connect with Stellar's RPC Server
func (c *StellarConfig) NewRpcClient() *rpcclient.Client {
	// Create a HTTP client
	httpClient := &http.Client{
		Timeout: 15 * time.Second,
	}

	// Create RPC Client
	client := rpcclient.NewClient(c.RpcURL, httpClient)
	return client
}

// YellowCardConfig holds all YellowCard-related configuration
type YellowCardConfig struct {
	PublicKey     string
	SecretKey     string
	BaseURL       string
	WebhookSecret string
	BusinessID    string // Registered business ID with YellowCard
	BusinessName  string // Registered business name with YellowCard
}

// FonbnkConfig holds all Fonbnk-related configuration
type FonbnkConfig struct {
	ClientID     string
	ClientSecret string
	BaseURL      string
}

// MoneyGramConfig holds all MoneyGram-related configuration.
//
// MoneyGram acts as both a Stellar SEP-1/9/10/24 anchor (for the cash-pickup
// off-ramp flow) and a REST API provider (for FX rates). The two sets of
// credentials are independent: HomeDomain + ServerSigningKey are derived
// from MG's published TOML and used for SEP-10 auth; ClientID + ClientSecret
// are issued through the MG developer portal for OAuth 2.0 client_credentials
// against the REST API.
//
// MoneyGram is the platform's default cash-pickup anchor — there is no
// enable flag; the credentials below are always required at boot.
type MoneyGramConfig struct {
	// SEP-1 / 9 / 10 / 24 — Stellar anchor protocol
	HomeDomain        string // e.g. "stellar.moneygram.com"
	ServerSigningKey  string // pinned from TOML, validated at boot
	NetworkPassphrase string // matches StellarConfig.NetworkPassphrase
	USDCIssuer        string // matches StellarConfig.USDCIssuer
	TransferServerURL string // override, otherwise resolved from TOML

	// REST API — OAuth 2.0 client_credentials (FX rates)
	ClientID     string
	ClientSecret string
	OAuthURL     string // token endpoint
	FXRateURL    string // GET /fx-rate/v1/rates

	// Custodial wallets. AuthSecret co-signs SEP-10; FundsSecret is the SEP-24
	// account and USDC send source. Both default to TREASURY_SECRET_KEY.
	AuthSecret  string
	FundsSecret string

	// Poller cadence. Zero leaves mgpoller.DefaultConfig's value in place
	// rather than config duplicating the defaults.
	PollInterval            time.Duration // MONEYGRAM_POLL_INTERVAL (seconds)
	PollMaxBatch            int           // MONEYGRAM_POLL_MAX_BATCH
	RefundSettleMaxAttempts int           // MONEYGRAM_REFUND_MAX_ATTEMPTS
}

// PaymentsConfig bundles all payment provider configurations
type PaymentsConfig struct {
	YellowCard YellowCardConfig
	Fonbnk     FonbnkConfig
	MoneyGram  MoneyGramConfig
}

// AfricasTalkingConfig holds all SMS/USSD-related configuration for Africa's Talking
type AfricasTalkingConfig struct {
	Username  string
	APIKey    string
	BaseURL   string
	SenderID  string // from AT_SENDER_ID (alphanumeric or shortcode)
	Shortcode string // from AT_SHORTCODE (numeric shortcode)

	// HTTPTimeout bounds a single SMS send attempt. From AT_HTTP_TIMEOUT
	// (seconds); zero when unset, leaving the adapter to apply its default.
	HTTPTimeout time.Duration
}

// ResolveSenderID returns the sender ID to use for SMS.
// Priority: SenderID > Shortcode > "" (AT defaults to AFRICASTKNG).
func (c *AfricasTalkingConfig) ResolveSenderID() string {
	if c.SenderID != "" {
		return c.SenderID
	}
	if c.Shortcode != "" {
		return c.Shortcode
	}
	return ""
}

// MobileConfig holds all mobile-related configuration
type MobileConfig struct {
	AfricasTalking AfricasTalkingConfig
	// SessionTimeout is the Redis TTL for a USSD session, refreshed on each
	// request. From USSD_SESSION_TIMEOUT (seconds); defaults to 5 minutes.
	SessionTimeout time.Duration
}

type AuthConfig struct {
	JWTSecret           string
	JWTExpiration       time.Duration
	JWTRefreshWindow    time.Duration
	ChallengeExpiration time.Duration
	PINLockoutDuration  time.Duration // from PIN_LOCKOUT_SECONDS (default 900 = 15min)
}

// New loads and returns a new Config struct populated from environment variables.
func New() (*Config, error) {
	// Load all variables from environment
	serverEnvironment := os.Getenv("SERVER_ENVIRONMENT")
	serverHost := os.Getenv("SERVER_HOST")
	coreServerPort := os.Getenv("CORE_SERVER_PORT")
	creditServerPort := os.Getenv("CREDIT_SERVER_PORT")

	dbHost := os.Getenv("DB_HOST")
	dbUser := os.Getenv("DB_USER")
	dbPassword := os.Getenv("DB_PASSWORD")
	dbName := os.Getenv("DB_NAME")
	dbPort := os.Getenv("DB_PORT")
	dbSSLMode := os.Getenv("DB_SSL_MODE")
	dbTimeZone := os.Getenv("DB_TIMEZONE")

	redisHost := os.Getenv("REDIS_HOST")
	redisPort := os.Getenv("REDIS_PORT")
	redisPassword := os.Getenv("REDIS_PASSWORD")

	stellarRpcURL := os.Getenv("STELLAR_RPC_URL")

	ycPublicKey := os.Getenv("YELLOW_CARD_PUBLIC_KEY")
	ycSecretKey := os.Getenv("YELLOW_CARD_SECRET_KEY")
	ycBusinessID := os.Getenv("YELLOW_CARD_BUSINESS_ID")
	ycBusinessName := os.Getenv("YELLOW_CARD_BUSINESS_NAME")

	fonbnkClientID := os.Getenv("FONBNK_CLIENT_ID")
	fonbnkClientSecret := os.Getenv("FONBNK_CLIENT_SECRET")

	// MoneyGram is the default cash-pickup anchor — always wired.
	mgHomeDomain := os.Getenv("MONEYGRAM_HOME_DOMAIN")
	mgServerSigningKey := os.Getenv("MONEYGRAM_SERVER_SIGNING_KEY")
	mgUSDCIssuer := os.Getenv("MONEYGRAM_USDC_ISSUER")
	mgTransferServerURL := os.Getenv("MONEYGRAM_TRANSFER_SERVER_URL")
	mgClientID := os.Getenv("MONEYGRAM_CLIENT_ID")
	mgClientSecret := os.Getenv("MONEYGRAM_CLIENT_SECRET")
	mgPollInterval, err := envSeconds("MONEYGRAM_POLL_INTERVAL")
	if err != nil {
		return nil, err
	}
	mgPollMaxBatch, err := envPositiveInt("MONEYGRAM_POLL_MAX_BATCH")
	if err != nil {
		return nil, err
	}
	mgRefundMaxAttempts, err := envPositiveInt("MONEYGRAM_REFUND_MAX_ATTEMPTS")
	if err != nil {
		return nil, err
	}

	mgOAuthURL := os.Getenv("MONEYGRAM_OAUTH_URL")
	mgFXRateURL := os.Getenv("MONEYGRAM_FX_RATE_URL")
	mgAuthSecret := os.Getenv("MONEYGRAM_AUTH_SECRET")
	mgFundsSecret := os.Getenv("MONEYGRAM_FUNDS_SECRET")

	mobileUsername := os.Getenv("AT_USERNAME")
	mobileAPIKey := os.Getenv("AT_API_KEY")

	treasurySecretKey := os.Getenv("TREASURY_SECRET_KEY")
	adminSecretKey := os.Getenv("ADMIN_SECRET_KEY")
	serverSecretKey := os.Getenv("SERVER_SECRET_KEY")
	jwtSecret := os.Getenv("JWT_SECRET")
	usdcIssuer := os.Getenv("USDC_ISSUER")
	contractID := os.Getenv("CONTRACT_ID")

	// Create a map of required variables to check
	required := map[string]string{
		"SERVER_ENVIRONMENT":        serverEnvironment,
		"SERVER_HOST":               serverHost,
		"CORE_SERVER_PORT":          coreServerPort,
		"CREDIT_SERVER_PORT":        creditServerPort,
		"DB_HOST":                   dbHost,
		"DB_USER":                   dbUser,
		"DB_PASSWORD":               dbPassword,
		"DB_NAME":                   dbName,
		"DB_PORT":                   dbPort,
		"REDIS_HOST":                redisHost,
		"REDIS_PORT":                redisPort,
		"STELLAR_RPC_URL":           stellarRpcURL,
		"YELLOW_CARD_PUBLIC_KEY":    ycPublicKey,
		"YELLOW_CARD_SECRET_KEY":    ycSecretKey,
		"YELLOW_CARD_BUSINESS_ID":   ycBusinessID,
		"YELLOW_CARD_BUSINESS_NAME": ycBusinessName,
		"FONBNK_CLIENT_ID":          fonbnkClientID,
		"FONBNK_CLIENT_SECRET":      fonbnkClientSecret,
		"TREASURY_SECRET_KEY":       treasurySecretKey,
		"ADMIN_SECRET_KEY":          adminSecretKey,
		"SERVER_SECRET_KEY":         serverSecretKey,
		"JWT_SECRET":                jwtSecret,
		"USDC_ISSUER":               usdcIssuer,
		"CONTRACT_ID":               contractID,
	}

	// Loop and check for empty values
	for key, value := range required {
		if value == "" {
			return nil, fmt.Errorf("environment variable %s is required but not set", key)
		}
	}

	// Handle non-required vars with defaults
	redisDBNum, _ := strconv.Atoi(os.Getenv("REDIS_DB_NUMBER"))

	// Parse idempotency TTL in seconds, default to 24 hours (86400 seconds)
	idempotencyTTL := 24 * time.Hour
	if ttlSeconds, err := strconv.Atoi(os.Getenv("REDIS_IDEMPOTENCY_TTL")); err == nil && ttlSeconds > 0 {
		idempotencyTTL = time.Duration(ttlSeconds) * time.Second
	}

	// Parse USSD session timeout in seconds, default to 5 minutes (300 seconds)
	ussdSessionTimeout := 5 * time.Minute
	if secs, err := strconv.Atoi(os.Getenv("USSD_SESSION_TIMEOUT")); err == nil && secs > 0 {
		ussdSessionTimeout = time.Duration(secs) * time.Second
	}

	ycBaseURL := os.Getenv("YELLOWCARD_BASE_URL")
	if ycBaseURL == "" {
		ycBaseURL = "https://sandbox.api.yellowcard.io/business"
	}

	fonbnkBaseURL := os.Getenv("FONBNK_BASE_URL")
	if fonbnkBaseURL == "" {
		fonbnkBaseURL = "https.sandbox-api.fonbnk.com"
	}

	mobileSandboxMode, err := strconv.ParseBool(os.Getenv("AT_SANDBOX_MODE"))
	if err != nil {
		return nil, fmt.Errorf("error parsing Africa's Talking sandbox mode: %v", err)
	}

	mobileBaseURL := os.Getenv("AT_BASE_URL")
	if mobileBaseURL == "" && mobileSandboxMode {
		mobileBaseURL = "https://api.sandbox.africastalking.com/version1"
	} else if mobileBaseURL == "" {
		mobileBaseURL = "https://api.africastalking.com/version1"
	}

	mobileSenderID := os.Getenv("AT_SENDER_ID")
	mobileShortcode := os.Getenv("AT_SHORTCODE")

	// Left zero when unset so the adapter applies its own default rather than
	// config duplicating the value.
	mobileHTTPTimeout, err := envSeconds("AT_HTTP_TIMEOUT")
	if err != nil {
		return nil, err
	}

	// Determine network passphrase based on environment
	networkPassphrase := network.PublicNetworkPassphrase
	if serverEnvironment == "development" {
		networkPassphrase = network.TestNetworkPassphrase
	}

	// Multi-Signature Configuration
	enableMultiSig, err := strconv.ParseBool(os.Getenv("ENABLE_MULTI_SIG"))
	if err != nil {
		return nil, fmt.Errorf("error parsing multi-sig enablement: %v", err)
	}

	multiSigLowThreshold, err := strconv.Atoi(os.Getenv("MULTI_SIG_LOW_THRESHOLD"))
	if err != nil {
		return nil, fmt.Errorf("error parsing multi-sig low threshold: %v", err)
	}

	multiSigMediumThreshold, err := strconv.Atoi(os.Getenv("MULTI_SIG_MEDIUM_THRESHOLD"))
	if err != nil {
		return nil, fmt.Errorf("error parsing multi-sig medium threshold: %v", err)
	}

	multiSigHighThreshold, err := strconv.Atoi(os.Getenv("MULTI_SIG_HIGH_THRESHOLD"))
	if err != nil {
		return nil, fmt.Errorf("error parsing multi-sig high threshold: %v", err)
	}

	if usdcIssuer != "" {
		_, err := keypair.ParseAddress(usdcIssuer)
		if err != nil {
			return nil, fmt.Errorf("invalid USDC_ISSUER: %w", err)
		}
	}

	if err := validateUSDCIssuerAlignment(mgUSDCIssuer, usdcIssuer); err != nil {
		return nil, err
	}

	if treasurySecretKey != "" {
		_, err := keypair.ParseFull(treasurySecretKey)
		if err != nil {
			return nil, fmt.Errorf("invalid treasury secret key: %w,", err)
		}
	}

	if adminSecretKey != "" {
		_, err := keypair.ParseFull(adminSecretKey)
		if err != nil {
			return nil, fmt.Errorf("invalid admin secret key: %w", err)
		}
	}

	if serverSecretKey != "" {
		_, err := keypair.ParseFull(serverSecretKey)
		if err != nil {
			return nil, fmt.Errorf("invalid server secret key: %w", err)
		}
	}

	if contractID != "" {
		isValid := strkey.IsValidContractAddress(contractID)
		if !isValid {
			return nil, fmt.Errorf("invalid contract address: %w", err)
		}
	}

	// Return the populated config struct
	return &Config{
		Postgres: PostgresConfig{
			Host:     dbHost,
			User:     dbUser,
			Password: dbPassword,
			DBName:   dbName,
			Port:     dbPort,
			SSLMode:  dbSSLMode,
			TimeZone: dbTimeZone,
		},
		Redis: RedisConfig{
			Host:           redisHost,
			Port:           redisPort,
			Password:       redisPassword,
			DBNumber:       redisDBNum,
			IdempotencyTTL: idempotencyTTL,
		},
		Server: ServerConfig{
			ServerEnvironment: serverEnvironment,
			ServerHost:        serverHost,
			CoreServerPort:    coreServerPort,
			CreditServerPort:  creditServerPort,
			PublicBaseURL:     strings.TrimRight(os.Getenv("PUBLIC_BASE_URL"), "/"),
		},
		Shortener: ShortenerConfig{
			APIKey:             os.Getenv("DUB_API_KEY"),
			BaseURL:            strings.TrimRight(os.Getenv("DUB_API_URL"), "/"),
			Domain:             os.Getenv("DUB_DOMAIN"),
			PreviewTitle:       os.Getenv("DUB_PREVIEW_TITLE"),
			PreviewDescription: os.Getenv("DUB_PREVIEW_DESCRIPTION"),
			ImagePreviewURL:    os.Getenv("DUB_IMAGE_PREVIEW_URL"),
		},
		Stellar: StellarConfig{
			RpcURL:                  stellarRpcURL,
			TreasurySecretKey:       treasurySecretKey,
			AdminSecretKey:          adminSecretKey,
			ServerSecretKey:         serverSecretKey,
			NetworkPassphrase:       networkPassphrase,
			EnableMultiSig:          enableMultiSig,
			MultiSigLowThreshold:    uint32(multiSigLowThreshold),
			MultiSigMediumThreshold: uint32(multiSigMediumThreshold),
			MultiSigHighThreshold:   uint32(multiSigHighThreshold),
			USDCIssuer:              usdcIssuer,
			ContractID:              contractID,
		},
		Payments: PaymentsConfig{
			YellowCard: YellowCardConfig{
				PublicKey:    ycPublicKey,
				SecretKey:    ycSecretKey,
				BaseURL:      ycBaseURL,
				BusinessID:   ycBusinessID,
				BusinessName: ycBusinessName,
			},
			Fonbnk: FonbnkConfig{
				ClientID:     fonbnkClientID,
				ClientSecret: fonbnkClientSecret,
				BaseURL:      fonbnkBaseURL,
			},
			MoneyGram: MoneyGramConfig{
				HomeDomain:        mgHomeDomain,
				ServerSigningKey:  mgServerSigningKey,
				NetworkPassphrase: networkPassphrase, // share with StellarConfig — must match anchor's TOML
				USDCIssuer:        firstNonEmpty(mgUSDCIssuer, usdcIssuer),
				TransferServerURL: mgTransferServerURL,
				ClientID:          mgClientID,
				ClientSecret:      mgClientSecret,
				OAuthURL:          mgOAuthURL,
				FXRateURL:         mgFXRateURL,
				AuthSecret:        firstNonEmpty(mgAuthSecret, treasurySecretKey),
				FundsSecret:       firstNonEmpty(mgFundsSecret, treasurySecretKey),

				PollInterval:            mgPollInterval,
				PollMaxBatch:            mgPollMaxBatch,
				RefundSettleMaxAttempts: mgRefundMaxAttempts,
			},
		},
		Mobile: MobileConfig{
			AfricasTalking: AfricasTalkingConfig{
				Username:    mobileUsername,
				APIKey:      mobileAPIKey,
				BaseURL:     mobileBaseURL,
				SenderID:    mobileSenderID,
				Shortcode:   mobileShortcode,
				HTTPTimeout: mobileHTTPTimeout,
			},
			SessionTimeout: ussdSessionTimeout,
		},
		Auth: AuthConfig{
			JWTSecret:           jwtSecret,
			JWTExpiration:       time.Hour * 24,
			JWTRefreshWindow:    time.Hour * 1,
			ChallengeExpiration: time.Hour * 5,
			PINLockoutDuration:  parsePINLockout(),
		},
	}, nil
}

// parsePINLockout reads PIN_LOCKOUT_SECONDS from the environment.
// Defaults to 900 (15 minutes) if unset or invalid.
func parsePINLockout() time.Duration {
	if s, err := strconv.Atoi(os.Getenv("PIN_LOCKOUT_SECONDS")); err == nil && s > 0 {
		return time.Duration(s) * time.Second
	}
	return 15 * time.Minute
}

// envSeconds reads a bare integer count of seconds, matching the convention
// used by USSD_SESSION_TIMEOUT and PIN_LOCKOUT_SECONDS. Returns zero when
// unset so callers can fall back to their own default. A malformed value is
// an error rather than a silent default: these exist to be tuned, and a typo
// quietly ignored would look like the tuning had no effect.
func envSeconds(key string) (time.Duration, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, nil
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return 0, fmt.Errorf("error parsing %s: expected a positive number of seconds, got %q", key, raw)
	}
	return time.Duration(secs) * time.Second, nil
}

// envPositiveInt reads a bare positive integer, zero when unset.
func envPositiveInt(key string) (int, error) {
	raw := os.Getenv(key)
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("error parsing %s: expected a positive integer, got %q", key, raw)
	}
	return n, nil
}

// firstNonEmpty returns the first non-empty argument, or "" if all are empty.
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

// Validate checks that all fields required to construct a working MoneyGram
// client are set.
//
// Validate intentionally does not require REST API credentials (ClientID /
// ClientSecret / OAuthURL / FXRateURL): the SEP-1/10/24 anchor flow can run
// without them, and those credentials are gated on Open Q #6 confirming MG
// issues REST API access to Ramps partners.
func (c *MoneyGramConfig) Validate() error {
	missing := []string{}
	if c.HomeDomain == "" {
		missing = append(missing, "MONEYGRAM_HOME_DOMAIN")
	}
	if c.ServerSigningKey == "" {
		missing = append(missing, "MONEYGRAM_SERVER_SIGNING_KEY")
	}
	if c.NetworkPassphrase == "" {
		missing = append(missing, "STELLAR network passphrase (set on StellarConfig)")
	}
	if c.USDCIssuer == "" {
		missing = append(missing, "MONEYGRAM_USDC_ISSUER or USDC_ISSUER")
	}
	if c.AuthSecret == "" {
		missing = append(missing, "MONEYGRAM_AUTH_SECRET or TREASURY_SECRET_KEY")
	}
	if c.FundsSecret == "" {
		missing = append(missing, "MONEYGRAM_FUNDS_SECRET or TREASURY_SECRET_KEY")
	}
	if len(missing) > 0 {
		return fmt.Errorf("moneygram config missing required values: %s", strings.Join(missing, ", "))
	}
	if _, err := keypair.ParseFull(c.AuthSecret); err != nil {
		return fmt.Errorf("moneygram MONEYGRAM_AUTH_SECRET is not a valid Stellar secret: %w", err)
	}
	if _, err := keypair.ParseFull(c.FundsSecret); err != nil {
		return fmt.Errorf("moneygram MONEYGRAM_FUNDS_SECRET is not a valid Stellar secret: %w", err)
	}
	return nil
}

// AuthAddress returns the public G... address of the SEP-10 auth wallet.
func (c *MoneyGramConfig) AuthAddress() (string, error) {
	kp, err := keypair.ParseFull(c.AuthSecret)
	if err != nil {
		return "", err
	}
	return kp.Address(), nil
}

// FundsAddress returns the public G... address of the funds wallet — the
// SEP-24 account and USDC send source.
func (c *MoneyGramConfig) FundsAddress() (string, error) {
	kp, err := keypair.ParseFull(c.FundsSecret)
	if err != nil {
		return "", err
	}
	return kp.Address(), nil
}

// HasRESTCredentials reports whether the REST API credentials are populated.
// Callers should fall back to YC for FX rates when this returns false.
func (c *MoneyGramConfig) HasRESTCredentials() bool {
	return c.ClientID != "" && c.ClientSecret != "" && c.OAuthURL != "" && c.FXRateURL != ""
}

// validateUSDCIssuerAlignment rejects a MoneyGram issuer override that names a
// different asset from the vault's.
//
// The vault is constructed with USDC_ISSUER and refunds are repaid into it, so
// an override describes an asset the vault cannot accept. Startup is the only
// place this surfaces loudly: at runtime it presents as refunds that silently
// never settle, because the on-ledger issuer check rejects every payment and
// the loan simply sits in refund_pending.
//
// An unset override is not a mismatch — it inherits USDC_ISSUER.
func validateUSDCIssuerAlignment(moneygramIssuer, stellarIssuer string) error {
	if moneygramIssuer == "" || stellarIssuer == "" {
		return nil
	}
	if moneygramIssuer != stellarIssuer {
		return fmt.Errorf(
			"MONEYGRAM_USDC_ISSUER (%s) must equal USDC_ISSUER (%s): the vault only accepts the latter",
			moneygramIssuer, stellarIssuer)
	}
	return nil
}
