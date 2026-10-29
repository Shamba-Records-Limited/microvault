package compliance

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/datatypes"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
	"github.com/Shamba-Records-Limited/microvault/pkg/telemetry"
)

// Alert subjects for deposit screening. The sanctioned one pages.
const (
	SubjectDepositSanctioned     = "Lender deposit screened as sanctioned"
	SubjectDepositNeedsReview    = "Lender deposit needs compliance review"
	SubjectDepositScreeningStuck = "Lender deposit could not be screened"
)

// screeningBackoff spaces retries of a screening that failed or is still
// pending at the provider; its length is the attempt limit.
var screeningBackoff = []time.Duration{
	time.Minute, 5 * time.Minute, 15 * time.Minute, time.Hour, 3 * time.Hour, 6 * time.Hour, 12 * time.Hour, 24 * time.Hour,
}

const depositBatch = 25

// DepositScreener screens queued vault deposits with the configured
// providers. A bad verdict alerts a person; it never touches the allowlist.
type DepositScreener struct {
	repo     repository.DepositScreeningRepository
	screener pkgcompliance.TransactionScreener
	alerts   alerts.Service
	interval time.Duration
	now      func() time.Time
	logger   *slog.Logger
}

// DepositScreenerDeps are DepositScreener's collaborators; Logger, Alerts
// and Interval are optional.
type DepositScreenerDeps struct {
	Repo     repository.DepositScreeningRepository
	Screener pkgcompliance.TransactionScreener
	Alerts   alerts.Service
	Interval time.Duration
	Logger   *slog.Logger
}

// NewDepositScreener builds the worker.
func NewDepositScreener(deps DepositScreenerDeps) *DepositScreener {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	interval := deps.Interval
	if interval <= 0 {
		interval = time.Minute
	}
	return &DepositScreener{
		repo:     deps.Repo,
		screener: deps.Screener,
		alerts:   deps.Alerts,
		interval: interval,
		now:      time.Now,
		logger:   logger.With("component", "deposit_screener"),
	}
}

// Start runs until ctx is cancelled.
func (s *DepositScreener) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()
	s.logger.InfoContext(ctx, "starting", "interval", s.interval)
	for {
		s.Tick(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Tick screens every deposit that is due.
func (s *DepositScreener) Tick(ctx context.Context) {
	ctx, span := telemetry.StartRoot(ctx, "deposit_screener.tick")
	defer span.End()
	due, err := s.repo.ListDue(ctx, s.now(), depositBatch)
	if err != nil {
		s.logger.ErrorContext(ctx, "could not list due deposit screenings", "error", err)
		return
	}
	for _, d := range due {
		if ctx.Err() != nil {
			return
		}
		s.screen(ctx, d)
	}
}

func (s *DepositScreener) screen(ctx context.Context, d *models.DepositScreening) {
	req := pkgcompliance.TxScreenRequest{Hash: d.TxHash, Address: d.Depositor, Recipient: d.Receiver}
	cp, err := s.repo.Counterparty(ctx, d.Depositor)
	if err != nil {
		s.retry(ctx, d, fmt.Sprintf("counterparty lookup: %v", err))
		return
	}
	if cp != nil {
		req.CustomerReference = cp.CustomerReference
	} else {
		req.CustomerReference = "unregistered-" + d.Depositor
	}

	callCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	res, err := s.screener.ScreenTransaction(callCtx, req)
	cancel()
	if err != nil {
		s.retry(ctx, d, err.Error())
		return
	}
	if res.Verdict == pkgcompliance.VerdictPending {
		s.retry(ctx, d, "provider has not finished the analysis")
		return
	}

	raw := []byte(res.Raw)
	if len(raw) == 0 {
		raw = []byte("{}")
	}
	record := &models.DepositScreening{
		Provider:    res.Provider,
		ProviderRef: res.ProviderRef,
		Verdict:     string(res.Verdict),
		Sanctioned:  res.Sanctioned,
		RiskScore:   res.RiskScore,
		RawPayload:  datatypes.JSON(raw),
	}
	if cp != nil {
		record.CounterpartyAddressID = &cp.AddressID
	}
	if err := s.repo.RecordResult(ctx, d.ID, record); err != nil {
		s.logger.ErrorContext(ctx, "could not record a deposit screening", "deposit_screening_id", d.ID, "error", err)
		return
	}

	switch res.Verdict {
	case pkgcompliance.VerdictRejected:
		s.alert(ctx, d, SubjectDepositSanctioned, fmt.Sprintf(
			"Deposit tx %s from %s was screened %s by %s (sanctioned: %t). Funds are in the vault; review the lender before any further deposit or redemption. Nothing was revoked automatically.",
			d.TxHash, d.Depositor, res.Verdict, res.Provider, res.Sanctioned))
	case pkgcompliance.VerdictReview:
		s.alert(ctx, d, SubjectDepositNeedsReview, fmt.Sprintf(
			"Deposit tx %s from %s needs compliance review (%s).", d.TxHash, d.Depositor, res.Provider))
	default:
	}
	if cp == nil {
		s.logger.WarnContext(ctx, "deposit from an address that is not a registered lender address",
			"tx_hash", d.TxHash, "depositor", d.Depositor)
	}
}

func (s *DepositScreener) retry(ctx context.Context, d *models.DepositScreening, reason string) {
	attempt := d.Attempts
	final := attempt >= len(screeningBackoff)-1
	next := s.now().Add(screeningBackoff[min(attempt, len(screeningBackoff)-1)])
	if err := s.repo.RecordAttemptFailed(ctx, d.ID, reason, next, final); err != nil {
		s.logger.ErrorContext(ctx, "could not record a failed deposit screening attempt", "deposit_screening_id", d.ID, "error", err)
		return
	}
	s.logger.WarnContext(ctx, "deposit screening attempt did not complete",
		"deposit_screening_id", d.ID, "tx_hash", d.TxHash, "attempt", attempt+1, "final", final, "reason", reason)
	if final {
		s.alert(ctx, d, SubjectDepositScreeningStuck, fmt.Sprintf(
			"Deposit tx %s from %s could not be screened after %d attempts: %s", d.TxHash, d.Depositor, attempt+1, reason))
	}
}

func (s *DepositScreener) alert(ctx context.Context, d *models.DepositScreening, subject, message string) {
	alerts.Raise(ctx, s.alerts, s.logger, subject, message)
	if err := s.repo.MarkAlerted(ctx, d.ID, s.now()); err != nil {
		s.logger.ErrorContext(ctx, "could not record the deposit alert", "deposit_screening_id", d.ID, "error", err)
	}
}
