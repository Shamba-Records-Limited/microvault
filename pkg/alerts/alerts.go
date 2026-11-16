// Package alerts raises ops alerts: things a human must act on. Every alert
// is emitted as one structured log line, which OpenObserve alert rules match:
//
//	level=ERROR msg="ops alert" alert_subject=… alert_message=… alert_ref=…
//
// plus whatever the context carries through logging.With (loan_id,
// loan_reference, trace_id). alert_ref names what the alert is about in one
// field, so a rule can group and print it whether or not a loan is involved. Delivery to Slack, Telegram and PagerDuty is
// OpenObserve's job, not the code's.
package alerts

import (
	"context"
	"log/slog"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
)

// Message and attribute keys OpenObserve alert rules match on. Changing any
// of them silently breaks alerting.
const (
	Message     = "ops alert"
	AttrSubject = "alert_subject"
	AttrMessage = "alert_message"
	AttrRef     = "alert_ref"
	AttrAccount = "account_id"
)

// Ref names what an alert is about from the attributes on ctx: the loan as
// "REFERENCE (id)", else the account, else the user, else "-".
func Ref(ctx context.Context) string {
	found := map[string]string{}
	for _, a := range logging.Attrs(ctx) {
		found[a.Key] = a.Value.String()
	}
	loanID, loanRef := found[pkgErrors.AttrLoanID], found[pkgErrors.AttrLoanReference]
	switch {
	case loanRef != "" && loanID != "":
		return loanRef + " (" + loanID + ")"
	case loanRef != "":
		return loanRef
	case loanID != "":
		return "loan " + loanID
	case found[AttrAccount] != "":
		return "account " + found[AttrAccount]
	case found[pkgErrors.AttrUserID] != "":
		return "user " + found[pkgErrors.AttrUserID]
	}
	return "-"
}

// Service raises an ops alert. subject is a stable, low-cardinality title
// that alert rules route on; message carries the specifics.
type Service interface {
	AlertOps(ctx context.Context, subject, message string) error
}

// LogAlerter raises an alert as the canonical log line.
type LogAlerter struct {
	Logger *slog.Logger
}

// AlertOps implements Service. It never fails.
func (l LogAlerter) AlertOps(ctx context.Context, subject, message string) error {
	logger := l.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.ErrorContext(ctx, Message, AttrSubject, subject, AttrMessage, message, AttrRef, Ref(ctx))
	return nil
}

// Raise sends an alert through svc. A nil svc, or one that fails, falls back
// to the log line, so an alert is never lost to a delivery problem.
func Raise(ctx context.Context, svc Service, logger *slog.Logger, subject, message string) {
	fallback := LogAlerter{Logger: logger}
	if svc == nil {
		_ = fallback.AlertOps(ctx, subject, message)
		return
	}
	if err := svc.AlertOps(ctx, subject, message); err != nil {
		if logger == nil {
			logger = slog.Default()
		}
		logger.WarnContext(ctx, "ops alert delivery failed, logging it instead", AttrSubject, subject, "error", err)
		_ = fallback.AlertOps(ctx, subject, message)
	}
}
