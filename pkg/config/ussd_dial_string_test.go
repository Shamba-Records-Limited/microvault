package config

import (
	"strings"
	"testing"
)

// requiredEnv lists every variable New() checks before it does any parsing or
// key validation, so a test can satisfy all of them with placeholders and
// isolate the one it cares about.
var requiredEnv = []string{
	"SERVER_ENVIRONMENT",
	"SERVER_HOST",
	"CORE_SERVER_PORT",
	"CREDIT_SERVER_PORT",
	"DB_HOST",
	"DB_USER",
	"DB_PASSWORD",
	"DB_NAME",
	"DB_PORT",
	"REDIS_HOST",
	"REDIS_PORT",
	"STELLAR_RPC_URL",
	"YELLOW_CARD_PUBLIC_KEY",
	"YELLOW_CARD_SECRET_KEY",
	"YELLOW_CARD_BUSINESS_ID",
	"YELLOW_CARD_BUSINESS_NAME",
	"FONBNK_CLIENT_ID",
	"FONBNK_CLIENT_SECRET",
	"TREASURY_SECRET_KEY",
	"ADMIN_SECRET_KEY",
	"SERVER_SECRET_KEY",
	"JWT_SECRET",
	"USDC_ISSUER",
	"CONTRACT_ID",
	"USSD_DIAL_STRING",
}

// The USSD service code differs between deployments — *789*10# on testnet,
// *384*52203# against the Africa's Talking sandbox — and notification copy
// interpolates it. There is no safe default to guess, so booting without it
// must fail rather than send a message with a hole where the code should be.
func TestDialStringIsRequired(t *testing.T) {
	for _, key := range requiredEnv {
		if key == "USSD_DIAL_STRING" {
			t.Setenv(key, "")
			continue
		}
		t.Setenv(key, "placeholder")
	}

	_, err := New()
	if err == nil {
		t.Fatal("New() succeeded with USSD_DIAL_STRING unset")
	}
	if !strings.Contains(err.Error(), "USSD_DIAL_STRING") {
		t.Errorf("error should name the missing variable, got: %v", err)
	}
}
