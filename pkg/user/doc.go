// Package user is the business-logic layer for registered users — the people the
// platform serves and the staff who administer it. A user is identified by a
// mobile number, and carries a national ID, a KYC state, a role, and a lifecycle
// status. Service wraps the user repository and adds validation, uniqueness, the
// state machines below, and the guardrails that protect administrators from
// locking themselves out.
//
// Build a Service with NewService. It creates users, looks them up (by ID,
// mobile number, or national ID), lists them with filters and pagination, and
// updates their profile, KYC, status, and role. It also exposes the counts the
// admin dashboards report on. Requests and responses are the DTOs in dto.go;
// failures are the sentinel errors in errors.go, which handlers map onto HTTP
// status codes. The mobile number and national ID are each unique across all
// users, and a duplicate is rejected at creation.
//
// # Three dimensions of state
//
// A user's state is tracked along three independent axes:
//
//   - KYC moves from pending to verified or rejected; a verified user can lapse
//     to expired, and expired or rejected users return to pending to try again.
//   - Status follows the same lifecycle as accounts — active, suspended, frozen,
//     blocked, closed — with closed terminal.
//   - Role is one of user, admin, or agent.
//
// KYC and status changes are checked against a transition table and an illegal
// move is rejected with ErrInvalidKYCTransition or ErrInvalidStatusTransition.
//
// # Administrative guardrails
//
// The privileged operations take the requester's ID alongside the target's, so
// the service can refuse changes that would compromise administration: a user
// cannot delete their own account (ErrCannotDeleteSelf), cannot change their own
// role (ErrCannotChangeOwnRole), and the last remaining admin cannot be deleted
// (ErrCannotDeleteLastAdmin). A deleted user cannot be modified until restored.
//
// # Transactions
//
// CreateWithTx runs inside a *gorm.DB the caller owns, so a user can be created
// in the same database transaction as their first account.
package user
