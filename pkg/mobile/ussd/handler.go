package ussd

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
	pinPkg "github.com/Shamba-Records-Limited/microvault/pkg/pin"
)

// sensitiveMenus lists menus where user input contains PII (PINs, national IDs, names).
var sensitiveMenus = map[string]bool{
	"register":                 true, // full name
	"register_national_id":     true, // national ID
	"pin_create":               true,
	"pin_confirm":              true,
	"pin_verify_loan":          true,
	"pin_verify_repay":         true,
	"pin_change_old":           true,
	"pin_change_new":           true,
	"pin_change_confirm":       true,
	"pin_recovery_national_id": true,
	"pin_recovery_q1":          true, // security answer
	"pin_recovery_q2":          true, // security answer
	"pin_recovery_new":         true,
	"pin_recovery_confirm":     true,
}

// redactPhone masks the middle digits of a phone number.
// e.g. "254799334972" → "2547XXXX972", "+254799334972" → "+2547XXXX972"
func redactPhone(phone string) string {
	digits := phone
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 6 {
		return phone // too short to redact meaningfully
	}
	// Keep first 4 and last 3 digits, mask the rest
	masked := digits[:4] + strings.Repeat("X", len(digits)-7) + digits[len(digits)-3:]
	return prefix + masked
}

// safeInput returns the input for logging, redacted if the current menu handles sensitive data.
func safeInput(menu, input string) string {
	if sensitiveMenus[menu] {
		return "[REDACTED]"
	}
	return fmt.Sprintf("%q", input)
}

// NewUSSDHandler creates a new USSD handler. The pinService and
// accountNotifier parameters may be nil; if so, PIN verification gates and
// registration SMS notifications are silently skipped.
func NewUSSDHandler(
	sessionManager *SessionManager,
	menuRegistry *MenuRegistry,
	userService UserService,
	loanService LoanService,
	rateService RateService,
	pinService PINService,
	accountNotifier contracts.AccountNotifier,
) *USSDHandler {
	if accountNotifier == nil {
		accountNotifier = &notifications.NoOpAccountNotifier{}
	}
	return &USSDHandler{
		sessionManager:  sessionManager,
		menuRegistry:    menuRegistry,
		userService:     userService,
		loanService:     loanService,
		rateService:     rateService,
		pinService:      pinService,
		accountNotifier: accountNotifier,
	}
}

// HandleRequest handles a USSD request
func (h *USSDHandler) HandleRequest(ctx context.Context, sessionID, phoneNumber, serviceCode, networkCode, input string) (string, error) {
	// Get or create session
	session, err := h.sessionManager.GetOrCreateSession(ctx, sessionID, phoneNumber, serviceCode, networkCode)
	if err != nil {
		return h.formatError("en", "session_expired"), nil
	}

	log.Printf("USSD Session - ID: %s, ServiceCode: %s, NetworkCode: %s, Phone: %s, CurrentMenu: %s, Input: %s",
		sessionID, serviceCode, networkCode, redactPhone(phoneNumber), session.CurrentMenu, safeInput(session.CurrentMenu, input))

	// Handle empty input (first request)
	if input == "" {
		return h.handleInitialRequest(ctx, session)
	}

	// Handle input based on current menu
	return h.handleMenuInput(ctx, session, input)
}

// handleInitialRequest handles the first USSD dial
func (h *USSDHandler) handleInitialRequest(ctx context.Context, session *Session) (string, error) {
	// If no user service is configured, go directly to main menu
	if h.userService == nil {
		return h.showMainMenu(session)
	}

	// Check if user is registered
	user, _, err := h.userService.GetUserWithAccounts(ctx, session.PhoneNumber)
	if err != nil || user == nil {
		// User not registered, show registration flow
		response, err := h.showRegistrationMenu(session)
		if err != nil {
			return "", err
		}
		log.Printf("After showRegistrationMenu - CurrentMenu: %s", session.CurrentMenu)
		// Save session with updated menu
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			log.Printf("ERROR: Failed to save session after registration menu: %v", err)
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		log.Printf("Session saved successfully - SessionID: %s, CurrentMenu: %s", session.SessionID, session.CurrentMenu)
		return response, nil
	}

	// User registered, associate with session and show main menu.
	// Always reset to main menu to prevent stale CurrentMenu from a
	// previous session with the same ID (AT retries, etc.).
	if userMap, ok := user.(map[string]interface{}); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}
	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMainMenu(session)
}

