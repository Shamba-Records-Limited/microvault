// Package mobilesasa implements the SMS provider adapter for Mobile Sasa.
package mobilesasa

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/mobilesasa"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
	"github.com/Shamba-Records-Limited/microvault/pkg/phone"
)

// ProviderName is the name the adapter is registered and recorded under.
const ProviderName = "mobilesasa"

const maxSendAttempts = 3

// Africa's Talking per-recipient codes the gateway verdicts map onto.
const (
	codeQueued          = 102
	codeInvalidSender   = 402
	codeInvalidPhone    = 403
	codeNoBalance       = 405
	codeRejectedGateway = 502
)

// Sender sends one message; *mobilesasa.Client satisfies it.
type Sender interface {
	Send(ctx context.Context, req mobilesasa.SendRequest) (mobilesasa.SendResult, error)
}

// Adapter sends SMS through Mobile Sasa, one v2 request per recipient so each
// carries its own message ID and tracking ID.
type Adapter struct {
	sender      Sender
	senderID    string
	callbackURL string
	backoff     time.Duration
}

// New builds the adapter. senderID is used when a request names none;
// callbackURL, when set, receives this deployment's delivery reports.
func New(sender Sender, senderID, callbackURL string) *Adapter {
	return &Adapter{sender: sender, senderID: senderID, callbackURL: callbackURL, backoff: time.Second}
}

var _ sms.SMSProvider = (*Adapter)(nil)

// SendSMS sends req to each of its recipients.
func (a *Adapter) SendSMS(ctx context.Context, req sms.SMSRequest) (sms.SMSResponse, error) {
	to := req.ToMultiple
	if len(to) == 0 && req.To != "" {
		to = []string{req.To}
	}
	if len(to) == 0 {
		return sms.SMSResponse{}, errors.New("mobilesasa: no recipients specified")
	}
	from := req.From
	if from == "" {
		from = a.senderID
	}
	if from == "" {
		return sms.SMSResponse{}, errors.New("mobilesasa: no sender ID configured")
	}

	recipients := make([]sms.Recipient, 0, len(to))
	results := make([]mobilesasa.SendResult, 0, len(to))
	for _, number := range to {
		r, res, err := a.sendOne(ctx, from, number, req.Message)
		if err != nil {
			return sms.SMSResponse{ProviderData: results, Recipients: recipients}, err
		}
		recipients = append(recipients, r)
		results = append(results, res)
	}
	return sms.SMSResponse{ProviderData: results, Recipients: recipients}, nil
}

// sendOne returns an error only when the whole send must stop: the account
// or token is wrong, or the gateway could not be reached. A message the
// gateway refused comes back as a rejected recipient.
func (a *Adapter) sendOne(ctx context.Context, from, number, message string) (sms.Recipient, mobilesasa.SendResult, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return sms.Recipient{}, mobilesasa.SendResult{}, fmt.Errorf("mobilesasa: tracking id: %w", err)
	}
	req := mobilesasa.SendRequest{
		SenderID: from, Phone: number, Message: message,
		TrackingID: id.String(), CallbackURL: a.callbackURL,
	}

	var lastErr error
	for attempt := 1; attempt <= maxSendAttempts; attempt++ {
		if attempt > 1 {
			select {
			case <-ctx.Done():
				return sms.Recipient{}, mobilesasa.SendResult{}, fmt.Errorf("mobilesasa: send abandoned after %d attempt(s): %w", attempt-1, lastErr)
			case <-time.After(time.Duration(attempt-1) * a.backoff):
			}
		}
		res, err := a.sender.Send(ctx, req)
		if err == nil {
			return sms.Recipient{
				Number: number, MessageID: res.MessageID, Status: "Queued",
				StatusCode: codeQueued, Cost: partsCost(res.Parts),
			}, res, nil
		}
		lastErr = err

		var apiErr *mobilesasa.APIError
		if errors.As(err, &apiErr) {
			if apiErr.Account() {
				return sms.Recipient{}, mobilesasa.SendResult{}, err
			}
			if !apiErr.Throttled() {
				return rejected(number, apiErr), mobilesasa.SendResult{}, nil
			}
		} else if !neverSent(err) {
			return sms.Recipient{}, mobilesasa.SendResult{}, err
		}
		slog.WarnContext(ctx, "sms: send attempt failed, will retry",
			slog.String("provider", ProviderName),
			slog.Int("attempt", attempt),
			slog.String("to", phone.Redact(number)),
			slog.String("error", err.Error()),
		)
	}
	return sms.Recipient{}, mobilesasa.SendResult{}, fmt.Errorf("mobilesasa: send failed after %d attempts: %w", maxSendAttempts, lastErr)
}

// neverSent reports whether the request failed before reaching the gateway,
// so sending again cannot duplicate it. A timeout after the request went out
// is not retried: the message may already be queued.
func neverSent(err error) bool {
	var opErr *net.OpError
	return errors.As(err, &opErr) && opErr.Op == "dial"
}

func rejected(number string, e *mobilesasa.APIError) sms.Recipient {
	code := codeRejectedGateway
	switch e.Code {
	case mobilesasa.CodeNoBalance:
		code = codeNoBalance
	case mobilesasa.CodeNotFound:
		code = codeInvalidSender
	case mobilesasa.CodeInvalid:
		if strings.Contains(strings.ToLower(e.Message), "phone") {
			code = codeInvalidPhone
		}
	}
	status := fmt.Sprintf("gateway answered HTTP %d", e.HTTPStatus)
	if e.Code != "" {
		status = fmt.Sprintf("%s (%s)", e.Message, e.Code)
	}
	return sms.Recipient{Number: number, Status: status, StatusCode: code}
}

func partsCost(parts int) string {
	switch {
	case parts <= 0:
		return ""
	case parts == 1:
		return "1 part"
	default:
		return fmt.Sprintf("%d parts", parts)
	}
}

// SendSingleSMS sends one message to one number.
func (a *Adapter) SendSingleSMS(ctx context.Context, to, message, from string) (sms.SMSResponse, error) {
	return a.SendSMS(ctx, sms.SMSRequest{To: to, Message: message, From: from})
}

// SendBulkSMS sends one message to each number.
func (a *Adapter) SendBulkSMS(ctx context.Context, to []string, message, from string) (sms.SMSResponse, error) {
	return a.SendSMS(ctx, sms.SMSRequest{ToMultiple: to, Message: message, From: from})
}
