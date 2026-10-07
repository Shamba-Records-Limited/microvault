// Package pilot holds the pilot access list: who may register, and keep
// using the service, while the pilot access gate is on.
//
// A person is approved per phone number and national ID. Both must match an
// active pilot_users row. One person may hold several rows (each new SIM is
// re-approved), so revoking and restoring act on every row sharing the
// national ID.
package pilot

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode"

	"github.com/samber/oops"
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// ErrNotFound is a phone number or national ID with no pilot_users row.
var ErrNotFound = errors.New("pilot: not found")

// Gate answers access questions while the pilot access gate is on. A nil
// Gate means the gate is off.
type Gate interface {
	// PhoneActive reports whether number has an active row.
	PhoneActive(ctx context.Context, number string) (bool, error)
	// Matches reports whether number has an active row for nationalID.
	Matches(ctx context.Context, number, nationalID string) (bool, error)
	// UserActive reports whether a registered user's current phone number and
	// national ID match an active row.
	UserActive(ctx context.Context, userID string) (bool, error)
}

// User is one pilot_users row.
type User struct {
	PhoneNumber     string `gorm:"primaryKey"`
	NationalID      string
	FullName        string
	Language        string
	Note            *string
	AddedBy         *string
	CreatedAt       time.Time
	UpdatedAt       time.Time
	RevokedAt       *time.Time
	RevokedReason   *string
	InvitedAt       *time.Time
	InviteCount     int
	LastInviteError *string
}

// TableName maps User to pilot_users.
func (User) TableName() string { return "pilot_users" }

// Active reports whether the row is not revoked.
func (u User) Active() bool { return u.RevokedAt == nil }

// Unlisted is a registered user without an active row matching both their
// phone number and national ID.
type Unlisted struct {
	ID           string
	MobileNumber string
	NationalID   *string
	FullName     *string
}

// Selector picks a person by phone number or national ID; exactly one is set.
type Selector struct {
	Phone      string
	NationalID string
}

// NormalizePhone returns phone as country code and subscriber digits with no
// leading +, the form users.mobile_number holds, reading 07…/01… as Kenyan.
// It returns "" when the number cannot be resolved.
func NormalizePhone(s string) string {
	e164 := phone.KenyaE164(s)
	if e164 == "" {
		e164 = phone.E164(s)
	}
	return strings.TrimPrefix(e164, "+")
}

// NormalizeNationalID strips whitespace and upper-cases id.
func NormalizeNationalID(id string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if unicode.IsSpace(r) {
			return -1
		}
		return r
	}, id))
}

// Store is the Postgres-backed pilot list.
type Store struct {
	db *gorm.DB
}

var _ Gate = (*Store)(nil)

// NewStore builds the store.
func NewStore(db *gorm.DB) (*Store, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &Store{db: db}, nil
}

func pilotErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainIdentity).Tags("pilot").With("op", op)
}

// nationalIDSQL normalises a national ID column the way NormalizeNationalID
// does.
func nationalIDSQL(col string) string {
	return `upper(regexp_replace(coalesce(` + col + `, ''), '\s', '', 'g'))`
}

func (s *Store) exists(ctx context.Context, op, query string, args ...any) (bool, error) {
	var found bool
	if err := s.db.WithContext(ctx).Raw("SELECT EXISTS ("+query+")", args...).Scan(&found).Error; err != nil {
		return false, pilotErr(op).Wrapf(err, "pilot lookup failed")
	}
	return found, nil
}

// PhoneActive implements Gate.
func (s *Store) PhoneActive(ctx context.Context, number string) (bool, error) {
	return s.exists(ctx, "phone_active",
		`SELECT 1 FROM pilot_users WHERE phone_number = ? AND revoked_at IS NULL`,
		NormalizePhone(number))
}

// Matches implements Gate.
func (s *Store) Matches(ctx context.Context, number, nationalID string) (bool, error) {
	id := NormalizeNationalID(nationalID)
	if id == "" {
		return false, nil
	}
	return s.exists(ctx, "matches",
		`SELECT 1 FROM pilot_users WHERE phone_number = ? AND national_id = ? AND revoked_at IS NULL`,
		NormalizePhone(number), id)
}