// handleMenuInput handles user input for current menu
func (h *USSDHandler) handleMenuInput(ctx context.Context, session *Session, input string) (string, error) {
	input = strings.TrimSpace(input)

	switch session.CurrentMenu {
	// Core navigation
	case "main":
		return h.handleMainMenu(ctx, session, input)
	case "language_select":
		return h.handleLanguageSelect(ctx, session, input)
	case "my_account":
		return h.handleMyAccount(ctx, session, input)

	// Registration flow
	case "register":
		return h.handleRegistration(ctx, session, input)
	case "register_national_id":
		return h.handleRegistrationNationalID(ctx, session, input)

	// PIN creation (during registration)
	case "pin_create":
		return h.handlePINCreate(ctx, session, input)
	case "pin_confirm":
		return h.handlePINConfirm(ctx, session, input)

	// Security questions setup (during registration or PIN manager)
	case "security_q1_select":
		return h.handleSecurityQ1Select(ctx, session, input)
	case "security_q1_answer":
		return h.handleSecurityQ1Answer(ctx, session, input)
	case "security_q2_select":
		return h.handleSecurityQ2Select(ctx, session, input)
	case "security_q2_answer":
		return h.handleSecurityQ2Answer(ctx, session, input)

	// PIN verification gates
	case "pin_verify_loan":
		return h.handlePINVerifyLoan(ctx, session, input)
	case "pin_verify_repay":
		return h.handlePINVerifyRepay(ctx, session, input)

	// PIN manager
	case "pin_manager":
		return h.handlePINManager(ctx, session, input)
	case "pin_change_old":
		return h.handlePINChangeOld(ctx, session, input)
	case "pin_change_new":
		return h.handlePINChangeNew(ctx, session, input)
	case "pin_change_confirm":
		return h.handlePINChangeConfirm(ctx, session, input)

	// PIN recovery
	case "pin_recovery_national_id":
		return h.handlePINRecoveryNationalID(ctx, session, input)
	case "pin_recovery_q1":
		return h.handlePINRecoveryQ1(ctx, session, input)
	case "pin_recovery_q2":
		return h.handlePINRecoveryQ2(ctx, session, input)
	case "pin_recovery_new":
		return h.handlePINRecoveryNew(ctx, session, input)
	case "pin_recovery_confirm":
		return h.handlePINRecoveryConfirm(ctx, session, input)

	// Loan flow
	case "request_loan", "loan_amount":
		return h.handleLoanAmount(ctx, session, input)
	case "loan_duration":
		return h.handleLoanDuration(ctx, session, input)
	case "repayment_schedule":
		return h.handleRepaymentSchedule(ctx, session, input)
	case "loan_confirm":
		return h.handleLoanConfirm(ctx, session, input)
	case "my_loans":
		return h.handleMyLoans(ctx, session)
	case "repay_loan":
		return h.handleRepayLoan(ctx, session, input)

	default:
		return h.showMainMenu(session)
	}
}

// handleMainMenu handles main menu selection (4-option menu).
func (h *USSDHandler) handleMainMenu(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1": // Request Loan
		session.CurrentMenu = "request_loan"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLoanAmountMenu(session)
	case "2": // Repay Loan — requires PIN
		if h.pinService != nil {
			session.CurrentMenu = "pin_verify_repay"
			if err := h.sessionManager.SaveSession(ctx, session); err != nil {
				return "", fmt.Errorf("failed to save session: %w", err)
			}
			return h.showMenu(session, "pin_verify_repay")
		}
		session.CurrentMenu = "repay_loan"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.handleRepayLoan(ctx, session, "")
	case "3": // My Loans
		session.CurrentMenu = "my_loans"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.handleMyLoans(ctx, session)
	case "4": // My Account
		session.CurrentMenu = "my_account"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showAccountMenu(session)
	default:
		mainMenu, err := h.showMainMenu(session)
		if err != nil {
			return "", err
		}
		return h.formatResponse(session.Language, "CON", "invalid_input") + "\n" + mainMenu, nil
	}
}

// handleLanguageSelect handles language selection
func (h *USSDHandler) handleLanguageSelect(ctx context.Context, session *Session, input string) (string, error) {
	var language string
	switch input {
	case "1":
		language = "en"
	case "2":
		language = "sw"
	case "3":
		language = "fr"
	case "0":
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	default:
		return h.showLanguageMenu(session)
	}

	session.Language = language
	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMainMenu(session)
}

