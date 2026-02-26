package notifications

// AccountTemplates contains fmt.Sprintf-style format strings for account and
// PIN lifecycle notification messages. Each field documents its expected
// arguments.
type AccountTemplates struct {
	// RegistrationSuccess formats the welcome message after successful
	// registration. Args: (FullName string).
	RegistrationSuccess string

	// RegistrationFailed formats the message when registration cannot be
	// completed. Args: (Reason string).
	RegistrationFailed string

	// WrongAttempt formats the alert sent when an incorrect PIN is entered.
	// Args: (RemainingAttempts int).
	WrongAttempt string

	// AccountLocked formats the security alert sent when the account is
	// locked due to repeated failed PIN attempts. Args: (LockedUntil string).
	AccountLocked string

	// PINChanged formats the confirmation sent after a successful PIN change.
	// No format args.
	PINChanged string

	// PINChangeFailed formats the alert sent when a PIN change attempt fails.
	// Args: (Reason string).
	PINChangeFailed string

	// PINReset formats the confirmation sent after a successful PIN reset via
	// the recovery flow. No format args.
	PINReset string

	// PINResetFailed formats the alert sent when a PIN reset attempt fails.
	// Args: (Reason string).
	PINResetFailed string
}

// DefaultAccountTemplates returns sensible default templates for all account
// and PIN notification messages. Pass nil to NewSMSAccountNotifier to use
// these defaults.
func DefaultAccountTemplates() *AccountTemplates {
	return &AccountTemplates{
		RegistrationSuccess: "Welcome to Shamba Records, %s! " +
			"Your account has been created and your PIN is set. " +
			"Dial *384# to request your first loan.",
		RegistrationFailed: "Your Shamba Records registration could not be completed. " +
			"Reason: %s. Please try again or contact support.",
		WrongAttempt: "ALERT: An incorrect PIN was entered on your Shamba Records account. " +
			"%d attempt(s) remaining before your account is locked.",
		AccountLocked: "SECURITY: Your Shamba Records account has been temporarily locked " +
			"due to multiple failed PIN attempts. Try again after %s or dial to reset your PIN.",
		PINChanged: "Your Shamba Records PIN has been changed successfully. " +
			"If you did not make this change, dial *384*1234# immediately to reset your PIN.",
		PINChangeFailed: "A PIN change attempt on your Shamba Records account was unsuccessful. " +
			"Reason: %s. If this was not you, please reset your PIN immediately.",
		PINReset: "Your Shamba Records PIN has been reset successfully. " +
			"You can now access your account with your new PIN.",
		PINResetFailed: "A PIN reset attempt on your Shamba Records account failed. " +
			"Reason: %s. Please try again or contact support.",
	}
}