// UserActive implements Gate.
func (s *Store) UserActive(ctx context.Context, userID string) (bool, error) {
	return s.exists(ctx, "user_active",
		`SELECT 1 FROM users u JOIN pilot_users p
		   ON p.phone_number = u.mobile_number
		  AND p.national_id = `+nationalIDSQL("u.national_id")+`
		  AND p.revoked_at IS NULL
		 WHERE u.id = ? AND u.deleted_at IS NULL`,
		userID)
}

// Add inserts u, or updates the row for its phone number and clears any
// revoke on it. Phone and national ID are normalised; Language defaults to en.
func (s *Store) Add(ctx context.Context, u User) (User, error) {
	u.PhoneNumber = NormalizePhone(u.PhoneNumber)
	u.NationalID = NormalizeNationalID(u.NationalID)
	u.FullName = strings.TrimSpace(u.FullName)
	if u.Language == "" {
		u.Language = "en"
	}
	switch {
	case u.PhoneNumber == "":
		return User{}, pilotErr("add").Code(pkgErrors.CodeMissingPhoneNumber).Errorf("phone number is not a valid mobile number")
	case u.NationalID == "":
		return User{}, pilotErr("add").Errorf("national id is required")
	case u.FullName == "":
		return User{}, pilotErr("add").Errorf("full name is required")
	}

	var out User
	err := s.db.WithContext(ctx).Raw(`
		INSERT INTO pilot_users (phone_number, national_id, full_name, language, note, added_by)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT (phone_number) DO UPDATE SET
			national_id    = EXCLUDED.national_id,
			full_name      = EXCLUDED.full_name,
			language       = EXCLUDED.language,
			note           = COALESCE(EXCLUDED.note, pilot_users.note),
			added_by       = COALESCE(EXCLUDED.added_by, pilot_users.added_by),
			updated_at     = NOW(),
			revoked_at     = NULL,
			revoked_reason = NULL
		RETURNING *`,
		u.PhoneNumber, u.NationalID, u.FullName, u.Language, u.Note, u.AddedBy).
		Scan(&out).Error
	if err != nil {
		return User{}, pilotErr("add").With("phone_number", phone.Redact(u.PhoneNumber)).Wrapf(err, "pilot add failed")
	}
	return out, nil
}

// Get returns the row for number, or ErrNotFound.
func (s *Store) Get(ctx context.Context, number string) (User, error) {
	var found []User
	if err := s.db.WithContext(ctx).Where("phone_number = ?", NormalizePhone(number)).Limit(1).Find(&found).Error; err != nil {
		return User{}, pilotErr("get").Wrapf(err, "pilot get failed")
	}
	if len(found) == 0 {
		return User{}, ErrNotFound
	}
	return found[0], nil
}

// List returns rows by creation time, active only unless includeRevoked.
func (s *Store) List(ctx context.Context, includeRevoked bool) ([]User, error) {
	q := s.db.WithContext(ctx).Order("created_at")
	if !includeRevoked {
		q = q.Where("revoked_at IS NULL")
	}
	var out []User
	if err := q.Find(&out).Error; err != nil {
		return nil, pilotErr("list").Wrapf(err, "pilot list failed")
	}
	return out, nil
}

// nationalIDFor resolves a selector to the person's national ID.
func (s *Store) nationalIDFor(ctx context.Context, sel Selector) (string, error) {
	if sel.NationalID != "" {
		id := NormalizeNationalID(sel.NationalID)
		found, err := s.exists(ctx, "resolve", `SELECT 1 FROM pilot_users WHERE national_id = ?`, id)
		if err != nil {
			return "", err
		}
		if !found {
			return "", ErrNotFound
		}
		return id, nil
	}
	u, err := s.Get(ctx, sel.Phone)
	if err != nil {
		return "", err
	}
	return u.NationalID, nil
}

