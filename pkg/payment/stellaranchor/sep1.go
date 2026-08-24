package stellaranchor

import (
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// tomlErr starts an error builder for SEP-1 work. The domain is shared with
// the rest of the anchor client; the tag is what separates TOML discovery from
// authentication and transfers when filtering.
func tomlErr() oops.OopsErrorBuilder {
	return oops.In(errDomain).Tags("sep1", "toml")
}

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
	errb := tomlErr().With("home_domain", homeDomain)

	if homeDomain == "" {
		return nil, errb.Code(pkgErrors.CodeMissingAccount).Wrapf(ErrInvalidConfig, "home domain is empty")
	}
	if httpClient == nil {
		httpClient = http.DefaultClient
	}

	url := "https://" + strings.TrimSuffix(homeDomain, "/") + "/.well-known/stellar.toml"
	errb = errb.With("url", url)

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the stellar.toml request")
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		// Both the transport cause and the sentinel are kept: callers match on
		// ErrTOMLFetch, and the cause is what says why.
		return nil, errb.Code(pkgErrors.CodeTransportFailed).
			With("cause", err.Error()).
			Wrapf(ErrTOMLFetch, "stellar.toml request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, errb.Code(pkgErrors.CodeHTTPError).
			With(pkgErrors.AttrStatusCode, resp.StatusCode).
			Wrapf(ErrTOMLFetch, "anchor returned a non-200 for stellar.toml")
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the stellar.toml body")
	}

	var parsed TOML
	if _, err := toml.Decode(string(body), &parsed); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode stellar.toml")
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
	// missingField reports a required stellar.toml key that is absent. The key
	// is an attribute so all four group as one error rather than four.
	missingField := func(field string) error {
		return tomlErr().
			Code(pkgErrors.CodeIncompleteResponse).
			With("field", field).
			Wrapf(ErrTOMLValidation, "stellar.toml is missing a required field")
	}

	// changedField reports a pinned value the anchor no longer matches. Both
	// sides are attributes: a signing-key rotation is diagnosed by comparing
	// them, and neither belongs in the message.
	changedField := func(field, got, want string) error {
		return tomlErr().
			Code(pkgErrors.CodeIncompleteResponse).
			With("field", field).
			With("got", got).
			With("want", want).
			Wrapf(ErrTOMLValidation, "stellar.toml value differs from the pinned expectation")
	}

	if t.SigningKey == "" {
		return missingField("SIGNING_KEY")
	}
	if t.WebAuthEndpoint == "" {
		return missingField("WEB_AUTH_ENDPOINT")
	}
	if t.TransferServerSEP24 == "" {
		return missingField("TRANSFER_SERVER_SEP0024")
	}
	if t.NetworkPassphrase == "" {
		return missingField("NETWORK_PASSPHRASE")
	}
	if opts.ExpectedNetworkPassphrase != "" && t.NetworkPassphrase != opts.ExpectedNetworkPassphrase {
		return changedField("NETWORK_PASSPHRASE", t.NetworkPassphrase, opts.ExpectedNetworkPassphrase)
	}
	if opts.ExpectedSigningKey != "" && t.SigningKey != opts.ExpectedSigningKey {
		return changedField("SIGNING_KEY", t.SigningKey, opts.ExpectedSigningKey)
	}
	if opts.ExpectedUSDCIssuer != "" {
		issuer := t.AssetIssuer("USDC")
		if issuer == "" {
			return missingField("CURRENCIES[USDC]")
		}
		if issuer != opts.ExpectedUSDCIssuer {
			return changedField("CURRENCIES[USDC].issuer", issuer, opts.ExpectedUSDCIssuer)
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
				return tomlErr().
					Code(pkgErrors.CodePermissionDenied).
					With("from_host", via[0].URL.Host).
					With("to_host", req.URL.Host).
					Errorf("cross-host redirect blocked while fetching stellar.toml")
			}
			return nil
		},
	}
}
