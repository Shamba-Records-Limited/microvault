package contracts

import "context"

// AccountNotifier defines the interface for sending account and PIN lifecycle
// notifications to users. Implementations may deliver via SMS, push, email, or
// any other transport. Methods must be safe for concurrent use.
type AccountNotifier interface {
	// NotifyRegistrationSuccess informs a user that their account has been
	// created and their PIN is set.
	NotifyRegistrationSuccess(ctx context.Context, n AccountNotification) error

	// NotifyRegistrationFailed informs a user that their registration could
	// not be completed, along with a human-readable reason.
	NotifyRegistrationFailed(ctx context.Context, n AccountNotification) error

	// NotifyPINWrongAttempt warns a user that an incorrect PIN was entered
	// and includes the number of remaining attempts before lockout.
	NotifyPINWrongAttempt(ctx context.Context, n AccountNotification) error

	// NotifyAccountLocked informs a user that their account has been
	// temporarily locked due to repeated failed PIN attempts.
	NotifyAccountLocked(ctx context.Context, n AccountNotification) error

	// NotifyPINChanged confirms that the user's PIN was changed successfully.
	NotifyPINChanged(ctx context.Context, n AccountNotification) error

	// NotifyPINChangeFailed alerts a user that a PIN change attempt was
	// unsuccessful, along with the reason.
	NotifyPINChangeFailed(ctx context.Context, n AccountNotification) error

	// NotifyPINReset confirms that the user's PIN was reset successfully
	// via the recovery flow.
	NotifyPINReset(ctx context.Context, n AccountNotification) error

	// NotifyPINResetFailed alerts a user that a PIN reset attempt failed,
	// along with the reason.
	NotifyPINResetFailed(ctx context.Context, n AccountNotification) error
}

// AccountNotification carries the data needed to notify a user about
// account and PIN lifecycle events. Not all fields are used by every
// notification method; see individual method documentation for which
// fields are relevant.
type AccountNotification struct {
	// UserID is the unique identifier of the user being notified.
	UserID string

	// PhoneNumber is the destination for SMS delivery (E.164 format).
	PhoneNumber string

	// FullName is used in registration welcome messages.
	FullName string

	// RemainingAttempts indicates how many PIN attempts remain before lockout.
	// Used by NotifyPINWrongAttempt.
	RemainingAttempts int

	// LockedUntil is a human-readable duration string (e.g. "15 minutes")
	// indicating how long until the lockout expires. Used by NotifyAccountLocked.
	LockedUntil string

	// Reason provides a human-readable explanation for failure notifications.
	Reason string

	// Language optionally pins the SMS language (ISO code: en/sw/fr). When
	// empty the notifier resolves it from the recipient's stored preference.
	Language string
}
