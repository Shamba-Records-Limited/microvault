// Package ussd provides the core USSD gateway handler and request routing.
package ussd

import (
	"context"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

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
// e.g. "254799334972" to "254799XXX972", "+254799334972" to "+254799XXX972"
func redactPhone(phone string) string {
	digits := phone
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 9 {
		return phone // too short to redact meaningfully
	}
	// Keep first 6 (country + network) and last 3 digits, mask the rest
	masked := digits[:6] + strings.Repeat("X", len(digits)-9) + digits[len(digits)-3:]
	return prefix + masked
}

// toInt extracts an int from a session data value. JSON (Redis) round-trips
// decode numbers as float64, so this handles both int and float64.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}

// toInt64 extracts an int64 from a session data value, handling the same
// JSON float64 round-trip issue as toInt.
func toInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case float64:
		return int64(n)
	case int:
		return int64(n)
	default:
		return 0
	}
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
	if userMap, ok := user.(map[string]any); ok {
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
	case "payout_method":
		return h.handlePayoutMethod(ctx, session, input)
	case "loan_birthdate":
		return h.handleLoanBirthdate(ctx, session, input)
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
	if userMap, ok := user.(map[string]any); ok {
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

// handleLoanAmount validates the requested amount against the active loan
// product's fiat limits, stores product defaults (duration, schedule), and
// skips directly to the confirmation screen.
func (h *USSDHandler) handleLoanAmount(ctx context.Context, session *Session, input string) (string, error) {
	cfg := h.loanService.GetProductConfig()
	if cfg == nil {
		return h.formatError(session.Language, "error"), nil
	}

	amount, err := strconv.ParseFloat(input, 64)
	if err != nil || amount <= 0 {
		return h.showLoanAmountMenu(session)
	}
	amountCents := int64(amount * 100)

	if amountCents < cfg.MinAmountCents {
		return fmt.Sprintf("END Minimum loan amount is %s %.0f",
			cfg.Currency, float64(cfg.MinAmountCents)/100), nil
	}
	if amountCents > cfg.MaxAmountCents {
		return fmt.Sprintf("END The amount requested exceeds the auto-approved limit of %s %.0f",
			cfg.Currency, float64(cfg.MaxAmountCents)/100), nil
	}

	session.Data["loan_amount_local"] = amountCents
	session.Data["local_currency"] = cfg.Currency
	session.Data["loan_duration"] = cfg.DurationDays
	session.Data["repayment_schedule"] = cfg.RepaymentSchedule
	session.Data["product_id"] = cfg.ProductID
	session.CurrentMenu = "payout_method"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "payout_method")
}

// handlePayoutMethod stores the chosen disbursement rail and either advances
// straight to confirmation (mobile money) or asks for the birth date that
// MoneyGram needs for SEP-9 KYC prefill (cash pickup).
func (h *USSDHandler) handlePayoutMethod(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1":
		session.Data["payout_method"] = "cash_pickup"
		session.CurrentMenu = "loan_birthdate"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "loan_birthdate")
	case "2":
		session.Data["payout_method"] = "mobile_money"
		session.CurrentMenu = "loan_confirm"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLoanConfirmation(ctx, session)
	default:
		return h.showMenu(session, "payout_method")
	}
}

// handleLoanBirthdate validates the SEP-9 birth date (YYYY-MM-DD) and routes
// to the confirmation screen. Cash-pickup only.
func (h *USSDHandler) handleLoanBirthdate(ctx context.Context, session *Session, input string) (string, error) {
	if !isISODate(input) {
		return h.formatResponse(session.Language, "CON",
			"Invalid format. Enter date of birth as YYYY-MM-DD:"), nil
	}
	session.Data["birth_date"] = input
	session.CurrentMenu = "loan_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.showLoanConfirmation(ctx, session)
}

