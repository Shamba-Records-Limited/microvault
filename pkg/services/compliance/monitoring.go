package compliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/merkle"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"
)

// Alert subjects for provider monitoring. The rejected one pages.
const (
	SubjectMonitoringRejected  = "Lender address rescreened as rejected"
	SubjectMonitoringFlagged   = "Lender address flagged by monitoring"
	SubjectMonitoringStuck     = "Lender address rescreen failed"
	SubjectMonitoringUnmatched = "Monitoring alert not matched to a lender address"
)

const monitoringBatch = 25

// AddressRescreener screens one lender address again and records the result.
type AddressRescreener interface {
	Rescreen(ctx context.Context, addressID string) (*pkgcompliance.Screening, error)
}

// Monitoring turns provider monitoring alerts into rescreens of the lender
// addresses they name. The rescreen may change an address's status in the
// portal; it never revokes on chain, which stays a person's action.
type Monitoring struct {
	repo       repository.MonitoringSignalRepository
	rescreener AddressRescreener
	alerts     alerts.Service
	interval   time.Duration
	now        func() time.Time
	logger     *slog.Logger
}

// MonitoringDeps are Monitoring's collaborators; Logger, Alerts and Interval
// are optional.
type MonitoringDeps struct {
	Repo       repository.MonitoringSignalRepository
	Rescreener AddressRescreener
	Alerts     alerts.Service
	Interval   time.Duration
	Logger     *slog.Logger
}

// NewMonitoring builds the service.
func NewMonitoring(deps MonitoringDeps) *Monitoring {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := deps.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	return &Monitoring{
		repo:       deps.Repo,
		rescreener: deps.Rescreener,
		alerts:     deps.Alerts,
		interval:   interval,
		now:        time.Now,
		logger:     logger.With("component", "compliance_monitoring"),
	}
}

