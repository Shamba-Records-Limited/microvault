package ussd

import (
	"context"
	"fmt"
	"strings"
	"unicode/utf8"
)

const (
	navBackInput = "0"
	navHomeInput = "00"

	// navHintBudget is the byte budget a rendered menu body must stay under for
	// the navigation hint to be appended. Africa's Talking truncates at 160
	// characters, so a hint is only worth showing when it fits.
	navHintBudget = 160
)

// navBackTargets maps a menu to the menu one step behind it.
//
// A menu absent from this map takes no back input, for one of three reasons:
// it already binds "0" itself (language_select, my_account, pin_manager), "0"
// already means something else there (register_bio, where it skips a field),
// or stepping back would weaken a verification gate (recover_sim_q1/q2,
// pin_recovery_q1/q2, pin_recovery_new/confirm).
var navBackTargets = map[string]string{
	"register":                 "language_select",
	"register_national_id":     "register",
	"pin_create":               "register_national_id",
	"pin_confirm":              "pin_create",
	"security_q1_select":       "pin_manager",
	"security_q1_answer":       "security_q1_select",
	"security_q2_select":       "security_q1_answer",
	"security_q2_answer":       "security_q2_select",
	"pin_change_old":           "pin_manager",
	"pin_change_new":           "pin_change_old",
	"pin_change_confirm":       "pin_change_new",
	"pin_recovery_national_id": "pin_manager",
	"loan_amount":              "main",
	"payout_method":            "loan_amount",
	"loan_confirm":             "payout_method",
	"pin_verify_loan":          "loan_confirm",
	// repay_loan renders a live list of loans rather than a registered menu,
	// so stepping back off the rail screen returns to main rather than
	// re-rendering a menu the registry does not hold.
	"repay_rail": "main",
}

// navBackClears lists the session keys a menu owns. Stepping back off a menu
// drops them so the step is genuinely re-collected rather than silently
// retaining a value the user was trying to correct.
var navBackClears = map[string][]string{
	"register_national_id": {"national_id"},
	"pin_confirm":          {"new_pin"},
	"security_q1_answer":   {"sq1_id"},
	"security_q2_select":   {"sq1_answer"},
	"security_q2_answer":   {"sq2_id"},
	"pin_change_new":       {"old_pin"},
	"pin_change_confirm":   {"new_pin"},
	"payout_method":        {"loan_amount_local", "local_currency", "loan_duration", "repayment_schedule", "product_id"},
	"loan_confirm":         {"payout_method"},
}

// navFlowKeys are the in-flight keys discarded when a user jumps home. Anything
// half-collected is abandoned deliberately: the main menu is a fresh start.
var navFlowKeys = []string{
	"new_pin", "old_pin", "pin_hash",
	"sq1_id", "sq1_answer", "sq2_id", "from_pin_manager",
	"loan_amount_local", "local_currency", "loan_duration",
	"repayment_schedule", "product_id", "payout_method",
	"bio_step", "bio_update", "bio_birth_date", "bio_address",
	"bio_city", "bio_postal_code",
}

// canGoBack reports whether the given menu accepts the back input for this
// session.
func canGoBack(session *Session, menuID string) bool {
	target, ok := navBackTargets[menuID]
	if !ok {
		return false
	}
	// The self-heal path enters pin_create directly on an existing account;
	// there is no registration step behind it to return to.
	if menuID == "pin_create" {
		if setOnly, _ := session.Data["set_pin_only"].(bool); setOnly {
			return false
		}
	}
	// Registration runs before a user exists, so anything rooted at the main
	// menu is unreachable.
	if session.UserID == "" && target == "main" {
		return false
	}
	return true
}

// canGoHome reports whether the given menu accepts the home input. Home is the
// main menu, so it needs a registered user and somewhere to return from.
func canGoHome(session *Session, menuID string) bool {
	if session.UserID == "" || menuID == "main" {
		return false
	}
	if setOnly, _ := session.Data["set_pin_only"].(bool); setOnly {
		return false
	}
	return true
}

// handleNavigation intercepts the global back and home inputs before a menu's
// own handler sees them. It returns handled=false when the input is not a
// navigation command, or when the current menu does not accept it, leaving the
// input to be processed normally.
func (h *USSDHandler) handleNavigation(ctx context.Context, session *Session, input string) (string, bool, error) {
	menuID := session.CurrentMenu

	switch input {
	case navHomeInput:
		if !canGoHome(session, menuID) {
			return "", false, nil
		}
		for _, k := range navFlowKeys {
			delete(session.Data, k)
		}
		session.CurrentMenu = "main"
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", true, fmt.Errorf("failed to save session: %w", err)
		}
		resp, err := h.showMainMenu(session)
		return resp, true, err

	case navBackInput:
		if !canGoBack(session, menuID) {
			return "", false, nil
		}
		for _, k := range navBackClears[menuID] {
			delete(session.Data, k)
		}
		target := navBackTargets[menuID]
		session.CurrentMenu = target
		if err := h.sessionManager.SaveSession(ctx, session); err != nil {
			return "", true, fmt.Errorf("failed to save session: %w", err)
		}
		resp, err := h.renderMenu(ctx, session, target)
		return resp, true, err
	}

	return "", false, nil
}

// renderMenu re-displays a menu by ID. Most menus are registry-backed, but a
// few are assembled at runtime (main, account, loan amount/confirmation) and
// need their own builder.
func (h *USSDHandler) renderMenu(ctx context.Context, session *Session, menuID string) (string, error) {
	switch menuID {
	case "main":
		return h.showMainMenu(session)
	case "my_account":
		return h.showAccountMenu(ctx, session)
	case "language_select":
		return h.showLanguageMenu(session)
	case "register":
		return h.showRegistrationMenu(session)
	case "register_national_id":
		return h.conWithNav(session, "register_national_id", "reg_enter_national_id"), nil
	case "loan_amount":
		return h.showLoanAmountMenu(session)
	case "loan_confirm":
		return h.showLoanConfirmation(ctx, session)
	default:
		return h.showMenu(session, menuID)
	}
}

// withNavHint appends the navigation footer to a rendered menu body when the
// menu accepts navigation input and the result still fits the AT display.
func (h *USSDHandler) withNavHint(session *Session, menuID, body string) string {
	back := canGoBack(session, menuID)
	home := canGoHome(session, menuID)

	var key string
	switch {
	case back && home:
		key = "nav_hint_both"
	case back:
		key = "nav_hint_back"
	case home:
		key = "nav_hint_home"
	default:
		return body
	}

	hint := GetLocalizedMessage(session.Language, key)
	if utf8.RuneCountInString(body)+utf8.RuneCountInString(hint)+1 > navHintBudget {
		return body
	}
	return body + "\n" + hint
}

// conWithNav renders a CON response for a localization key with the navigation
// footer attached.
func (h *USSDHandler) conWithNav(session *Session, menuID, messageKey string) string {
	body := GetLocalizedMessage(session.Language, messageKey)
	return "CON " + h.withNavHint(session, menuID, strings.TrimSpace(body))
}

// conNav is conWithNav for the menu the session is currently on — used by the
// re-prompt paths, which stay put after a validation failure.
func (h *USSDHandler) conNav(session *Session, messageKey string) string {
	return h.conWithNav(session, session.CurrentMenu, messageKey)
}

// conNavText is conNav for an already-rendered body rather than a message key.
func (h *USSDHandler) conNavText(session *Session, body string) string {
	return "CON " + h.withNavHint(session, session.CurrentMenu, body)
}
