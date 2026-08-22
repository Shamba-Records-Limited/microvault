package ussd

import (
	"context"
	"fmt"
	"log"
	"math"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/moneygram"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
	pinPkg "github.com/Shamba-Records-Limited/microvault/pkg/pin"
)

// menus lists all menus including menus where user input contains PII (PINs, national IDs, names, birth dates, addresses).
var menus = map[string]bool{
	"register":                 true, // full name
	"register_national_id":     true, // national ID
	"bio_edit":                 true, // SEP-9 detail value (birth date, address, city, postal code)
	"pin_create":               true,
	"pin_confirm":              true,
	"loan_confirm":             true, // PIN — the loan terms and the PIN gate share one screen
	"pin_verify_repay":         true,
	"pin_change_old":           true,
	"pin_change_new":           true,
	"pin_change_confirm":       true,
	"pin_recovery_national_id": true,
	"pin_recovery_q1":          true, // security answer
	"pin_recovery_q2":          true, // security answer
	"recover_sim_q1":           true, // security answer (new-SIM recovery)
	"recover_sim_q2":           true, // security answer (new-SIM recovery)
	"pin_recovery_new":         true,
	"pin_recovery_confirm":     true,
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
	if menus[menu] {
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
	loanNotifier contracts.LoanNotifier,
) *USSDHandler {
	if accountNotifier == nil {
		accountNotifier = &notifications.NoOpAccountNotifier{}
	}
	if loanNotifier == nil {
		loanNotifier = &notifications.NoOpLoanNotifier{}
	}
	return &USSDHandler{
		sessionManager:  sessionManager,
		menuRegistry:    menuRegistry,
		userService:     userService,
		loanService:     loanService,
		rateService:     rateService,
		pinService:      pinService,
		accountNotifier: accountNotifier,
		loanNotifier:    loanNotifier,
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
		sessionID, serviceCode, networkCode, phone.Redact(phoneNumber), session.CurrentMenu, safeInput(session.CurrentMenu, input))

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
		// New user — choose language first, then register in that language.
		session.CurrentMenu = "language_select"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			log.Printf("ERROR: Failed to save session before language select: %v", err)
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showLanguageMenu(session)
	}

	// User registered, associate with session and show main menu.
	// Always reset to main menu to prevent stale CurrentMenu from a
	// previous session with the same ID (AT retries, etc.).
	if userMap, ok := user.(map[string]any); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}

	// Self-heal: a registered user with no PIN can't use the account and would
	// otherwise be silently blocked at every PIN gate. The atomic-insert flow
	// makes this impossible going forward, but guard against legacy/imported or
	// manually-created rows by routing them to set a PIN.
	if h.pinService != nil && session.UserID != "" {
		if hasPIN, err := h.pinService.HasPIN(ctx, session.UserID); err == nil && !hasPIN {
			session.Data["set_pin_only"] = true
			session.CurrentMenu = "pin_create"
			if err := h.sessionManager.SaveSession(ctx, session); err != nil {
				return "", fmt.Errorf("failed to save session: %w", err)
			}
			return h.showMenu(session, "pin_create")
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

	// Global back/home navigation is resolved before the per-menu handlers so
	// no handler has to implement it. Menus that bind "0" themselves, or where
	// stepping back would weaken a verification gate, are excluded by
	// navBackTargets and fall through untouched.
	if resp, handled, err := h.handleNavigation(ctx, session, input); handled {
		return resp, err
	}

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
	case "my_details":
		return h.handleMyDetails(ctx, session, input)
	case "bio_edit":
		return h.handleBioEdit(ctx, session, input)

	// New-SIM account recovery (reached from the duplicate-national-ID case)
	case "recover_offer":
		return h.handleRecoverOffer(ctx, session, input)
	case "recover_sim_q1":
		return h.handleRecoverSimQ1(ctx, session, input)
	case "recover_sim_q2":
		return h.handleRecoverSimQ2(ctx, session, input)

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
		return h.showAccountMenu(ctx, session)
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
		// Back: unregistered users must still pick a language before registering.
		if session.UserID == "" {
			return h.showLanguageMenu(session)
		}
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	default:
		return h.showLanguageMenu(session)
	}

	session.Language = language

	// New (unregistered) users continue into registration in the chosen
	// language; registered users return to the main menu.
	if session.UserID == "" {
		response, err := h.showRegistrationMenu(session)
		if err != nil {
			return "", err
		}
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return response, nil
	}

	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMainMenu(session)
}

// handleRegistration handles user registration
func (h *USSDHandler) handleRegistration(ctx context.Context, session *Session, input string) (string, error) {
	log.Printf("handleRegistration called - SessionID: %s", session.SessionID)

	// Store name (required).
	fullName := strings.TrimSpace(input)
	if fullName == "" {
		return h.conNav(session, "reg_name_required"), nil
	}
	session.Data["full_name"] = fullName

	// Ask for national ID
	session.CurrentMenu = "register_national_id"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	log.Printf("Registration - stored full_name, updated CurrentMenu to: register_national_id")

	return h.conWithNav(session, "register_national_id", "reg_enter_national_id"), nil
}

