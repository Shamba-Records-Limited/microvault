package phone

import "strings"

// Redact masks the middle of a phone number for safe logging, keeping the first
// six and last three digits: "254711222111" becomes "254711***111". A leading
// "+" is preserved. Numbers with nine or fewer digits are returned unchanged —
// there is nothing in the middle worth masking.
func Redact(phone string) string {
	digits := phone
	prefix := ""
	if strings.HasPrefix(phone, "+") {
		prefix = "+"
		digits = phone[1:]
	}
	if len(digits) <= 9 {
		return phone
	}
	return prefix + digits[:6] + "***" + digits[len(digits)-3:]
}

// Normalize ensures the number is in international format with a leading "+",
// trimming surrounding whitespace. It neither strips separators nor validates.
func Normalize(phone string) string {
	phone = strings.TrimSpace(phone)
	if phone == "" {
		return phone
	}
	if !strings.HasPrefix(phone, "+") {
		phone = "+" + phone
	}
	return phone
}

// E164 normalises a phone number to the "+<country><subscriber>" form. Separators
// are removed, a "00" international prefix becomes "+", and a bare international
// number gains a "+". A national-format number (leading "0", no country code)
// cannot be resolved without country context and yields "" so the caller omits
// the field rather than sending a wrong one.
func E164(phone string) string {
	var b strings.Builder
	for _, r := range phone {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	digits := b.String()
	if digits == "" {
		return ""
	}
	digits = strings.TrimPrefix(digits, "00")
	if digits == "" || strings.HasPrefix(digits, "0") {
		return ""
	}
	return "+" + digits
}

// Format strips non-digits from a phone number and prepends a country code when
// the number is not already international. countryCode defaults to "+254"
// (Kenya) when empty.
func Format(phoneNumber, countryCode string) string {
	cleaned := ""
	for _, r := range phoneNumber {
		if r >= '0' && r <= '9' {
			cleaned += string(r)
		}
	}

	if len(cleaned) > 0 && cleaned[0] != '+' && !(len(cleaned) >= 2 && cleaned[:2] == "00") {
		if countryCode == "" {
			countryCode = "+254" // Default to Kenya
		}
		cleaned = countryCode + cleaned
	}

	return cleaned
}