// EventKey identifies a delivery so a provider's retries are queued once.
func EventKey(body []byte) string {
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

// HandleMerkleWebhook queues a rescreen for every lender address a verified
// Compass notification names. A notification naming none is kept as
// unmatched and alerted, so a lender alert the parser missed still reaches a
// person.
func (m *Monitoring) HandleMerkleWebhook(ctx context.Context, body []byte) error {
	w, err := merkle.ParseWebhook(body)
	if err != nil {
		return fmt.Errorf("decode merkle webhook: %w", err)
	}
	if w.IsReport() {
		return nil
	}
	matched, err := m.repo.MatchAddresses(ctx, w.Addresses)
	if err != nil {
		return err
	}
	key := EventKey(body)
	if len(matched) == 0 {
		n, err := m.repo.Enqueue(ctx, []*models.MonitoringSignal{{
			Provider: "merkle", Event: w.EventType, EventKey: key, Status: models.MonitoringSignalUnmatched,
			Payload: datatypes.JSON(body), ReceivedAt: m.now(),
		}})
		if err != nil {
			return err
		}
		if n > 0 {
			alerts.Raise(ctx, m.alerts, m.logger, SubjectMonitoringUnmatched, fmt.Sprintf(
				"Merkle Science sent a %q notification that names no screened lender address (addresses found: %d). Check it in Compass.",
				w.EventType, len(w.Addresses)))
		}
		return nil
	}
	_, err = m.queue(ctx, "merkle", w.EventType, key, body, matched)
	return err
}

// QueueCounterparty queues a rescreen of every screened address of a
// counterparty, for a provider event about the business as a whole.
func (m *Monitoring) QueueCounterparty(ctx context.Context, provider, event, counterpartyID string, body []byte) (int64, error) {
	addrs, err := m.repo.CounterpartyAddresses(ctx, counterpartyID)
	if err != nil {
		return 0, err
	}
	return m.queue(ctx, provider, event, EventKey(body), body, addrs)
}

func (m *Monitoring) queue(ctx context.Context, provider, event, key string, body []byte, addrs []repository.MonitoredAddress) (int64, error) {
	signals := make([]*models.MonitoringSignal, 0, len(addrs))
	for _, a := range addrs {
		signals = append(signals, &models.MonitoringSignal{
			Provider: provider, Event: event, EventKey: key,
			CounterpartyID: &a.CounterpartyID, CounterpartyAddressID: &a.AddressID,
			Status: models.MonitoringSignalQueued, Payload: datatypes.JSON(body), ReceivedAt: m.now(),
		})
	}
	return m.repo.Enqueue(ctx, signals)
}

// Start runs until ctx is cancelled.
func (m *Monitoring) Start(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	m.logger.InfoContext(ctx, "starting", "interval", m.interval)
	for {
		m.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick rescreens every address whose signal is due.
func (m *Monitoring) Tick(ctx context.Context) {
	ctx, span := telemetry.StartRoot(ctx, "compliance_monitoring.tick")
	defer span.End()
	due, err := m.repo.ListDue(ctx, m.now(), monitoringBatch)
	if err != nil {
		m.logger.ErrorContext(ctx, "could not list due monitoring signals", "error", err)
		return
	}
	for _, sig := range due {
		if ctx.Err() != nil {
			return
		}
		m.rescreen(ctx, sig)
	}
}

func (m *Monitoring) rescreen(ctx context.Context, sig *models.MonitoringSignal) {
	if sig.CounterpartyAddressID == nil {
		m.retry(ctx, sig, "signal has no lender address", true)
		return
	}
	addressID := *sig.CounterpartyAddressID
	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	res, err := m.rescreener.Rescreen(callCtx, addressID)
	cancel()
	if err != nil {
		m.retry(ctx, sig, err.Error(), false)
		return
	}
	if res.Verdict == pkgcompliance.VerdictPending {
		m.retry(ctx, sig, "provider has not finished the analysis", false)
		return
	}
	if err := m.repo.RecordResult(ctx, sig.ID, string(res.Verdict), res.Provider, m.now()); err != nil {
		m.logger.ErrorContext(ctx, "could not record a monitoring rescreen", "signal_id", sig.ID, "error", err)
		return
	}

	source := fmt.Sprintf("%s %q", sig.Provider, sig.Event)
	if res.Verdict == pkgcompliance.VerdictRejected {
		m.alert(ctx, sig, SubjectMonitoringRejected, fmt.Sprintf(
			"After a %s alert, lender address %s was rescreened as rejected by %s (sanctioned: %t). The address is now rejected in the portal but stays on the vault allowlist until a person revokes it.",
			source, addressID, res.Provider, res.Sanctioned))
		return
	}
	m.alert(ctx, sig, SubjectMonitoringFlagged, fmt.Sprintf(
		"After a %s alert, lender address %s was rescreened by %s with verdict %s. Review the alert and the address in the portal.",
		source, addressID, res.Provider, res.Verdict))
}

func (m *Monitoring) retry(ctx context.Context, sig *models.MonitoringSignal, reason string, final bool) {
	attempt := sig.Attempts
	final = final || attempt >= len(screeningBackoff)-1
	next := m.now().Add(screeningBackoff[min(attempt, len(screeningBackoff)-1)])
	if err := m.repo.RecordAttemptFailed(ctx, sig.ID, reason, next, final); err != nil {
		m.logger.ErrorContext(ctx, "could not record a failed monitoring rescreen", "signal_id", sig.ID, "error", err)
		return
	}
	m.logger.WarnContext(ctx, "monitoring rescreen did not complete",
		"signal_id", sig.ID, "attempt", attempt+1, "final", final, "reason", reason)
	if final {
		addressID := ""
		if sig.CounterpartyAddressID != nil {
			addressID = *sig.CounterpartyAddressID
		}
		m.alert(ctx, sig, SubjectMonitoringStuck, fmt.Sprintf(
			"A %s %q alert for lender address %s could not be rescreened after %d attempts: %s",
			sig.Provider, sig.Event, addressID, attempt+1, reason))
	}
}

func (m *Monitoring) alert(ctx context.Context, sig *models.MonitoringSignal, subject, message string) {
	alerts.Raise(ctx, m.alerts, m.logger, subject, message)
	if err := m.repo.MarkAlerted(ctx, sig.ID, m.now()); err != nil {
		m.logger.ErrorContext(ctx, "could not record the monitoring alert", "signal_id", sig.ID, "error", err)
	}
}
