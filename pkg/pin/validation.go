// Package pin provides PIN management services including creation, verification,
// change, reset, and security question handling for USSD-based user
// authentication.
package pin

import (
	"errors"
	"fmt"
	"strings"
	"unicode"
)

// PIN validation errors.
var (
	// ErrPINLength is returned when the PIN is not exactly PINLength digits.
	ErrPINLength = fmt.Errorf("PIN must be exactly %d digits", PINLength)

	// ErrPINNotDigits is returned when the PIN contains non-digit characters.
	ErrPINNotDigits = errors.New("PIN must contain only digits")

	// ErrPINAllSame is returned when every digit in the PIN is identical
	// (e.g. 0000, 1111).
	ErrPINAllSame = errors.New("PIN must not be all the same digit")

	// ErrPINSequential is returned when the PIN is a sequential run of
	// ascending or descending digits (e.g. 1234, 9876).
	ErrPINSequential = errors.New("PIN must not be a sequential number")
)

// PINLength is the required number of digits in a valid PIN.
const PINLength = 4

// ValidatePIN checks that pin satisfies all strength requirements:
//   - Exactly PINLength digits
//   - Not all the same digit (e.g. 1111)
//   - Not a sequential ascending or descending run (e.g. 1234, 4321)
//
// Returns nil if the PIN is acceptable.
func ValidatePIN(pin string) error {
	if len(pin) != PINLength {
		return ErrPINLength
	}

	for _, r := range pin {
		if !unicode.IsDigit(r) {
			return ErrPINNotDigits
		}
	}

	if isAllSame(pin) {
		return ErrPINAllSame
	}

	if isSequential(pin) {
		return ErrPINSequential
	}

	return nil
}

// NormalizeAnswer prepares a security question answer for hashing by trimming
// whitespace and lowercasing. This reduces false negatives from trivial
// formatting differences (e.g. "Nakuru" vs "nakuru").
func NormalizeAnswer(answer string) string {
	return strings.ToLower(strings.TrimSpace(answer))
}

// isAllSame returns true if every character in s is identical.
func isAllSame(s string) bool {
	if len(s) == 0 {
		return true
	}
	first := s[0]
	for i := 1; i < len(s); i++ {
		if s[i] != first {
			return false
		}
	}
	return true
}

// isSequential returns true if the digits in s form a strictly ascending or
// strictly descending run with a step of 1 (e.g. "1234", "9876").
func isSequential(s string) bool {
	if len(s) < 2 {
		return false
	}

	ascending := true
	descending := true
	for i := 1; i < len(s); i++ {
		diff := int(s[i]) - int(s[i-1])
		if diff != 1 {
			ascending = false
		}
		if diff != -1 {
			descending = false
		}
	}

	return ascending || descending
}
