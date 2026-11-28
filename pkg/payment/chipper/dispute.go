package chipper

import (
	"encoding/json"
	"time"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// Dispute statuses. Accepted and declined are final.
const (
	DisputeOpen        = "open"
	DisputeUnderReview = "under_review"
	DisputeAccepted    = "accepted"
	DisputeDeclined    = "declined"
)

// Dispute is a challenge to a completed collection; Chipper holds its
// amount on the balance until it is resolved.
type Dispute struct {
	ID            string          `json:"id"`
	CollectionID  string          `json:"collectionId"`
	Amount        decimal.Decimal `json:"amount"`
	Currency      string          `json:"currency"`
	Category      string          `json:"category"`
	Status        string          `json:"status"`
	StatusMessage *string         `json:"statusMessage"`
	ResolvedAt    *time.Time      `json:"resolvedAt"`
	CreatedAt     time.Time       `json:"createdAt"`
	UpdatedAt     time.Time       `json:"updatedAt"`
}

// Dispute decodes an event's Data as a dispute.
func (e Event) Dispute() (*Dispute, error) {
	var d Dispute
	if err := json.Unmarshal(e.Data, &d); err != nil {
		return nil, chipperErr("webhook_dispute").With("event", e.Event).
			Code(pkgErrors.CodeDecodeFailed).Wrapf(err, "could not decode the dispute")
	}
	return &d, nil
}
