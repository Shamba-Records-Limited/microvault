package pin

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/notifications"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Service constants.
const (
	// MaxPINAttempts is the number of consecutive wrong PIN entries allowed
	// before the account is locked.
	MaxPINAttempts = 3

	// LockoutDuration is how long an account stays locked after exceeding
	// MaxPINAttempts.
	LockoutDuration = 15 * time.Minute

	// BcryptCost is the bcrypt work factor used for hashing PINs and
	// security question answers.
	BcryptCost = 10
)

// Service errors.
var (
	// ErrAccountLocked is returned when a PIN operation is attempted on a
	// locked account.
	ErrAccountLocked = errors.New("account is temporarily locked")

	// ErrPINNotSet is returned when a PIN operation requires an existing PIN
	// but the user has not set one.
	ErrPINNotSet = errors.New("PIN has not been set")

	// ErrPINIncorrect is returned when the supplied PIN does not match the
	// stored hash.
	ErrPINIncorrect = errors.New("incorrect PIN")

	// ErrPINSameAsOld is returned when a new PIN is identical to the current
	// one during a change operation.
	ErrPINSameAsOld = errors.New("new PIN must be different from current PIN")

	// ErrSecurityAnswerMismatch is returned when one or more security
	// question answers do not match.
	ErrSecurityAnswerMismatch = errors.New("security answers do not match")

	// ErrInsufficientQuestions is returned when fewer than the required
	// number of security questions are provided.
	ErrInsufficientQuestions = errors.New("at least 2 security questions are required")
)

// QuestionAnswer pairs a predefined question ID with the user's plaintext
// answer. The answer is normalized and hashed before storage.
type QuestionAnswer struct {
	QuestionID int
	Answer     string
}

// pinErr starts an error builder for PIN operations.
func pinErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainIdentity).Tags("pin").With(pkgErrors.AttrOperation, op)
}

// Service provides PIN management operations including creation, verification,
// change, reset, and security question handling. It sends SMS notifications
// as side effects of certain operations via an [contracts.AccountNotifier].
type Service struct {
	userRepo    repository.UserRepository
	sqRepo      *SecurityQuestionRepository
	notifier    contracts.AccountNotifier
	maxAttempts int
	lockout     time.Duration
}

// NewService creates a new PIN management service. If notifier is nil, a
// [notifications.NoOpAccountNotifier] is used. If lockout is 0, the default
// [LockoutDuration] is applied.
func NewService(
	userRepo repository.UserRepository,
	sqRepo *SecurityQuestionRepository,
	notifier contracts.AccountNotifier,
	lockout time.Duration,
) *Service {
	if notifier == nil {
		notifier = &notifications.NoOpAccountNotifier{}
	}
	if lockout == 0 {
		lockout = LockoutDuration
	}
	return &Service{
		userRepo:    userRepo,
		sqRepo:      sqRepo,
		notifier:    notifier,
		maxAttempts: MaxPINAttempts,
		lockout:     lockout,
	}
}

// HashPIN validates a raw PIN and returns its bcrypt hash at the shared
// BcryptCost. It is the single entry point for turning a PIN into a stored
// hash — used by SetPIN, ResetPIN, and atomic registration — so PIN validation
// and the cost factor live in one place.
func HashPIN(pin string) (string, error) {
	if err := ValidatePIN(pin); err != nil {
		return "", pinErr("set_pin").Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "PIN failed validation")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(pin), BcryptCost)
	if err != nil {
		return "", pinErr("set_pin").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash the PIN")
	}
	return string(hash), nil
}

