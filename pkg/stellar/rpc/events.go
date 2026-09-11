package rpc

import (
	"context"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// EventsGetter is the interface for fetching contract events, so tests can
// stand in for the RPC client the same way TransactionGetter does for
// PollTransaction.
type EventsGetter interface {
	GetEvents(ctx context.Context, request protocol.GetEventsRequest) (protocol.GetEventsResponse, error)
}

func eventsErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainCompliance).Tags("rpc", "events").With(pkgErrors.AttrOperation, op)
}

// FetchVaultEvents returns events emitted by contractID from fromLedger
// onward (inclusive), up to limit events.
//
// This does not walk multiple pages within one call — soroban RPC's cursor
// is opaque, and at the vault's current volume one page comfortably covers a
// watch interval. If a returned page is exactly limit-sized there may be more
// events than were fetched; the caller (pkg/services/vaultwatch) logs that
// case rather than silently under-counting, since this is a compliance
// canary and a gap here should be loud.
func FetchVaultEvents(ctx context.Context, client EventsGetter, contractID string, fromLedger uint32, limit uint) (protocol.GetEventsResponse, error) {
	errb := eventsErr("fetch_vault_events").
		With(pkgErrors.AttrAddress, contractID).
		With("from_ledger", fromLedger)

	if contractID == "" {
		return protocol.GetEventsResponse{}, errb.Code(pkgErrors.CodeMissingAccount).
			Errorf("contract ID is empty")
	}

	resp, err := client.GetEvents(ctx, protocol.GetEventsRequest{
		StartLedger: fromLedger,
		Filters: []protocol.EventFilter{
			{ContractIDs: []string{contractID}},
		},
		Pagination: &protocol.PaginationOptions{Limit: limit},
	})
	if err != nil {
		return protocol.GetEventsResponse{}, errb.Code(pkgErrors.CodeTransportFailed).
			Wrapf(err, "could not fetch vault events")
	}
	return resp, nil
}
