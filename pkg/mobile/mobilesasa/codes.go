package mobilesasa

// Response codes on the v1/v2 envelope.
const (
	CodeAccepted     = "0200"
	CodeCreated      = "0201"
	CodeUnauthorized = "0401"
	CodeNoBalance    = "0402"
	CodeForbidden    = "0403"
	CodeNotFound     = "0404"
	CodeDuplicate    = "0409"
	CodeInvalid      = "0422"
)

// Delivery statuses on the harmonised DLR status field.
const (
	StatusDelivered   = "Delivered"
	StatusFailed      = "Failed"
	StatusRejected    = "Rejected"
	StatusExpired     = "Expired"
	StatusUnreachable = "Unreachable"
	StatusSent        = "Sent"
)

// mobileNetworkCodes maps the gateway's network names and its USSD
// networkCode digits to the MCC+MNC codes the rest of the stack uses.
var mobileNetworkCodes = map[string]string{
	"safaricom": "63902",
	"airtel":    "63903",
	"telkom":    "63907",
	"equitel":   "63999",
	"1":         "63902",
	"2":         "63903",
	"3":         "63907",
	"4":         "63999",
}

// MobileNetworkCode returns the MCC+MNC for a gateway network name or USSD
// networkCode. A value already in MCC+MNC form is returned unchanged; an
// unknown one returns "".
func MobileNetworkCode(v string) string {
	if code, ok := mobileNetworkCodes[v]; ok {
		return code
	}
	if len(v) == 5 && v[:3] == "639" {
		return v
	}
	return ""
}
