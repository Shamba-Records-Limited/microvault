package mobilesasa

import (
	"context"
	"errors"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/mobilesasa"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

type fakeSender struct {
	calls   []mobilesasa.SendRequest
	replies []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error)
}

func (f *fakeSender) Send(_ context.Context, req mobilesasa.SendRequest) (mobilesasa.SendResult, error) {
	f.calls = append(f.calls, req)
	reply := f.replies[0]
	if len(f.replies) > 1 {
		f.replies = f.replies[1:]
	}
	return reply(req)
}

func accept(id string, parts int) func(mobilesasa.SendRequest) (mobilesasa.SendResult, error) {
	return func(mobilesasa.SendRequest) (mobilesasa.SendResult, error) {
		return mobilesasa.SendResult{MessageID: id, Parts: parts}, nil
	}
}

func fail(err error) func(mobilesasa.SendRequest) (mobilesasa.SendResult, error) {
	return func(mobilesasa.SendRequest) (mobilesasa.SendResult, error) { return mobilesasa.SendResult{}, err }
}

func adapter(f *fakeSender) *Adapter {
	a := New(f, "MICROVAULT", "https://core.example/api/v1/mobile/sms/mobilesasa/delivery?token=t")
	a.backoff = time.Millisecond
	return a
}

func TestSendAcceptsEachRecipientWithItsOwnTrackingID(t *testing.T) {
	f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){accept("m-1", 1), accept("m-2", 3)}}
	resp, err := adapter(f).SendBulkSMS(t.Context(), []string{"0712000001", "0733000002"}, "hello", "")
	require.NoError(t, err)

	require.Len(t, f.calls, 2)
	assert.Equal(t, "MICROVAULT", f.calls[0].SenderID, "falls back to the configured sender")
	assert.NotEmpty(t, f.calls[0].TrackingID)
	assert.NotEqual(t, f.calls[0].TrackingID, f.calls[1].TrackingID)
	assert.Contains(t, f.calls[0].CallbackURL, "/mobilesasa/delivery")

	require.Len(t, resp.Recipients, 2)
	assert.True(t, resp.Recipients[0].Accepted())
	assert.Equal(t, "m-1", resp.Recipients[0].MessageID)
	assert.Equal(t, "1 part", resp.Recipients[0].Cost)
	assert.Equal(t, "3 parts", resp.Recipients[1].Cost)
}

func TestRefusedMessageIsARejectedRecipient(t *testing.T) {
	for code, want := range map[string]int{
		mobilesasa.CodeNoBalance: codeNoBalance,
		mobilesasa.CodeNotFound:  codeInvalidSender,
		mobilesasa.CodeDuplicate: codeRejectedGateway,
	} {
		f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
			fail(&mobilesasa.APIError{HTTPStatus: 200, Code: code, Message: "refused"}),
		}}
		resp, err := adapter(f).SendSingleSMS(t.Context(), "0712345678", "hi", "OTHER")
		require.NoError(t, err, code)
		require.Len(t, resp.Recipients, 1)
		r := resp.Recipients[0]
		assert.False(t, r.Accepted(), code)
		assert.Equal(t, want, r.StatusCode, code)
		assert.Equal(t, "refused ("+code+")", r.Status)
		assert.Len(t, f.calls, 1, "a refusal is never retried")
		assert.Equal(t, "OTHER", f.calls[0].SenderID)
	}

	f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
		fail(&mobilesasa.APIError{Code: mobilesasa.CodeInvalid, Message: "The phone is not a valid phone number."}),
	}}
	resp, err := adapter(f).SendSingleSMS(t.Context(), "07", "hi", "")
	require.NoError(t, err)
	assert.Equal(t, codeInvalidPhone, resp.Recipients[0].StatusCode)
}

func TestGatewayErrorIsNotRetried(t *testing.T) {
	f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
		fail(&mobilesasa.APIError{HTTPStatus: 502, Message: "<html>bad gateway</html>"}),
	}}
	resp, err := adapter(f).SendSingleSMS(t.Context(), "0712345678", "hi", "")
	require.NoError(t, err)
	assert.Len(t, f.calls, 1, "a 5xx may already have queued the message")
	assert.Equal(t, "gateway answered HTTP 502", resp.Recipients[0].Status)
	assert.False(t, resp.Recipients[0].Accepted())
}

