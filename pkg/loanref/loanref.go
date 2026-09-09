// Package loanref generates and validates short loan references.
//
// A loan reference is the only binding between a paybill payment and the loan
// it repays. It is typed by the borrower on a feature phone, read aloud over
// the phone, and resolved by C2B validation and C2B Hakikisha before a database
// is ever consulted. Every property of the format follows from those three
// constraints: it is short, read-aloud safe, non-enumerable, and carries a
// check character so a mistyped reference fails in milliseconds rather than
// after a round trip.
//
// The format is a 2-character prefix, 6 random Crockford base32 characters,
// and 1 check character — 9 characters total, three under Daraja's 12-character
// AccountReference cap.
package loanref

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// alphabet is the Crockford base32 alphabet: 32 characters excluding I, L, O
// and U — the glyphs that get misread and misheard. Read-aloud safe, and all
// characters are GSM 03.38-safe so a reference survives SMS and USSD prompts
// without forcing UCS-2.
const alphabet = "0123456789ABCDEFGHJKMNPQRSTVWXYZ"

const (
	// prefixLen is the length of the product prefix. Fixed at 2 because the
	// reference has exactly 3 characters of headroom under the Daraja cap, and
	// 1 of them is the check character.
	prefixLen = 2

	// randomLen is the number of random characters. 32^6 ≈ 1.07e9 values.
	randomLen = 6

	// referenceLen is the total reference length.
	referenceLen = prefixLen + randomLen + 1
)

// DefaultPrefix is used when none is configured.
const DefaultPrefix = "MV"

// alphabetIndex maps a byte to its position in the alphabet, or -1.
var alphabetIndex [256]int

func init() {
	for i := range alphabetIndex {
		alphabetIndex[i] = -1
	}
	for i := range len(alphabet) {
		alphabetIndex[alphabet[i]] = i
	}
}

// ValidatePrefix rejects any prefix that is not exactly prefixLen characters
// from the Crockford alphabet. The confusables I, L, O and U are excluded
// deliberately: a prefix that allows them reintroduces the misreading the
// alphabet exists to prevent, on a value that is read aloud and typed on a
// feature phone. A wrong value is a configuration error and must fail startup,
// not the first repayment.
func ValidatePrefix(prefix string) error {
	if len(prefix) != prefixLen {
		return fmt.Errorf("loan reference prefix must be exactly %d characters, got %d", prefixLen, len(prefix))
	}
	for i := 0; i < len(prefix); i++ {
		if alphabetIndex[prefix[i]] < 0 {
			return fmt.Errorf("loan reference prefix %q contains %q, which is not in the Crockford base32 alphabet (I, L, O and U are excluded)", prefix, string(prefix[i]))
		}
	}
	return nil
}

// checkChar computes the Crockford check character over the given body
// (prefix + random characters) as a simple modulo-32 sum of the alphabet
// positions. It varies with the prefix, so the C2B validator re-derives it
// from the same configured prefix.
func checkChar(body string) byte {
	var sum int
	for i := 0; i < len(body); i++ {
		sum += alphabetIndex[body[i]]
	}
	return alphabet[sum%len(alphabet)]
}

// Validate reports whether ref is a well-formed reference under prefix: right
// length, all characters in the alphabet, and a correct check character. A
// value that fails Validate never reaches a database lookup.
func Validate(prefix, ref string) bool {
	if len(ref) != referenceLen {
		return false
	}
	if !strings.HasPrefix(ref, prefix) {
		return false
	}
	body := ref[:referenceLen-1]
	for i := 0; i < len(body); i++ {
		if alphabetIndex[body[i]] < 0 {
			return false
		}
	}
	return checkChar(body) == ref[referenceLen-1]
}

// Generate produces a new reference under prefix, sourcing randomness from
// crypto/rand so references are not enumerable. The previous format embedded a
// millisecond timestamp in hex; that made the reference space searchable, which
// is unacceptable now that the reference alone binds a payment to a loan.
//
// Generate does not check uniqueness against the database: the caller inserts
// under a unique index and retries on conflict. At 6 random characters the
// birthday bound is comfortable for years, so a conflict is rare and a bounded
// retry turns it into a second attempt rather than a 500.
func Generate(prefix string) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}

	body := make([]byte, prefixLen+randomLen)
	copy(body, prefix)

	raw := make([]byte, randomLen)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("loan reference: could not read randomness: %w", err)
	}
	for i := 0; i < randomLen; i++ {
		body[prefixLen+i] = alphabet[raw[i]&0x1f]
	}

	return string(body) + string(checkChar(string(body))), nil
}

// GenerateDeterministic produces a reference derived from a fixed seed, used by
// the backfill migration so a re-run produces the same reference for the same
// row rather than a new random one. It is not for new loans: two seeds that
// differ produce uncorrelated references, but the same seed always produces the
// same one.
func GenerateDeterministic(prefix string, seed [16]byte) (string, error) {
	if err := ValidatePrefix(prefix); err != nil {
		return "", err
	}

	body := make([]byte, prefixLen+randomLen)
	copy(body, prefix)
	for i := 0; i < randomLen; i++ {
		body[prefixLen+i] = alphabet[seed[i]&0x1f]
	}

	return string(body) + string(checkChar(string(body))), nil
}