// handleRegistration handles user registration
func (h *USSDHandler) handleRegistration(ctx context.Context, session *Session, input string) (string, error) {
	log.Printf("handleRegistration called - SessionID: %s", session.SessionID)

	// Store name
	session.Data["full_name"] = input

	// Ask for national ID
	session.CurrentMenu = "register_national_id"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	log.Printf("Registration - stored full_name, updated CurrentMenu to: register_national_id")

	return h.formatResponse(session.Language, "CON", "Enter your national ID:"), nil
}

// handleRegistrationNationalID registers the user and chains into PIN creation
// if the PIN service is available, otherwise ends with success.
func (h *USSDHandler) handleRegistrationNationalID(ctx context.Context, session *Session, input string) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	fullName, _ := session.Data["full_name"].(string)

	// Register user.
	user, _, err := h.userService.RegisterUser(ctx, &RegisterUserRequest{
		MobileNumber:      session.PhoneNumber,
		NetworkCode:       session.NetworkCode,
		FullName:          fullName,
		NationalID:        input,
		PreferredLanguage: session.Language,
	})

	if err != nil {
		_ = h.accountNotifier.NotifyRegistrationFailed(ctx, contracts.AccountNotification{
			PhoneNumber: session.PhoneNumber,
			Reason:      "Account creation failed. Please try again.",
		})
		return h.formatError(session.Language, "error"), nil
	}

	// Associate user with session.
	if userMap, ok := user.(map[string]interface{}); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}

	// If PIN service is available, chain into PIN creation flow.
	if h.pinService != nil {
		session.CurrentMenu = "pin_create"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "pin_create")
	}

	// No PIN service — finish registration immediately.
	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.formatResponse(session.Language, "END", "registration_success"), nil
}

// handleLoanAmount handles loan amount input in KES
func (h *USSDHandler) handleLoanAmount(ctx context.Context, session *Session, input string) (string, error) {
	amount, err := strconv.ParseFloat(input, 64)
	if err != nil || amount < 500 || amount > 500000 {
		return h.showLoanAmountMenu(session)
	}

	// Store local currency amount in cents
	session.Data["loan_amount_local"] = int64(amount * 100) // KES cents
	session.Data["local_currency"] = "KES"
	session.CurrentMenu = "loan_duration"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showLoanDurationMenu(session)
}

// handleLoanDuration handles loan duration selection
func (h *USSDHandler) handleLoanDuration(ctx context.Context, session *Session, input string) (string, error) {
	var days int
	switch input {
	case "1":
		days = 7
	case "2":
		days = 14
	case "3":
		days = 30
	case "4":
		days = 90
	case "0":
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	default:
		return h.showLoanDurationMenu(session)
	}

	session.Data["loan_duration"] = days
	session.CurrentMenu = "repayment_schedule"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showRepaymentScheduleMenu(session)
}

// handleRepaymentSchedule handles repayment schedule selection
func (h *USSDHandler) handleRepaymentSchedule(ctx context.Context, session *Session, input string) (string, error) {
	var schedule string
	switch input {
	case "1":
		schedule = "lump_sum"
	case "2":
		schedule = "weekly"
	case "3":
		schedule = "bi_weekly"
	case "4":
		schedule = "monthly"
	case "0":
		session.CurrentMenu = "loan_duration"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLoanDurationMenu(session)
	default:
		return h.showRepaymentScheduleMenu(session)
	}

	session.Data["repayment_schedule"] = schedule
	session.CurrentMenu = "loan_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showLoanConfirmation(ctx, session)
}

// handleLoanConfirm handles loan confirmation. When PIN service is available,
// pressing "1" routes to PIN verification before submitting the loan.
func (h *USSDHandler) handleLoanConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if input != "1" {
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	}

	// Gate loan submission behind PIN verification.
	if h.pinService != nil {
		session.CurrentMenu = "pin_verify_loan"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "pin_verify_loan")
	}

	return h.submitLoan(ctx, session)
}

