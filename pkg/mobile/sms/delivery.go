package sms

// DeliveryReport is one delivery-report callback in Africa's Talking's
// vocabulary; other providers' reports are mapped onto it.
type DeliveryReport struct {
	ID            string // provider message ID (matches messageId from send response)
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
