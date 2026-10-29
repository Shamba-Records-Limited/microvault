package didit

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Session statuses for business (KYB) sessions.
const (
	StatusNotStarted   = "NOT_STARTED"
	StatusInProgress   = "IN_PROGRESS"
	StatusAwaitingUser = "AWAITING_USER"
	StatusInReview     = "IN_REVIEW"
	StatusResubmitted  = "RESUBMITTED"
	StatusApproved     = "APPROVED"
	StatusDeclined     = "DECLINED"
	StatusAbandoned    = "ABANDONED"
	StatusExpired      = "EXPIRED"
)

// Session is a created verification session and the hosted link to send to
// the lender's contact.
type Session struct {
	ID     string `json:"session_id"`
	Number int64  `json:"session_number"`
	URL    string `json:"url"`
}

type createSessionRequest struct {
	WorkflowID string `json:"workflow_id"`
	VendorData string `json:"vendor_data"`
	Callback   string `json:"callback,omitempty"`
}

// CreateKYBSession starts a business verification for vendorData, our
// counterparty id, under the KYB workflow.
func (c *Client) CreateKYBSession(ctx context.Context, workflowID, vendorData, callback string) (*Session, error) {
	const op = "create_kyb_session"
	if workflowID == "" || vendorData == "" {
		return nil, diditErr(op).Code(pkgErrors.CodeInvalidConfig).Errorf("workflow id and vendor data are required")
	}
	var s Session
	if _, err := c.do(ctx, op, http.MethodPost, "/v3/session/", createSessionRequest{
		WorkflowID: workflowID, VendorData: vendorData, Callback: callback,
	}, &s); err != nil {
		return nil, err
	}
	return &s, nil
}

// Decision is a session's decision; Raw keeps the whole body for the record.
type Decision struct {
	SessionID  string          `json:"session_id"`
	Status     string          `json:"status"`
	VendorData string          `json:"vendor_data"`
	Raw        json.RawMessage `json:"-"`
}

// KYBDecision fetches a session's current decision.
func (c *Client) KYBDecision(ctx context.Context, sessionID string) (*Decision, error) {
	const op = "kyb_decision"
	var d Decision
	raw, err := c.do(ctx, op, http.MethodGet, "/v3/session/"+url.PathEscape(sessionID)+"/decision/", nil, &d)
	if err != nil {
		return nil, err
	}
	d.Raw = raw
	return &d, nil
}