// submitLoan executes the actual loan request after all gates (PIN, eligibility)
// have passed.
func (h *USSDHandler) submitLoan(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil || h.loanService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Get loan details from session
	amount, _ := session.Data["loan_amount"].(int64)
	duration, _ := session.Data["loan_duration"].(int)
	schedule, _ := session.Data["repayment_schedule"].(string)

	// Get user and first account
	userData, accounts, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil || len(accounts) == 0 {
		return h.formatError(session.Language, "error"), nil
	}

	// Extract account ID
	var accountID string
	if accMap, ok := accounts[0].(map[string]interface{}); ok {
		if id, ok := accMap["id"].(string); ok {
			accountID = id
		}
	}

	// Extract user disbursement details for off-ramp
	var recipientName, nationalID, countryCode, networkCode, networkName string
	if userMap, ok := userData.(map[string]interface{}); ok {
		if v, ok := userMap["full_name"].(*string); ok && v != nil {
			recipientName = *v
		}
		if v, ok := userMap["national_id"].(*string); ok && v != nil {
			nationalID = *v
		}
		if v, ok := userMap["country_code"].(string); ok {
			countryCode = v
		}
		if v, ok := userMap["momo_network_code"].(string); ok {
			networkCode = v
		}
		if v, ok := userMap["momo_network_name"].(string); ok {
			networkName = v
		}
	}

	// Extract local currency data from session
	localAmount, _ := session.Data["loan_amount_local"].(int64)
	localCurrency, _ := session.Data["local_currency"].(string)
	conversionRate, _ := session.Data["conversion_rate"].(float64)

	// Submit loan request
	_, err = h.loanService.RequestLoan(ctx, &LoanRequest{
		UserID:          session.UserID,
		AccountID:       accountID,
		PhoneNumber:     session.PhoneNumber,
		RecipientName:   recipientName,
		NationalID:      nationalID,
		CountryCode:     countryCode,
		NetworkCode:     networkCode,
		NetworkName:     networkName,
		PrincipalAmount: amount,
		PrincipalAsset:  "USDC",
		DurationDays:    duration,
		RepaymentSched:  schedule,
		LocalAmount:     localAmount,
		LocalCurrency:   localCurrency,
		ConversionRate:  conversionRate,
	})

	if err != nil {
		if strings.Contains(err.Error(), "credit score") {
			return h.formatResponse(session.Language, "END", "insufficient_credit"), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	return h.formatResponse(session.Language, "END", "loan_request_submitted"), nil
}

// handleMyLoans shows user's loans
func (h *USSDHandler) handleMyLoans(ctx context.Context, session *Session) (string, error) {
	if h.loanService == nil {
		return h.formatResponse(session.Language, "END", "no_loans"), nil
	}

	loans, err := h.loanService.GetUserLoans(ctx, session.UserID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	if len(loans) == 0 {
		return h.formatResponse(session.Language, "END", "no_loans"), nil
	}

	var response strings.Builder
	response.WriteString("Your Loans:\n")
	for i, loan := range loans {
		if i >= 5 { // Limit to 5 loans for USSD display
			break
		}

		// Extract loan details
		var loanRef = "N/A"
		var status = "Unknown"
		var displayAmount string

		if loanMap, ok := loan.(map[string]interface{}); ok {
			if ref, ok := loanMap["loan_number"].(string); ok {
				loanRef = ref
			}
			if st, ok := loanMap["status"].(string); ok {
				status = st
			}
			// Prefer KES amount if available
			if kesAmt, ok := loanMap["disbursement_amount_kes"].(*int64); ok && kesAmt != nil {
				displayAmount = fmt.Sprintf("KES %.0f", float64(*kesAmt)/100.0)
			} else if amt, ok := loanMap["total_amount"].(int64); ok {
				displayAmount = fmt.Sprintf("KES %.0f", float64(amt)/1e7*153.50)
			}
		}

		response.WriteString(fmt.Sprintf("%d. %s\n%s - %s\n\n", i+1, loanRef, displayAmount, status))
	}

	return h.formatResponse(session.Language, "END", response.String()), nil
}

// handleRepayLoan handles loan repayment
func (h *USSDHandler) handleRepayLoan(ctx context.Context, session *Session, input string) (string, error) {
	if h.loanService == nil {
		return h.formatResponse(session.Language, "END", "no_active_loans"), nil
	}

	// Get active loans
	loans, err := h.loanService.GetUserLoans(ctx, session.UserID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Filter for active/disbursed loans only
	var activeLoans []interface{}
	for _, loan := range loans {
		if loanMap, ok := loan.(map[string]interface{}); ok {
			if status, ok := loanMap["status"].(string); ok {
				if status == "disbursed" || status == "defaulted" {
					activeLoans = append(activeLoans, loan)
				}
			}
		}
	}

	if len(activeLoans) == 0 {
		return h.formatResponse(session.Language, "END", "no_active_loans"), nil
	}

	// Show repayment information
	var response strings.Builder
	response.WriteString("Repay via M-Pesa:\n")
	response.WriteString("PayBill: 123456\n") // TODO: Use actual paybill from config
	response.WriteString("Account: Your Loan Number\n\n")
	response.WriteString("Active Loans:\n")

	for i, loan := range activeLoans {
		if i >= 3 { // Limit to 3 loans
			break
		}

		var loanRef = "N/A"
		var displayAmount string

		if loanMap, ok := loan.(map[string]interface{}); ok {
			if ref, ok := loanMap["loan_number"].(string); ok {
				loanRef = ref
			}
			// Prefer KES repayment amount if available
			if kesAmt, ok := loanMap["repayment_amount_kes"].(*int64); ok && kesAmt != nil {
				displayAmount = fmt.Sprintf("KES %.0f", float64(*kesAmt)/100.0)
			} else if amt, ok := loanMap["total_amount"].(int64); ok {
				displayAmount = fmt.Sprintf("KES %.0f", float64(amt)/1e7*153.50)
			}
		}

		response.WriteString(fmt.Sprintf("Loan: %s\nDue: %s\n\n", loanRef, displayAmount))
	}

	return h.formatResponse(session.Language, "END", response.String()), nil
}

// handleMyAccount handles the account submenu (PIN Manager, Change Language).
func (h *USSDHandler) handleMyAccount(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1": // PIN Manager
		session.CurrentMenu = "pin_manager"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "pin_manager")
	case "2": // Change Language
		session.CurrentMenu = "language_select"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLanguageMenu(session)
	case "0": // Main Menu
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	default:
		return h.showAccountMenu(session)
	}
}

// Menu display helpers

func (h *USSDHandler) showMainMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("main")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showLanguageMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("language_select")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showRegistrationMenu(session *Session) (string, error) {
	session.CurrentMenu = "register"
	menu, _ := h.menuRegistry.Get("register")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showLoanAmountMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("loan_amount")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showLoanDurationMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("loan_duration")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showRepaymentScheduleMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("repayment_schedule")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showAccountMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("my_account")
	return "CON " + menu.Render(session.Language), nil
}

func (h *USSDHandler) showLoanConfirmation(ctx context.Context, session *Session) (string, error) {
	if h.loanService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	localAmountCents, _ := session.Data["loan_amount_local"].(int64)
	duration, _ := session.Data["loan_duration"].(int)

	// Fetch exchange rate
	var buyRate float64
	if h.rateService != nil {
		rate, err := h.rateService.GetExchangeRate(ctx, "KES")
		if err != nil {
			log.Printf("showLoanConfirmation: failed to get exchange rate: %v", err)
			return h.formatError(session.Language, "error"), nil
		}
		buyRate = rate
	} else {
		buyRate = 153.50 // fallback
	}

	// Convert KES to USDC stroops: (KES / rate) * 1e7
	localAmount := float64(localAmountCents) / 100.0
	usdcStroops := int64(localAmount / buyRate * 1e7)

	// Store converted amount and rate in session
	session.Data["loan_amount"] = usdcStroops
	session.Data["conversion_rate"] = buyRate

	// Check eligibility with USDC amount
	approval, err := h.loanService.CheckLoanEligibility(ctx, session.UserID, usdcStroops, duration)
	if err != nil || !approval.Approved {
		reason := "Unknown error"
		if approval != nil {
			reason = approval.Reason
		}
		return h.formatResponse(session.Language, "END", fmt.Sprintf("Loan not approved: %s", reason)), nil
	}

	// Calculate estimated interest in KES
	interestRate := approval.InterestRate * 100
	interestAmountKES := localAmount * approval.InterestRate * float64(duration) / 365.0
	totalKES := localAmount + interestAmountKES

	var response strings.Builder
	response.WriteString("Loan Summary:\n")
	response.WriteString(fmt.Sprintf("Amount: KES %.0f\n", localAmount))
	response.WriteString(fmt.Sprintf("Duration: %d days\n", duration))
	response.WriteString(fmt.Sprintf("Interest: ~%.1f%%\n", interestRate))
	response.WriteString(fmt.Sprintf("Est. Total: ~KES %.0f\n", totalKES))
	response.WriteString(fmt.Sprintf("Rate: 1 USD = %.2f KES\n\n", buyRate))
	response.WriteString("1. Confirm\n0. Cancel")

	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.formatResponse(session.Language, "CON", response.String()), nil
}

// ============================================================================
// PIN Creation Handlers (Registration Flow)
// ============================================================================

// handlePINCreate validates and temporarily stores a new PIN during registration.
func (h *USSDHandler) handlePINCreate(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Validate PIN format/strength before confirming.
	if err := pinPkg.ValidatePIN(input); err != nil {
		return h.formatResponse(session.Language, "CON", "pin_invalid"), nil
	}

	session.Data["new_pin"] = input
	session.CurrentMenu = "pin_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_confirm")
}

// handlePINConfirm compares the confirmed PIN with the stored value, sets it
// via the PIN service, and chains into security question setup.
func (h *USSDHandler) handlePINConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	newPIN, _ := session.Data["new_pin"].(string)
	if input != newPIN {
		// Mismatch — go back to creation.
		delete(session.Data, "new_pin")
		session.CurrentMenu = "pin_create"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "CON", "pin_mismatch"), nil
	}

	// Set PIN.
	if err := h.pinService.SetPIN(ctx, session.UserID, newPIN); err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	delete(session.Data, "new_pin")

	// Chain into security question setup.
	session.CurrentMenu = "security_q1_select"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "security_q1_select")
}

