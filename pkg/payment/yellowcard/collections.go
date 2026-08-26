package yellowcard

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Collections are YellowCard's pay-in direction: funds move from a payer's
// bank or mobile-money account into our YellowCard balance. The endpoints sit
// under /receive — YellowCard renamed Collections to Receives alongside the
// Payments-to-Sends rename, and only the current paths are wired here.
//
// This file is the client surface only. Nothing in the platform consumes it
// yet.

// collectionErr starts an error builder for one collection call, scoped to the
// collection it is about. Every failure below therefore names both the
// operation and which collection it concerned.
func collectionErr(op, collectionID string) oops.OopsErrorBuilder {
	return ycErr(op).With(pkgErrors.AttrCollectionID, collectionID)
}

// collectionCall performs one JSON request against the collections API and
// decodes the response.
//
// The send-side methods in yellowcard.go each inline this sequence. Repeating
// it another eight times for one direction is not worth the symmetry, so the
// collection methods share it. Signed and decoded identically either way.
//
// errb carries the operation and identifiers, so the caller decides what an
// on-call engineer sees rather than this function guessing.
func collectionCall[T any](ctx context.Context, y *YellowcardAdapter, errb oops.OopsErrorBuilder, method, endpoint string, body any) (*T, error) {
	var reader io.Reader = http.NoBody
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not encode the request")
		}
		reader = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, y.baseURL+endpoint, reader)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the request")
	}

	resp, err := y.httpClient.Do(req)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeTransportFailed).Wrapf(err, "request did not complete")
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return nil, parseErrorWith(errb, resp)
	}

	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, errb.Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the response")
	}
	return &out, nil
}

// SubmitCollection opens a collection request, locking in a rate.
//
// ForceAccept is set unconditionally. Left false, YellowCard parks the request
// in pending_approval awaiting an explicit accept, and that window expires
// after five minutes in production and ten in sandbox. Nothing here has a
// human in the loop — the payer has already committed to the amount — so an
// approval step can only fail the collection.
func (y *YellowcardAdapter) SubmitCollection(ctx context.Context, req CollectionRequest) (*Collection, error) {
	req.ForceAccept = true
	return collectionCall[Collection](ctx, y, ycErr("submit_collection").With(pkgErrors.AttrSequenceID, req.SequenceID), http.MethodPost, "/receive", req)
}

// AcceptCollection approves a collection request for execution.
//
// SubmitCollection sets forceAccept, so a collection opened through this
// client never reaches pending_approval and never needs this. It is wrapped
// for requests submitted elsewhere.
func (y *YellowcardAdapter) AcceptCollection(ctx context.Context, collectionID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, collectionErr("accept_collection", collectionID), http.MethodPost, "/receive/"+collectionID+"/accept", nil)
}

// DenyCollection rejects a collection request awaiting approval. See
// AcceptCollection for why this is not on the normal path.
func (y *YellowcardAdapter) DenyCollection(ctx context.Context, collectionID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, collectionErr("deny_collection", collectionID), http.MethodPost, "/receive/"+collectionID+"/deny", nil)
}

// LookupCollection retrieves a collection by YellowCard's own ID.
func (y *YellowcardAdapter) LookupCollection(ctx context.Context, collectionID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, collectionErr("lookup_collection", collectionID), http.MethodGet, "/receive/"+collectionID, nil)
}

// LookupCollectionBySequenceID retrieves a collection by the sequence ID we
// supplied at submission. This is the lookup that works after a crash between
// submitting and persisting YellowCard's ID.
func (y *YellowcardAdapter) LookupCollectionBySequenceID(ctx context.Context, sequenceID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, ycErr("lookup_collection_by_sequence_id").With(pkgErrors.AttrSequenceID, sequenceID), http.MethodGet, "/receive/sequence-id/"+sequenceID, nil)
}

// CancelCollection stops a collection that has not yet completed.
func (y *YellowcardAdapter) CancelCollection(ctx context.Context, collectionID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, collectionErr("cancel_collection", collectionID), http.MethodPost, "/receive/"+collectionID+"/cancel", nil)
}

// RefundCollection returns collected funds to the payer.
//
// YellowCard accepts this only from complete, cancelled or refund_failed;
// anything else answers 400 PaymentInvalidState.
func (y *YellowcardAdapter) RefundCollection(ctx context.Context, collectionID string) (*Collection, error) {
	return collectionCall[Collection](ctx, y, collectionErr("refund_collection", collectionID), http.MethodPost, "/receive/"+collectionID+"/refund", nil)
}

// ListCollectionsParams filters and paginates a collection listing. The zero
// value asks YellowCard for its own defaults: 100 per page, ranged and sorted
// on createdAt, newest first.
type ListCollectionsParams struct {
	StartDate time.Time
	EndDate   time.Time
	// StartAt is the offset the page begins at.
	StartAt int
	PerPage int
	// RangeBy and SortBy accept "createdAt" or "updatedAt"; OrderBy accepts
	// "asc" or "desc".
	RangeBy string
	SortBy  string
	OrderBy string
}

// query renders the params as a query string, omitting anything unset so
// YellowCard applies its own default rather than a zero.
func (p ListCollectionsParams) query() string {
	v := url.Values{}
	if !p.StartDate.IsZero() {
		v.Set("startDate", p.StartDate.UTC().Format(time.RFC3339))
	}
	if !p.EndDate.IsZero() {
		v.Set("endDate", p.EndDate.UTC().Format(time.RFC3339))
	}
	if p.StartAt > 0 {
		v.Set("startAt", strconv.Itoa(p.StartAt))
	}
	if p.PerPage > 0 {
		v.Set("perPage", strconv.Itoa(p.PerPage))
	}
	if p.RangeBy != "" {
		v.Set("rangeBy", p.RangeBy)
	}
	if p.SortBy != "" {
		v.Set("sortBy", p.SortBy)
	}
	if p.OrderBy != "" {
		v.Set("orderBy", p.OrderBy)
	}
	return v.Encode()
}

// ListCollections retrieves a page of collection requests.
func (y *YellowcardAdapter) ListCollections(ctx context.Context, params ListCollectionsParams) ([]Collection, error) {
	endpoint := "/receives"
	if q := params.query(); q != "" {
		endpoint += "?" + q
	}

	resp, err := collectionCall[CollectionsResponse](ctx, y, ycErr("list_collections"), http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	return resp.Collections, nil
}
