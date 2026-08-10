package pin

import (
	"testing"
	"time"

	"golang.org/x/crypto/bcrypt"
)

func TestHashPIN(t *testing.T) {
	hash, err := HashPIN("2846")
	if err != nil {
		t.Fatalf("HashPIN valid: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte("2846")) != nil {
		t.Error("hash does not verify against the original PIN")
	}
	// Salted: two hashes of the same PIN differ but both verify.
	hash2, _ := HashPIN("2846")
	if hash == hash2 {
		t.Error("expected salted hashes to differ")
	}
	// A weak PIN is rejected before hashing.
	if _, err := HashPIN("1111"); err == nil {
		t.Error("HashPIN should reject an all-same PIN")
	}
}

func TestFormatLockDuration(t *testing.T) {
	cases := []struct {
		delta time.Duration
		want  string
	}{
		{15 * time.Minute, "15 minutes"},
		{5 * time.Minute, "5 minutes"},
		{30 * time.Second, "1 minute"}, // rounds up to a minute
		{-time.Minute, "1 minute"},     // already elapsed
	}
	for _, c := range cases {
		if got := formatLockDuration(time.Now().Add(c.delta)); got != c.want {
			t.Errorf("formatLockDuration(+%s) = %q, want %q", c.delta, got, c.want)
		}
	}
}
