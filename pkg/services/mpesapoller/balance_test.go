package mpesapoller

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/mpesa"
)

type fakeBalanceQuerier struct {
	gotRequests []mpesa.AccountBalanceRequest
	err         error
}

func (f *fakeBalanceQuerier) AccountBalance(_ context.Context, req mpesa.AccountBalanceRequest) (*mpesa.AsyncAck, error) {
	f.gotRequests = append(f.gotRequests, req)
	if f.err != nil {
		return nil, f.err
	}
	return &mpesa.AsyncAck{ResponseCode: "0"}, nil
}

type fakeBalanceRepo struct {
	recorded []uint
}

func (f *fakeBalanceRepo) RecordQuery(_ context.Context, _ string, shortcode uint) error {
	f.recorded = append(f.recorded, shortcode)
	return nil
}

func (f *fakeBalanceRepo) ResolveQuery(context.Context, string) (uint, error) {
	return 0, nil
}

func (f *fakeBalanceRepo) RecordBalance(context.Context, uint, string, string, int64, time.Time) error {
	return nil
}

// TestBalancePoller_PassesConfiguredURLs is a regression test: the poller
// used to call AccountBalance with a zero-valued AsyncURLs on every tick,
// which pkg/payment/mpesa/async.go's validate() unconditionally rejects —
// the balance poller could never succeed, on any environment, since it was
// written. See yellowcard-offramp-webhook-race-2026-09-10.md in the
// knowledge vault (§ added 2026-09-11) for the trace that caught it.
func TestBalancePoller_PassesConfiguredURLs(t *testing.T) {
	q := &fakeBalanceQuerier{}
	repo := &fakeBalanceRepo{}
	p := NewBalancePoller(BalancePollerDeps{
		Client:              q,
		Queries:             repo,
		CollectionShortcode: 174379,
		ResultURL:           "https://example.com/api/v1/callbacks/daraja/slug/balance/result",
		QueueTimeOutURL:     "https://example.com/api/v1/callbacks/daraja/slug/balance/timeout",
	})

	p.tick(context.Background())

	require.Len(t, q.gotRequests, 1)
	got := q.gotRequests[0]
	assert.Equal(t, uint(174379), got.PartyA)
	assert.Equal(t, "https://example.com/api/v1/callbacks/daraja/slug/balance/result", got.URLs.ResultURL)
	assert.Equal(t, "https://example.com/api/v1/callbacks/daraja/slug/balance/timeout", got.URLs.QueueTimeOutURL)
	assert.Equal(t, []uint{174379}, repo.recorded)
}

func TestBalancePoller_QueriesBothShortcodesWhenDistinct(t *testing.T) {
	q := &fakeBalanceQuerier{}
	repo := &fakeBalanceRepo{}
	p := NewBalancePoller(BalancePollerDeps{
		Client:                q,
		Queries:               repo,
		CollectionShortcode:   174379,
		DisbursementShortcode: 600000,
		ResultURL:             "https://example.com/result",
		QueueTimeOutURL:       "https://example.com/timeout",
	})

	p.tick(context.Background())

	assert.Len(t, q.gotRequests, 2)
	assert.ElementsMatch(t, []uint{174379, 600000}, repo.recorded)
}

func TestBalancePoller_SkipsDisbursementWhenSameAsCollection(t *testing.T) {
	q := &fakeBalanceQuerier{}
	repo := &fakeBalanceRepo{}
	p := NewBalancePoller(BalancePollerDeps{
		Client:                q,
		Queries:               repo,
		CollectionShortcode:   174379,
		DisbursementShortcode: 174379,
		ResultURL:             "https://example.com/result",
		QueueTimeOutURL:       "https://example.com/timeout",
	})

	p.tick(context.Background())

	assert.Len(t, q.gotRequests, 1, "must not double-query the same shortcode")
}
