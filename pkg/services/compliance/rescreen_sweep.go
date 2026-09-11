package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// rescreenSweepBatchSize bounds one tick's rescreens. At institutional
// depositor cardinality this is generous — the source design doc §3 notes
// Elliptic's 500-requests-per-minute limit is "generous for our volume"
// even for a full sweep; one address per synchronous call, well within it.
const rescreenSweepBatchSize = 50

// RescreenSweep is the source design doc §6's "our own sweep": neither of
// Elliptic's own rescreening mechanisms (three rescreens over a window, or
// retries on a failed call) amount to standing monitoring, so an allowlist
// that gates money needs its own scheduled walk of every address whose
// screening has gone stale.
//
// Unlike a batch-endpoint sweep, this reuses Service.ScreenAndRecord one
// address at a time — Phase 1 deliberately didn't build the
// POST /v2/wallet batch endpoint (out of scope, see pkg/compliance/elliptic
// doc.go), and at this volume the sync endpoint in a loop costs nothing
// extra worth a second client code path for.
type RescreenSweep struct {
	repo     repository.CounterpartyRepository
	service  *Service
	interval time.Duration
	logger   *slog.Logger
}

// RescreenSweepDeps are RescreenSweep's collaborators; all required except
// Logger.
type RescreenSweepDeps struct {
	Repo     repository.CounterpartyRepository
	Service  *Service
	Interval time.Duration
	Logger   *slog.Logger
}

// NewRescreenSweep builds the sweep.
func NewRescreenSweep(deps RescreenSweepDeps) *RescreenSweep {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	return &RescreenSweep{
		repo:     deps.Repo,
		service:  deps.Service,
		interval: deps.Interval,
		logger:   logger.With("component", "rescreen_sweep"),
	}
}

// Start runs the sweep until ctx is cancelled — the same shape as every
// other ticker in this codebase.
func (s *RescreenSweep) Start(ctx context.Context) {
	ticker := time.NewTicker(s.interval)
	defer ticker.Stop()

	s.logger.Info("starting", "interval", s.interval)
	s.tick(ctx)

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("shutting down")
			return
		case <-ticker.C:
			s.tick(ctx)
		}
	}
}

// tick first makes staleness visible (flip any lapsed approved address to
// AddressStatusExpired, a bulk statement independent of the rescreen
// attempts below), then tries to rescreen a batch of expired addresses. A
// rescreen that fails leaves the address visibly expired rather than
// silently still showing a stale "approved" badge — see
// Service.applyVerdict's VerdictPending branch for why a failed or
// still-processing rescreen never claims a status it doesn't have.
func (s *RescreenSweep) tick(ctx context.Context) {
	touched, err := s.repo.MarkExpiredAddresses(ctx)
	if err != nil {
		s.logger.Error("could not mark expired addresses", "error", err)
	} else if touched > 0 {
		s.logger.Info("marked addresses expired", "count", touched)
	}

	addrs, err := s.repo.ListAddressesByStatus(ctx, []string{string(models.AddressStatusExpired)}, rescreenSweepBatchSize, 0)
	if err != nil {
		s.logger.Error("could not list expired addresses to rescreen", "error", err)
		return
	}
	for _, addr := range addrs {
		if err := s.service.ScreenAndRecord(ctx, addr.ID); err != nil {
			s.logger.Error("rescreen failed, address stays expired until the next sweep",
				"address_id", addr.ID, "error", err)
		}
	}
}
