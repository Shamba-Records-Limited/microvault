package compliance_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/compliance"
)

type fixed struct {
	verdict    compliance.Verdict
	sanctioned bool
	err        error
}

func (f fixed) ScreenAddress(_ context.Context, req compliance.ScreenRequest) (*compliance.Screening, error) {
	if f.err != nil {
		return nil, f.err
	}
	return &compliance.Screening{Address: req.Address, Verdict: f.verdict, Sanctioned: f.sanctioned, Raw: json.RawMessage(`{"v":"` + string(f.verdict) + `"}`)}, nil
}

func TestCombineTakesTheStricterVerdict(t *testing.T) {
	cases := []struct {
		a, b, want compliance.Verdict
	}{
		{compliance.VerdictApproved, compliance.VerdictApproved, compliance.VerdictApproved},
		{compliance.VerdictApproved, compliance.VerdictReview, compliance.VerdictReview},
		{compliance.VerdictUnscreenable, compliance.VerdictApproved, compliance.VerdictUnscreenable},
		{compliance.VerdictUnscreenable, compliance.VerdictPending, compliance.VerdictPending},
		{compliance.VerdictReview, compliance.VerdictRejected, compliance.VerdictRejected},
	}
	for _, c := range cases {
		s, err := compliance.Combine(
			compliance.Named{Name: "elliptic", Screener: fixed{verdict: c.a}},
			compliance.Named{Name: "merkle", Screener: fixed{verdict: c.b, sanctioned: c.b == compliance.VerdictRejected}},
		).ScreenAddress(context.Background(), compliance.ScreenRequest{Address: "GABC"})
		require.NoError(t, err)
		assert.Equal(t, c.want, s.Verdict, "%s + %s", c.a, c.b)
		assert.Equal(t, "elliptic+merkle", s.Provider)
		assert.Len(t, s.Parts, 2)
		assert.Equal(t, c.b == compliance.VerdictRejected, s.Sanctioned)

		var raw map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(s.Raw, &raw))
		assert.Contains(t, raw, "elliptic")
		assert.Contains(t, raw, "merkle")
	}
}

func TestCombineFailsWhenAnyProviderFails(t *testing.T) {
	_, err := compliance.Combine(
		compliance.Named{Name: "elliptic", Screener: fixed{verdict: compliance.VerdictApproved}},
		compliance.Named{Name: "merkle", Screener: fixed{err: errors.New("down")}},
	).ScreenAddress(context.Background(), compliance.ScreenRequest{Address: "GABC"})
	require.Error(t, err, "a partial answer must never approve an address")
}

func TestCombineOfOneIsThatProvider(t *testing.T) {
	f := fixed{verdict: compliance.VerdictReview}
	assert.Equal(t, compliance.Screener(f), compliance.Combine(compliance.Named{Name: "elliptic", Screener: f}))
}

type fixedTx struct{ verdict compliance.Verdict }

func (f fixedTx) ScreenTransaction(_ context.Context, req compliance.TxScreenRequest) (*compliance.Screening, error) {
	return &compliance.Screening{TxHash: req.Hash, Verdict: f.verdict, Raw: json.RawMessage(`{}`)}, nil
}

func TestCombineTxTakesTheStricterVerdict(t *testing.T) {
	s, err := compliance.CombineTx(
		compliance.NamedTx{Name: "elliptic", Screener: fixedTx{compliance.VerdictApproved}},
		compliance.NamedTx{Name: "merkle", Screener: fixedTx{compliance.VerdictReview}},
	).ScreenTransaction(context.Background(), compliance.TxScreenRequest{Hash: "abc", Address: "GABC"})
	require.NoError(t, err)
	assert.Equal(t, compliance.VerdictReview, s.Verdict)
	assert.Equal(t, "abc", s.TxHash)
	assert.Equal(t, "elliptic+merkle", s.Provider)
}