// ============================================================================
// Security Question Handlers
// ============================================================================

// handleSecurityQ1Select stores the selected question ID and prompts for answer.
func (h *USSDHandler) handleSecurityQ1Select(ctx context.Context, session *Session, input string) (string, error) {
	qID, err := strconv.Atoi(input)
	if err != nil || qID < 1 || qID > 5 {
		return h.showMenu(session, "security_q1_select")
	}

	session.Data["sq1_id"] = qID
	session.CurrentMenu = "security_q1_answer"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "security_q1_answer")
}

// handleSecurityQ1Answer stores the first answer and shows question 2 selection.
func (h *USSDHandler) handleSecurityQ1Answer(ctx context.Context, session *Session, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return h.showMenu(session, "security_q1_answer")
	}

	session.Data["sq1_answer"] = input
	session.CurrentMenu = "security_q2_select"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "security_q2_select")
}

// handleSecurityQ2Select stores the second question ID (must differ from Q1).
func (h *USSDHandler) handleSecurityQ2Select(ctx context.Context, session *Session, input string) (string, error) {
	qID, err := strconv.Atoi(input)
	if err != nil || qID < 1 || qID > 5 {
		return h.showMenu(session, "security_q2_select")
	}

	// Must differ from Q1.
	q1ID, _ := session.Data["sq1_id"].(int)
	if qID == q1ID {
		return h.showMenu(session, "security_q2_select")
	}

	session.Data["sq2_id"] = qID
	session.CurrentMenu = "security_q2_answer"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "security_q2_answer")
}

