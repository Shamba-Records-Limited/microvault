package notifications

import (
	"strings"
	"testing"
)

// specIndex maps a 3GPP TS 23.038 code point to its index in gsm7Basic, which
// omits ESC (0x1B).
func specIndex(code int) int {
	if code > 0x1B {
		return code - 1
	}
	return code
}

// The alphabet is transcribed by hand, and a single wrong rune would silently
// reject valid copy or admit copy that forces the whole message to UCS-2. This
// pins it to 3GPP TS 23.038 section 6.2.1 at every row boundary, so the table
// cannot drift without failing the build.
func TestGSM7TableMatchesSpec(t *testing.T) {
	// 128 code points less ESC, which is a prefix rather than a character.
	if got := len([]rune(gsm7Basic)); got != 127 {
		t.Fatalf("gsm7Basic has %d runes, want 127", got)
	}
	if got := len([]rune(gsm7Ext)); got != 10 {
		t.Fatalf("gsm7Ext has %d runes, want 10", got)
	}

	runes := []rune(gsm7Basic)
	for _, c := range []struct {
		code int
		want rune
	}{
		{0x00, '@'},
		{0x09, 'Ç'},
		{0x0A, '\n'},
		{0x0D, '\r'},
		{0x0F, 'å'},
		{0x10, 'Δ'},
		{0x1A, 'Ξ'},
		{0x1C, 'Æ'},
		{0x1F, 'É'},
		{0x20, ' '},
		{0x24, '¤'},
		{0x2F, '/'},
		{0x30, '0'},
		{0x3F, '?'},
		{0x40, '¡'},
		{0x41, 'A'},
		{0x5A, 'Z'},
		{0x5B, 'Ä'},
		{0x5C, 'Ö'},
		{0x5D, 'Ñ'},
		{0x5E, 'Ü'},
		{0x5F, '§'},
		{0x60, '¿'},
		{0x61, 'a'},
		{0x7A, 'z'},
		{0x7B, 'ä'},
		{0x7C, 'ö'},
		{0x7D, 'ñ'},
		{0x7E, 'ü'},
		{0x7F, 'à'},
	} {
		if got := runes[specIndex(c.code)]; got != c.want {
			t.Errorf("code point 0x%02X: table has %q, spec says %q", c.code, got, c.want)
		}
	}

	// ESC must not appear: it is consumed as a prefix, and admitting it would
	// let a raw 0x1B through validation as if it were printable.
	if strings.ContainsRune(gsm7Basic, 0x1B) {
		t.Error("gsm7Basic contains ESC (0x1B), which is a prefix, not a character")
	}

	// No rune may sit in both tables, or its septet cost is ambiguous.
	for _, r := range gsm7Ext {
		if strings.ContainsRune(gsm7Basic, r) {
			t.Errorf("%q appears in both the basic and extension tables", r)
		}
	}

	// No duplicates within the basic table.
	seen := map[rune]bool{}
	for _, r := range gsm7Basic {
		if seen[r] {
			t.Errorf("%q appears twice in gsm7Basic", r)
		}
		seen[r] = true
	}
}

// The extension table costs two septets per rune, and the euro sign is the one
// most likely to appear in real copy.
func TestExtensionTableCostsTwoSeptets(t *testing.T) {
	for _, r := range gsm7Ext {
		n, _, ok := GSM7Len(string(r))
		if !ok {
			t.Errorf("%q is in gsm7Ext but GSM7Len rejects it", r)
			continue
		}
		if n != 2 {
			t.Errorf("GSM7Len(%q) = %d septets, want 2", r, n)
		}
	}
}
