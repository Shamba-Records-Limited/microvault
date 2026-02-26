package pin

import (
	"errors"
	"testing"
)

func TestValidatePIN(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pin     string
		wantErr error
	}{
		// Valid PINs
		{"valid four digits", "2580", nil},
		{"valid with zero prefix", "0827", nil},
		{"valid mixed", "9371", nil},

		// Wrong length
		{"too short", "123", ErrPINLength},
		{"too long", "12345", ErrPINLength},
		{"empty", "", ErrPINLength},

		// Non-digit characters
		{"contains letter", "12a4", ErrPINNotDigits},
		{"all letters", "abcd", ErrPINNotDigits},
		{"contains space", "12 4", ErrPINNotDigits},
		{"contains symbol", "12#4", ErrPINNotDigits},

		// All same digit
		{"all zeros", "0000", ErrPINAllSame},
		{"all ones", "1111", ErrPINAllSame},
		{"all fives", "5555", ErrPINAllSame},
		{"all nines", "9999", ErrPINAllSame},

		// Sequential ascending
		{"ascending 0123", "0123", ErrPINSequential},
		{"ascending 1234", "1234", ErrPINSequential},
		{"ascending 3456", "3456", ErrPINSequential},
		{"ascending 6789", "6789", ErrPINSequential},

		// Sequential descending
		{"descending 9876", "9876", ErrPINSequential},
		{"descending 4321", "4321", ErrPINSequential},
		{"descending 3210", "3210", ErrPINSequential},

		// Non-sequential that look similar
		{"not sequential 1357", "1357", nil},
		{"not sequential 2468", "2468", nil},
		{"not sequential 1243", "1243", nil},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			err := ValidatePIN(tt.pin)
			if tt.wantErr == nil {
				if err != nil {
					t.Errorf("ValidatePIN(%q) = %v; want nil", tt.pin, err)
				}
				return
			}
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ValidatePIN(%q) = %v; want %v", tt.pin, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeAnswer(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"lowercase unchanged", "nakuru", "nakuru"},
		{"uppercase to lower", "NAKURU", "nakuru"},
		{"mixed case", "Nakuru", "nakuru"},
		{"leading spaces", "  nakuru", "nakuru"},
		{"trailing spaces", "nakuru  ", "nakuru"},
		{"both spaces", "  Nakuru  ", "nakuru"},
		{"tabs and newlines", "\tNakuru\n", "nakuru"},
		{"empty string", "", ""},
		{"only spaces", "   ", ""},
		{"multi word", "  My First Pet  ", "my first pet"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := NormalizeAnswer(tt.input)
			if got != tt.want {
				t.Errorf("NormalizeAnswer(%q) = %q; want %q", tt.input, got, tt.want)
			}
		})
	}
}
