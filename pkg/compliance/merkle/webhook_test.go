package merkle

import (
	"testing"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWebhookKeyValid(t *testing.T) {
	assert.True(t, WebhookKeyValid("k", "k"))
	assert.False(t, WebhookKeyValid("k", "x"))
	assert.False(t, WebhookKeyValid("", ""), "an unset key never matches")
}

func TestParseWebhookCollectsStellarAddressesAnywhere(t *testing.T) {
	a, b := keypair.MustRandom().Address(), keypair.MustRandom().Address()
	body := []byte(`{"event_type":"alert:created","data":{"alert":{"identifier":"` + a + `","blockchain":13},
		"items":[{"counterparty":"` + b + `"},{"identifier":"` + a + `"},{"identifier":"0xabc"}]}}`)
	w, err := ParseWebhook(body)
	require.NoError(t, err)
	assert.Equal(t, "alert:created", w.EventType)
	assert.ElementsMatch(t, []string{a, b}, w.Addresses)
	assert.False(t, w.IsReport())
}

func TestParseWebhookReportAndBadBody(t *testing.T) {
	w, err := ParseWebhook([]byte(`{"event_type":"report:address.generated","data":{"status":"completed"}}`))
	require.NoError(t, err)
	assert.True(t, w.IsReport())
	assert.Empty(t, w.Addresses)

	_, err = ParseWebhook([]byte(`not json`))
	assert.Error(t, err)
}