func (s *Service) SetPIN(ctx context.Context, userID, pin string) error {
	hashStr, err := HashPIN(pin)
	if err != nil {
		return err
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	now := time.Now()
	user.PinHash = &hashStr
	user.PinSetAt = &now
	user.PinAttempts = 0
	user.PinLockedUntil = nil

	if err := s.userRepo.Update(ctx, user); err != nil {
		return pinErr("set_pin").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not save the PIN")
	}

	return nil
}

// VerifyPIN checks the supplied PIN against the stored hash. On failure it
// increments the attempt counter and sends an SMS notification. If the
// maximum attempts are exceeded the account is locked and a lockout
// notification is sent.
func (s *Service) VerifyPIN(ctx context.Context, userID, pin string) (bool, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	if user.PinHash == nil {
		return false, ErrPINNotSet
	}

	// Check lockout.
	if user.PinLockedUntil != nil && time.Now().Before(*user.PinLockedUntil) {
		// Public: the borrower sees this wording on a feature phone, so it
		// must be GSM-7 clean and free of anything technical.
		return false, pinErr("verify_pin").
			With(pkgErrors.AttrUserID, userID).
			With("locked_until", user.PinLockedUntil.Format(time.RFC3339)).
			Code(pkgErrors.CodeAccountLocked).
			Public("Account locked. Try again after "+formatLockDuration(*user.PinLockedUntil)+".").
			Wrapf(ErrAccountLocked, "PIN verification attempted while the account is locked")
	}

	// Compare PIN.
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PinHash), []byte(pin)); err != nil {
		return s.handleFailedAttempt(ctx, user)
	}

	// Success — reset attempt counter.
	if user.PinAttempts > 0 {
		user.PinAttempts = 0
		user.PinLockedUntil = nil
		if err := s.userRepo.Update(ctx, user); err != nil {
			return true, pinErr("verify_pin").With(pkgErrors.AttrUserID, userID).
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not reset the PIN attempt counter")
		}
	}

	return true, nil
}

// ChangePIN verifies the old PIN then sets a new one. The new PIN must differ
// from the old and pass strength validation. Sends an SMS on success or
// failure.
func (s *Service) ChangePIN(ctx context.Context, userID, oldPin, newPin string) error {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	if user.PinHash == nil {
		s.notifyChangeFailed(ctx, user, ErrPINNotSet.Error())
		return ErrPINNotSet
	}

	// Verify old PIN without incrementing attempt counter on failure —
	// that is handled by the caller via VerifyPIN if desired.
	if err := bcrypt.CompareHashAndPassword([]byte(*user.PinHash), []byte(oldPin)); err != nil {
		s.notifyChangeFailed(ctx, user, ErrPINIncorrect.Error())
		return ErrPINIncorrect
	}

	// Reject same PIN.
	if oldPin == newPin {
		s.notifyChangeFailed(ctx, user, ErrPINSameAsOld.Error())
		return ErrPINSameAsOld
	}

	// Validate new PIN strength.
	if err := ValidatePIN(newPin); err != nil {
		s.notifyChangeFailed(ctx, user, err.Error())
		return pinErr("change_pin").Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "new PIN failed validation")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPin), BcryptCost)
	if err != nil {
		return pinErr("change_pin").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash the new PIN")
	}

	hashStr := string(hash)
	now := time.Now()
	user.PinHash = &hashStr
	user.PinSetAt = &now

	if err := s.userRepo.Update(ctx, user); err != nil {
		return pinErr("change_pin").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not save the new PIN")
	}

	// Notify success (off the request path — see notifyAsync).
	changedNote := contracts.AccountNotification{
		UserID:      userID,
		PhoneNumber: user.MobileNumber,
	}
	s.notifyAsync(func(ctx context.Context) error {
		return s.notifier.NotifyPINChanged(ctx, changedNote)
	})

	return nil
}

// ResetPIN sets a new PIN without requiring the old one. This is called after
// the caller has verified the user's identity via security questions. It
// clears any lockout state. Sends an SMS on success or failure.
func (s *Service) ResetPIN(ctx context.Context, userID, newPin string) error {
	slog.Info("pin: reset initiated", slog.String("user_id", userID))

	if err := ValidatePIN(newPin); err != nil {
		return pinErr("reset_pin").Code(pkgErrors.CodeInvalidAmount).Wrapf(err, "new PIN failed validation")
	}

	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(newPin), BcryptCost)
	if err != nil {
		s.notifyResetFailed(ctx, user, "internal error")
		return pinErr("reset_pin").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash the new PIN")
	}

	hashStr := string(hash)
	now := time.Now()
	user.PinHash = &hashStr
	user.PinSetAt = &now
	user.PinAttempts = 0
	user.PinLockedUntil = nil

	if err := s.userRepo.Update(ctx, user); err != nil {
		s.notifyResetFailed(ctx, user, "failed to save PIN")
		return pinErr("reset_pin").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not save the reset PIN")
	}

	slog.Info("pin: reset succeeded", slog.String("user_id", userID))

	resetNote := contracts.AccountNotification{
		UserID:      userID,
		PhoneNumber: user.MobileNumber,
	}
	s.notifyAsync(func(ctx context.Context) error {
		return s.notifier.NotifyPINReset(ctx, resetNote)
	})

	return nil
}

