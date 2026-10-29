package compliance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// KYB alert subjects; none pages, each asks a person to act in the portal.
const (
	SubjectKYBVerified    = "Lender KYB verified by Didit"
	SubjectKYBNeedsReview = "Lender KYB needs review"
	SubjectKYBDeclined    = "Lender KYB declined by Didit"
)

// kybProvider names Didit on counterparty rows and events.
const kybProvider = "didit"

// KYBVerifier creates hosted business-verification sessions.
type KYBVerifier interface {
	CreateKYBSession(ctx context.Context, workflowID, vendorData, callback string) (*didit.Session, error)
}

// KYB runs lender business verification with Didit: it starts sessions and
// records the status changes Didit reports. Approving the lender stays a
// person's decision in the portal.
type KYB struct {
	counterparties repository.CounterpartyRepository
	repo           repository.KYBRepository
	verifier       KYBVerifier
	workflowID     string
	callback       string
	alerts         alerts.Service
	now            func() time.Time
	logger         *slog.Logger
}

// KYBDeps are KYB's collaborators. Verifier may be nil when Didit is not
// configured; Start then fails with ErrKYBNotConfigured.
type KYBDeps struct {
	Counterparties repository.CounterpartyRepository
	Repo           repository.KYBRepository
	Verifier       KYBVerifier
	WorkflowID     string
	// Callback is where Didit sends the lender's contact after the hosted
	// flow; empty uses Didit's default.
	Callback string
	Alerts   alerts.Service
	Logger   *slog.Logger
}

// NewKYB builds the KYB service.
func NewKYB(deps KYBDeps) *KYB {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &KYB{
		counterparties: deps.Counterparties,
		repo:           deps.Repo,
		verifier:       deps.Verifier,
		workflowID:     deps.WorkflowID,
		callback:       deps.Callback,
		alerts:         deps.Alerts,
		now:            time.Now,
		logger:         logger.With("component", "compliance_kyb"),
	}
}

// Configured reports whether Didit verification is available.
func (k *KYB) Configured() bool {
	return k != nil && k.verifier != nil && k.workflowID != ""
}

// openStatuses are session statuses that can still reach a decision; a
// session in one of them is reused rather than replaced.
var openStatuses = map[string]bool{
	didit.StatusNotStarted:   true,
	didit.StatusInProgress:   true,
	didit.StatusAwaitingUser: true,
	didit.StatusInReview:     true,
	didit.StatusResubmitted:  true,
}

// Start returns the hosted verification link for a counterparty, creating a
// Didit session unless an open one exists.
func (k *KYB) Start(ctx context.Context, counterpartyID string) (string, error) {
	if !k.Configured() {
		return "", ErrKYBNotConfigured
	}
	cp, err := k.counterparties.GetByID(ctx, counterpartyID)
	if err != nil {
		return "", err
	}
	if cp.KYBStatus == models.CounterpartyKYBApproved {
		return "", ErrKYBAlreadyApproved
	}
	if cp.KYBSessionID != nil && cp.KYBSessionURL != "" && openStatuses[cp.KYBSessionStatus] {
		return cp.KYBSessionURL, nil
	}
	s, err := k.verifier.CreateKYBSession(ctx, k.workflowID, cp.ID, k.callback)
	if err != nil {
		return "", err
	}
	if err := k.repo.SetSession(ctx, cp.ID, kybProvider, s.ID, s.URL, didit.StatusNotStarted, k.now()); err != nil {
		return "", err
	}
	return s.URL, nil
}

// HandleEvent records a verified Didit webhook. Events for other session
// kinds are ignored; an event for a session that is no longer the
// counterparty's current one is logged and dropped.
func (k *KYB) HandleEvent(ctx context.Context, ev *didit.Event, body []byte) error {
	if ev.Data.SessionKind != "business" || ev.Data.Status == "" {
		return nil
	}
	if ev.Data.VendorData == "" || ev.Data.SessionID == "" {
		return fmt.Errorf("didit event has no vendor data or session id")
	}
	name := ev.Event
	if name == "" {
		name = ev.WebhookType
	}
	err := k.repo.RecordEvent(ctx, &models.CounterpartyKYBEvent{
		CounterpartyID: ev.Data.VendorData,
		Provider:       kybProvider,
		SessionID:      ev.Data.SessionID,
		Event:          name,
		Status:         ev.Data.Status,
		PreviousStatus: ev.Data.PreviousStatus,
		Payload:        datatypes.JSON(body),
		ReceivedAt:     k.now(),
	})
	switch {
	case err == nil:
	case errors.Is(err, repository.ErrKYBSessionMismatch), errors.Is(err, repository.ErrCounterpartyNotFound):
		k.logger.WarnContext(ctx, "didit event for an unknown or superseded session",
			"counterparty_id", ev.Data.VendorData, "session_id", ev.Data.SessionID, "status", ev.Data.Status)
		return nil
	default:
		return err
	}

	switch ev.Data.Status {
	case didit.StatusApproved:
		k.alert(ctx, SubjectKYBVerified, fmt.Sprintf("Didit approved counterparty %s (session %s). Review and approve it in the portal.", ev.Data.VendorData, ev.Data.SessionID))
	case didit.StatusInReview:
		k.alert(ctx, SubjectKYBNeedsReview, fmt.Sprintf("Didit needs a manual review for counterparty %s (session %s).", ev.Data.VendorData, ev.Data.SessionID))
	case didit.StatusDeclined:
		k.alert(ctx, SubjectKYBDeclined, fmt.Sprintf("Didit declined counterparty %s (session %s). Review the decision in the portal.", ev.Data.VendorData, ev.Data.SessionID))
	}
	return nil
}

func (k *KYB) alert(ctx context.Context, subject, message string) {
	alerts.Raise(ctx, k.alerts, k.logger, subject, message)
}
