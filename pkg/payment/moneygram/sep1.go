package moneygram

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

// TOML is the parsed subset of a Stellar SEP-1 stellar.toml that matters for
// the MoneyGram Ramps integration. Fields not used by this SDK are not
// captured — extend if your wallet needs more.
type TOML struct {
	Version             string     `toml:"VERSION"`
	NetworkPassphrase   string     `toml:"NETWORK_PASSPHRASE"`
	SigningKey          string     `toml:"SIGNING_KEY"`
	WebAuthEndpoint     string     `toml:"WEB_AUTH_ENDPOINT"`
	TransferServerSEP24 string     `toml:"TRANSFER_SERVER_SEP0024"`
	Accounts            []string   `toml:"ACCOUNTS"`
	Currencies          []Currency `toml:"CURRENCIES"`
}

// Currency is one entry from the SEP-1 [[CURRENCIES]] array.
type Currency struct {
	Code            string `toml:"code"`
	Issuer          string `toml:"issuer"`
	IsAssetAnchored bool   `toml:"is_asset_anchored"`
	AnchorAssetType string `toml:"anchor_asset_type"`
	AnchorAsset     string `toml:"anchor_asset"`
	Name            string `toml:"name"`
	Desc            string `toml:"desc"`
}

// FetchTOML retrieves and parses the SEP-1 stellar.toml served at
// https://{homeDomain}/.well-known/stellar.toml. It does NOT validate
// semantically — call TOML.Validate to enforce environment-specific
// expectations (passphrase, signing key, USDC issuer).
//
// httpClient may be nil; http.DefaultClient is used in that case. The
// response is capped at 64 KiB — well above the size of any well-formed
// stellar.toml — to avoid pathological responses exhausting memory.
func FetchTOML(ctx context.Context, httpClient *http.Client, homeDomain string) (*TOML, error) {
	if homeDomain == "" {
		return nil, fmt.Errorf("moneygram: %w: empty home domain", ErrInvalidConfig)
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	url := "https://" + strings.TrimSuffix(homeDomain, "/") + "/.well-known/stellar.toml"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("moneygram: build TOML request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("moneygram: %w: %v", ErrTOMLFetch, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("moneygram: %w: HTTP %d from %s", ErrTOMLFetch, resp.StatusCode, url)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, fmt.Errorf("moneygram: read TOML body: %w", err)
	}

	var parsed TOML
	if _, err := toml.Decode(string(body), &parsed); err != nil {
		return nil, fmt.Errorf("moneygram: decode TOML: %w", err)
	}
	return &parsed, nil
}

// ValidateOptions configures TOML.Validate. Set ExpectedSigningKey when you
// have committed to a specific MG signing key for the environment — a
// mismatch should be a hard fail rather than silent drift.
type ValidateOptions struct {
	ExpectedNetworkPassphrase string
	ExpectedSigningKey        string // optional; "" disables the check
	ExpectedUSDCIssuer        string // optional; "" disables the check
}

// Validate returns nil if the TOML carries every field needed to drive a
// SEP-10 + SEP-24 withdrawal flow against MoneyGram, and matches any
// expectations supplied in opts.
func (t *TOML) Validate(opts ValidateOptions) error {
	if t.SigningKey == "" {
		return fmt.Errorf("moneygram: %w: SIGNING_KEY missing", ErrTOMLValidation)
	}
	if t.WebAuthEndpoint == "" {
		return fmt.Errorf("moneygram: %w: WEB_AUTH_ENDPOINT missing", ErrTOMLValidation)
	}
	if t.TransferServerSEP24 == "" {
		return fmt.Errorf("moneygram: %w: TRANSFER_SERVER_SEP0024 missing", ErrTOMLValidation)
	}
	if t.NetworkPassphrase == "" {
		return fmt.Errorf("moneygram: %w: NETWORK_PASSPHRASE missing", ErrTOMLValidation)
	}
	if opts.ExpectedNetworkPassphrase != "" && t.NetworkPassphrase != opts.ExpectedNetworkPassphrase {
		return fmt.Errorf("moneygram: %w: NETWORK_PASSPHRASE mismatch (got %q, want %q)",
			ErrTOMLValidation, t.NetworkPassphrase, opts.ExpectedNetworkPassphrase)
	}
	if opts.ExpectedSigningKey != "" && t.SigningKey != opts.ExpectedSigningKey {
		return fmt.Errorf("moneygram: %w: SIGNING_KEY rotated (got %q, want %q)",
			ErrTOMLValidation, t.SigningKey, opts.ExpectedSigningKey)
	}
	if opts.ExpectedUSDCIssuer != "" {
		issuer := t.AssetIssuer("USDC")
		if issuer == "" {
			return fmt.Errorf("moneygram: %w: USDC currency missing", ErrTOMLValidation)
		}
		if issuer != opts.ExpectedUSDCIssuer {
			return fmt.Errorf("moneygram: %w: USDC issuer changed (got %q, want %q)",
				ErrTOMLValidation, issuer, opts.ExpectedUSDCIssuer)
		}
	}
	return nil
}

// AssetIssuer returns the issuer pubkey for the named currency code, or ""
// if the code is not in the [[CURRENCIES]] list.
func (t *TOML) AssetIssuer(code string) string {
	for _, c := range t.Currencies {
		if c.Code == code {
			return c.Issuer
		}
	}
	return ""
}

// DefaultTOMLClient returns an http.Client suitable for fetching SEP-1 TOMLs:
// short timeout, no redirects to other hosts. Use this if you don't already
// have an http.Client to inject.
func DefaultTOMLClient() *http.Client {
	return &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			// SEP-1 TOMLs should not redirect off-host; reject to avoid
			// being pointed at an attacker-controlled stellar.toml.
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("moneygram: cross-host redirect to %s blocked", req.URL.Host)
			}
			return nil
		},
	}
}