// handleSecurityQ2Answer stores both security questions and completes
// registration (or security question update from PIN manager).
func (h *USSDHandler) handleSecurityQ2Answer(ctx context.Context, session *Session, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		return h.showMenu(session, "security_q2_answer")
	}

	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	q1ID, _ := session.Data["sq1_id"].(int)
	q1Answer, _ := session.Data["sq1_answer"].(string)
	q2ID, _ := session.Data["sq2_id"].(int)

	err := h.pinService.SetSecurityQuestions(ctx, session.UserID, []pinPkg.QuestionAnswer{
		{QuestionID: q1ID, Answer: q1Answer},
		{QuestionID: q2ID, Answer: input},
	})
	if err != nil {
		log.Printf("handleSecurityQ2Answer: failed to save security questions: %v", err)
		return h.formatError(session.Language, "error"), nil
	}

	// Clean up transient data.
	delete(session.Data, "sq1_id")
	delete(session.Data, "sq1_answer")
	delete(session.Data, "sq2_id")

	// Determine if this is registration completion or PIN manager update.
	fromPINManager, _ := session.Data["from_pin_manager"].(bool)
	if fromPINManager {
		delete(session.Data, "from_pin_manager")
		session.CurrentMenu = "pin_manager"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "END", "security_q_success"), nil
	}

	// Registration complete — send welcome SMS.
	fullName, _ := session.Data["full_name"].(string)
	_ = h.accountNotifier.NotifyRegistrationSuccess(ctx, contracts.AccountNotification{
		PhoneNumber: session.PhoneNumber,
		FullName:    fullName,
	})

	return h.formatResponse(session.Language, "END", "registration_complete"), nil
}

// ============================================================================
// PIN Verification Gate Handlers
// ============================================================================

// handlePINVerifyLoan verifies the user's PIN before submitting a loan request.
func (h *USSDHandler) handlePINVerifyLoan(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.submitLoan(ctx, session)
	}

	ok, err := h.pinService.VerifyPIN(ctx, session.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "locked") {
			return h.formatResponse(session.Language, "END", "pin_locked"), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		// Wrong PIN — let user retry (service already sent SMS + incremented attempts).
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return "CON " + msg, nil
	}

	return h.submitLoan(ctx, session)
}

