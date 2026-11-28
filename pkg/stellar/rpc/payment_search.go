package rpc

import (
	"bytes"
	"context"
	"encoding/json"
	"time"

	protocol "github.com/stellar/go-stellar-sdk/protocols/rpc"
	"github.com/stellar/go-stellar-sdk/strkey"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

const (
	ledgerSeconds      = 5
	searchLedgerMargin = 12
	searchPageLimit    = 100
	searchMaxPages     = 40
)

// PaymentSearchClient is the part of the RPC client a payment search uses.
type PaymentSearchClient interface {
	EventsGetter
	TransactionGetter
	GetLatestLedger(ctx context.Context) (protocol.GetLatestLedgerResponse, error)
}

// PaymentSearch names a classic payment to look for: an asset paid to
// Destination in a transaction carrying Memo, on or after Since.
//
// A memo is no secret, so anyone can send a payment carrying it. MinStroops
// skips smaller matches, so dust sent under a borrower's memo cannot stand in
// for the real payment.
type PaymentSearch struct {
	Destination string
	AssetCode   string
	AssetIssuer string
	Memo        string
	Since       time.Time
	MinStroops  int64
}

// FoundPayment is the transaction that paid the destination and what it paid.
type FoundPayment struct {
	TxHash        string
	Ledger        int32
	AmountStroops int64
}

// PaymentFinder finds payments by memo through the asset contract's transfer
// events, for payers that do not report the transaction hash.
type PaymentFinder struct {
	client     PaymentSearchClient
	passphrase string
}

// NewPaymentFinder returns a PaymentFinder on the given network.
func NewPaymentFinder(client PaymentSearchClient, networkPassphrase string) *PaymentFinder {
	return &PaymentFinder{client: client, passphrase: networkPassphrase}
}

// Find returns the earliest transaction since s.Since that carries s.Memo and
// pays s.Destination at least s.MinStroops in the asset. Only plain payment
// operations count, not path payments. ok is false when none is visible yet. The
// search reaches back only as far as the RPC node's event retention.
func (f *PaymentFinder) Find(ctx context.Context, s PaymentSearch) (found FoundPayment, ok bool, err error) {
	errb := verifyErr("find_payment").With(pkgErrors.AttrAddress, s.Destination).With("memo", s.Memo)
	switch {
	case s.Destination == "" || s.AssetCode == "" || s.AssetIssuer == "":
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeMissingAccount).Errorf("destination and asset are required")
	case s.Memo == "":
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeInvalidMemo).Errorf("memo is required")
	}

	filter, err := f.transferFilter(s)
	if err != nil {
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeInvalidConfig).Wrapf(err, "could not build the event filter")
	}
	start, err := f.startLedger(ctx, s.Since)
	if err != nil {
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the latest ledger")
	}

	req := protocol.GetEventsRequest{
		StartLedger: start,
		Filters:     []protocol.EventFilter{filter},
		Pagination:  &protocol.PaginationOptions{Limit: searchPageLimit},
	}
	seen := map[string]bool{}
	for range searchMaxPages {
		resp, err := f.client.GetEvents(ctx, req)
		if oldest, out := OutOfRetention(err); out && req.StartLedger != 0 && req.StartLedger < oldest {
			req.StartLedger = oldest
			continue
		}
		if err != nil {
			return FoundPayment{}, false, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read transfer events")
		}
		for _, ev := range resp.Events {
			if ev.TransactionHash == "" || seen[ev.TransactionHash] {
				continue
			}
			seen[ev.TransactionHash] = true
			p, matched, err := f.paymentIn(ctx, ev.TransactionHash, s)
			if err != nil {
				return FoundPayment{}, false, err
			}
			if matched {
				p.Ledger = ev.Ledger
				return p, true, nil
			}
		}
		// A response covers a bounded window of ledgers, so a short page with a
		// cursor short of the latest ledger still has more to scan.
		if resp.Cursor == "" {
			return FoundPayment{}, false, nil
		}
		cursor, err := protocol.ParseCursor(resp.Cursor)
		if err != nil {
			return FoundPayment{}, false, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not read the events cursor")
		}
		if cursor.Ledger >= resp.LatestLedger {
			return FoundPayment{}, false, nil
		}
		req.StartLedger = 0
		req.Pagination = &protocol.PaginationOptions{Cursor: &cursor, Limit: searchPageLimit}
	}
	return FoundPayment{}, false, errb.Code(pkgErrors.CodeIncompleteResponse).
		With("pages", searchMaxPages).Errorf("too many transfer events to search")
}