// isISODate is a light YYYY-MM-DD shape check — full validity is enforced by
// MoneyGram inside the webview, so we only catch obvious typos here.
func isISODate(s string) bool {
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i, r := range s {
		if i == 4 || i == 7 {
			continue
		}
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
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
// have passed. KEStoUSDC conversion happens in the adapter's RequestLoan method,
// so this function passes the fiat amount and lets the adapter handle conversion.
func (h *USSDHandler) submitLoan(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil || h.loanService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	// Get loan details from session.
	duration := toInt(session.Data["loan_duration"])
	schedule, _ := session.Data["repayment_schedule"].(string)
	productID, _ := session.Data["product_id"].(string)

	// Fetch exchange rate for KES to USDC conversion.
	localAmount := toInt64(session.Data["loan_amount_local"])
	localCurrency, _ := session.Data["local_currency"].(string)

	var buyRate float64
	if h.rateService != nil {
		rate, err := h.rateService.GetExchangeRate(ctx, localCurrency)
		if err != nil {
			log.Printf("submitLoan: failed to get exchange rate: %v", err)
			return h.formatError(session.Language, "error"), nil
		}
		buyRate = rate
	} else {
		buyRate = 153.50 // fallback
	}

	// Convert local currency to USDC stroops: (fiat / rate) * 1e7
	fiatAmount := float64(localAmount) / 100.0
	usdcStroops := int64(fiatAmount / buyRate * 1e7)

	// Get user and first account.
	userData, accounts, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil || len(accounts) == 0 {
		return h.formatError(session.Language, "error"), nil
	}

	var accountID, stellarAddress string
	var accountIndex uint32
	if accMap, ok := accounts[0].(map[string]any); ok {
		if id, ok := accMap["id"].(string); ok {
			accountID = id
		}
		if pk, ok := accMap["public_key"].(string); ok {
			stellarAddress = pk
		}
		// account_index is the BIP-44 derivation index used to derive this
		// Stellar account from the treasury seed. Reuse it as the MG SEP-10
		// child-account index so the memo is stable across restarts.
		if v, ok := accMap["account_index"].(int); ok && v >= 0 {
			accountIndex = uint32(v)
		}
	}

	// Extract user disbursement details for off-ramp.
	var recipientName, nationalID, countryCode, networkCode, networkName string
	if userMap, ok := userData.(map[string]any); ok {
		if v, ok := userMap["full_name"].(string); ok {
			recipientName = v
		}
		if v, ok := userMap["national_id"].(string); ok {
			nationalID = v
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

	// Fire off the disbursement pipeline asynchronously so the USSD session
	// ends immediately. The user is notified via SMS on success or failure.
	payoutMethod, _ := session.Data["payout_method"].(string)
	birthDate, _ := session.Data["birth_date"].(string)

	loanReq := &LoanRequest{
		UserID:          session.UserID,
		AccountID:       accountID,
		StellarAddress:  stellarAddress,
		ProductID:       productID,
		PhoneNumber:     session.PhoneNumber,
		RecipientName:   recipientName,
		NationalID:      nationalID,
		CountryCode:     countryCode,
		NetworkCode:     networkCode,
		NetworkName:     networkName,
		PrincipalAmount: usdcStroops,
		PrincipalAsset:  "USDC",
		DurationDays:    duration,
		RepaymentSched:  schedule,
		LocalAmount:     localAmount,
		LocalCurrency:   localCurrency,
		ConversionRate:  buyRate,
		PayoutMethod:    payoutMethod,
		BirthDate:       birthDate,
	}
	// Cash-pickup needs the per-user Stellar derivation index so the MG
	// poller can re-derive the SEP-10 child memo on restart. This is the
	// real BIP-44 account_index from the accounts row, not a hash.
	if payoutMethod == "cash_pickup" {
		loanReq.ChildAccountIndex = accountIndex
	}
	go func() {
		// Use a detached context so the pipeline isn't cancelled when the
		// USSD request context expires.
		bgCtx := context.Background()
		if _, err := h.loanService.RequestLoan(bgCtx, loanReq); err != nil {
			log.Printf("async loan disbursement failed: user=%s error=%v", loanReq.UserID, err)
		}
	}()

	localKES := float64(localAmount) / 100
	return fmt.Sprintf("END Your loan of %s %.0f is being processed. You will receive a notification when disbursement is successful.",
		localCurrency, localKES), nil
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

		if loanMap, ok := loan.(map[string]any); ok {
			if ref, ok := loanMap["loan_reference"].(*string); ok && ref != nil {
				loanRef = *ref
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
	var activeLoans []any
	for _, loan := range loans {
		if loanMap, ok := loan.(map[string]any); ok {
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

		if loanMap, ok := loan.(map[string]any); ok {
			if ref, ok := loanMap["loan_reference"].(*string); ok && ref != nil {
				loanRef = *ref
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
	cfg := h.loanService.GetProductConfig()
	if cfg == nil {
		return h.formatError(session.Language, "error"), nil
	}
	minFiat := float64(cfg.MinAmountCents) / 100
	maxFiat := float64(cfg.MaxAmountCents) / 100
	return fmt.Sprintf("CON Enter amount to borrow in %s (min %.0f, max %.0f):",
		cfg.Currency, minFiat, maxFiat), nil
}

func (h *USSDHandler) showAccountMenu(session *Session) (string, error) {
	menu, _ := h.menuRegistry.Get("my_account")
	return "CON " + menu.Render(session.Language), nil
}

// showLoanConfirmation displays a summary with principal amount and duration
// only. APR, estimated total, and exchange rate are intentionally omitted —
// the repayment total is computed later and the APR is dynamic.
func (h *USSDHandler) showLoanConfirmation(_ context.Context, session *Session) (string, error) {
	cfg := h.loanService.GetProductConfig()
	if cfg == nil {
		return h.formatError(session.Language, "error"), nil
	}

	localAmountCents := toInt64(session.Data["loan_amount_local"])
	localAmount := float64(localAmountCents) / 100
	duration := cfg.DurationDays

	summary := fmt.Sprintf("Loan of %s %.0f for %d days\n\n1. Confirm\n0. Cancel",
		cfg.Currency, localAmount, duration)
	return "CON " + summary, nil
}

//
// # PIN Creation Handlers (Registration Flow)
//

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

//
// # Security Question Handlers
//

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
	q1ID := toInt(session.Data["sq1_id"])
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

	q1ID := toInt(session.Data["sq1_id"])
	q1Answer, _ := session.Data["sq1_answer"].(string)
	q2ID := toInt(session.Data["sq2_id"])

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

//
// # PIN Verification Gate Handlers
//

// handlePINVerifyLoan verifies the user's PIN before submitting a loan request.
func (h *USSDHandler) handlePINVerifyLoan(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.submitLoan(ctx, session)
	}

	ok, err := h.pinService.VerifyPIN(ctx, session.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "locked") {
			return h.formatLockedMessage(ctx, session), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		// Wrong PIN — let user retry (service already sent SMS + incremented attempts).
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
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
			return h.formatLockedMessage(ctx, session), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return "CON " + msg, nil
	}

	session.CurrentMenu = "repay_loan"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.handleRepayLoan(ctx, session, "")
}

//
// # PIN Manager Handlers
//

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
			return h.formatLockedMessage(ctx, session), nil
		}
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
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

//
// # PIN Recovery Handlers
//

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
	if userMap, ok := user.(map[string]any); ok {
		if v, ok := userMap["national_id"].(string); ok {
			storedNationalID = v
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

	// Bail out if no security questions were stored.
	if session.Data["recovery_q1_id"] == nil || session.Data["recovery_q2_id"] == nil {
		return h.formatResponse(session.Language, "END", "recovery_no_questions"), nil
	}

	session.CurrentMenu = "pin_recovery_q1"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	// Show the specific question text.
	q1ID := toInt(session.Data["recovery_q1_id"])
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

	q2ID := toInt(session.Data["recovery_q2_id"])
	qKey := fmt.Sprintf("sq_%d", q2ID)
	qText := GetLocalizedMessage(session.Language, qKey)
	return fmt.Sprintf("CON %s", qText), nil
}

// handlePINRecoveryQ2 verifies both security answers and proceeds to new PIN.
func (h *USSDHandler) handlePINRecoveryQ2(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	q1ID := toInt(session.Data["recovery_q1_id"])
	a1, _ := session.Data["recovery_a1"].(string)
	q2ID := toInt(session.Data["recovery_q2_id"])

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

//
// # Helper Methods
//

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
	remaining, err := h.pinService.GetRemainingAttempts(ctx, userID)
	if err != nil {
		return 0
	}
	return remaining
}

// formatLockedMessage formats the pin_locked message with the actual lockout expiry time.
func (h *USSDHandler) formatLockedMessage(ctx context.Context, session *Session) string {
	lockedUntil := "a few minutes"
	if h.pinService != nil {
		if locked, until, err := h.pinService.IsLocked(ctx, session.UserID); err == nil && locked {
			remaining := time.Until(until).Round(time.Minute)
			if remaining <= 0 {
				lockedUntil = "less than a minute"
			} else if remaining < time.Hour {
				mins := int(remaining.Minutes())
				if mins == 1 {
					lockedUntil = "1 minute"
				} else {
					lockedUntil = fmt.Sprintf("%d minutes", mins)
				}
			} else {
				lockedUntil = fmt.Sprintf("%d minutes", int(remaining.Minutes()))
			}
		}
	}
	msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_locked"), lockedUntil)
	return "END " + msg
}

// formatResponse formats a response with type prefix
func (h *USSDHandler) formatResponse(language, responseType, message string) string {
	return fmt.Sprintf("%s %s", responseType, GetLocalizedMessage(language, message))
}

// formatError formats an error response
func (h *USSDHandler) formatError(language, errorKey string) string {
	return fmt.Sprintf("END %s", GetLocalizedMessage(language, errorKey))
}