// handlePINVerifyRepay verifies the user's PIN before showing repayment info.
func (h *USSDHandler) handlePINVerifyRepay(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.handleRepayLoan(ctx, session, "")
	}

	ok, err := h.pinService.VerifyPIN(ctx, session.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "locked") {
			return h.formatResponse(session.Language, "END", "pin_locked"), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return "CON " + msg, nil
	}

	session.CurrentMenu = "repay_loan"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.handleRepayLoan(ctx, session, "")
}

// ============================================================================
// PIN Manager Handlers
// ============================================================================

// handlePINManager routes PIN Manager submenu selections.
func (h *USSDHandler) handlePINManager(ctx context.Context, session *Session, input string) (string, error) {
	// Check if account is locked — redirect to recovery.
	if h.pinService != nil {
		locked, _, err := h.pinService.IsLocked(ctx, session.UserID)
		if err == nil && locked {
			session.CurrentMenu = "pin_recovery_national_id"
			if err := h.sessionManager.SaveSession(ctx, session); err != nil {
				return "", fmt.Errorf("failed to save session: %w", err)
			}
			return h.showMenu(session, "pin_recovery_national_id")
		}
	}

	switch input {
	case "1": // Change PIN
		session.CurrentMenu = "pin_change_old"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "pin_change_old")
	case "2": // Security Questions
		session.Data["from_pin_manager"] = true
		session.CurrentMenu = "security_q1_select"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "security_q1_select")
	case "0": // Back
		session.CurrentMenu = "my_account"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showAccountMenu(session)
	default:
		return h.showMenu(session, "pin_manager")
	}
}

// handlePINChangeOld verifies the current PIN before allowing a change.
func (h *USSDHandler) handlePINChangeOld(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	ok, err := h.pinService.VerifyPIN(ctx, session.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "locked") {
			return h.formatResponse(session.Language, "END", "pin_locked"), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return "CON " + msg, nil
	}

	session.Data["old_pin"] = input
	session.CurrentMenu = "pin_change_new"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_change_new")
}

// handlePINChangeNew validates the new PIN and prompts for confirmation.
func (h *USSDHandler) handlePINChangeNew(ctx context.Context, session *Session, input string) (string, error) {
	if err := pinPkg.ValidatePIN(input); err != nil {
		return h.formatResponse(session.Language, "CON", "pin_invalid"), nil
	}

	session.Data["new_pin"] = input
	session.CurrentMenu = "pin_change_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_change_confirm")
}

// handlePINChangeConfirm confirms the new PIN and executes the change.
func (h *USSDHandler) handlePINChangeConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	newPIN, _ := session.Data["new_pin"].(string)
	if input != newPIN {
		delete(session.Data, "new_pin")
		session.CurrentMenu = "pin_change_new"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "CON", "pin_mismatch"), nil
	}

	oldPIN, _ := session.Data["old_pin"].(string)
	if err := h.pinService.ChangePIN(ctx, session.UserID, oldPIN, newPIN); err != nil {
		log.Printf("handlePINChangeConfirm: ChangePIN failed: %v", err)
		return h.formatError(session.Language, "error"), nil
	}

	delete(session.Data, "old_pin")
	delete(session.Data, "new_pin")

	return h.formatResponse(session.Language, "END", "pin_changed"), nil
}

// ============================================================================
// PIN Recovery Handlers
// ============================================================================

// handlePINRecoveryNationalID verifies the user's national ID to begin recovery.
func (h *USSDHandler) handlePINRecoveryNationalID(ctx context.Context, session *Session, input string) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Look up user and check national ID matches.
	user, _, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	var storedNationalID string
	if userMap, ok := user.(map[string]interface{}); ok {
		if v, ok := userMap["national_id"].(*string); ok && v != nil {
			storedNationalID = *v
		}
	}

	if storedNationalID == "" || storedNationalID != input {
		return h.formatResponse(session.Language, "END", "recovery_id_wrong"), nil
	}

	// Get user's security question IDs for prompting.
	if h.pinService != nil {
		qIDs, err := h.pinService.GetUserQuestionIDs(ctx, session.UserID)
		if err == nil && len(qIDs) >= 2 {
			session.Data["recovery_q1_id"] = qIDs[0]
			session.Data["recovery_q2_id"] = qIDs[1]
		}
	}

	session.CurrentMenu = "pin_recovery_q1"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	// Show the specific question text.
	q1ID, _ := session.Data["recovery_q1_id"].(int)
	qKey := fmt.Sprintf("sq_%d", q1ID)
	qText := GetLocalizedMessage(session.Language, qKey)
	return fmt.Sprintf("CON %s", qText), nil
}

