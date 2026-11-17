package sms

import (
	"errors"
	"fmt"
	"net/url"
)

// ReportParser reads one provider's delivery-report callback. header reads a
// request header by name; body is the raw request body.
type ReportParser func(header func(string) string, body []byte) (DeliveryReport, error)

// ErrReportUnauthenticated marks a callback that failed the provider's own
// authentication, as opposed to a malformed one.
var ErrReportUnauthenticated = errors.New("sms: delivery report failed authentication")

// ParseFormReport reads the form-encoded report Africa's Talking posts.
func ParseFormReport(_ func(string) string, body []byte) (DeliveryReport, error) {
	form, err := url.ParseQuery(string(body))
	if err != nil {
		return DeliveryReport{}, fmt.Errorf("sms: decode delivery report: %w", err)
	}
	r := DeliveryReport{
		ID:            form.Get("id"),
		Status:        form.Get("status"),
		PhoneNumber:   form.Get("phoneNumber"),
		NetworkCode:   form.Get("networkCode"),
		FailureReason: form.Get("failureReason"),
	}
	if r.ID == "" || r.Status == "" {
		return DeliveryReport{}, fmt.Errorf("sms: delivery report is missing id or status")
	}
	return r, nil
}
