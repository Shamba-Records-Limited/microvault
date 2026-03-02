package sms

// DeliveryReport represents an SMS delivery report callback from Africa's Talking.
// AT sends these as form-encoded POST requests to the configured callback URL.
type DeliveryReport struct {
	ID            string // AT message ID (matches messageId from send response)
	Status        string // Sent|Submitted|Buffered|Rejected|Success|Failed|AbsentSubscriber|Expired
	PhoneNumber   string
	NetworkCode   string
	FailureReason string // Only present if Rejected or Failed
}

// IsFinalStatus returns true if the delivery status is terminal (no further updates expected).
func (d DeliveryReport) IsFinalStatus() bool {
	switch d.Status {
	case "Success", "Failed", "Rejected":
		return true
	default:
		return false
	}
}
