package chipper

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

func TestGetCollection(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/v1/collections/col_1", r.URL.Path)
		writeJSON(t, w, http.StatusOK, `{"collection":{"id":"col_1","type":"mobile_money","status":"reversed",
			"from":{"accountNumber":"254711111111","amount":"2578.10","currency":"KES"},"to":{"amount":"2578.10","currency":"KES"},"createdAt":"2026-11-28T10:00:00Z"}}`)
	})
	col, err := c.GetCollection(context.Background(), "col_1")
	require.NoError(t, err)
	assert.Equal(t, CollectionReversed, col.Status)

	_, err = c.GetCollection(context.Background(), "")
	assert.Equal(t, pkgErrors.CodeMissingAccount, codeOf(t, err))
}
