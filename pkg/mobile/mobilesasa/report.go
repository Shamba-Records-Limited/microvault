package mobilesasa

import (
	"encoding/json"
	"fmt"
	"strings"
)

// DeliveryReport is the JSON body of a delivery-report callback.
type DeliveryReport struct {
	DeliveryStatus string `json:"deliveryStatus"`
	Status         string `json:"status"`
	DeliveryTime   string `json:"deliveryTime"`
	Reference      string `json:"reference"`
	TrackingID     string `json:"trackingId"`
	MSISDN         string `json:"msisdn"`
	Cost           string `json:"cost"`
	Sender         string `json:"sender"`
	DLR            struct {
		TrackingID   string `json:"tracking_id"`
		Gateway      string `json:"gateway"`
		DeliveryCode string `json:"delivery_code"`
		Description  string `json:"description"`
	} `json:"dlr"`
}

// ParseDeliveryReport decodes a delivery-report body.
func ParseDeliveryReport(body []byte) (DeliveryReport, error) {
	var r DeliveryReport
	if err := json.Unmarshal(body, &r); err != nil {
		return DeliveryReport{}, fmt.Errorf("mobilesasa: decode delivery report: %w", err)
	}
	if r.Reference == "" || r.Status == "" {
		return DeliveryReport{}, fmt.Errorf("mobilesasa: delivery report is missing reference or status")
	}
	return r, nil
}

// Final reports whether no later report will follow.
func (r DeliveryReport) Final() bool { return r.Status != StatusSent }

// CarrierReason is the carrier's own wording for a report that did not
// deliver, empty for a delivered or interim one.
func (r DeliveryReport) CarrierReason() string {
	if r.Status == StatusDelivered || r.Status == StatusSent {
		return ""
	}
	if r.DeliveryStatus != "" {
		return r.DeliveryStatus
	}
	return r.DLR.Description
}

// HashedMSISDN reports whether the recipient arrived as a SHA-256 hash, as
// some Safaricom traffic does, rather than a phone number.
func (r DeliveryReport) HashedMSISDN() bool {
	if len(r.MSISDN) != 64 {
		return false
	}
	return strings.Trim(strings.ToLower(r.MSISDN), "0123456789abcdef") == ""
}

// USSDServiceCode returns the dialled code without the subscriber input the
// gateway splices into it: "*657*45*1*2500#" with text "1*2500" is "*657*45#".
func USSDServiceCode(serviceCode, text string) string {
	if text == "" {
		return serviceCode
	}
	if base, ok := strings.CutSuffix(serviceCode, "*"+text+"#"); ok && base != "" {
		return base + "#"
	}
	return serviceCode
}
