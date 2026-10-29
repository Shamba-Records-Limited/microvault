package elliptic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

func TestScreenTransactionSendsAHolisticSourceOfFunds(t *testing.T) {
	var got transactionScreeningRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/analyses/synchronous", r.URL.Path)
		assert.NotEmpty(t, r.Header.Get("x-access-sign"))
		require.NoError(t, json.NewDecoder(r.Body).Decode(&got))
		_, _ = w.Write([]byte(`{"id":"a1","screening_id":"s1","risk_score":2.5,"process_status":"complete"}`))
	}))
	defer server.Close()
	c := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: server.URL, Thresholds: Thresholds{Review: 5, Reject: 9}})

	s, err := c.ScreenTransaction(context.Background(), compliance.TxScreenRequest{
		Hash: "abc", Address: "GLENDER", Recipient: "CVAULT", CustomerReference: "kyb-1",
	})
	require.NoError(t, err)
	assert.Equal(t, "holistic", got.Subject.Asset)
	assert.Equal(t, "holistic", got.Subject.Blockchain)
	assert.Equal(t, "transaction", got.Subject.Type)
	assert.Equal(t, "abc", got.Subject.Hash)
	assert.Equal(t, "address", got.Subject.OutputType)
	assert.Equal(t, "CVAULT", got.Subject.OutputAddress, "the output is the address that received the deposit")
	assert.Equal(t, "source_of_funds", got.Type)
	assert.Equal(t, "kyb-1", got.CustomerReference)
	assert.Equal(t, compliance.VerdictApproved, s.Verdict)
	assert.Equal(t, "abc", s.TxHash)
	assert.Equal(t, Provider, s.Provider)
}

func TestTransactionVerdicts(t *testing.T) {
	score := func(f float64) *float64 { return &f }
	th := Thresholds{Review: 5, Reject: 9}
	assert.Equal(t, compliance.VerdictRejected, transactionVerdict(transactionScreeningResponse{RiskScore: score(1), ProcessStatus: "complete"}, true, th))
	assert.Equal(t, compliance.VerdictPending, transactionVerdict(transactionScreeningResponse{RiskScore: score(1), ProcessStatus: "complete", Predictive: true}, false, th), "too close to the tip")
	assert.Equal(t, compliance.VerdictPending, transactionVerdict(transactionScreeningResponse{ProcessStatus: "running"}, false, th))
	assert.Equal(t, compliance.VerdictReview, transactionVerdict(transactionScreeningResponse{RiskScore: score(6), ProcessStatus: "complete"}, false, th))
}

func TestScreenTransactionNeedsRecipientAndCustomer(t *testing.T) {
	c := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: "http://unused"})
	_, err := c.ScreenTransaction(context.Background(), compliance.TxScreenRequest{Hash: "abc", CustomerReference: "x"})
	assert.Error(t, err)
	_, err = c.ScreenTransaction(context.Background(), compliance.TxScreenRequest{Hash: "abc", Recipient: "C"})
	assert.Error(t, err)
}

func TestScreenTransactionNotYetOnChainIsPending(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()
	c := NewClient(Config{APIKey: "k", APISecret: testSecret, BaseURL: server.URL})
	s, err := c.ScreenTransaction(context.Background(), compliance.TxScreenRequest{Hash: "abc", Recipient: "C", CustomerReference: "x"})
	require.NoError(t, err)
	assert.Equal(t, compliance.VerdictPending, s.Verdict)
}
