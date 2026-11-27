package chippermon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/govalues/decimal"

	"github.com/Shamba-Records-Limited/microvault/pkg/alerts"
)

// Alert subjects.
const (
	SubjectFloatLow       = "Chipper float low"
	SubjectFloatRecovered = "Chipper float recovered"
	SubjectFeeMismatch    = "Chipper fee mismatch"
)

// BalanceReader reports the spendable USDC on the Chipper balance.
type BalanceReader interface {
	AvailableBalance(ctx context.Context) (float64, error)
}

// FloatMonitorConfig sets the threshold and pacing. RealertEvery repeats the
// alert while the float stays low.
type FloatMonitorConfig struct {
	Threshold    decimal.Decimal
	Interval     time.Duration
	RealertEvery time.Duration
}

// FloatMonitor alerts when the Chipper USDC float falls below the threshold,
// and once more when it recovers.
type FloatMonitor struct {
	balance BalanceReader
	alerts  alerts.Service
	logger  *slog.Logger
	cfg     FloatMonitorConfig
	now     func() time.Time

	low       bool
	lastAlert time.Time
}

// NewFloatMonitor builds a monitor; Interval defaults to 5 min and
// RealertEvery to 6 h.
func NewFloatMonitor(balance BalanceReader, svc alerts.Service, logger *slog.Logger, cfg FloatMonitorConfig) *FloatMonitor {
	if cfg.Interval <= 0 {
		cfg.Interval = 5 * time.Minute
	}
	if cfg.RealertEvery <= 0 {
		cfg.RealertEvery = 6 * time.Hour
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &FloatMonitor{balance: balance, alerts: svc, logger: logger.With("component", "chipper_float"), cfg: cfg, now: time.Now}
}

// Run checks the float until ctx is cancelled.
func (m *FloatMonitor) Run(ctx context.Context) {
	m.logger.InfoContext(ctx, "chipper float monitor started", "threshold_usdc", m.cfg.Threshold.String())
	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		m.Check(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

// Check reads the float once and alerts on a crossing.
func (m *FloatMonitor) Check(ctx context.Context) {
	raw, err := m.balance.AvailableBalance(ctx)
	if err != nil {
		m.logger.WarnContext(ctx, "chipper float unreadable", "error", err)
		return
	}
	available, err := decimal.NewFromFloat64(raw)
	if err != nil {
		m.logger.WarnContext(ctx, "chipper float out of range", "balance", raw, "error", err)
		return
	}

	now := m.now()
	switch below := available.Cmp(m.cfg.Threshold) < 0; {
	case below && (!m.low || now.Sub(m.lastAlert) >= m.cfg.RealertEvery):
		m.low, m.lastAlert = true, now
		alerts.Raise(ctx, m.alerts, m.logger, SubjectFloatLow, fmt.Sprintf(
			"Chipper USDC float is %s, below the %s threshold. Payout fees are drawn from it; top it up from the treasury before payouts fail with insufficient_funds.",
			available.String(), m.cfg.Threshold.String()))
	case !below && m.low:
		m.low = false
		alerts.Raise(ctx, m.alerts, m.logger, SubjectFloatRecovered, fmt.Sprintf(
			"Chipper USDC float is back at %s, above the %s threshold.", available.String(), m.cfg.Threshold.String()))
	}
}