// handleRegistrationNationalID stores the (required) national ID and advances
// straight to PIN creation. Bio and security questions are deferred to the
// account menu (see ussd-registration-redesign) to keep the flow inside the
// USSD session budget. Nothing is persisted until the PIN is confirmed, so a
// dropped session leaves no half-registered account.
func (h *USSDHandler) handleRegistrationNationalID(ctx context.Context, session *Session, input string) (string, error) {
	nationalID := strings.TrimSpace(input)
	if nationalID == "" {
		return h.conNav(session, "reg_national_id_required"), nil
	}

	// Reject an already-registered national ID up front, before the user spends
	// the rest of the session on a PIN. Re-prompt (CON) so a mistyped digit can
	// be corrected. The DB unique constraint remains the real guard.
	if taken, err := h.userService.NationalIDExists(ctx, nationalID); err != nil {
		log.Printf("handleRegistrationNationalID: national ID check failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	} else if taken {
		// Usually this is the same person on a new SIM (lost phone), so offer
		// account recovery rather than dead-ending. Ownership is verified in
		// the recovery flow before anything is rebound.
		session.Data["recover_national_id"] = nationalID
		session.CurrentMenu = "recover_offer"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "CON", "recover_offer"), nil
	}

	session.Data["national_id"] = nationalID

	// No PIN service configured — create the account immediately (no PIN).
	if h.pinService == nil {
		return h.completeRegistration(ctx, session)
	}

	session.CurrentMenu = "pin_create"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.showMenu(session, "pin_create")
}

// handleRecoverOffer handles the choice presented when registration hits an
// already-registered national ID: recover the existing account onto this SIM,
// or re-enter the ID (in case of a typo).
//
// Recovery is gated on security questions. The registered SIM is gone, so the
// possession factor that backs a same-SIM PIN reset is absent; national ID
// alone is semi-public and far too weak to transfer an account holding loans.
// Without security questions this must go to a human.
func (h *USSDHandler) handleRecoverOffer(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1": // Recover this account on this phone
		nationalID, _ := session.Data["recover_national_id"].(string)
		userID, err := h.userService.GetUserIDByNationalID(ctx, nationalID)
		if err != nil {
			log.Printf("handleRecoverOffer: lookup by national ID failed for %s: %v", phone.Redact(session.PhoneNumber), err)
			return h.formatError(session.Language, "error"), nil
		}
		if userID == "" {
			// The ID was taken a moment ago but resolves to nothing now — treat
			// as a typo and let them re-enter.
			session.CurrentMenu = "register_national_id"
			if err := h.sessionManager.SaveSession(ctx, session); err != nil {
				return "", fmt.Errorf("failed to save session: %w", err)
			}
			return h.formatResponse(session.Language, "CON", "reg_enter_national_id"), nil
		}

		if h.pinService == nil {
			return h.formatResponse(session.Language, "END", "recover_contact_support"), nil
		}
		qIDs, err := h.pinService.GetUserQuestionIDs(ctx, userID)
		if err != nil {
			log.Printf("handleRecoverOffer: GetUserQuestionIDs failed for %s: %v", phone.Redact(session.PhoneNumber), err)
			return h.formatError(session.Language, "error"), nil
		}
		if len(qIDs) < 2 {
			// No knowledge factor available — cannot self-serve an account move.
			return h.formatResponse(session.Language, "END", "recover_contact_support"), nil
		}

		session.Data["recover_user_id"] = userID
		session.Data["recover_sq1_id"] = qIDs[0]
		session.Data["recover_sq2_id"] = qIDs[1]
		session.CurrentMenu = "recover_sim_q1"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return "CON " + GetLocalizedMessage(session.Language, fmt.Sprintf("sq_%d", qIDs[0])), nil

	case "2": // Re-enter the national ID
		delete(session.Data, "recover_national_id")
		session.CurrentMenu = "register_national_id"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "CON", "reg_enter_national_id"), nil

	default:
		return h.formatResponse(session.Language, "CON", "recover_offer"), nil
	}
}

// handleRecoverSimQ1 stores the first security answer and prompts for the second.
func (h *USSDHandler) handleRecoverSimQ1(ctx context.Context, session *Session, input string) (string, error) {
	if strings.TrimSpace(input) == "" {
		q1ID := toInt(session.Data["recover_sq1_id"])
		return "CON " + GetLocalizedMessage(session.Language, fmt.Sprintf("sq_%d", q1ID)), nil
	}

	session.Data["recover_sq1_answer"] = input
	session.CurrentMenu = "recover_sim_q2"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	q2ID := toInt(session.Data["recover_sq2_id"])
	return "CON " + GetLocalizedMessage(session.Language, fmt.Sprintf("sq_%d", q2ID)), nil
}