// transferFilter matches the asset contract's transfer events into the
// destination: topics transfer, from, to, asset.
func (f *PaymentFinder) transferFilter(s PaymentSearch) (protocol.EventFilter, error) {
	asset, err := xdr.NewCreditAsset(s.AssetCode, s.AssetIssuer)
	if err != nil {
		return protocol.EventFilter{}, err
	}
	id, err := asset.ContractID(f.passphrase)
	if err != nil {
		return protocol.EventFilter{}, err
	}
	contract, err := strkey.Encode(strkey.VersionByteContract, id[:])
	if err != nil {
		return protocol.EventFilter{}, err
	}
	account, err := xdr.AddressToAccountId(s.Destination)
	if err != nil {
		return protocol.EventFilter{}, err
	}

	sym := xdr.ScSymbol("transfer")
	transfer, err := xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}.MarshalBinary()
	if err != nil {
		return protocol.EventFilter{}, err
	}
	to, err := xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &xdr.ScAddress{
		Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: &account,
	}}.MarshalBinary()
	if err != nil {
		return protocol.EventFilter{}, err
	}
	anyOne := protocol.WildCardExactOne
	return protocol.EventFilter{
		EventType:   protocol.EventTypeSet{protocol.EventTypeContract: nil},
		ContractIDs: []string{contract},
		Topics: []protocol.TopicFilter{{
			{ScVal: transfer}, {Wildcard: &anyOne}, {ScVal: to}, {Wildcard: &anyOne},
		}},
	}, nil
}

// startLedger estimates the ledger that closed at since, with a margin.
// Without a close time it starts at the beginning of retention.
func (f *PaymentFinder) startLedger(ctx context.Context, since time.Time) (uint32, error) {
	latest, err := f.client.GetLatestLedger(ctx)
	if err != nil {
		return 0, err
	}
	if latest.LedgerCloseTime <= 0 {
		return 1, nil
	}
	back := int64(searchLedgerMargin)
	if elapsed := latest.LedgerCloseTime - since.Unix(); elapsed > 0 {
		back += elapsed / ledgerSeconds
	}
	if back >= int64(latest.Sequence) {
		return 1, nil
	}
	return latest.Sequence - uint32(back), nil
}

// paymentIn reads a transaction and reports what it paid the destination when
// it carries the memo.
func (f *PaymentFinder) paymentIn(ctx context.Context, txHash string, s PaymentSearch) (FoundPayment, bool, error) {
	errb := verifyErr("find_payment").With(pkgErrors.AttrTxHash, txHash)
	resp, err := f.client.GetTransaction(ctx, protocol.GetTransactionRequest{Hash: txHash, Format: protocol.FormatJSON})
	if err != nil {
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the transaction")
	}
	if resp.Status != protocol.TransactionStatusSuccess || len(resp.EnvelopeJSON) == 0 {
		return FoundPayment{}, false, nil
	}
	// UseNumber keeps a 64-bit id memo exact.
	dec := json.NewDecoder(bytes.NewReader(resp.EnvelopeJSON))
	dec.UseNumber()
	var envelope any
	if err := dec.Decode(&envelope); err != nil {
		return FoundPayment{}, false, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the transaction envelope")
	}
	if memo, ok := findMemo(envelope); !ok || memo != s.Memo {
		return FoundPayment{}, false, nil
	}

	var total int64
	for _, p := range collectPayments(envelope) {
		if p.Destination == s.Destination && p.AssetCode == s.AssetCode && p.AssetIssuer == s.AssetIssuer {
			total += p.AmountStroops
		}
	}
	if total <= 0 || total < s.MinStroops {
		return FoundPayment{}, false, nil
	}
	return FoundPayment{TxHash: txHash, AmountStroops: total}, true, nil
}

// findMemo reads an id or text memo from a decoded envelope, at any depth so
// fee-bump envelopes are covered.
func findMemo(node any) (string, bool) {
	switch n := node.(type) {
	case map[string]any:
		if m, ok := n["memo"].(map[string]any); ok {
			switch v := lookupMemo(m).(type) {
			case string:
				return v, true
			case json.Number:
				return v.String(), true
			}
		}
		for _, v := range n {
			if memo, ok := findMemo(v); ok {
				return memo, true
			}
		}
	case []any:
		for _, v := range n {
			if memo, ok := findMemo(v); ok {
				return memo, true
			}
		}
	}
	return "", false
}

func lookupMemo(m map[string]any) any {
	if v, ok := m["id"]; ok {
		return v
	}
	return m["text"]
}
