package notifications

import (
	"fmt"
	"strings"
	"testing"
)

// The cash-pickup initiation SMS must stay within a single GSM-7 segment (160
// chars) so the short-link URL is never split across concatenated parts —
// splitting breaks both wrapping and auto-linkification in SMS inboxes.
func TestCashPickupInitiatedFitsOneSegment(t *testing.T) {
	tmpl := DefaultLoanTemplates()

	// Representative worst-case inputs: a full loan reference and a short-link
	// on the configured public domain.
	ref := "LR-19F6C760B82-4bb8"
	link := "https://microvault.outray.app/r/Xk9f2aQ7ab"

	msg := fmt.Sprintf(tmpl.CashPickupInitiated, ref, link)

	if len(msg) > 160 {
		t.Errorf("message is %d chars, exceeds one GSM-7 segment (160):\n%s", len(msg), msg)
	}

	// The URL must sit alone on its own line for reliable linkification.
	if !strings.Contains(msg, "\n"+link+"\n") {
		t.Errorf("URL is not isolated on its own line:\n%s", msg)
	}
}