// handleRecoverSimQ2 verifies both answers and, on success, rebinds the account
// to the dialing SIM. The user must still sign in with their existing PIN — the
// rebind moves the account, it does not reset credentials.
func (h *USSDHandler) handleRecoverSimQ2(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	userID, _ := session.Data["recover_user_id"].(string)
	q1ID := toInt(session.Data["recover_sq1_id"])
	a1, _ := session.Data["recover_sq1_answer"].(string)
	q2ID := toInt(session.Data["recover_sq2_id"])

	ok, err := h.pinService.VerifySecurityAnswers(ctx, userID, []pinPkg.QuestionAnswer{
		{QuestionID: q1ID, Answer: a1},
		{QuestionID: q2ID, Answer: input},
	})
	if err != nil {
		log.Printf("handleRecoverSimQ2: VerifySecurityAnswers failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}
	if !ok {
		h.clearRecoverySession(session)
		return h.formatResponse(session.Language, "END", "recovery_answers_wrong"), nil
	}

	if err := h.userService.RebindMobileNumber(ctx, userID, session.PhoneNumber); err != nil {
		log.Printf("handleRecoverSimQ2: rebind failed for user %s: %v", userID, err)
		return h.formatError(session.Language, "error"), nil
	}
	log.Printf("account recovered onto new SIM: user=%s phone=%s", userID, phone.Redact(session.PhoneNumber))

	h.clearRecoverySession(session)
	session.UserID = userID
	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.formatResponse(session.Language, "END", "recover_success"), nil
}

// clearRecoverySession drops the transient new-SIM recovery data.
func (h *USSDHandler) clearRecoverySession(session *Session) {
	delete(session.Data, "recover_national_id")
	delete(session.Data, "recover_user_id")
	delete(session.Data, "recover_sq1_id")
	delete(session.Data, "recover_sq1_answer")
	delete(session.Data, "recover_sq2_id")
}

// bioFields is the ordered set of optional SEP-9 fields a user can maintain
// from My Details. Order fixes the picker's option numbers, so appending is
// safe but reordering renumbers the menu.
var bioFields = []struct {
	key       string // session key suffix, user-map key, and BioUpdate field
	labelKey  string // short label shown in the picker
	promptKey string // prompt shown when editing the field
}{
	{"birth_date", "label_birth_date", "bio_prompt_birth_date"},
	{"address", "label_address", "bio_prompt_address"},
	{"city", "label_city", "bio_prompt_city"},
	{"postal_code", "label_postal_code", "bio_prompt_postal_code"},
}

// bioValueBudget caps a stored value in the picker. Africa's Talking truncates
// the whole screen at 160 characters, and an address alone can exceed that, so
// long values are elided rather than allowed to push the list off the display.
const bioValueBudget = 22

// elide shortens s to at most bioValueBudget runes, marking any cut.
func elide(s string) string {
	r := []rune(s)
	if len(r) <= bioValueBudget {
		return s
	}
	return string(r[:bioValueBudget-1]) + "…"
}

// showMyDetails renders the field picker with each field's stored value, so a
// user can see what is on file before choosing what to change.
func (h *USSDHandler) showMyDetails(ctx context.Context, session *Session, notice string) (string, error) {
	userData, _, err := h.userService.GetUserWithAccounts(ctx, session.UserID)
	if err != nil {
		log.Printf("showMyDetails: lookup failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}
	values, _ := userData.(map[string]any)

	var sb strings.Builder
	if notice != "" {
		sb.WriteString(notice)
		sb.WriteString("\n")
	}
	sb.WriteString(GetLocalizedMessage(session.Language, "my_details_title"))
	unset := GetLocalizedMessage(session.Language, "my_details_not_set")
	for i, f := range bioFields {
		current, _ := values[f.key].(string)
		if current == "" {
			current = unset
		}
		fmt.Fprintf(&sb, "\n%d. %s: %s",
			i+1, GetLocalizedMessage(session.Language, f.labelKey), elide(current))
	}
	sb.WriteString("\n")
	sb.WriteString(GetLocalizedMessage(session.Language, "my_details_back"))

	return "CON " + sb.String(), nil
}

// handleMyDetails routes a picker selection to the single-field editor. "0"
// returns to the account menu; the picker binds it itself rather than relying
// on back navigation, so it is absent from navBackTargets.
func (h *USSDHandler) handleMyDetails(ctx context.Context, session *Session, input string) (string, error) {
	if input == "0" {
		session.CurrentMenu = "my_account"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showAccountMenu(ctx, session)
	}

	choice, err := strconv.Atoi(strings.TrimSpace(input))
	if err != nil || choice < 1 || choice > len(bioFields) {
		return h.showMyDetails(ctx, session, GetLocalizedMessage(session.Language, "invalid_input"))
	}

	field := bioFields[choice-1]
	session.Data["bio_field"] = field.key
	session.CurrentMenu = "bio_edit"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.conWithNav(session, "bio_edit", field.promptKey), nil
}

// handleBioEdit writes one field and returns to the picker.
//
// Only the edited field is sent: UpdateBio ignores empty values, so the other
// three are left untouched rather than rewritten with what the screen happened
// to be showing. There is deliberately no way to clear a field from here — an
// empty entry re-prompts rather than erasing, since "" is indistinguishable
// from "leave alone" in BioUpdate.
func (h *USSDHandler) handleBioEdit(ctx context.Context, session *Session, input string) (string, error) {
	key, _ := session.Data["bio_field"].(string)
	if key == "" {
		session.CurrentMenu = "my_details"
		return h.showMyDetails(ctx, session, "")
	}

	value := strings.TrimSpace(input)
	if value == "" {
		return h.conWithNav(session, "bio_edit", promptKeyFor(key)), nil
	}
	if key == "birth_date" && !isISODate(value) {
		return h.conWithNav(session, "bio_edit", "bio_invalid_date"), nil
	}

	var bio BioUpdate
	switch key {
	case "birth_date":
		bio.BirthDate = value
	case "address":
		bio.Address = value
	case "city":
		bio.City = value
	case "postal_code":
		bio.PostalCode = value
	}

	if err := h.userService.UpdateBio(ctx, session.UserID, bio); err != nil {
		log.Printf("handleBioEdit: UpdateBio(%s) failed for %s: %v", key, phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}

	delete(session.Data, "bio_field")
	session.CurrentMenu = "my_details"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.showMyDetails(ctx, session, GetLocalizedMessage(session.Language, "bio_field_saved"))
}

// promptKeyFor returns the edit prompt for a bio field key.
func promptKeyFor(key string) string {
	for _, f := range bioFields {
		if f.key == key {
			return f.promptKey
		}
	}
	return "invalid_input"
}

// completeRegistration is the final step of registration: it creates the user,
// account row, and PIN in a single atomic insert from the data collected in the
// session (name, national ID, and — when a PIN service is configured — the
// hashed PIN), fires the async on-chain account creation, sends the welcome
// SMS, and ends the session. Reaching here means every prior step succeeded, so
// no partial account is ever left behind.
func (h *USSDHandler) completeRegistration(ctx context.Context, session *Session) (string, error) {
	if h.userService == nil {
		return h.formatError(session.Language, "error"), nil
	}

	fullName, _ := session.Data["full_name"].(string)
	nationalID, _ := session.Data["national_id"].(string)
	pinHash, _ := session.Data["pin_hash"].(string)

	regReq := &RegisterUserRequest{
		MobileNumber:      session.PhoneNumber,
		NetworkCode:       session.NetworkCode,
		FullName:          fullName,
		NationalID:        nationalID,
		PreferredLanguage: session.Language,
	}
	if pinHash != "" {
		now := time.Now()
		regReq.PinHash = pinHash
		regReq.PinSetAt = &now
	}

	user, _, err := h.userService.RegisterUser(ctx, regReq)
	if err != nil {
		log.Printf("completeRegistration: RegisterUser failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		failNote := contracts.AccountNotification{
			PhoneNumber: session.PhoneNumber,
			Reason:      "Account creation failed. Please try again.",
			Language:    session.Language,
		}
		h.notifyAsync(func(bg context.Context) error {
			return h.accountNotifier.NotifyRegistrationFailed(bg, failNote)
		})
		return h.formatError(session.Language, "error"), nil
	}

	if userMap, ok := user.(map[string]any); ok {
		if id, ok := userMap["id"].(string); ok {
			session.UserID = id
		}
	}

	// Clear transient registration data.
	delete(session.Data, "pin_hash")
	delete(session.Data, "national_id")

	// Welcome SMS confirming the account is active. Fired off the request path:
	// a slow SMS gateway must not delay the END screen (AT sandbox has timed out
	// at 5s, which would breach the USSD turn deadline). Phase 5 enriches this
	// with the deferred-step nudges (security questions, bio).
	welcomeNote := contracts.AccountNotification{
		PhoneNumber: session.PhoneNumber,
		FullName:    fullName,
		Language:    session.Language,
	}
	h.notifyAsync(func(bg context.Context) error {
		return h.accountNotifier.NotifyRegistrationSuccess(bg, welcomeNote)
	})

	session.CurrentMenu = "main"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.formatResponse(session.Language, "END", "registration_success"), nil
}

// notifyAsync sends an account notification off the USSD request path. SMS
// delivery must never block or delay a USSD response — a slow gateway can
// breach the turn deadline. Uses a detached context (the request ctx is
// cancelled the moment the handler returns).
//
// The timeout is a leak guard, not a delivery deadline — the notifier's retry
// sequence is finite and self-bounding, so it decides when to give up. This
// must never cancel a send mid-sequence, since that suppresses retries that
// were meant to run (at 15s it cancelled during the first attempt and no retry
// ever ran). Delivery stays best effort, but a give-up is logged rather than
// dropped — a silently lost welcome SMS looks identical to one never sent.
func (h *USSDHandler) notifyAsync(send func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()
		if err := send(ctx); err != nil {
			log.Printf("notifyAsync: notification delivery failed: %v", err)
		}
	}()
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
		return "END " + Format(session.Language, "loan_min_amount", cfg.Currency, float64(cfg.MinAmountCents)/100), nil
	}
	if amountCents > cfg.MaxAmountCents {
		return "END " + Format(session.Language, "loan_max_amount", cfg.Currency, float64(cfg.MaxAmountCents)/100), nil
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

// handlePayoutMethod stores the chosen disbursement rail (mobile money or cash
// pickup) and advances to confirmation. Cash pickup carries an anchor-side
// minimum that mobile money does not, so it is enforced here rather than
// against the loan product.
func (h *USSDHandler) handlePayoutMethod(ctx context.Context, session *Session, input string) (string, error) {
	switch input {
	case "1":
		if body, ok := h.cashPickupBelowMinimum(ctx, session); ok {
			return body, nil
		}
		session.Data["payout_method"] = "cash_pickup"
	case "2":
		session.Data["payout_method"] = "mobile_money"
	default:
		return h.showMenu(session, "payout_method")
	}
	session.CurrentMenu = "loan_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.showLoanConfirmation(ctx, session)
}

// payoutOptionCashPickup is the payout menu key for the MoneyGram rail.
const payoutOptionCashPickup = "1"

// cashPickupBelowMinimum reports whether the pending loan converts to less than
// the anchor's withdraw minimum, returning the payout menu re-rendered without
// the cash-pickup option so the borrower can switch rails instead of losing the
// session. Leaving the option on screen would just loop them back here.
//
// It fails open: without a usable rate the loan proceeds and the anchor
// rejects it downstream, which beats blocking every cash pickup during an FX
// outage.
func (h *USSDHandler) cashPickupBelowMinimum(ctx context.Context, session *Session) (string, bool) {
	if h.rateService == nil {
		return "", false
	}
	currency, _ := session.Data["local_currency"].(string)
	rate, err := h.rateService.GetExchangeRate(ctx, currency)
	if err != nil || rate <= 0 {
		log.Printf("cashPickupBelowMinimum: exchange rate unavailable for %s: %v", currency, err)
		return "", false
	}

	fiatAmount := float64(toInt64(session.Data["loan_amount_local"])) / 100.0
	if fiatAmount/rate >= moneygram.MinWithdrawUSD {
		return "", false
	}

	menu, err := h.menuRegistry.Get("payout_method")
	if err != nil {
		return h.formatError(session.Language, "error"), true
	}

	// Option keys are preserved, not renumbered: the handler and the borrower's
	// session both key off "2" for mobile money.
	remaining := *menu
	remaining.Options = nil
	for _, opt := range menu.Options {
		if opt.Key == payoutOptionCashPickup {
			continue
		}
		remaining.Options = append(remaining.Options, opt)
	}

	msg := Format(session.Language, "loan_cash_pickup_min", currency, math.Ceil(moneygram.MinWithdrawUSD*rate))
	return "CON " + h.withNavHint(session, "payout_method", msg+"\n"+remaining.Render(session.Language)), true
}

// handleLoanConfirm handles loan confirmation. When PIN service is available,
// pressing "1" routes to PIN verification before submitting the loan.
// handleLoanConfirm accepts the PIN entered on the confirmation screen. The
// terms and the PIN gate are one screen, so entering a correct PIN is both the
// acceptance of the displayed terms and the authorization to borrow — there is
// no separate keystroke to agree.
//
// Navigation is intercepted upstream: "0" steps back to the payout picker and
// "00" abandons the flow, so anything reaching here was meant as a PIN. A
// malformed entry re-renders the terms rather than cancelling, because
// cancelling a loan on a typo loses the user their place in a metered session.
func (h *USSDHandler) handleLoanConfirm(ctx context.Context, session *Session, input string) (string, error) {
	if h.pinService == nil {
		return h.submitLoan(ctx, session)
	}

	if !isPINShaped(input) {
		return h.showLoanConfirmationWith(ctx, session, GetLocalizedMessage(session.Language, "loan_confirm_pin_format"))
	}

	ok, err := h.pinService.VerifyPIN(ctx, session.UserID, input)
	if err != nil {
		if strings.Contains(err.Error(), "locked") {
			return h.formatLockedMessage(ctx, session), nil
		}
		log.Printf("VerifyPIN system error (menu=%s) for %s: %v", session.CurrentMenu, phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		// Wrong PIN — the service has already sent the alert SMS and
		// incremented the attempt counter. Re-show the terms alongside the
		// warning so the user never types a PIN against a loan they can no
		// longer see.
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
		warning := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return h.showLoanConfirmationWith(ctx, session, warning)
	}

	return h.submitLoan(ctx, session)
}

// isPINShaped reports whether input could be a PIN entry. It checks shape only
// — length and digits — deliberately not the strength rules ValidatePIN applies
// at creation, since an account may hold a PIN that predates those rules and
// must still be able to authenticate.
func isPINShaped(input string) bool {
	if len(input) != pinPkg.PINLength {
		return false
	}
	for _, r := range input {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
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

	var sellRate float64
	if h.rateService != nil {
		rate, err := h.rateService.GetExchangeRate(ctx, localCurrency)
		if err != nil {
			log.Printf("submitLoan: failed to get exchange rate: %v", err)
			return h.formatError(session.Language, "error"), nil
		}
		sellRate = rate
	} else {
		sellRate = 153.50 // fallback
	}

	// Convert local currency to USDC stroops: (fiat / rate) * 1e7
	fiatAmount := float64(localAmount) / 100.0
	usdcStroops := int64(fiatAmount / sellRate * 1e7)

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

	// Extract user disbursement details for off-ramp. The bio fields are
	// optional SEP-9 prefill for MoneyGram cash pickup (empty when the user
	// skipped them at registration — MoneyGram then collects them in its
	// webview).
	var recipientName, nationalID, countryCode, networkCode, networkName string
	var birthDate, address, city, postalCode string
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
		if v, ok := userMap["birth_date"].(string); ok {
			birthDate = v
		}
		if v, ok := userMap["address"].(string); ok {
			address = v
		}
		if v, ok := userMap["city"].(string); ok {
			city = v
		}
		if v, ok := userMap["postal_code"].(string); ok {
			postalCode = v
		}
	}

	// Fire off the disbursement pipeline asynchronously so the USSD session
	// ends immediately. The user is notified via SMS on success or failure.
	payoutMethod, _ := session.Data["payout_method"].(string)

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
		ConversionRate:  sellRate,
		PayoutMethod:    payoutMethod,
		BirthDate:       birthDate,
		Address:         address,
		City:            city,
		PostalCode:      postalCode,
	}
	// Cash-pickup needs the per-user Stellar derivation index so the MG
	// poller can re-derive the SEP-10 child memo on restart. This is the
	// real BIP-44 account_index from the accounts row, not a hash. Always set:
	// cash-pickup uses it for the SEP-10 child memo, and the disbursement
	// pipeline uses it to ensure the on-chain identity exists before lending.
	loanReq.ChildAccountIndex = accountIndex
	go func() {
		// Use a detached context so the pipeline isn't cancelled when the
		// USSD request context expires.
		bgCtx := context.Background()
		if _, err := h.loanService.RequestLoan(bgCtx, loanReq); err != nil {
			log.Printf("async loan disbursement failed: user=%s error=%v", loanReq.UserID, err)
		}
	}()

	localKES := float64(localAmount) / 100
	return "END " + Format(session.Language, "loan_processing", localCurrency, localKES), nil
}

// handleMyLoans texts the borrower a statement of their current loan rather
// than rendering it on screen. A statement runs to reference, amount and due
// date, which crowds a 160-character USSD display and is gone the moment the
// session ends; an SMS stays on the handset to refer back to at an agent.
//
// Only the most recent loan is sent. The product allows one active loan at a
// time — a borrower clears it before drawing again — so the newest record is
// the one that matters. GetUserLoans returns newest first (the repository
// orders by created_at DESC), so that is index 0.
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

	if !isQuotable(loanStatus(loans[0])) {
		// No origination borrow_index yet, so there is nothing to price
		// against. Say the loan is still processing rather than reporting a
		// failure the borrower cannot act on.
		return h.formatResponse(session.Language, "END", "my_loans_processing"), nil
	}

	note, ok := h.loanStatement(ctx, session, loans[0])
	if !ok {
		return h.formatError(session.Language, "error"), nil
	}

	if err := h.loanNotifier.NotifyLoanStatement(ctx, note); err != nil {
		log.Printf("handleMyLoans: statement SMS failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}

	return h.formatResponse(session.Language, "END", "my_loans_sent"), nil
}

// loanStatus reads the status off a GetUserLoans record.
func loanStatus(loan any) string {
	loanMap, ok := loan.(map[string]any)
	if !ok {
		return ""
	}
	st, _ := loanMap["status"].(string)
	return st
}

// isQuotable reports whether a loan has reached a state where the vault holds
// an origination borrow_index to price against. Before disbursement there is
// none, so a quote failure there is the normal case rather than an error.
func isQuotable(status string) bool {
	switch strings.ToLower(status) {
	case "disbursed", "repaid", "defaulted":
		return true
	default:
		return false
	}
}

// loanStatement builds the notification the statement SMS renders from.
//
// The amount is the live payoff figure from GetRepaymentQuote, not the sum
// disbursed: principal scaled by the vault's borrow_index growth since
// origination, plus any fees the quote already folds in. Anything added to
// that quote later — administrative fees, taxes — reaches this SMS without
// touching it. Quoting hard-fails rather than falling back to a stale figure,
// so a borrower is never told they owe a number nobody stands behind.
//
// Reports false when the record has no reference or the quote is unavailable.
func (h *USSDHandler) loanStatement(ctx context.Context, session *Session, loan any) (contracts.LoanNotification, bool) {
	loanMap, ok := loan.(map[string]any)
	if !ok {
		return contracts.LoanNotification{}, false
	}

	note := contracts.LoanNotification{
		PhoneNumber: session.PhoneNumber,
		UserID:      session.UserID,
		Language:    session.Language,
	}

	if id, ok := loanMap["id"].(string); ok {
		note.LoanID = id
	}
	if ref, ok := loanMap["loan_reference"].(*string); ok && ref != nil {
		note.LoanReference = *ref
	}
	if st, ok := loanMap["status"].(string); ok {
		note.Status = st
	}
	if due, ok := loanMap["due_date"].(*time.Time); ok {
		note.DueDate = due
	}

	if note.LoanID == "" || note.LoanReference == "" {
		log.Printf("loanStatement: loan record lacks an id or reference for %s", phone.Redact(session.PhoneNumber))
		return contracts.LoanNotification{}, false
	}

	quote, err := h.loanService.GetRepaymentQuote(ctx, note.LoanID)
	if err != nil {
		log.Printf("loanStatement: quote failed for loan %s: %v", note.LoanReference, err)
		return contracts.LoanNotification{}, false
	}

	note.Amount = quote.AmountUSDCStroops
	note.DisplayAmount = float64(quote.AmountLocalCents) / 100.0
	note.DisplayCurrency = quote.LocalCurrency

	return note, true
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
	// TODO: Use actual paybill from config (currently baked into repay_header).
	response.WriteString(GetLocalizedMessage(session.Language, "repay_header"))
	response.WriteString("\n")

	for i, loan := range activeLoans {
		if i >= 3 { // Limit to 3 loans
			break
		}

		var loanRef = "N/A"
		var loanID string
		var displayAmount = "-"

		if loanMap, ok := loan.(map[string]any); ok {
			if ref, ok := loanMap["loan_reference"].(*string); ok && ref != nil {
				loanRef = *ref
			}
			if id, ok := loanMap["id"].(string); ok {
				loanID = id
			}
		}

		// Live amount owed — quote against current vault index + FX. Hard
		// fail surface: if the quote can't be obtained, show "—" rather
		// than a stale stored value the borrower might act on.
		if loanID != "" {
			if quote, err := h.loanService.GetRepaymentQuote(ctx, loanID); err == nil {
				displayAmount = fmt.Sprintf("%s %.2f",
					quote.LocalCurrency,
					float64(quote.AmountLocalCents)/100.0)
			} else {
				log.Printf("repayment quote failed for loan %s: %v", loanID, err)
			}
		}

		response.WriteString(Format(session.Language, "repay_loan_line", loanRef, displayAmount))
		response.WriteString("\n\n")
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
	case "3": // Personal details — optional SEP-9 bio for faster cash pickup.
		session.CurrentMenu = "my_details"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMyDetails(ctx, session, "")
	case "0": // Main Menu
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMainMenu(session)
	default:
		return h.showAccountMenu(ctx, session)
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
	return "CON " + h.withNavHint(session, "register", menu.Render(session.Language)), nil
}

func (h *USSDHandler) showLoanAmountMenu(session *Session) (string, error) {
	cfg := h.loanService.GetProductConfig()
	if cfg == nil {
		return h.formatError(session.Language, "error"), nil
	}
	minFiat := float64(cfg.MinAmountCents) / 100
	maxFiat := float64(cfg.MaxAmountCents) / 100
	body := Format(session.Language, "loan_amount_prompt", cfg.Currency, minFiat, maxFiat)
	return "CON " + h.withNavHint(session, "loan_amount", body), nil
}

// showAccountMenu renders My Account, prefixed with a non-blocking nudge while
// the account is missing security questions. The nudge is advisory only —
// nothing is gated on it — and is the in-menu counterpart to the welcome SMS
// for users who ignored it.
func (h *USSDHandler) showAccountMenu(ctx context.Context, session *Session) (string, error) {
	body, err := h.renderAccountMenu(session)
	if err != nil {
		return "", err
	}
	if h.pinService != nil && session.UserID != "" {
		if ids, qErr := h.pinService.GetUserQuestionIDs(ctx, session.UserID); qErr == nil && len(ids) == 0 {
			return "CON " + GetLocalizedMessage(session.Language, "badge_no_security_q") + "\n" + body, nil
		}
	}
	return "CON " + body, nil
}

func (h *USSDHandler) renderAccountMenu(session *Session) (string, error) {
	menu, err := h.menuRegistry.Get("my_account")
	if err != nil {
		return "", fmt.Errorf("render my_account menu: %w", err)
	}
	return menu.Render(session.Language), nil
}

// showLoanConfirmation displays the terms — principal amount and duration only
// — above the PIN prompt that accepts them. APR, estimated total, and exchange
// rate are intentionally omitted: the repayment total is computed later and the
// APR is dynamic.
func (h *USSDHandler) showLoanConfirmation(ctx context.Context, session *Session) (string, error) {
	return h.showLoanConfirmationWith(ctx, session, "")
}

// showLoanConfirmationWith renders the confirmation screen with an optional
// leading notice — a wrong-PIN warning or a format reminder. The notice goes
// above the terms so the terms stay adjacent to the PIN prompt they authorize.
func (h *USSDHandler) showLoanConfirmationWith(_ context.Context, session *Session, notice string) (string, error) {
	cfg := h.loanService.GetProductConfig()
	if cfg == nil {
		return h.formatError(session.Language, "error"), nil
	}

	localAmountCents := toInt64(session.Data["loan_amount_local"])
	localAmount := float64(localAmountCents) / 100
	duration := cfg.DurationDays

	body := Format(session.Language, "loan_confirm_summary", cfg.Currency, localAmount, duration)
	if notice != "" {
		body = notice + "\n" + body
	}
	return "CON " + h.withNavHint(session, "loan_confirm", body), nil
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
		return h.conNav(session, "pin_invalid"), nil
	}

	session.Data["new_pin"] = input
	session.CurrentMenu = "pin_confirm"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}

	return h.showMenu(session, "pin_confirm")
}

// handlePINConfirm compares the confirmed PIN with the stored value, hashes it,
// and finalises registration by creating the account atomically (user + account
// + PIN in one insert). Security questions and bio are deferred to the account
// menu, so this is the last step of the shortened registration flow.
func (h *USSDHandler) handlePINConfirm(ctx context.Context, session *Session, input string) (string, error) {
	newPIN, _ := session.Data["new_pin"].(string)
	if input != newPIN {
		// Mismatch — go back to creation.
		delete(session.Data, "new_pin")
		session.CurrentMenu = "pin_create"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.conNav(session, "pin_mismatch"), nil
	}

	// Self-heal path: an existing PIN-less user (routed here by the
	// handleInitialRequest guard) is setting a PIN on their account, not
	// registering a new one — so set it directly rather than re-creating.
	if setOnly, _ := session.Data["set_pin_only"].(bool); setOnly {
		if err := h.pinService.SetPIN(ctx, session.UserID, newPIN); err != nil {
			log.Printf("handlePINConfirm: SetPIN (self-heal) failed for %s: %v", phone.Redact(session.PhoneNumber), err)
			return h.formatError(session.Language, "error"), nil
		}
		delete(session.Data, "new_pin")
		delete(session.Data, "set_pin_only")
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.formatResponse(session.Language, "END", "pin_changed"), nil
	}

	hash, err := pinPkg.HashPIN(newPIN)
	if err != nil {
		log.Printf("handlePINConfirm: hash PIN failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}
	delete(session.Data, "new_pin")
	session.Data["pin_hash"] = hash

	// PIN confirmed — persist the whole account in one atomic call.
	return h.completeRegistration(ctx, session)
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

	// Clean up transient data. Security-question setup is only ever reached
	// from the account menu (PIN Manager) now — registration no longer chains
	// here — so completion always just ends with success.
	delete(session.Data, "sq1_id")
	delete(session.Data, "sq1_answer")
	delete(session.Data, "sq2_id")
	delete(session.Data, "from_pin_manager")

	session.CurrentMenu = "pin_manager"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", fmt.Errorf("failed to save session: %w", err)
	}
	return h.formatResponse(session.Language, "END", "security_q_success"), nil
}

//
// # PIN Verification Gate Handlers
//

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
		log.Printf("VerifyPIN system error (menu=%s) for %s: %v", session.CurrentMenu, phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return h.conNavText(session, msg), nil
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
		return h.showAccountMenu(ctx, session)
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
		log.Printf("VerifyPIN system error (menu=%s) for %s: %v", session.CurrentMenu, phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}

	if !ok {
		remaining := h.getRemainingAttempts(ctx, session.UserID)
		if remaining <= 0 {
			return h.formatLockedMessage(ctx, session), nil
		}
		msg := fmt.Sprintf(GetLocalizedMessage(session.Language, "pin_wrong"), remaining)
		return h.conNavText(session, msg), nil
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
		return h.conNav(session, "pin_invalid"), nil
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
		return h.conNav(session, "pin_mismatch"), nil
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

	// Two-tier reset. The national ID has matched and the request physically
	// originates from the registered SIM (the telco authenticates the MSISDN),
	// so possession + knowledge is the baseline factor pair.
	//
	//   - security questions set -> they are REQUIRED (the stronger factor;
	//     the national-ID-only path is closed for that user).
	//   - not set -> proceed straight to a new PIN on this SIM.
	//
	// See ussd-registration-redesign: refusing a reset here used to strand any
	// user who never completed the (previously unreachable) security-question
	// step — a forgotten PIN meant a permanently unusable account.
	if session.Data["recovery_q1_id"] == nil || session.Data["recovery_q2_id"] == nil {
		session.Data["recovery_no_sq"] = true
		session.CurrentMenu = "pin_recovery_new"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", fmt.Errorf("failed to save session: %w", err)
		}
		return h.showMenu(session, "pin_recovery_new")
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
	if err != nil {
		log.Printf("handlePINRecoveryQ2: VerifySecurityAnswers failed for %s: %v", phone.Redact(session.PhoneNumber), err)
		return h.formatError(session.Language, "error"), nil
	}
	if !ok {
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
		return h.conNav(session, "pin_invalid"), nil
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
		return h.conNav(session, "pin_mismatch"), nil
	}

	if err := h.pinService.ResetPIN(ctx, session.UserID, newPIN); err != nil {
		log.Printf("handlePINRecoveryConfirm: ResetPIN failed: %v", err)
		return h.formatError(session.Language, "error"), nil
	}

	// Clean up recovery data.
	delete(session.Data, "recovery_new_pin")
	delete(session.Data, "recovery_q1_id")
	delete(session.Data, "recovery_q2_id")

	// This reset used only national ID + SIM possession. Nudge the user to add
	// security questions, which closes that weaker path and is the only
	// self-service way to recover if they ever lose this phone.
	noSQ, _ := session.Data["recovery_no_sq"].(bool)
	delete(session.Data, "recovery_no_sq")
	if noSQ {
		return h.formatResponse(session.Language, "END", "recovery_success_add_sq"), nil
	}

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
	return "CON " + h.withNavHint(session, menuID, menu.Render(session.Language)), nil
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
