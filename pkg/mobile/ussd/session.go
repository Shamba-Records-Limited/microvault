package ussd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/Shamba-Records-Limited/microvault/pkg/phone"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// NewSessionManager creates a new session manager
// ErrSessionNotFound marks a session that is genuinely absent from the cache,
// as distinct from a cache that cannot be read. Both end the borrower's turn,
// but only one of them is an outage.
var ErrSessionNotFound = errors.New("session not found")

// sessionErr starts an error builder for session persistence.
func sessionErr(op, sessionID string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainUSSD).
		Tags("ussd", "session").
		With(pkgErrors.AttrOperation, op).
		With("session_id", sessionID)
}

func NewSessionManager(cache *redis.Client, sessionDuration time.Duration) *SessionManager {
	if sessionDuration == 0 {
		sessionDuration = 5 * time.Minute // Default 5 minutes
	}

	return &SessionManager{
		cache:           cache,
		sessionDuration: sessionDuration,
	}
}

// GetSession retrieves a session by session ID
func (sm *SessionManager) GetSession(ctx context.Context, sessionID string) (*Session, error) {
	key := sm.sessionKey(sessionID)

	// Get the JSON data from Redis
	data, err := sm.cache.Get(ctx, key).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			log.Printf("GetSession - session not found for key: %s", key)
			// Wraps the sentinel so a genuinely absent session is
			// distinguishable from a cache that cannot be read.
			return nil, sessionErr("get", sessionID).Code(pkgErrors.CodeNotFound).
				Wrapf(ErrSessionNotFound, "session is not in the cache")
		}
		return nil, sessionErr("get", sessionID).Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the session")
	}

	// Unmarshal JSON into Session struct
	var session Session
	if err := json.Unmarshal([]byte(data), &session); err != nil {
		return nil, sessionErr("get", sessionID).Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the stored session")
	}

	log.Printf("GetSession successful - Key: %s, CurrentMenu: %s, Phone: %s", key, session.CurrentMenu, phone.Redact(session.PhoneNumber))
	return &session, nil
}

// CreateSession creates a new USSD session
func (sm *SessionManager) CreateSession(ctx context.Context, sessionID, phoneNumber, serviceCode, networkCode string) (*Session, error) {
	log.Printf("CreateSession - Creating new session for SessionID: %s, Phone: %s", sessionID, phone.Redact(phoneNumber))

	session := &Session{
		SessionID:     sessionID,
		PhoneNumber:   phoneNumber,
		ServiceCode:   serviceCode,
		NetworkCode:   networkCode,
		CurrentMenu:   "main",
		PreviousMenus: []string{},
		Language:      "en",
		Data:          make(map[string]any),
		CreatedAt:     time.Now(),
		UpdatedAt:     time.Now(),
	}

	if err := sm.SaveSession(ctx, session); err != nil {
		return nil, sessionErr("create", sessionID).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not create the session")
	}

	log.Printf("CreateSession - New session created successfully")
	return session, nil
}

// SaveSession saves a session to cache
func (sm *SessionManager) SaveSession(ctx context.Context, session *Session) error {
	session.UpdatedAt = time.Now()

	// Marshal session to JSON
	data, err := json.Marshal(session)
	if err != nil {
		return sessionErr("save", session.SessionID).Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the session")
	}

	key := sm.sessionKey(session.SessionID)
	if err := sm.cache.Set(ctx, key, data, sm.sessionDuration).Err(); err != nil {
		return sessionErr("save", session.SessionID).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not save the session")
	}

	log.Printf("SaveSession successful - Key: %s, CurrentMenu: %s, Duration: %v", key, session.CurrentMenu, sm.sessionDuration)
	return nil
}

// UpdateSession updates session data
func (sm *SessionManager) UpdateSession(ctx context.Context, sessionID string, updates func(*Session)) error {
	session, err := sm.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}

	updates(session)

	return sm.SaveSession(ctx, session)
}

// NavigateToMenu navigates to a new menu
func (sm *SessionManager) NavigateToMenu(ctx context.Context, sessionID, menu string) error {
	return sm.UpdateSession(ctx, sessionID, func(s *Session) {
		s.PreviousMenus = append(s.PreviousMenus, s.CurrentMenu)
		s.CurrentMenu = menu
	})
}

// GoBack navigates to previous menu
func (sm *SessionManager) GoBack(ctx context.Context, sessionID string) error {
	return sm.UpdateSession(ctx, sessionID, func(s *Session) {
		if len(s.PreviousMenus) > 0 {
			s.CurrentMenu = s.PreviousMenus[len(s.PreviousMenus)-1]
			s.PreviousMenus = s.PreviousMenus[:len(s.PreviousMenus)-1]
		}
	})
}

// SetData sets a data value in the session
func (sm *SessionManager) SetData(ctx context.Context, sessionID, key string, value any) error {
	return sm.UpdateSession(ctx, sessionID, func(s *Session) {
		s.Data[key] = value
	})
}

// GetData retrieves a data value from the session
func (sm *SessionManager) GetData(ctx context.Context, sessionID, key string) (any, error) {
	session, err := sm.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}

	value, ok := session.Data[key]
	if !ok {
		return nil, sessionErr("get_data", session.SessionID).With("key", key).Code(pkgErrors.CodeNotFound).Errorf("key is not in the session data")
	}

	return value, nil
}

// SetUserID associates a user ID with the session
func (sm *SessionManager) SetUserID(ctx context.Context, sessionID, userID string) error {
	return sm.UpdateSession(ctx, sessionID, func(s *Session) {
		s.UserID = userID
	})
}

// SetLanguage sets the session language
func (sm *SessionManager) SetLanguage(ctx context.Context, sessionID, language string) error {
	return sm.UpdateSession(ctx, sessionID, func(s *Session) {
		s.Language = language
	})
}

// DeleteSession deletes a session
func (sm *SessionManager) DeleteSession(ctx context.Context, sessionID string) error {
	key := sm.sessionKey(sessionID)
	if err := sm.cache.Del(ctx, key).Err(); err != nil {
		return sessionErr("delete", sessionID).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not delete the session")
	}
	return nil
}

// ExtendSession extends the session expiration
func (sm *SessionManager) ExtendSession(ctx context.Context, sessionID string) error {
	key := sm.sessionKey(sessionID)
	if err := sm.cache.Expire(ctx, key, sm.sessionDuration).Err(); err != nil {
		return sessionErr("extend", sessionID).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not extend the session TTL")
	}
	return nil
}

// sessionKey generates a cache key for a session
func (sm *SessionManager) sessionKey(sessionID string) string {
	return fmt.Sprintf("ussd_session:%s", sessionID)
}

// GetOrCreateSession gets an existing session or creates a new one
func (sm *SessionManager) GetOrCreateSession(ctx context.Context, sessionID, phoneNumber, serviceCode, networkCode string) (*Session, error) {
	session, err := sm.GetSession(ctx, sessionID)
	if err != nil {
		// Session doesn't exist, create new one
		return sm.CreateSession(ctx, sessionID, phoneNumber, serviceCode, networkCode)
	}

	// Extend existing session
	if err := sm.ExtendSession(ctx, sessionID); err != nil {
		return nil, sessionErr("extend", sessionID).Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not extend the session TTL")
	}

	return session, nil
}
