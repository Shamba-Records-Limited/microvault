package models

import "testing"

// TxCategoryFor replaced a stored column, so it has to answer for every type
// that exists. A new TxType added without a case here would silently settle as
// off_chain — the same class of quiet wrongness the column was dropped for.
func TestTxCategoryForCoversEveryType(t *testing.T) {
	want := map[string]string{
		TxTypeVaultBorrow:    TxCategoryOnChain,
		TxTypeVaultRepay:     TxCategoryOnChain,
		TxTypeAnchorTransfer: TxCategoryOnChain,
		TxTypeRefund:         TxCategoryOnChain,
		TxTypeOffRamp:        TxCategoryOffChain,
		TxTypeFiatFailover:   TxCategoryOffChain,
	}

	for txType, category := range want {
		if got := TxCategoryFor(txType); got != category {
			t.Errorf("TxCategoryFor(%q) = %q, want %q", txType, got, category)
		}
	}
}
