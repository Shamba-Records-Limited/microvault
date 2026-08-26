package webhook

import (
	"context"
	"errors"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/payment/yellowcard"
)

type fakeDisb struct {
	statuses         []string
	notifiedComplete bool
	notifiedFailed   bool
	completion       *CompletionFinancials
	direct           bool
	directErr        error
	updateErr        error
}

func (f *fakeDisb) UpdateDisbursementStatus(_ string, status string) error {
	if f.updateErr != nil {
		return f.updateErr
	}
	f.statuses = append(f.statuses, status)
	return nil
}

func (f *fakeDisb) NotifyDisbursementComplete(string) error { f.notifiedComplete = true; return nil }

func (f *fakeDisb) RecordDisbursementCompletion(_ string, fin CompletionFinancials) error {
	f.completion = &fin
	return nil
}
func (f *fakeDisb) NotifyDisbursementFailed(string) error    { f.notifiedFailed = true; return nil }
func (f *fakeDisb) RepayVault(string) error                  { return nil }
func (f *fakeDisb) SetSettlementMethod(string, string) error { return nil }
func (f *fakeDisb) IsDirectSettlement(string) (bool, error)  { return f.direct, f.directErr }

func (f *fakeDisb) last() string {
	if len(f.statuses) == 0 {
		return ""
	}
	return f.statuses[len(f.statuses)-1]
}

type fakeAlerts struct{ count int }

func (f *fakeAlerts) AlertOps(string, string) error { f.count++; return nil }

func TestProcessYellowCardEvent_Table(t *testing.T) {
	cases := []struct {
		name         string
		event        string
		status       string
		direct       bool
		wantStatus   string // last status update ("" = none expected)
		wantComplete bool
		wantFailed   bool
		wantAlert    bool
	}{
		{"disbursement complete", yellowcard.EventDisbursementComplete, "", false, yellowcard.DisbursementComplete, true, false, false},
		{"payment complete", yellowcard.EventPaymentComplete, "", false, yellowcard.DisbursementComplete, true, false, false},
		{"disbursement failed (direct)", yellowcard.EventDisbursementFailed, "", true, yellowcard.DisbursementRefundPending, false, false, true},
		{"disbursement failed (fiat)", yellowcard.EventDisbursementFailed, "", false, yellowcard.DisbursementFailed, false, true, true},
		{"status complete", "", yellowcard.StatusComplete, false, yellowcard.DisbursementComplete, true, false, false},
		{"status failed (direct)", "", yellowcard.StatusFailed, true, yellowcard.DisbursementRefundPending, false, false, true},
		{"status failed (fiat)", "", yellowcard.StatusFailed, false, yellowcard.DisbursementFailed, false, true, true},
		{"status expired (fiat)", "", yellowcard.StatusExpired, false, yellowcard.DisbursementFailed, false, true, true},
		{"pending liquidity", "", yellowcard.StatusPendingLiquidity, false, "", false, false, true},
		{"pending refund", "", yellowcard.StatusPendingRefund, false, yellowcard.DisbursementRefundPending, false, false, false},
		{"refunded", "", yellowcard.StatusRefunded, false, yellowcard.DisbursementRefundReceived, false, false, false},
		{"refund failed", "", yellowcard.StatusRefundFailed, false, yellowcard.DisbursementFailed, false, false, true},
		{"processing", "", yellowcard.StatusProcessing, false, yellowcard.DisbursementProcessing, false, false, false},
		{"pending settlement", "", yellowcard.StatusPendingSettlement, false, yellowcard.DisbursementDirectSubmitted, false, false, false},
		{"unknown status", "", "who-knows", false, "", false, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			disb := &fakeDisb{direct: c.direct}
			alerts := &fakeAlerts{}
			svc := NewService(disb, alerts, nil, nil)

			err := svc.ProcessYellowCardEvent(yellowcard.WebhookEvent{
				Event: c.event, Status: c.status, SequenceID: "seq-1", PaymentID: "pay-1",
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if disb.last() != c.wantStatus {
				t.Errorf("last status = %q, want %q", disb.last(), c.wantStatus)
			}
			if disb.notifiedComplete != c.wantComplete {
				t.Errorf("notifiedComplete = %v, want %v", disb.notifiedComplete, c.wantComplete)
			}
			if disb.notifiedFailed != c.wantFailed {
				t.Errorf("notifiedFailed = %v, want %v", disb.notifiedFailed, c.wantFailed)
			}
			if (alerts.count > 0) != c.wantAlert {
				t.Errorf("alert fired = %v, want %v", alerts.count > 0, c.wantAlert)
			}
		})
	}
}

type fakePayments struct {
	details *yellowcard.PaymentDetails
	err     error
}

func (f *fakePayments) LookupPayment(context.Context, string) (*yellowcard.PaymentDetails, error) {
	return f.details, f.err
}

func TestProcessComplete_RecordsFinancials(t *testing.T) {
	disb := &fakeDisb{}
	payments := &fakePayments{details: &yellowcard.PaymentDetails{
		ConvertedAmount:       5000,
		ServiceFeeAmountUSD:   1.5,
		ServiceFeeAmountLocal: 200,
		PartnerFeeAmountUSD:   0.5,
		PartnerFeeAmountLocal: 60,
	}}
	svc := NewService(disb, nil, nil, payments)

	if err := svc.ProcessYellowCardEvent(yellowcard.WebhookEvent{
		Event: yellowcard.EventPaymentComplete, SequenceID: "seq-1", PaymentID: "pay-1",
	}); err != nil {
		t.Fatal(err)
	}
	if disb.completion == nil {
		t.Fatal("completion financials not recorded")
	}
	if disb.completion.ConvertedAmountLocal != 5000 || disb.completion.ServiceFeeAmountLocal != 200 {
		t.Errorf("financials mapped wrong: %+v", *disb.completion)
	}
}

func TestFailedEvent_DirectLookupError(t *testing.T) {
	disb := &fakeDisb{directErr: errors.New("db down")}
	svc := NewService(disb, &fakeAlerts{}, nil, nil)
	err := svc.ProcessYellowCardEvent(yellowcard.WebhookEvent{
		Event: yellowcard.EventDisbursementFailed, SequenceID: "seq-1",
	})
	if err == nil {
		t.Error("expected error when IsDirectSettlement fails")
	}
}

func TestUpdateStatusError_Propagates(t *testing.T) {
	disb := &fakeDisb{updateErr: errors.New("write failed")}
	svc := NewService(disb, nil, nil, nil)
	err := svc.ProcessYellowCardEvent(yellowcard.WebhookEvent{
		Event: yellowcard.EventDisbursementComplete, SequenceID: "seq-1",
	})
	if err == nil {
		t.Error("expected UpdateDisbursementStatus error to propagate")
	}
}
