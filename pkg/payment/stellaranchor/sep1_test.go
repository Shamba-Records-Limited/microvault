package stellaranchor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// productionTOML is the verbatim production stellar.toml used as a parser fixture.
const productionTOML = `
ACCOUNTS = []
VERSION = "0.1.0"
NETWORK_PASSPHRASE = "Public Global Stellar Network ; September 2015"
SIGNING_KEY = "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL"

WEB_AUTH_ENDPOINT = "https://stellar.moneygram.com/stellaradapterservice/auth"
TRANSFER_SERVER_SEP0024 = "https://stellar.moneygram.com/stellaradapterservice/sep24"

[[CURRENCIES]]
code = "USDC"
issuer = "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
is_asset_anchored = true
anchor_asset_type = "fiat"
anchor_asset = "USD"
name = "USD Coin"
`

func TestFetchTOML_ParsesProductionFixture(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/.well-known/stellar.toml", r.URL.Path)
		_, _ = w.Write([]byte(productionTOML))
	}))
	defer srv.Close()

	// Strip the scheme — FetchTOML re-adds it as https://.
	host := srv.Listener.Addr().String()
	got, err := FetchTOML(context.Background(), srv.Client(), host)
	require.NoError(t, err)

	assert.Equal(t, "0.1.0", got.Version)
	assert.Equal(t, "Public Global Stellar Network ; September 2015", got.NetworkPassphrase)
	assert.Equal(t, "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL", got.SigningKey)
	assert.Equal(t, "https://stellar.moneygram.com/stellaradapterservice/auth", got.WebAuthEndpoint)
	assert.Equal(t, "https://stellar.moneygram.com/stellaradapterservice/sep24", got.TransferServerSEP24)
	assert.Equal(t, "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN", got.AssetIssuer("USDC"))
	assert.Empty(t, got.Accounts)
}

func TestFetchTOML_RejectsNon200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := FetchTOML(context.Background(), srv.Client(), srv.Listener.Addr().String())
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrTOMLFetch)
}

func TestFetchTOML_EmptyHomeDomain(t *testing.T) {
	_, err := FetchTOML(context.Background(), nil, "")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrInvalidConfig)
}

func TestTOML_Validate(t *testing.T) {
	good := &TOML{
		NetworkPassphrase:   "Public Global Stellar Network ; September 2015",
		SigningKey:          "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
		WebAuthEndpoint:     "https://stellar.moneygram.com/stellaradapterservice/auth",
		TransferServerSEP24: "https://stellar.moneygram.com/stellaradapterservice/sep24",
		Currencies: []Currency{
			{Code: "USDC", Issuer: "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"},
		},
	}

	t.Run("happy path", func(t *testing.T) {
		err := good.Validate(ValidateOptions{
			ExpectedNetworkPassphrase: "Public Global Stellar Network ; September 2015",
			ExpectedSigningKey:        "GD5NUMEX7LYHXGXCAD4PGW7JDMOUY2DKRGY5XZHJS5IONVHDKCJYGVCL",
			ExpectedUSDCIssuer:        "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN",
		})
		assert.NoError(t, err)
	})

	t.Run("network passphrase mismatch", func(t *testing.T) {
		err := good.Validate(ValidateOptions{ExpectedNetworkPassphrase: "Test SDF Network ; September 2015"})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTOMLValidation)
	})

	t.Run("signing key rotation", func(t *testing.T) {
		err := good.Validate(ValidateOptions{ExpectedSigningKey: "GA111111111111111111111111111111111111111111111111111111"})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTOMLValidation)
		assert.Contains(t, err.Error(), "rotated")
	})

	t.Run("USDC issuer changed", func(t *testing.T) {
		err := good.Validate(ValidateOptions{ExpectedUSDCIssuer: "GBOTHERISSUER111111111111111111111111111111111111111111"})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTOMLValidation)
	})

	t.Run("missing required field", func(t *testing.T) {
		bad := *good
		bad.WebAuthEndpoint = ""
		err := bad.Validate(ValidateOptions{})
		require.Error(t, err)
		assert.ErrorIs(t, err, ErrTOMLValidation)
	})
}

func TestTOML_AssetIssuer_Missing(t *testing.T) {
	t1 := &TOML{Currencies: []Currency{{Code: "USDC", Issuer: "GISSUER"}}}
	assert.Equal(t, "GISSUER", t1.AssetIssuer("USDC"))
	assert.Equal(t, "", t1.AssetIssuer("USDT"))
	assert.Equal(t, "", (&TOML{}).AssetIssuer("USDC"))
}
