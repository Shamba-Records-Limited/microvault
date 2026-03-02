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
	Postgres PostgresConfig
	Redis    RedisConfig
	Server   ServerConfig
	Stellar  StellarConfig
	Payments PaymentsConfig
	Mobile   MobileConfig
	Auth     AuthConfig
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

// PaymentsConfig bundles all payment provider configurations
type PaymentsConfig struct {
	YellowCard YellowCardConfig
	Fonbnk     FonbnkConfig
}

// AfricasTalkingConfig holds all SMS/USSD-related configuration for Africa's Talking
type AfricasTalkingConfig struct {
	Username  string
	APIKey    string
	BaseURL   string
	SenderID  string // from AT_SENDER_ID (alphanumeric or shortcode)
	Shortcode string // from AT_SHORTCODE (numeric shortcode)
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
}

type AuthConfig struct {
	JWTSecret           string
	JWTExpiration       time.Duration
	JWTRefreshWindow    time.Duration
	ChallengeExpiration time.Duration
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

	ycBaseURL := os.Getenv("YELLOWCARD_BASE_URL")
	if ycBaseURL == "" {
		ycBaseURL = "https://sandbox.api.yellowcard.io"
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
		},
		Mobile: MobileConfig{
			AfricasTalking: AfricasTalkingConfig{
				Username:  mobileUsername,
				APIKey:    mobileAPIKey,
				BaseURL:   mobileBaseURL,
				SenderID:  mobileSenderID,
				Shortcode: mobileShortcode,
			},
		},
		Auth: AuthConfig{
			JWTSecret:           jwtSecret,
			JWTExpiration:       time.Hour * 24,
			JWTRefreshWindow:    time.Hour * 1,
			ChallengeExpiration: time.Hour * 5,
		},
	}, nil
}