// Revoke revokes every active row for the selected person and returns them.
func (s *Store) Revoke(ctx context.Context, sel Selector, reason string) ([]User, error) {
	id, err := s.nationalIDFor(ctx, sel)
	if err != nil {
		return nil, err
	}
	var out []User
	err = s.db.WithContext(ctx).Raw(`
		UPDATE pilot_users SET revoked_at = NOW(), revoked_reason = ?, updated_at = NOW()
		WHERE national_id = ? AND revoked_at IS NULL
		RETURNING *`, reason, id).Scan(&out).Error
	if err != nil {
		return nil, pilotErr("revoke").Wrapf(err, "pilot revoke failed")
	}
	return out, nil
}

// Restore clears the revoke on every row for the selected person and returns
// them.
func (s *Store) Restore(ctx context.Context, sel Selector) ([]User, error) {
	id, err := s.nationalIDFor(ctx, sel)
	if err != nil {
		return nil, err
	}
	var out []User
	err = s.db.WithContext(ctx).Raw(`
		UPDATE pilot_users SET revoked_at = NULL, revoked_reason = NULL, updated_at = NOW()
		WHERE national_id = ? AND revoked_at IS NOT NULL
		RETURNING *`, id).Scan(&out).Error
	if err != nil {
		return nil, pilotErr("restore").Wrapf(err, "pilot restore failed")
	}
	return out, nil
}

// Registered reports whether number already belongs to a registered user.
func (s *Store) Registered(ctx context.Context, number string) (bool, error) {
	return s.exists(ctx, "registered",
		`SELECT 1 FROM users WHERE mobile_number = ? AND deleted_at IS NULL`,
		NormalizePhone(number))
}

// PendingInvites returns active rows never invited whose number has not
// registered.
func (s *Store) PendingInvites(ctx context.Context) ([]User, error) {
	var out []User
	err := s.db.WithContext(ctx).Raw(`
		SELECT p.* FROM pilot_users p
		WHERE p.revoked_at IS NULL AND p.invited_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM users u WHERE u.mobile_number = p.phone_number AND u.deleted_at IS NULL)
		ORDER BY p.created_at`).Scan(&out).Error
	if err != nil {
		return nil, pilotErr("pending_invites").Wrapf(err, "pilot pending invites failed")
	}
	return out, nil
}

// RecordInvite records one invite attempt for number: a success stamps
// invited_at, a failure only the error. Both count the attempt.
func (s *Store) RecordInvite(ctx context.Context, number string, sendErr error) error {
	var lastErr *string
	if sendErr != nil {
		msg := sendErr.Error()
		lastErr = &msg
	}
	err := s.db.WithContext(ctx).Exec(`
		UPDATE pilot_users SET
			invite_count      = invite_count + 1,
			invited_at        = CASE WHEN ?::text IS NULL THEN NOW() ELSE invited_at END,
			last_invite_error = ?,
			updated_at        = NOW()
		WHERE phone_number = ?`, lastErr, lastErr, NormalizePhone(number)).Error
	if err != nil {
		return pilotErr("record_invite").Wrapf(err, "pilot record invite failed")
	}
	return nil
}

// Unlisted returns registered users the gate would lock out.
func (s *Store) Unlisted(ctx context.Context) ([]Unlisted, error) {
	var out []Unlisted
	err := s.db.WithContext(ctx).Raw(`
		SELECT u.id, u.mobile_number, u.national_id, u.full_name FROM users u
		WHERE u.deleted_at IS NULL
		  AND NOT EXISTS (
			SELECT 1 FROM pilot_users p
			WHERE p.phone_number = u.mobile_number
			  AND p.national_id = ` + nationalIDSQL("u.national_id") + `
			  AND p.revoked_at IS NULL)
		ORDER BY u.created_at`).Scan(&out).Error
	if err != nil {
		return nil, pilotErr("unlisted").Wrapf(err, "pilot unlisted failed")
	}
	return out, nil
}
