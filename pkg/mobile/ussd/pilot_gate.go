package ussd

import (
	"context"
	"log/slog"

	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// pilotIDMaxMisses is how many national IDs that do not match the number's
// approval end the session.
const pilotIDMaxMisses = 2

// pilotPhoneGate stops an unregistered number that has no active pilot row.
// A failed lookup stops it too: the gate fails closed.
func (h *USSDHandler) pilotPhoneGate(ctx context.Context, session *Session) (string, bool) {
	if h.pilotGate == nil {
		return "", false
	}
	ok, err := h.pilotGate.PhoneActive(ctx, session.PhoneNumber)
	if err != nil {
		slog.ErrorContext(ctx, "pilot phone check failed", slog.String("phone_number", phone.Redact(session.PhoneNumber)), slog.Any("error", err))
		return h.formatError(session.Language, "error"), true
	}
	if !ok {
		return h.formatResponse(session.Language, "END", "pilot_closed"), true
	}
	return "", false
}

// pilotUserGate stops a registered user whose phone number and national ID no
// longer match an active pilot row.
func (h *USSDHandler) pilotUserGate(ctx context.Context, session *Session) (string, bool) {
	if h.pilotGate == nil || session.UserID == "" {
		return "", false
	}
	ok, err := h.pilotGate.UserActive(ctx, session.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "pilot user check failed", slog.String("user_id", session.UserID), slog.Any("error", err))
		return h.formatError(session.Language, "error"), true
	}
	if !ok {
		slog.InfoContext(ctx, "pilot access suspended", slog.String("user_id", session.UserID))
		return h.formatResponse(session.Language, "END", "pilot_suspended"), true
	}
	return "", false
}

// pilotIDGate checks the entered national ID against the number's approval.
// A mismatch re-prompts until pilotIDMaxMisses, then ends the session.
func (h *USSDHandler) pilotIDGate(ctx context.Context, session *Session, nationalID string) (body string, stop bool, err error) {
	if h.pilotGate == nil {
		return "", false, nil
	}
	ok, lookupErr := h.pilotGate.Matches(ctx, session.PhoneNumber, nationalID)
	if lookupErr != nil {
		slog.ErrorContext(ctx, "pilot id check failed", slog.String("phone_number", phone.Redact(session.PhoneNumber)), slog.Any("error", lookupErr))
		return h.formatError(session.Language, "error"), true, nil
	}
	if ok {
		delete(session.Data, "pilot_id_misses")
		return "", false, nil
	}

	misses := toInt(session.Data["pilot_id_misses"]) + 1
	if misses >= pilotIDMaxMisses {
		return h.formatResponse(session.Language, "END", "pilot_closed"), true, nil
	}
	session.Data["pilot_id_misses"] = misses
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", true, sessionSaveErr(session, err)
	}
	return h.conNav(session, "pilot_id_mismatch"), true, nil
}
