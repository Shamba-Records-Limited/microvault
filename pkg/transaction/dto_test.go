package transaction

import (
	"reflect"
	"testing"

	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// TestChangedFields_OnlyEmitsSetFields verifies the partial-update mapping does
// not leak nil fields, which would clobber existing columns with NULL.
func TestChangedFields_OnlyEmitsSetFields(t *testing.T) {
	if got := (UpdateTransactionRequest{}).changedFields(); len(got) != 0 {
		t.Fatalf("empty request emitted %d fields: %v", len(got), got)
	}

	status := "submitted"
	hash := "deadbeef"
	req := UpdateTransactionRequest{Status: &status, StellarTxHash: &hash}
	got := req.changedFields()
	if len(got) != 2 {
		t.Fatalf("expected 2 fields, got %d: %v", len(got), got)
	}
	if got["status"] != &status || got["stellar_tx_hash"] != &hash {
		t.Fatalf("wrong columns/values: %v", got)
	}
}

// TestChangedFields_EveryPointerFieldIsMapped fills every pointer field via
// reflection and asserts changedFields emits one column per field, each a real
// updatable transaction column. Catches a field added to the request or the
// repository allow-list but not the other.
func TestChangedFields_EveryPointerFieldIsMapped(t *testing.T) {
	var req UpdateTransactionRequest
	v := reflect.ValueOf(&req).Elem()
	pointerFields := 0
	for i := 0; i < v.NumField(); i++ {
		f := v.Field(i)
		if f.Kind() != reflect.Ptr || !f.CanSet() {
			continue
		}
		f.Set(reflect.New(f.Type().Elem()))
		pointerFields++
	}

	got := req.changedFields()

	if len(got) != pointerFields {
		t.Fatalf("UpdateTransactionRequest has %d pointer fields but changedFields emitted %d; a field is missing from changedFields", pointerFields, len(got))
	}
	for col := range got {
		if !repository.IsTransactionUpdatableColumn(col) {
			t.Errorf("changedFields emits column %q which is NOT in the repository allow-list; UpdateFields would reject it", col)
		}
	}
}
