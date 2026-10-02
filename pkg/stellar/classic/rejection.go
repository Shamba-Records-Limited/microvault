package classic

import (
	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/rpc"
)

// rejectionError builds the error for a stellar-core refusal of op. See
// rpc.RejectionError.
func rejectionError(op string, resp protocol.SendTransactionResponse) error {
	return rpc.RejectionError(classicErr(op), resp)
}
