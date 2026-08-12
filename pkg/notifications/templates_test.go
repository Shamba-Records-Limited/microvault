package notifications

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// GSM 03.38 default alphabet. Runes outside it force the whole SMS to UCS-2,
// which cuts a segment from 160 characters to 70; extension-table runes are
// escaped and cost two septets each.
const (
	gsm7Basic = "@£$¥èéùìòÇ\nØø\rÅåΔ_ΦΓΛΩΠΨΣΘΞÆæßÉ !\"#¤%&'()*+,-./0123456789:;<=>?¡" +
		"ABCDEFGHIJKLMNOPQRSTUVWXYZÄÖÑÜ§¿abcdefghijklmnopqrstuvwxyzäöñüà"
	gsm7Ext = "^{}\\[~]|€\f"
)

// gsm7Len returns the septet count of s, or the first rune outside GSM 03.38.
func gsm7Len(s string) (int, rune, bool) {
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

// Every template must encode as GSM-7. One accented rune outside the alphabet
// silently halves the segment budget for the whole message.
func TestTemplatesAreGSM7(t *testing.T) {
	check := func(lang string, v any) {
		s := reflect.ValueOf(v).Elem()
		for i := range s.NumField() {
			text := s.Field(i).String()
			if _, bad, ok := gsm7Len(text); !ok {
				t.Errorf("%s %s: %q is outside GSM 03.38:\n%s",
					lang, s.Type().Field(i).Name, bad, text)
			}
		}
	}
	for lang, tmpl := range localizedLoanTemplates() {
		check(lang, tmpl)
	}
	for lang, tmpl := range localizedAccountTemplates() {
		check(lang, tmpl)
	}
}

// The cash-pickup initiation SMS must stay within a single GSM-7 segment so the
// short-link URL is never split across concatenated parts — splitting breaks
// both wrapping and auto-linkification in SMS inboxes.
func TestCashPickupInitiatedFitsOneSegment(t *testing.T) {
	// Representative worst-case inputs: a full loan reference and a short-link
	// on the configured public domain.
	ref := "LR-19F6C760B82-4bb8"
	link := "https://microvault.outray.app/r/Xk9f2aQ7ab"

	for lang, tmpl := range localizedLoanTemplates() {
		msg := fmt.Sprintf(tmpl.CashPickupInitiated, ref, link)

		n, bad, ok := gsm7Len(msg)
		if !ok {
			t.Errorf("%s: %q is outside GSM 03.38:\n%s", lang, bad, msg)
			continue
		}
		if n > 160 {
			t.Errorf("%s: message is %d septets, exceeds one GSM-7 segment (160):\n%s", lang, n, msg)
		}

		// The URL must close the message on its own line: linkification is most
		// reliable when nothing follows the link, and Google Messages only
		// offers a preview when the link starts or ends the body.
		if !strings.HasSuffix(msg, "\n"+link) {
			t.Errorf("%s: URL does not end the message on its own line:\n%s", lang, msg)
		}
	}
}