// handlePINRecoveryQ1 verifies the answer to the first security question.
func (h *USSDHandler) handlePINRecoveryQ1(ctx context.Context, session *Session, input string) (string, error) {
	session.Data["recovery_a1"] = input
	session.CurrentMenu = "pin_recovery_q2"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	q2ID, _ := session.Data["recovery_q2_id"].(int)
	qKey := fmt.Sprintf("sq_%d", q2ID)
	qText := GetLocalizedMessage(session.Language, qKey)
	return fmt.Sprintf("CON %s", qText), nil
}

// handlePINRecoveryQ2 verifies both security answers and proceeds to new PIN.
func (h *USSDHandler) handlePINRecoveryQ2(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	q1ID, _ := session.Data["recovery_q1_id"].(int)
	a1, _ := session.Data["recovery_a1"].(string)
	q2ID, _ := session.Data["recovery_q2_id"].(int)

	ok, err := h.pinService.VerifySecurityAnswers(ctx, session.UserID, []pinPkg.QuestionAnswer{
		{QuestionID: q1ID, Answer: a1},
		{QuestionID: q2ID, Answer: input},
	})
	if err != nil || !ok {
		return h.formatResponse(session.Language, "END", "recovery_answers_wrong"), nil
	}

	// Answers verified — proceed to new PIN.
	delete(session.Data, "recovery_a1")
	session.CurrentMenu = "pin_recovery_new"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_recovery_new")
}

// handlePINRecoveryNew validates the new PIN during recovery.
func (h *USSDHandler) handlePINRecoveryNew(ctx context.Context, session *Session, input string) (string, error) {
	if err := pinPkg.ValidatePIN(input); err != nil {
		return h.formatResponse(session.Language, "CON", "pin_invalid"), nil
	}

	session.Data["recovery_new_pin"] = input
	session.CurrentMenu = "pin_recovery_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_recovery_confirm")
}

// handlePINRecoveryConfirm confirms and resets the PIN.
func (h *USSDHandler) handlePINRecoveryConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	newPIN, _ := session.Data["recovery_new_pin"].(string)
	if input != newPIN {
		delete(session.Data, "recovery_new_pin")
		session.CurrentMenu = "pin_recovery_new"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "CON", "pin_mismatch"), nil
	}

	if err := h.pinService.ResetPIN(ctx, session.UserID, newPIN); err != nil {
		log.Printf("handlePINRecoveryConfirm: ResetPIN failed: %v", err)
		return h.formatError(session.Language, "error"), nil
	}

	// Clean up recovery data.
	delete(session.Data, "recovery_new_pin")
	delete(session.Data, "recovery_q1_id")
	delete(session.Data, "recovery_q2_id")

	return h.formatResponse(session.Language, "END", "recovery_success"), nil
}

// ============================================================================
// Helper Methods
// ============================================================================

// showMenu is a generic helper that renders a registered menu by ID.
func (h *USSDHandler) showMenu(session *Session, menuID string) (string, error) {
	menu, err := h.menuRegistry.Get(menuID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}
	return "CON " + menu.Render(session.Language), nil
}

// getRemainingAttempts returns how many PIN attempts the user has left. If the
// PIN service is unavailable or the lookup fails, it returns 0 as a safe
// fallback.
func (h *USSDHandler) getRemainingAttempts(ctx context.Context, userID string) int {
	if h.pinService == nil {
		return 0
	}
	// The PIN service's VerifyPIN already handles attempt tracking. We
	// approximate remaining attempts by checking lock status.
	locked, _, err := h.pinService.IsLocked(ctx, userID)
	if err != nil || locked {
		return 0
	}
	// Default remaining attempts — the exact count is managed by the service.
	return pinPkg.MaxPINAttempts - 1
}

// formatResponse formats a response with type prefix
func (h *USSDHandler) formatResponse(language, responseType, message string) string {
	return fmt.Sprintf("%s %s", responseType, GetLocalizedMessage(language, message))
}

// formatError formats an error response
func (h *USSDHandler) formatError(language, errorKey string) string {
	return fmt.Sprintf("END %s", GetLocalizedMessage(language, errorKey))
}