// IsLocked reports whether the user's account is currently locked due to
// failed PIN attempts. If locked, it returns the time the lockout expires.
func (s *Service) IsLocked(ctx context.Context, userID string) (bool, time.Time, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, time.Time{}, pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	if user.PinLockedUntil != nil && time.Now().Before(*user.PinLockedUntil) {
		return true, *user.PinLockedUntil, nil
	}

	return false, time.Time{}, nil
}

// HasPIN reports whether the user has a PIN set.
func (s *Service) HasPIN(ctx context.Context, userID string) (bool, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return false, pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}
	return user.PinHash != nil, nil
}

// SetSecurityQuestions stores hashed answers for the given questions. At least
// 2 questions are required. Answers are normalized (trimmed, lowercased)
// before hashing with bcrypt. Existing questions for the user are upserted.
func (s *Service) SetSecurityQuestions(ctx context.Context, userID string, questions []QuestionAnswer) error {
	if len(questions) < 2 {
		return ErrInsufficientQuestions
	}

	sqModels := make([]models.SecurityQuestion, len(questions))
	for i, qa := range questions {
		normalized := NormalizeAnswer(qa.Answer)
		hash, err := bcrypt.GenerateFromPassword([]byte(normalized), BcryptCost)
		if err != nil {
			return pinErr("set_security_questions").With("question_id", qa.QuestionID).
				Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash a security answer")
		}
		sqModels[i] = models.SecurityQuestion{
			UserID:     userID,
			QuestionID: qa.QuestionID,
			AnswerHash: string(hash),
		}
	}

	if err := s.sqRepo.UpsertForUser(ctx, sqModels); err != nil {
		return pinErr("set_security_questions").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not save the security questions")
	}

	return nil
}

// VerifySecurityAnswers checks the supplied answers against the stored hashes
// for the given user. All answers must match for the result to be true.
// Answers are normalized before comparison.
func (s *Service) VerifySecurityAnswers(ctx context.Context, userID string, answers []QuestionAnswer) (bool, error) {
	stored, err := s.sqRepo.GetByUserID(ctx, userID)
	if err != nil {
		return false, pinErr("verify_security_answers").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the security questions")
	}

	// Build lookup by question ID.
	hashByQID := make(map[int]string, len(stored))
	for _, sq := range stored {
		hashByQID[sq.QuestionID] = sq.AnswerHash
	}

	for _, qa := range answers {
		storedHash, ok := hashByQID[qa.QuestionID]
		if !ok {
			return false, nil
		}
		normalized := NormalizeAnswer(qa.Answer)
		//nolint:nilerr // a hash mismatch means the answer was wrong, which is a
		// result rather than a failure; a non-nil error here would read as a
		// system fault and lock the user out of recovery.
		if err := bcrypt.CompareHashAndPassword([]byte(storedHash), []byte(normalized)); err != nil {
			return false, nil
		}
	}

	return true, nil
}

// GetUserQuestionIDs returns the predefined question IDs that the user has
// configured for recovery. Returns an empty slice if none are set.
func (s *Service) GetUserQuestionIDs(ctx context.Context, userID string) ([]int, error) {
	ids, err := s.sqRepo.GetQuestionIDsByUserID(ctx, userID)
	if err != nil {
		return nil, pinErr("get_question_ids").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the security question ids")
	}
	return ids, nil
}

