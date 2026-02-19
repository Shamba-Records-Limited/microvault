package ussd

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
)

// NewUSSDHandler creates a new USSD handler
func NewUSSDHandler(
	sessionManager *SessionManager,
	menuRegistry *MenuRegistry,
	userService UserService,
	loanService LoanService,
	rateService RateService,
) *USSDHandler {
	return &USSDHandler{
		sessionManager: sessionManager,
		menuRegistry:   menuRegistry,
		userService:    userService,
		loanService:    loanService,
		rateService:    rateService,
	}
}

// HandleRequest handles a USSD request
func (h *USSDHandler) HandleRequest(ctx context.Context, sessionID, phoneNumber, serviceCode, networkCode, input string) (string, error) {
	// Get or create session
	session, err := h.sessionManager.GetOrCreateSession(ctx, sessionID, phoneNumber, serviceCode, networkCode)
	if err != nil {
		return h.formatError("en", "session_expired"), nil
	}

	log.Printf("USSD Session - ID: %s, ServiceCode%s, NetworkCode: %s, Phone: %s, CurrentMenu: %s, Input: %q",
		sessionID, serviceCode, networkCode, phoneNumber, session.CurrentMenu, input)

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

	// User registered, associate with session and show main menu
	// Extract user ID (assuming user has an ID field)
	if userMap, ok := user.(map[string]interface{}); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMainMenu(session)
}

// handleMenuInput handles user input for current menu
func (h *USSDHandler) handleMenuInput(ctx context.Context, session *Session, input string) (string, error) {
	input = strings.TrimSpace(input)

	switch session.CurrentMenu {
	case "main":
		return h.handleMainMenu(ctx, session, input)
	case "language_select":
		return h.handleLanguageSelect(ctx, session, input)
	case "register":
		return h.handleRegistration(ctx, session, input)
	case "register_national_id":
		return h.handleRegistrationNationalID(ctx, session, input)
	case "check_balance":
		return h.handleCheckBalance(ctx, session)
	case "request_loan":
		return h.handleLoanAmount(ctx, session, input)
	case "loan_amount":
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
	case "my_account":
		return h.handleMyAccount(ctx, session, input)
	case "credit_score":
		return h.handleCreditScore(ctx, session)
	default:
		return h.showMainMenu(session)
	}
}

// handleMainMenu handles main menu selection
func (h *USSDHandler) handleMainMenu(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1": // Check Balance
		session.CurrentMenu = "check_balance"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.handleCheckBalance(ctx, session)
	case "2": // Request Loan
		session.CurrentMenu = "request_loan"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLoanAmountMenu(session)
	case "3": // My Loans
		session.CurrentMenu = "my_loans"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.handleMyLoans(ctx, session)
	case "4": // Repay Loan
		session.CurrentMenu = "repay_loan"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.handleRepayLoan(ctx, session, "")
	case "5": // My Account
		session.CurrentMenu = "my_account"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showAccountMenu(session)
	case "9": // Change Language
		session.CurrentMenu = "language_select"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLanguageMenu(session)
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
	log.Printf("handleRegistration called - Input: %q, SessionID: %s", input, session.SessionID)

	// Store name
	session.Data["full_name"] = input

	// Ask for national ID
	session.CurrentMenu = "register_national_id"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	log.Printf("Registration - stored full_name: %q, updated CurrentMenu to: register_national_id", input)

	return h.formatResponse(session.Language, "CON", "Enter your national ID:"), nil
}

// handleRegistrationNationalID completes registration
func (h *USSDHandler) handleRegistrationNationalID(ctx context.Context, session *Session, input string) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	fullName, _ := session.Data["full_name"].(string)

	// Register user
	user, _, err := h.userService.RegisterUser(ctx, &RegisterUserRequest{
		MobileNumber:      session.PhoneNumber,
		NetworkCode:       session.NetworkCode,
		FullName:          fullName,
		NationalID:        input,
		PreferredLanguage: session.Language,
	})

	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Associate user with session
	if userMap, ok := user.(map[string]interface{}); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}
	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.formatResponse(session.Language, "END", "registration_success"), nil
}

