package mobilesasa

import (
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/mobilesasa"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

// statuses maps the gateway's harmonised status onto the delivery
// vocabulary the SMS log and portal read.
var statuses = map[string]string{
	mobilesasa.StatusDelivered:   "Success",
	mobilesasa.StatusFailed:      "Failed",
	mobilesasa.StatusRejected:    "Rejected",
	mobilesasa.StatusExpired:     "Expired",
	mobilesasa.StatusUnreachable: "Failed",
	mobilesasa.StatusSent:        "Sent",
}

// ReportParser reads Mobile Sasa's JSON delivery reports. With a verifier,
// a report whose signature does not check out is refused.
func ReportParser(v *mobilesasa.Verifier) sms.ReportParser {
	return func(header func(string) string, body []byte) (sms.DeliveryReport, error) {
		if v != nil {
			if err := v.Verify(header, body); err != nil {
				return sms.DeliveryReport{}, sms.ErrReportUnauthenticated
			}
		}
		r, err := mobilesasa.ParseDeliveryReport(body)
		if err != nil {
			return sms.DeliveryReport{}, err
		}
		return toReport(r), nil
	}
}

func toReport(r mobilesasa.DeliveryReport) sms.DeliveryReport {
	status, ok := statuses[r.Status]
	if !ok {
		status = r.Status
	}
	out := sms.DeliveryReport{
		ID:            r.Reference,
		Status:        status,
		NetworkCode:   mobilesasa.MobileNetworkCode(r.DLR.Gateway),
		FailureReason: r.CarrierReason(),
	}
	if !r.HashedMSISDN() {
		out.PhoneNumber = r.MSISDN
	}
	return out
}
