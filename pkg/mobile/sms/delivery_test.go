package sms

import "testing"

func TestStoredPhone(t *testing.T) {
	for in, want := range map[string]string{
		"+254712345678": "254712345678",
		"0712345678":    "254712345678",
		"254712345678":  "254712345678",
		"+12025550143":  "12025550143",
	} {
		if got := StoredPhone(in); got != want {
			t.Errorf("StoredPhone(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMessageFinal(t *testing.T) {
	for status, final := range map[string]bool{
		StatusSubmitted: false, "Sent": false, "Buffered": false, "AbsentSubscriber": false,
		"Success": true, "Failed": true, StatusRejected: true, "Expired": true, StatusSendFailed: true,
	} {
		if got := (Message{Status: status}).Final(); got != final {
			t.Errorf("Final(%s) = %v, want %v", status, got, final)
		}
	}
}