// GetRemainingAttempts returns how many PIN attempts the user has left before
// lockout. Returns 0 if the account is locked.
func (s *Service) GetRemainingAttempts(ctx context.Context, userID string) (int, error) {
	user, err := s.userRepo.GetByID(ctx, userID)
	if err != nil {
		return 0, pinErr("get_user").With(pkgErrors.AttrUserID, userID).
			Code(pkgErrors.CodeNotFound).Wrapf(err, "could not load the user")
	}

	if user.PinLockedUntil != nil && time.Now().Before(*user.PinLockedUntil) {
		return 0, nil
	}

	remaining := s.maxAttempts - user.PinAttempts
	if remaining < 0 {
		remaining = 0
	}
	return remaining, nil
}

// formatLockDuration returns a human-readable relative duration string
// (e.g. "15 minutes", "1 minute") from a lock expiry time. This avoids
// sending absolute timestamps that depend on the user's timezone.
func formatLockDuration(until time.Time) string {
	remaining := time.Until(until).Round(time.Minute)
	mins := int(remaining.Minutes())
	if mins <= 1 {
		return "1 minute"
	}
	return fmt.Sprintf("%d minutes", mins)
}

// notifyAsync sends an account notification off the caller's request path. PIN
// operations run on the USSD turn, where a slow SMS gateway would otherwise
// block the response and risk breaching the USSD deadline. Uses a detached
// context (the request ctx is cancelled when the turn returns), bounded by its
// own timeout; the send is best-effort and its error dropped.
func (s *Service) notifyAsync(send func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = send(ctx)
	}()
}

func (s *Service) handleFailedAttempt(ctx context.Context, user *models.User) (bool, error) {
	user.PinAttempts++

	if user.PinAttempts >= s.maxAttempts {
		lockUntil := time.Now().Add(s.lockout)
		user.PinLockedUntil = &lockUntil

		if err := s.userRepo.Update(ctx, user); err != nil {
			return false, pinErr("handle_failed_attempt").With(pkgErrors.AttrUserID, user.ID).
				Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not lock the account")
		}

		lockedNote := contracts.AccountNotification{
			UserID:      user.ID,
			PhoneNumber: user.MobileNumber,
			LockedUntil: formatLockDuration(lockUntil),
		}
		s.notifyAsync(func(ctx context.Context) error {
			return s.notifier.NotifyAccountLocked(ctx, lockedNote)
		})

		return false, pinErr("handle_failed_attempt").
			With(pkgErrors.AttrUserID, user.ID).
			With("locked_until", lockUntil.Format(time.RFC3339)).
			Code(pkgErrors.CodeAccountLocked).
			Public("Account locked. Try again after "+formatLockDuration(lockUntil)+".").
			Wrapf(ErrAccountLocked, "account locked after too many failed PIN attempts")
	}

	if err := s.userRepo.Update(ctx, user); err != nil {
		return false, pinErr("handle_failed_attempt").With(pkgErrors.AttrUserID, user.ID).
			Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not update the PIN attempt counter")
	}

	wrongNote := contracts.AccountNotification{
		UserID:            user.ID,
		PhoneNumber:       user.MobileNumber,
		RemainingAttempts: s.maxAttempts - user.PinAttempts,
	}
	s.notifyAsync(func(ctx context.Context) error {
		return s.notifier.NotifyPINWrongAttempt(ctx, wrongNote)
	})

	return false, nil
}

// notifyChangeFailed sends a PIN change failure notification, ignoring
// delivery errors (best-effort).
func (s *Service) notifyChangeFailed(_ context.Context, user *models.User, reason string) {
	note := contracts.AccountNotification{
		UserID:      user.ID,
		PhoneNumber: user.MobileNumber,
		Reason:      reason,
	}
	s.notifyAsync(func(ctx context.Context) error {
		return s.notifier.NotifyPINChangeFailed(ctx, note)
	})
}

// notifyResetFailed sends a PIN reset failure notification, ignoring
// delivery errors (best-effort).
func (s *Service) notifyResetFailed(_ context.Context, user *models.User, reason string) {
	note := contracts.AccountNotification{
		UserID:      user.ID,
		PhoneNumber: user.MobileNumber,
		Reason:      reason,
	}
	s.notifyAsync(func(ctx context.Context) error {
		return s.notifier.NotifyPINResetFailed(ctx, note)
	})
}
