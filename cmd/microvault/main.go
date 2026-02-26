package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/Shamba-Records-Limited/microvault/cmd/microvault/docs"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/fonbnk"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"

	routes "github.com/Shamba-Records-Limited/microvault/internal/core/pkg/routes"
	"github.com/Shamba-Records-Limited/microvault/pkg/account"
	"github.com/Shamba-Records-Limited/microvault/pkg/auth"
	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/controllers"
	"github.com/Shamba-Records-Limited/microvault/pkg/health"
	"github.com/Shamba-Records-Limited/microvault/pkg/middleware"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
	smsAfrica "github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms/providers/africastalking"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd"
	ussdadapters "github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd/adapters"
	ussdAfrica "github.com/Shamba-Records-Limited/microvault/pkg/mobile/ussd/providers/africastalking"
	mvnotifications "github.com/Shamba-Records-Limited/microvault/pkg/notifications"
	"github.com/Shamba-Records-Limited/microvault/pkg/pin"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar"
	"github.com/Shamba-Records-Limited/microvault/pkg/user"
	"github.com/Shamba-Records-Limited/microvault/pkg/validation"
	"github.com/Shamba-Records-Limited/microvault/platform/cache"
	"github.com/Shamba-Records-Limited/microvault/platform/database"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/swagger"
	_ "github.com/joho/godotenv/autoload"
)

