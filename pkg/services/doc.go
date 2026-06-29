// Package services holds the primitives shared by the business-logic service
// layer. It carries no behaviour of its own — just the common vocabulary the
// account, transaction, and user services build on.
//
// The sentinel errors (ErrNotFound, ErrUnauthorized, ErrForbidden, ErrConflict)
// are the transport-neutral failures a service can return for handlers to map
// onto HTTP status codes. Pagination is the shared list-query input, and
// PaginatedResponse is the generic envelope a paginated result is returned in.
package services
