package mobilesasa

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func headers(h map[string]string) func(string) string {
	return func(k string) string { return h[k] }
}

func TestVerifierChecksTheSignature(t *testing.T) {
	now := time.Unix(1_756_400_000, 0)
	v := NewVerifier("whsec_test")
	v.now = func() time.Time { return now }
	body := []byte(`{"reference":"r1","status":"Delivered"}`)
	ts := strconv.FormatInt(now.Unix(), 10)
	good := "t=" + ts + ",v1=" + Sign("whsec_test", ts, body)

	require.NoError(t, v.Verify(headers(map[string]string{HeaderSignature: good}), body))

	for name, h := range map[string]map[string]string{
		"tampered body":  {HeaderSignature: "t=" + ts + ",v1=" + Sign("whsec_test", ts, []byte("other"))},
		"wrong secret":   {HeaderSignature: "t=" + ts + ",v1=" + Sign("whsec_other", ts, body)},
		"stale":          {HeaderSignature: "t=" + strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10) + ",v1=" + Sign("whsec_test", strconv.FormatInt(now.Add(-10*time.Minute).Unix(), 10), body)},
		"malformed":      {HeaderSignature: "garbage"},
		"no headers":     {},
		"wrong secret h": {HeaderSecret: "whsec_nope"},
		"bad sig, good secret header": {
			HeaderSignature: "t=" + ts + ",v1=00",
			HeaderSecret:    "whsec_test",
		},
	} {
		assert.ErrorIs(t, v.Verify(headers(h), body), ErrBadSignature, name)
	}

	assert.NoError(t, v.Verify(headers(map[string]string{HeaderSecret: "whsec_test"}), body), "secret header alone")
	assert.ErrorIs(t, NewVerifier("").Verify(headers(map[string]string{HeaderSecret: ""}), body), ErrBadSignature)
}

func TestMobileNetworkCode(t *testing.T) {
	for in, want := range map[string]string{
		"safaricom": "63902", "airtel": "63903", "telkom": "63907", "equitel": "63999",
		"1": "63902", "2": "63903", "3": "63907", "4": "63999",
		"63902": "63902", "airtouch": "", "": "", "9": "",
	} {
		assert.Equal(t, want, MobileNetworkCode(in), in)
	}
}

func TestUSSDServiceCodeDropsTheSplicedInput(t *testing.T) {
	assert.Equal(t, "*657*45#", USSDServiceCode("*657*45*1*2500#", "1*2500"))
	assert.Equal(t, "*657*45#", USSDServiceCode("*657*45#", ""))
	assert.Equal(t, "*728#", USSDServiceCode("*728*3#", "3"))
	assert.Equal(t, "*657*45#", USSDServiceCode("*657*45#", "1"), "already clean")
}

func TestParseDeliveryReport(t *testing.T) {
	r, err := ParseDeliveryReport([]byte(`{"deliveryStatus":"AbsentSubscriber","status":"Unreachable","reference":"ref-1","msisdn":"254712345678","dlr":{"gateway":"safaricom","description":"AbsentSubscriber"}}`))
	require.NoError(t, err)
	assert.Equal(t, "ref-1", r.Reference)
	assert.Equal(t, "AbsentSubscriber", r.CarrierReason())
	assert.True(t, r.Final())
	assert.False(t, r.HashedMSISDN())

	delivered := DeliveryReport{Status: StatusDelivered, DeliveryStatus: "DeliveredToTerminal"}
	assert.Empty(t, delivered.CarrierReason())
	assert.False(t, DeliveryReport{Status: StatusSent}.Final())
	assert.True(t, DeliveryReport{MSISDN: "9532b0ef1e65c7dadf2d133e05cfe9b00fb8e9ad5806182bcb5255c2e7e9648b"}.HashedMSISDN())

	_, err = ParseDeliveryReport([]byte(`{"status":"Delivered"}`))
	assert.Error(t, err, "no reference")
	_, err = ParseDeliveryReport([]byte(`not json`))
	assert.Error(t, err)
}

func TestClientSend(t *testing.T) {
	type seen struct {
		path, auth string
		body       map[string]string
	}
	var got seen
	reply := func(status int, body string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			raw, _ := io.ReadAll(r.Body)
			got = seen{path: r.URL.Path, auth: r.Header.Get("Authorization")}
			_ = json.Unmarshal(raw, &got.body)
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
	}

	srv := reply(200, `{"status":true,"responseCode":"0200","message":"Accepted","messageId":"m-2","cost":2}`)
	res, err := NewClient(srv.URL, "mbs_tok", time.Second).Send(t.Context(), SendRequest{
		SenderID: "MICROVAULT", Phone: "0712345678", Message: "hi", TrackingID: "t-1", CallbackURL: "https://x/cb",
	})
	srv.Close()
	require.NoError(t, err)
	assert.Equal(t, SendResult{MessageID: "m-2", Parts: 2}, res)
	assert.Equal(t, "/v2/send/message", got.path)
	assert.Equal(t, "Bearer mbs_tok", got.auth)
	assert.Equal(t, map[string]string{"senderID": "MICROVAULT", "phone": "0712345678", "message": "hi", "trackingId": "t-1", "callbackUrl": "https://x/cb"}, got.body)

	srv = reply(200, `{"status":true,"responseCode":"0200","message":"Accepted","messageId":"m-1"}`)
	_, err = NewClient(srv.URL, "mbs_tok", time.Second).Send(t.Context(), SendRequest{SenderID: "S", Phone: "0712345678", Message: "hi"})
	srv.Close()
	require.NoError(t, err)
	assert.Equal(t, "/v1/send/message", got.path, "no tracking id sends on v1")

	cases := []struct {
		status          int
		body            string
		code            string
		throttled, acct bool
	}{
		{200, `{"status":false,"responseCode":"0422","message":"The phone is not a valid phone number."}`, CodeInvalid, false, false},
		{402, `{"status":false,"responseCode":"0402","message":"Insufficient balance"}`, CodeNoBalance, false, false},
		{401, `{"status":false,"responseCode":"0401","message":"Unauthenticated"}`, CodeUnauthorized, false, true},
		{429, `Too Many Requests`, "", true, false},
		{502, `<html>bad gateway</html>`, "", false, false},
	}
	for _, c := range cases {
		srv = reply(c.status, c.body)
		_, err = NewClient(srv.URL, "mbs_tok", time.Second).Send(t.Context(), SendRequest{SenderID: "S", Phone: "0712345678", Message: "hi", TrackingID: "t"})
		srv.Close()
		var apiErr *APIError
		require.True(t, errors.As(err, &apiErr), c.body)
		assert.Equal(t, c.code, apiErr.Code, c.body)
		assert.Equal(t, c.throttled, apiErr.Throttled(), c.body)
		assert.Equal(t, c.acct, apiErr.Account(), c.body)
	}
}
