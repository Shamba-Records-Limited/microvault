package airtelpoller

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeSummaryQuerier struct {
	pages    [][]airtel.SummaryTransaction
	requests []airtel.SummaryRequest
	err      error
}

func (f *fakeSummaryQuerier) TransactionsSummary(_ context.Context, req airtel.SummaryRequest) (*airtel.SummaryResponse, error) {
	f.requests = append(f.requests, req)
	if f.err != nil {
		return nil, f.err
	}

	page := req.Offset / pageSize
	resp := &airtel.SummaryResponse{}
	if page < len(f.pages) {
		resp.Data.Transactions = f.pages[page]
		resp.Data.Count = len(f.pages[page])
	}
	return resp, nil
}

type fakeSummaryRepo struct {
	upserted []*models.AirtelTransaction
	err      error
}

func (f *fakeSummaryRepo) UpsertFromSummary(_ context.Context, tx *models.AirtelTransaction) error {
	if f.err != nil {
		return f.err
	}
	f.upserted = append(f.upserted, tx)
	return nil
}

func (f *fakeSummaryRepo) RecordCallback(context.Context, *models.AirtelTransaction) error {
	return nil
}

func (f *fakeSummaryRepo) GetByPartnerID(context.Context, string) (*models.AirtelTransaction, error) {
	return nil, repository.ErrAirtelNotFound
}

func (f *fakeSummaryRepo) GetByAirtelMoneyID(context.Context, string) (*models.AirtelTransaction, error) {
	return nil, repository.ErrAirtelNotFound
}

func (f *fakeSummaryRepo) DuePoll(context.Context, int) ([]*models.AirtelTransaction, error) {
	return nil, nil
}

func (f *fakeSummaryRepo) Confirm(context.Context, string, models.AirtelTransactionConfirmVia, string, string, string) error {
	return nil
}

func (f *fakeSummaryRepo) UpdatePoll(context.Context, string, time.Time) error { return nil }
func (f *fakeSummaryRepo) StopPoll(context.Context, string) error              { return nil }

func (f *fakeSummaryRepo) ListUnappliedConfirmed(context.Context, int) ([]*models.AirtelTransaction, error) {
	return nil, nil
}

func (f *fakeSummaryRepo) SetAppliedStroops(context.Context, string, int64) error { return nil }

func (f *fakeSummaryRepo) SumAppliedStroopsByLoan(context.Context, string) (int64, error) {
	return 0, nil
}

type fakeCursor struct {
	at       time.Time
	advanced []time.Time
	getErr   error
}

func (f *fakeCursor) Get(context.Context) (time.Time, error) {
	if f.getErr != nil {
		return time.Time{}, f.getErr
	}
	return f.at, nil
}

func (f *fakeCursor) Advance(_ context.Context, at time.Time) error {
	f.advanced = append(f.advanced, at)
	f.at = at
	return nil
}

func settled(id, receipt, amount string) airtel.SummaryTransaction {
	var entry airtel.SummaryTransaction
	entry.Transaction.ID = id
	entry.Transaction.AirtelMoneyID = receipt
	entry.Transaction.Amount = amount
	entry.Transaction.Status = "TS"
	entry.Transaction.ReferenceNumber = "MV-" + id
	entry.Transaction.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	entry.Payer.MSISDN = "733123456"
	return entry
}

func pending(id string) airtel.SummaryTransaction {
	entry := settled(id, "", "500.00")
	entry.Transaction.Status = "TIP"
	return entry
}

func newSweeper(t *testing.T, q *fakeSummaryQuerier, repo *fakeSummaryRepo, cursor *fakeCursor) *SummarySweeper {
	t.Helper()
	return NewSummarySweeper(SummarySweeperDeps{
		Client:   q,
		Repo:     repo,
		Cursor:   cursor,
		Interval: time.Minute,
	})
}

func TestSweep_RecordsSettledOnly(t *testing.T) {
	q := &fakeSummaryQuerier{pages: [][]airtel.SummaryTransaction{{
		settled("mv-1", "AM1", "500.00"),
		pending("mv-2"),
		settled("mv-3", "AM3", "1,250.50"),
	}}}
	repo := &fakeSummaryRepo{}
	cursor := &fakeCursor{at: time.Now().Add(-time.Hour)}

	newSweeper(t, q, repo, cursor).sweep(context.Background())

	if len(repo.upserted) != 2 {
		t.Fatalf("upserted %d rows, want 2 settled", len(repo.upserted))
	}
	for _, tx := range repo.upserted {
		if !tx.Confirmed {
			t.Fatalf("%s was not confirmed; the summary only reports settled transactions", tx.PartnerTxnID)
		}
		if tx.ConfirmedVia == nil || *tx.ConfirmedVia != models.AirtelConfirmViaSummary {
			t.Fatalf("%s confirmed via %v", tx.PartnerTxnID, tx.ConfirmedVia)
		}
		if tx.AirtelMoneyID == nil {
			t.Fatalf("%s was recorded with no receipt", tx.PartnerTxnID)
		}
	}
	if repo.upserted[1].AmountMinor != 125_050 {
		t.Fatalf("amount = %d minor, want 125050", repo.upserted[1].AmountMinor)
	}
	if len(cursor.advanced) != 1 {
		t.Fatalf("cursor advanced %d times, want 1", len(cursor.advanced))
	}
}

