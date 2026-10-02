package alerts

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Shamba-Records-Limited/microvault/pkg/logging"
)

func captureLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(logging.NewHandler(slog.NewJSONHandler(&buf, nil))), &buf
}

func lines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(l), &m))
		out = append(out, m)
	}
	return out
}

type recordingService struct {
	err   error
	calls int
	attrs []slog.Attr
}

func (r *recordingService) AlertOps(ctx context.Context, _, _ string) error {
	r.calls++
	r.attrs = logging.Attrs(ctx)
	return r.err
}

func TestLogAlerter_EmitsTheShapeAlertRulesMatch(t *testing.T) {
	logger, buf := captureLogger()
	ctx := logging.WithLoan(t.Context(), "loan-1", "REF-1")

	require.NoError(t, LogAlerter{Logger: logger}.AlertOps(ctx, "Vault repay attempts exhausted", "details"))

	got := lines(t, buf)
	require.Len(t, got, 1)
	assert.Equal(t, "ERROR", got[0]["level"])
	assert.Equal(t, Message, got[0]["msg"])
	assert.Equal(t, "Vault repay attempts exhausted", got[0][AttrSubject])
	assert.Equal(t, "details", got[0][AttrMessage])
	assert.Equal(t, "loan-1", got[0]["loan_id"])
	assert.Equal(t, "REF-1", got[0]["loan_reference"])
}

func TestRaise(t *testing.T) {
	tests := []struct {
		name      string
		svc       *recordingService
		wantLines []string
	}{
		{name: "nil service logs the alert", svc: nil, wantLines: []string{Message}},
		{name: "working service is used and nothing is logged", svc: &recordingService{}, wantLines: nil},
		{name: "failing service still logs the alert", svc: &recordingService{err: errors.New("down")},
			wantLines: []string{"ops alert delivery failed, logging it instead", Message}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, buf := captureLogger()
			ctx := logging.WithLoan(t.Context(), "loan-1", "")
			var svc Service
			if tt.svc != nil {
				svc = tt.svc
			}

			Raise(ctx, svc, logger, "subject", "message")

			var msgs []string
			for _, l := range lines(t, buf) {
				msgs = append(msgs, l["msg"].(string))
			}
			assert.Equal(t, tt.wantLines, msgs)
			if tt.svc != nil {
				assert.Equal(t, 1, tt.svc.calls)
				assert.Equal(t, []slog.Attr{slog.String("loan_id", "loan-1")}, tt.svc.attrs,
					"the service receives the loan context")
			}
		})
	}
}
