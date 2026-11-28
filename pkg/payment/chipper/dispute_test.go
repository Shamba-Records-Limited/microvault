package chipper

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func TestEventDispute(t *testing.T) {
	ev := Event{Event: EventDisputeCreated, Data: json.RawMessage(`{"id":"dsp_1","collectionId":"col_1","amount":"2578.10","currency":"KES",
		"category":"chargeback","status":"open","statusMessage":null,"resolvedAt":null,"createdAt":"2026-11-28T10:00:00Z","updatedAt":"2026-11-28T10:00:00Z"}`)}
	d, err := ev.Dispute()
	require.NoError(t, err)
	assert.Equal(t, "col_1", d.CollectionID)
	assert.Equal(t, "2578.10", d.Amount.String())
	assert.Equal(t, DisputeOpen, d.Status)

	_, err = Event{Event: EventDisputeCreated, Data: json.RawMessage(`[]`)}.Dispute()
	assert.Equal(t, pkgErrors.CodeDecodeFailed, codeOf(t, err))
}