func TestAccountFailureStopsTheSend(t *testing.T) {
	f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
		fail(&mobilesasa.APIError{HTTPStatus: 401, Code: mobilesasa.CodeUnauthorized, Message: "Unauthenticated"}),
	}}
	_, err := adapter(f).SendBulkSMS(t.Context(), []string{"0712000001", "0733000002"}, "hi", "")
	require.Error(t, err)
	assert.Len(t, f.calls, 1)
}

func TestOnlyRequestsThatNeverWentOutAreRetried(t *testing.T) {
	f := &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
		fail(&mobilesasa.APIError{HTTPStatus: 429}),
		fail(&net.OpError{Op: "dial", Err: errors.New("connection refused")}),
		accept("m-1", 1),
	}}
	resp, err := adapter(f).SendSingleSMS(t.Context(), "0712345678", "hi", "")
	require.NoError(t, err)
	assert.Len(t, f.calls, 3)
	assert.Equal(t, f.calls[0].TrackingID, f.calls[2].TrackingID, "a retry keeps the tracking id")
	assert.True(t, resp.Recipients[0].Accepted())

	f = &fakeSender{replies: []func(mobilesasa.SendRequest) (mobilesasa.SendResult, error){
		fail(&net.OpError{Op: "read", Err: errors.New("i/o timeout")}),
	}}
	_, err = adapter(f).SendSingleSMS(t.Context(), "0712345678", "hi", "")
	require.Error(t, err)
	assert.Len(t, f.calls, 1, "a timeout after sending may already be queued")
}

func TestSendNeedsRecipientsAndASender(t *testing.T) {
	_, err := adapter(&fakeSender{}).SendSMS(t.Context(), sms.SMSRequest{Message: "hi"})
	require.Error(t, err)
	_, err = New(&fakeSender{}, "", "").SendSingleSMS(t.Context(), "0712345678", "hi", "")
	require.Error(t, err)
}

func TestReportParserMapsOntoTheLogVocabulary(t *testing.T) {
	parse := ReportParser(nil)
	none := func(string) string { return "" }
	for status, want := range map[string]string{
		"Delivered": "Success", "Failed": "Failed", "Rejected": "Rejected",
		"Expired": "Expired", "Unreachable": "Failed", "Sent": "Sent", "Odd": "Odd",
	} {
		r, err := parse(none, []byte(`{"status":"`+status+`","deliveryStatus":"AbsentSubscriber","reference":"m-1","msisdn":"254712345678","dlr":{"gateway":"airtel"}}`))
		require.NoError(t, err)
		assert.Equal(t, want, r.Status, status)
		assert.Equal(t, "m-1", r.ID)
		assert.Equal(t, "63903", r.NetworkCode)
		assert.Equal(t, "254712345678", r.PhoneNumber)
	}

	r, err := parse(none, []byte(`{"status":"Delivered","deliveryStatus":"DeliveredToTerminal","reference":"m-1","msisdn":"9532b0ef1e65c7dadf2d133e05cfe9b00fb8e9ad5806182bcb5255c2e7e9648b"}`))
	require.NoError(t, err)
	assert.Empty(t, r.PhoneNumber, "a hashed msisdn is not a phone number")
	assert.Empty(t, r.FailureReason)
	assert.True(t, r.IsFinalStatus())

	_, err = parse(none, []byte(`{"status":"Delivered"}`))
	require.Error(t, err)
	assert.NotErrorIs(t, err, sms.ErrReportUnauthenticated)
}

func TestReportParserRefusesAnUnsignedReport(t *testing.T) {
	body := []byte(`{"status":"Delivered","reference":"m-1"}`)
	parse := ReportParser(mobilesasa.NewVerifier("whsec_test"))

	_, err := parse(func(string) string { return "" }, body)
	require.ErrorIs(t, err, sms.ErrReportUnauthenticated)

	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := "t=" + ts + ",v1=" + mobilesasa.Sign("whsec_test", ts, body)
	r, err := parse(func(k string) string {
		if k == mobilesasa.HeaderSignature {
			return sig
		}
		return ""
	}, body)
	require.NoError(t, err)
	assert.Equal(t, "Success", r.Status)
}