// The window bounds go on the wire as epoch integers, which is the one
// endpoint in the catalogue that takes them.
func TestSweep_PassesTheWindow(t *testing.T) {
	start := time.Now().Add(-2 * time.Hour)
	q := &fakeSummaryQuerier{}
	cursor := &fakeCursor{at: start}

	newSweeper(t, q, &fakeSummaryRepo{}, cursor).sweep(context.Background())

	if len(q.requests) != 1 {
		t.Fatalf("made %d requests, want 1", len(q.requests))
	}

	req := q.requests[0]
	if !req.From.Equal(start) {
		t.Fatalf("window start = %v, want the cursor position %v", req.From, start)
	}
	if !req.From.Before(req.To) {
		t.Fatalf("window is not ordered: %v to %v", req.From, req.To)
	}
	if req.Limit != pageSize {
		t.Fatalf("limit = %d, want %d", req.Limit, pageSize)
	}
	if req.Offset != 0 {
		t.Fatalf("first page offset = %d, want 0", req.Offset)
	}

	// The cursor advances to the window's end, not past it, so the next tick
	// starts exactly where this one stopped and no interval is skipped.
	if len(cursor.advanced) != 1 {
		t.Fatalf("cursor advanced %d times, want 1", len(cursor.advanced))
	}
	if !cursor.advanced[0].Equal(req.To) {
		t.Fatalf("cursor advanced to %v, want the window end %v", cursor.advanced[0], req.To)
	}
}

// A failed window leaves the cursor put, so the next tick re-walks it. Every
// write is an upsert, so re-walking cannot double-credit.
func TestSweep_FailureDoesNotAdvanceTheCursor(t *testing.T) {
	q := &fakeSummaryQuerier{err: errors.New("gateway down")}
	cursor := &fakeCursor{at: time.Now().Add(-time.Hour)}

	newSweeper(t, q, &fakeSummaryRepo{}, cursor).sweep(context.Background())

	if len(cursor.advanced) != 0 {
		t.Fatal("the cursor advanced past a window that failed")
	}
}

func TestSweep_RecordFailureDoesNotAdvanceTheCursor(t *testing.T) {
	q := &fakeSummaryQuerier{pages: [][]airtel.SummaryTransaction{{settled("mv-1", "AM1", "500.00")}}}
	repo := &fakeSummaryRepo{err: repository.ErrFailedToRecordAirtel}
	cursor := &fakeCursor{at: time.Now().Add(-time.Hour)}

	newSweeper(t, q, repo, cursor).sweep(context.Background())

	if len(cursor.advanced) != 0 {
		t.Fatal("the cursor advanced past a window whose rows did not persist")
	}
}

// An amount that will not parse is not zero. Recording it as zero would
// credit a loan nothing and mark the payment handled.
func TestSweep_SkipsUnparseableAmounts(t *testing.T) {
	q := &fakeSummaryQuerier{pages: [][]airtel.SummaryTransaction{{
		settled("mv-1", "AM1", "not-a-number"),
		settled("mv-2", "AM2", "500.00"),
	}}}
	repo := &fakeSummaryRepo{}
	cursor := &fakeCursor{at: time.Now().Add(-time.Hour)}

	newSweeper(t, q, repo, cursor).sweep(context.Background())

	if len(repo.upserted) != 1 {
		t.Fatalf("upserted %d rows, want only the parseable one", len(repo.upserted))
	}
	if repo.upserted[0].PartnerTxnID != "mv-2" {
		t.Fatalf("recorded %q", repo.upserted[0].PartnerTxnID)
	}
}

func TestSweep_Paginates(t *testing.T) {
	full := make([]airtel.SummaryTransaction, pageSize)
	for i := range full {
		full[i] = settled("mv-page1-"+string(rune('a'+i%26))+string(rune('a'+i/26)), "AM", "100.00")
	}
	q := &fakeSummaryQuerier{pages: [][]airtel.SummaryTransaction{
		full,
		{settled("mv-page2", "AM2", "200.00")},
	}}
	repo := &fakeSummaryRepo{}
	cursor := &fakeCursor{at: time.Now().Add(-time.Hour)}

	newSweeper(t, q, repo, cursor).sweep(context.Background())

	if len(q.requests) != 2 {
		t.Fatalf("made %d requests, want 2 pages", len(q.requests))
	}
	if q.requests[1].Offset != pageSize {
		t.Fatalf("second page offset = %d, want %d", q.requests[1].Offset, pageSize)
	}
	if len(repo.upserted) != pageSize+1 {
		t.Fatalf("upserted %d rows, want %d", len(repo.upserted), pageSize+1)
	}
}

func TestSweep_SkipsWhenCursorIsCurrent(t *testing.T) {
	q := &fakeSummaryQuerier{}
	cursor := &fakeCursor{at: time.Now().Add(time.Hour)}

	newSweeper(t, q, &fakeSummaryRepo{}, cursor).sweep(context.Background())

	if len(q.requests) != 0 {
		t.Fatal("swept a window that has not opened yet")
	}
}

func TestSweep_CursorReadFailureIsSurvivable(t *testing.T) {
	q := &fakeSummaryQuerier{}
	cursor := &fakeCursor{getErr: errors.New("db down")}

	newSweeper(t, q, &fakeSummaryRepo{}, cursor).sweep(context.Background())

	if len(q.requests) != 0 {
		t.Fatal("swept without knowing where the cursor was")
	}
}
