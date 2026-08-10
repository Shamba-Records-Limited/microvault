package config

import (
	"testing"
	"time"
)

func TestEnvSeconds(t *testing.T) {
	const key = "TEST_ENV_SECONDS"

	// Unset -> zero, no error.
	if d, err := envSeconds(key); err != nil || d != 0 {
		t.Errorf("unset: d=%v err=%v, want 0/nil", d, err)
	}

	t.Setenv(key, "45")
	if d, err := envSeconds(key); err != nil || d != 45*time.Second {
		t.Errorf("valid: d=%v err=%v, want 45s/nil", d, err)
	}

	for _, bad := range []string{"abc", "0", "-5", "45s"} {
		t.Setenv(key, bad)
		if _, err := envSeconds(key); err == nil {
			t.Errorf("envSeconds(%q) expected error", bad)
		}
	}
}

func TestEnvPositiveInt(t *testing.T) {
	const key = "TEST_ENV_INT"

	if n, err := envPositiveInt(key); err != nil || n != 0 {
		t.Errorf("unset: n=%d err=%v, want 0/nil", n, err)
	}

	t.Setenv(key, "10")
	if n, err := envPositiveInt(key); err != nil || n != 10 {
		t.Errorf("valid: n=%d err=%v, want 10/nil", n, err)
	}

	for _, bad := range []string{"abc", "0", "-1"} {
		t.Setenv(key, bad)
		if _, err := envPositiveInt(key); err == nil {
			t.Errorf("envPositiveInt(%q) expected error", bad)
		}
	}
}

func TestFirstNonEmpty(t *testing.T) {
	if got := firstNonEmpty("", "a", "b"); got != "a" {
		t.Errorf("= %q, want a", got)
	}
	if got := firstNonEmpty("", ""); got != "" {
		t.Errorf("= %q, want empty", got)
	}
	if got := firstNonEmpty("x"); got != "x" {
		t.Errorf("= %q, want x", got)
	}
}
