package notifications

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/contracts"
	"github.com/Shamba-Records-Limited/microvault/pkg/mobile/sms"
)

type stubProvider struct {
	resp sms.SMSResponse
	err  error
}

func (p stubProvider) SendSMS(context.Context, sms.SMSRequest) (sms.SMSResponse, error) {
	return p.resp, p.err
}

func (p stubProvider) SendSingleSMS(context.Context, string, string, string) (sms.SMSResponse, error) {
	return p.resp, p.err
}

func (p stubProvider) SendBulkSMS(context.Context, []string, string, string) (sms.SMSResponse, error) {
	return p.resp, p.err
}

type recorded struct{ rows []sms.Message }

func (r *recorded) Record(_ context.Context, m *sms.Message) error {
	r.rows = append(r.rows, *m)
	return nil
}

func TestSMSNotifierRecordsEachOutcome(t *testing.T) {
	msg := Message{To: "+254712345678", Body: "hi", Kind: KindLoanDisbursed, Ref: "L1"}
	for name, tc := range map[string]struct {
		provider   stubProvider
		wantErr    error
		wantStatus string
		wantID     string
		wantReason string
	}{
		"accepted": {
			provider:   stubProvider{resp: sms.SMSResponse{Recipients: []sms.Recipient{{MessageID: "ATXid_1", Status: "Success", StatusCode: 101, Cost: "KES 0.80"}}}},
			wantStatus: sms.StatusSubmitted, wantID: "ATXid_1",
		},
		"rejected": {
			provider:   stubProvider{resp: sms.SMSResponse{Recipients: []sms.Recipient{{MessageID: "None", Status: "InvalidPhoneNumber", StatusCode: 403}}}},
			wantErr:    ErrRejected,
			wantStatus: sms.StatusRejected, wantReason: "InvalidPhoneNumber",
		},
		"transport failure": {
			provider:   stubProvider{err: errors.New("gateway down")},
			wantStatus: sms.StatusSendFailed, wantReason: "gateway down",
		},
	} {
		t.Run(name, func(t *testing.T) {
			rec := &recorded{}
			err := NewSMSNotifier(tc.provider, "MICROVAULT", WithSendRecorder(rec, "africastalking")).Send(context.Background(), msg)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
			} else if tc.provider.err != nil {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Len(t, rec.rows, 1)
			got := rec.rows[0]
			assert.Equal(t, tc.wantStatus, got.Status)
			assert.Equal(t, KindLoanDisbursed, got.Kind)
			assert.Equal(t, "L1", got.Ref)
			assert.Equal(t, "africastalking", got.Provider)
			assert.Equal(t, tc.wantReason, got.FailureReason)
			if tc.wantID == "" {
				assert.Nil(t, got.ProviderMessageID)
			} else {
				require.NotNil(t, got.ProviderMessageID)
				assert.Equal(t, tc.wantID, *got.ProviderMessageID)
			}
		})
	}
}

func TestNotifiersTagKindAndRef(t *testing.T) {
	fn := &fakeNotifier{}
	loans := newLoanNotifier(t, fn)
	require.NoError(t, loans.NotifyLoanDisbursed(context.Background(), contracts.LoanNotification{LoanID: "L1", PhoneNumber: "254712345678"}))
	assert.Equal(t, KindLoanDisbursed, fn.kind)
	assert.Equal(t, "L1", fn.ref)

	for kind, send := range map[string]func() error{
		KindLoanRepaid: func() error {
			return loans.NotifyLoanRepaid(context.Background(), contracts.LoanNotification{LoanID: "L1"})
		},
		KindRepaymentPaybill: func() error {
			return loans.NotifyRepaymentPaybill(context.Background(), contracts.LoanNotification{LoanID: "L1"})
		},
		KindCashPickupReady: func() error {
			return loans.NotifyLoanCashPickupReady(context.Background(), contracts.LoanNotification{LoanID: "L1"})
		},
	} {
		require.NoError(t, send())
		assert.Equal(t, kind, fn.kind)
	}

	accounts, err := NewSMSAccountNotifier(fn)
	require.NoError(t, err)
	require.NoError(t, accounts.NotifyPilotInvite(context.Background(), contracts.AccountNotification{PhoneNumber: "254712345678"}))
	assert.Equal(t, KindPilotInvite, fn.kind)
}
