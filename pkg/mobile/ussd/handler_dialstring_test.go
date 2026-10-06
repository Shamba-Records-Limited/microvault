package ussd

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

type recordingAlerts struct {
	mu       sync.Mutex
	subjects []string
}

func (r *recordingAlerts) AlertOps(_ context.Context, subject, _ string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.subjects = append(r.subjects, subject)
	return nil
}

func newDialStringHarness(t *testing.T, dial string) (*USSDHandler, *recordingAlerts) {
	t.Helper()
	h := newHarness(t, &fakeUserSvc{getErr: errors.New("not found")}, &fakePINSvc{})
	rec := &recordingAlerts{}
	h.dialString = dial
	h.alerts = rec
	return h, rec
}

func TestHandleRequest_DialStringMismatchAlertsOncePerCode(t *testing.T) {
	h, rec := newDialStringHarness(t, "*384*52203#")
	ctx := context.Background()

	for _, sess := range []string{"s1", "s2"} {
		_, err := h.HandleRequest(ctx, sess, "254711000111", "*384#", "63902", "")
		assert.NoError(t, err)
	}
	_, _ = h.HandleRequest(ctx, "s3", "254711000111", "*789*10#", "63902", "")

	assert.Equal(t, []string{dialStringMismatchSubject, dialStringMismatchSubject}, rec.subjects)
}

func TestHandleRequest_DialStringMatchOrUnsetDoesNotAlert(t *testing.T) {
	h, rec := newDialStringHarness(t, "*384*52203#")
	_, _ = h.HandleRequest(context.Background(), "s1", "254711000111", " *384*52203# ", "63902", "")
	assert.Empty(t, rec.subjects)

	h, rec = newDialStringHarness(t, "")
	_, _ = h.HandleRequest(context.Background(), "s1", "254711000111", "*384#", "63902", "")
	assert.Empty(t, rec.subjects)
}