// @title microvault API
// @version 1.0
// @description A headless ERC-4626 tokenized vault engine for microlending built on the stellar network.
// @termsOfService http://swagger.io/terms/
// @contact.name API Support
// @contact.email smugane@shambarecords.com
// @license.name AGPL-3.0
// @license.url https://www.gnu.org/licenses/agpl-3.0.en.html
// @host localhost:8080
// @BasePath /
func main() {
	// Load configuration on startup
	cfg, err := config.New()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// ---- Initialize Validation Service ----
	// Initialize validation service used universally across controllers
	validationService := validation.NewValidatorService()
	log.Println("Validation service initialized")

	// ---- Initialize Platform Services ----
	// Load database
	_, err = database.GetConnection("core", &cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	log.Println("Database connection successful")

	// Load cache
	redisClient, err := cache.GetConnection("core", &cfg.Redis)
	if err != nil {
		log.Fatalf("Failed to connect to redis: %v", err)
	}

	log.Println("Redis connection successful")

	// ---- Initialize Authentication Services ----
	// Initialize challenge store with Redis
	challengeStore, err := auth.NewRedisStore(redisClient, "microvault:auth")
	if err != nil {
		log.Fatalf("Failed to initialize challenge store: %v", err)
	}

	// Initialize challenge service
	challengeService, err := auth.NewChallengeService(&cfg.Auth, &cfg.Stellar, challengeStore)
	if err != nil {
		log.Fatalf("Failed to initialize challenge service: %v", err)
	}

	// Initialize JWT service
	jwtService := auth.NewJWTService(&cfg.Auth)

	// Initialize auth controller
	authController := controllers.NewAuthController(challengeService, jwtService, cfg.Stellar.AdminPublicKey, validationService)

	// ---- Initialize Stellar Related Services ----
	// Initialize stellar client
	stellarClient := cfg.Stellar.NewRpcClient()
	log.Println("Stellar RPC Client initialized.")

	// Close stellar client
	defer func() {
		log.Println("Closing Stellar client...")
		if err := stellarClient.Close(); err != nil {
			log.Printf("Error closing Stellar client: %v", err)
		}
	}()

	// Initialize Stellar service with both classic and Soroban support
	stellarService := stellar.NewService(
		stellarClient,
		cfg.Stellar.NetworkPassphrase,
		cfg.Stellar.TreasurySecretKey,
		cfg.Stellar.AdminSecretKey,
		cfg.Stellar.ContractID,
		cfg.Stellar.USDCIssuer,
	)
	log.Println("Stellar service initialized")

	// ---- Initialize payments service ----
	// Initialize providers
	fonbnkProvider := fonbnk.NewFonbnkAdapter(
		cfg.Payments.Fonbnk.ClientID,
		cfg.Payments.Fonbnk.ClientSecret,
		cfg.Payments.Fonbnk.BaseURL,
	)

	yellowCardProvider := yellowcard.NewYellowcardAdapter(
		cfg.Payments.YellowCard.PublicKey,
		cfg.Payments.YellowCard.SecretKey,
		cfg.Payments.YellowCard.BaseURL,
	)

	// Register providers
	paymentService := payment.NewService()

	paymentService.RegisterProvider("yellowcard", yellowCardProvider)
	paymentService.RegisterProvider("fonbnk", fonbnkProvider)

	// ---- Initialize YellowCard OffRamp Service (for loan disbursement) ----
	// This service handles Mobile Money disbursements via YellowCard
	// Uncomment when ready to integrate with loan disbursement flow
	//
	// yellowCardOffRampAdapter := ussdadapters.NewYellowCardOffRampAdapter(
	// 	ussdadapters.YellowCardOffRampConfig{
	// 		Adapter:      yellowCardProvider,
	// 		BusinessID:   cfg.Payments.YellowCard.BusinessID,
	// 		BusinessName: cfg.Payments.YellowCard.BusinessName,
	// 	},
	// )
	// log.Println("YellowCard OffRamp adapter initialized")
	//
	// Usage in loan disbursement:
	// result, err := yellowCardOffRampAdapter.InitiateOffRamp(ctx, ussdadapters.OffRampRequest{
	// 	LoanID:           loan.ID,
	// 	UserID:           user.ID,
	// 	RecipientName:    user.FullName,
	// 	AmountUSD:        loan.Amount,
	// 	DestinationPhone: user.MobileNumber,
	// 	CountryCode:      user.CountryCode,
	// 	NetworkCode:      user.MomoNetworkCode,
	// 	NetworkName:      user.MomoNetworkName,
	// 	IdempotencyKey:   fmt.Sprintf("loan_%s", loan.ID),
	// })

	// ---- Initialize Repositories ----
	db, err := database.GetConnection("core", &cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to get database connection for repositories: %v", err)
	}

	repos, err := repository.NewRepositories(db)
	if err != nil {
		log.Fatalf("Failed to initialize repositories: %v", err)
	}
	log.Println("Repositories initialized successfully")

	// ---- Initialize Core Services ----
	// User and Account services
	userService := user.NewService(repos.User)
	accountService := account.NewService(repos.Account, repos.User)
	log.Println("User and Account services initialized")

	// ---- Initialize Mobile Providers ----
	// Initialize USSD providers
	timeout := time.Duration(3 * time.Minute)
	sessionMgr := ussd.NewSessionManager(redisClient, timeout)
	menuRegistry := ussd.NewMenuRegistry()
	preset := ussd.NewStandardLoanMenuPreset()
	preset.Initialize(menuRegistry)

	// Get database connection for USSD adapter
	db, err = database.GetConnection("core", &cfg.Postgres)
	if err != nil {
		log.Fatalf("Failed to get database connection for USSD adapter: %v", err)
	}

	// Create USSD User Service Adapter with Stellar service integration
	userServiceAdapter, err := ussdadapters.NewUserServiceAdapter(
		userService,
		accountService,
		stellarService,
		db,
		ussdadapters.WalletConfig{
			TreasuryPrivateKey: cfg.Stellar.TreasurySecretKey,
			USDCIssuer:         cfg.Stellar.USDCIssuer,
			EnableMultiSig:     cfg.Stellar.EnableMultiSig,
			LowThreshold:       uint32(cfg.Stellar.MultiSigLowThreshold),
			MediumThreshold:    uint32(cfg.Stellar.MultiSigMediumThreshold),
			HighThreshold:      uint32(cfg.Stellar.MultiSigHighThreshold),
		},
	)
	if err != nil {
		log.Fatalf("Failed to initialize USSD user service adapter: %v", err)
	}
	log.Println("USSD user service adapter initialized")

	// Initialize SMS providers
	AfricasTalkingSMSProvider := smsAfrica.NewAfricasTalkingSMSAdapter(
		cfg.Mobile.AfricasTalking.Username,
		cfg.Mobile.AfricasTalking.APIKey,
		cfg.Mobile.AfricasTalking.BaseURL,
	)

	// Register SMS providers
	smsService := sms.NewSMSService()
	smsService.RegisterProvider("africastalking", AfricasTalkingSMSProvider)

	// ---- Initialize Notification Service ----
	// Create notifier (NoOp if AT provider not configured)
	var notifier mvnotifications.Notifier
	atProvider, providerErr := smsService.GetProvider("africastalking")
	if providerErr == nil {
		notifier = mvnotifications.NewSMSNotifier(atProvider, "microvault")
	} else {
		notifier = &mvnotifications.NoOpNotifier{}
	}
	loanNotifier := mvnotifications.NewSMSLoanNotifier(notifier, nil)
	_ = loanNotifier // will be wired to adapters when loan disbursement is integrated

	// Initialize account notifier for registration and PIN lifecycle SMS
	accountNotifier := mvnotifications.NewSMSAccountNotifier(notifier, nil)
	log.Println("Notification service initialized")

	// ---- Initialize PIN Service ----
	pinRepo := pin.NewSecurityQuestionRepository(db)
	pinService := pin.NewService(repos.User, pinRepo, accountNotifier)
	log.Println("PIN service initialized")

	// Initialize USSD handler with real services
	// Note: loanService and disbursementService are nil - will be implemented later
	handler := ussd.NewUSSDHandler(sessionMgr, menuRegistry, userServiceAdapter, nil, nil, pinService, accountNotifier)
	ussdService := ussd.NewUSSDService(handler)

	// Register USSD providers
	AfricasTalkingUSSDProvider := ussdAfrica.NewAfricasTalkingUSSDAdapter(
		cfg.Mobile.AfricasTalking.Username,
		cfg.Mobile.AfricasTalking.APIKey,
	)
	ussdService.RegisterProvider("africastalking", AfricasTalkingUSSDProvider)

	// Initialize USSD controller
	ussdController := controllers.NewUSSDController(ussdService)

	// ---- Initialize Application ----
	// Create a new fiber app
	app := fiber.New()

	// Initialize health checker middleware
	healthCheck := health.NewChecker(stellarClient, "core", "core")

	// Initialize middleware & pass health checker middleware
	middleware.FiberMiddleware(app, healthCheck)

	// Define swagger routes
	app.Get("/swagger/*", swagger.New(swagger.Config{
		DeepLinking:  false,
		DocExpansion: "list",
		OAuth: &swagger.OAuthConfig{
			AppName:  "OAuth Provider",
			ClientId: "21bb4edc-05a7-4afc-86f1-2e151e4ba6e2",
		},
		OAuth2RedirectUrl: "http://localhost:8080/swagger/oauth2-redirect.html",
	}))

	// Serve redoc.html at the root route
	app.Get("/", func(c *fiber.Ctx) error {
		return c.SendFile("./cmd/microvault/docs/redoc-static.html")
	})

	// Initialize webhook controller (event handler will be wired when disbursement service is ready)
	webhookController := controllers.NewWebhookController(nil, cfg.Payments.YellowCard.WebhookSecret)

	routes.PublicRoutes(app, authController, ussdController, webhookController) // Register public routes

	// Create a channel to listen for OS signals
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	// Run the server in a separate goroutine
	go func() {
		log.Printf("Starting core server on %s", cfg.Server.CoreAddr())
		if err := app.Listen(cfg.Server.CoreAddr()); err != nil {
			log.Printf("Server Listen error: %v", err)
		}
	}()

	// Block main goroutine until a signal is received
	sig := <-sigChan
	log.Printf("Received signal %s. Shutting down gracefully...", sig)

	// Tell Fiber to shut down
	if err := app.Shutdown(); err != nil {
		log.Printf("Fiber shutdown error: %v", err)
	}
	log.Println("Fiber server shut down.")

	// Close database connections
	if err := database.CloseAll(); err != nil {
		log.Printf("Database shutdown error: %v", err)
	} else {
		log.Println("Database connections closed successfully.")
	}

	// Close cache connections
	if err := cache.CloseAll(); err != nil {
		log.Printf("Cache shutdown error: %v", err)
	} else {
		log.Println("Cache connections closed successfully.")
	}

	log.Println("Core application shutdown complete.")
}