// handleCheckBalance shows account balance
func (h *USSDHandler) handleCheckBalance(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	_, accounts, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil || len(accounts) == 0 {
		return h.formatError(session.Language, "error"), nil
	}

	var response strings.Builder
	response.WriteString(GetLocalizedMessage(session.Language, "balance"))
	response.WriteString("\n\n")

	for i, account := range accounts {
		// Try to extract balance from account (assuming it's a map or struct)
		var balance float64
		var asset = "USD"

		if accMap, ok := account.(map[string]interface{}); ok {
			if bal, ok := accMap["balance"].(int64); ok {
				balance = float64(bal) / 1000000 // Convert from stroops
			}
			if assetStr, ok := accMap["asset"].(string); ok {
				asset = assetStr
			}
		}

		response.WriteString(fmt.Sprintf("Account %d: %.2f %s\n", i+1, balance, asset))
	}

	return h.formatResponse(session.Language, "END", response.String()), nil
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

// handleLoanConfirm handles loan confirmation
func (h *USSDHandler) handleLoanConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if input != "1" {
		return h.showMainMenu(session)
	}

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

// handleMyAccount handles account menu
func (h *USSDHandler) handleMyAccount(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1": // View Profile
		return h.handleViewProfile(ctx, session)
	case "2": // Credit Score
		return h.handleCreditScore(ctx, session)
	case "0": // Main Menu
		return h.showMainMenu(session)
	default:
		return h.showAccountMenu(session)
	}
}

// handleViewProfile shows user profile
func (h *USSDHandler) handleViewProfile(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	user, _, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	var response strings.Builder
	response.WriteString("Profile:\n")

	// Extract user details
	if userMap, ok := user.(map[string]interface{}); ok {
		if name, ok := userMap["full_name"].(string); ok {
			response.WriteString(fmt.Sprintf("Name: %s\n", name))
		}
		if phone, ok := userMap["mobile_number"].(string); ok {
			response.WriteString(fmt.Sprintf("Phone: %s\n", phone))
		}
		if kyc, ok := userMap["kyc_status"].(string); ok {
			response.WriteString(fmt.Sprintf("KYC: %s\n", kyc))
		}
		if status, ok := userMap["status"].(string); ok {
			response.WriteString(fmt.Sprintf("Status: %s", status))
		}
	}

	return h.formatResponse(session.Language, "END", response.String()), nil
}

// handleCreditScore shows credit score
func (h *USSDHandler) handleCreditScore(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil || h.loanService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	user, _, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil {
		return h.formatError(session.Language, "error"), nil
	}

	var response strings.Builder
	response.WriteString("Credit Info:\n")

	// Extract KYC status
	if userMap, ok := user.(map[string]interface{}); ok {
		if kyc, ok := userMap["kyc_status"].(string); ok {
			response.WriteString(fmt.Sprintf("KYC Status: %s\n", kyc))
		}
	}

	// Get loan history summary
	loans, err := h.loanService.GetUserLoans(ctx, session.UserID)
	if err == nil {
		totalLoans := len(loans)
		repaidCount := 0

		for _, loan := range loans {
			if loanMap, ok := loan.(map[string]interface{}); ok {
				if status, ok := loanMap["status"].(string); ok {
					if status == "repaid" {
						repaidCount++
					}
				}
			}
		}

		response.WriteString(fmt.Sprintf("Total Loans: %d\n", totalLoans))
		response.WriteString(fmt.Sprintf("Repaid: %d\n", repaidCount))

		if totalLoans > 0 {
			repaymentRate := float64(repaidCount) / float64(totalLoans) * 100
			response.WriteString(fmt.Sprintf("Repayment Rate: %.1f%%", repaymentRate))
		}
	}

	return h.formatResponse(session.Language, "END", response.String()), nil
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

// formatResponse formats a response with type prefix
func (h *USSDHandler) formatResponse(language, responseType, message string) string {
	return fmt.Sprintf("%s %s", responseType, GetLocalizedMessage(language, message))
}

// formatError formats an error response
func (h *USSDHandler) formatError(language, errorKey string) string {
	return fmt.Sprintf("END %s", GetLocalizedMessage(language, errorKey))
}
