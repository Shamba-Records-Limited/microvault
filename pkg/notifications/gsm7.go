package notifications

import (
	"strings"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
)

// GSM 03.38 default alphabet. Runes outside it force the whole SMS to UCS-2,
// which cuts a segment from 160 characters to 70; extension-table runes are
// escaped and cost two septets each.
//
// gsm7Basic holds 127 runes, not 128: ESC (0x1B) is the prefix that introduces
// an extension-table rune, never a character in its own right, so it is absent
// and every code point above 0x1B sits one index lower than its spec position.
// TestGSM7TableMatchesSpec pins that mapping.
const (
	gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	gsm7Ext = "^{}\\[~]|€\f"
)

// GSM7Len returns the septet count of s. When s contains a rune outside GSM
// 03.38 it returns ok=false along with the offending rune, and the count is
// meaningless.
func GSM7Len(s string) (septets int, bad rune, ok bool) {
	n := 0
	for _, r := range s {
		switch {
		case strings.ContainsRune(gsm7Basic, r):
			n++
		case strings.ContainsRune(gsm7Ext, r):
			n += 2
		default:
			return 0, r, false
		}
	}
	return n, 0, true
}

// Segments returns how many concatenated SMS parts a GSM-7 message of the given
// septet count occupies. Concatenation headers cost seven septets per part, so
// anything past a single segment carries 153 rather than 160.
func Segments(septets int) int {
	switch {
	case septets <= 0:
		return 0
	case septets <= 160:
		return 1
	default:
		return (septets + 152) / 153
	}
}

// SentinelLoanNotification returns a fully-populated loan notification used to
// exercise templates at construction time. Every field carries a worst-case
// realistic value so validation measures the longest message a template can
// produce, not the shortest.
func SentinelLoanNotification() contracts.LoanNotification {
	due := time.Now().Add(30 * 24 * time.Hour)
	return contracts.LoanNotification{
		LoanID:            "9f6c7601-b824-4bb8-91a2-0d3e5f7a1c22",
		LoanReference:     "LR-19F6C760B82-4bb8",
		UserID:            "1c2d3e4f-5a6b-7c8d-9e0f-1a2b3c4d5e6f",
		PhoneNumber:       "254711000111",
		Amount:            30100000000,
		DisplayAmount:     3010.00,
		DisplayCurrency:   "KES",
		Status:            "disbursed",
		Reason:            "insufficient repayment history",
		RemainingBalance:  1505.00,
		DueDate:           &due,
		InteractiveURL:    "https://microvault.outray.app/r/Xk9f2aQ7ab",
		CashPickupRef:     "79342377",
		CashPickupInfoURL: "https://mgv.link/Xk9f2aQ7ab",
	}
}

// SentinelAccountNotification returns a fully-populated account notification
// for the same purpose as [SentinelLoanNotification].
func SentinelAccountNotification() contracts.AccountNotification {
	return contracts.AccountNotification{
		UserID:            "1c2d3e4f-5a6b-7c8d-9e0f-1a2b3c4d5e6f",
		PhoneNumber:       "254711000111",
		FullName:          "Alice Wanjiku Kamau",
		RemainingAttempts: 2,
		LockedUntil:       "15 minutes",
		Reason:            "security answers did not match",
	}
}
