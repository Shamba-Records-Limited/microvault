package compliance

import (
	"context"
	"log/slog"
	"time"

	"github.com/stellar/go-stellar-sdk/strkey"
	"gorm.io/datatypes"

	pkgcompliance "github.com/Shamba-Records-Limited/microvault/pkg/compliance"
	"github.com/Shamba-Records-Limited/microvault/pkg/compliance/didit"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

// defaultScreeningValidity is about three months, used when Deps.ScreeningValidity is zero.
const defaultScreeningValidity = 90 * 24 * time.Hour

// Service runs the KYB lifecycle described in doc.go.
type Service struct {
	repo              repository.CounterpartyRepository
	screener          pkgcompliance.Screener
	screeningValidity time.Duration
	requireVerified   bool
	logger            *slog.Logger
}

// Deps are Service's collaborators; all required except Logger and
// ScreeningValidity.
type Deps struct {
	Repo              repository.CounterpartyRepository
	Screener          pkgcompliance.Screener
	ScreeningValidity time.Duration
	// RequireKYBVerified refuses ApproveKYB until the KYB provider has
	// approved the business; set when Didit is configured.
	RequireKYBVerified bool
	Logger             *slog.Logger
}

// NewService builds the lifecycle service.
func NewService(deps Deps) *Service {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	validity := deps.ScreeningValidity
	if validity <= 0 {
		validity = defaultScreeningValidity
	}
	return &Service{
		repo:              deps.Repo,
		screener:          deps.Screener,
		screeningValidity: validity,
		requireVerified:   deps.RequireKYBVerified,
		logger:            logger.With("component", "compliance_lifecycle"),
	}
}

// SubmitAddress validates address locally before writing anything — "a
// typo should not cost an API call" — then
// records it. If the counterparty's KYB is already approved, it is
// screened immediately; otherwise it waits at AddressStatusPending until
// ApproveKYB screens it.
func (s *Service) SubmitAddress(ctx context.Context, counterpartyID, address string) (*models.CounterpartyAddress, error) {
	if !strkey.IsValidEd25519PublicKey(address) {
		return nil, ErrInvalidAddress
	}

	cp, err := s.repo.GetByID(ctx, counterpartyID)
	if err != nil {
		return nil, err
	}

	addr := &models.CounterpartyAddress{
		CounterpartyID: counterpartyID,
		Address:        address,
		Status:         models.AddressStatusPending,
		OnchainState:   models.OnchainStateAbsent,
	}
	if err := s.repo.AddAddress(ctx, addr); err != nil {
		return nil, err
	}

	if cp.KYBStatus == models.CounterpartyKYBApproved {
		if err := s.ScreenAndRecord(ctx, addr.ID); err != nil {
			// The address row is saved either way — a screening failure
			// here is not a submission failure, it leaves the address at
			// AddressStatusPending for a retry (via the admin's rescreen
			// action): if the provider is down, onboarding stalls and nothing
			// is corrupted.
			s.logger.ErrorContext(ctx, "initial screening failed after address submission",
				"address_id", addr.ID, "error", err)
		}
	}

	return addr, nil
}

// ApproveKYB approves the counterparty's KYB status, then screens every
// address still waiting for it; addresses already decided keep their status.
func (s *Service) ApproveKYB(ctx context.Context, counterpartyID, actor string) error {
	if s.requireVerified {
		cp, err := s.repo.GetByID(ctx, counterpartyID)
		if err != nil {
			return err
		}
		if cp.KYBSessionStatus != didit.StatusApproved {
			return ErrKYBNotVerified
		}
	}
	if err := s.repo.ApproveKYB(ctx, counterpartyID, actor); err != nil {
		return err
	}

	addrs, err := s.repo.ListAddressesByCounterparty(ctx, counterpartyID)
	if err != nil {
		return err
	}
	for _, addr := range addrs {
		if addr.Status != models.AddressStatusPending {
			continue
		}
		if err := s.ScreenAndRecord(ctx, addr.ID); err != nil {
			s.logger.ErrorContext(ctx, "screening failed during KYB approval",
				"counterparty_id", counterpartyID, "address_id", addr.ID, "error", err)
		}
	}
	return nil
}

// ScreenAndRecord is the core operation: screen one address via
// pkg/compliance.Screener and apply the verdict branches. Also the admin's "rescreen now" action — screening
// is idempotent to call again, and address_screenings is append-only, so a
// manual rescreen is simply another call to this with no separate path.
func (s *Service) ScreenAndRecord(ctx context.Context, addressID string) error {
	_, err := s.Rescreen(ctx, addressID)
	return err
}

// Rescreen is ScreenAndRecord returning the screening it recorded.
func (s *Service) Rescreen(ctx context.Context, addressID string) (*pkgcompliance.Screening, error) {
	return s.screen(ctx, addressID, false)
}

// Recheck screens an address again after a provider's monitoring alert. It
// can only tighten: a rejected or review verdict is applied, a clean one
// only renews an address that is already approved or expired, and it never
// queues an allowlist write, so a person's rejection or revocation stands.
func (s *Service) Recheck(ctx context.Context, addressID string) (*pkgcompliance.Screening, error) {
	return s.screen(ctx, addressID, true)
}

func (s *Service) screen(ctx context.Context, addressID string, tightenOnly bool) (*pkgcompliance.Screening, error) {
	addr, err := s.repo.GetAddressByID(ctx, addressID)
	if err != nil {
		return nil, err
	}
	if addr.Counterparty == nil {
		return nil, ErrCounterpartyMissing
	}

	screening, err := s.screener.ScreenAddress(ctx, pkgcompliance.ScreenRequest{
		Address:           addr.Address,
		CustomerReference: addr.Counterparty.EllipticCustomerReference,
	})
	if err != nil {
		return nil, err
	}

	newStatus, expiresAt := s.applyVerdict(screening.Verdict)
	if tightenOnly {
		newStatus, expiresAt = tighten(addr.Status, newStatus, expiresAt)
	}

	// Queue the on-chain allow write only when the address is newly
	// approved from a clean slate (onchain_state absent): approved queues
	// an on-chain allowlist write and onchain_state goes absent → pending. A rescreen of an
	// address that is already pending/approved/revoked must not touch
	// onchain_state here: re-confirming a still-clean verdict is not a
	// reason to re-queue a write, and an address a human explicitly
	// revoked does not get silently re-queued by an automated rescreen —
	// that needs its own deliberate admin action.
	var newOnchainState *models.CounterpartyAddressOnchainState
	if !tightenOnly && newStatus != nil && *newStatus == models.AddressStatusApproved && addr.OnchainState == models.OnchainStateAbsent {
		state := models.OnchainStatePending
		newOnchainState = &state
	}

	rawPayload := []byte(screening.Raw)
	if len(rawPayload) == 0 {
		rawPayload = []byte("{}")
	}
	record := &models.AddressScreening{
		AddressID:           addressID,
		EllipticAnalysisID:  screening.AnalysisID,
		EllipticScreeningID: screening.ScreeningID,
		ScreeningSource:     "sync",
		Provider:            screening.Provider,
		ProviderRef:         screening.ProviderRef,
		RiskScore:           screening.RiskScore,
		Sanctioned:          screening.Sanctioned,
		Verdict:             string(screening.Verdict),
		RawPayload:          datatypes.JSON(rawPayload),
	}

	if err := s.repo.RecordScreening(ctx, record, newStatus, newOnchainState, expiresAt); err != nil {
		return nil, err
	}
	return screening, nil
}

// tighten limits an automated status change to one that never loosens what
// is recorded: review and rejected apply (rejected is kept over review), and
// approval applies only to an address that is approved or expired already.
func tighten(current models.CounterpartyAddressStatus, next *models.CounterpartyAddressStatus, expires *time.Time) (*models.CounterpartyAddressStatus, *time.Time) {
	if next == nil {
		return nil, nil
	}
	switch *next {
	case models.AddressStatusRejected:
		return next, nil
	case models.AddressStatusReview:
		if current == models.AddressStatusRejected {
			return nil, nil
		}
		return next, nil
	default:
		if current == models.AddressStatusApproved || current == models.AddressStatusExpired {
			return next, expires
		}
		return nil, nil
	}
}

// applyVerdict maps a compliance.Verdict to the address's next status and
// expiry. A nil
// status means "leave the address's current status untouched" — used for
// VerdictPending so a rescreen that hasn't resolved yet can never silently
// downgrade an address that was already approved.
func (s *Service) applyVerdict(verdict pkgcompliance.Verdict) (*models.CounterpartyAddressStatus, *time.Time) {
	status := func(v models.CounterpartyAddressStatus) *models.CounterpartyAddressStatus { return &v }

	switch verdict {
	case pkgcompliance.VerdictApproved:
		expires := time.Now().Add(s.screeningValidity)
		return status(models.AddressStatusApproved), &expires
	case pkgcompliance.VerdictRejected:
		return status(models.AddressStatusRejected), nil
	case pkgcompliance.VerdictReview:
		return status(models.AddressStatusReview), nil
	case pkgcompliance.VerdictUnscreenable:
		// Provisional approval: no on-chain history to judge, so the
		// address is allowed to deposit, but it is not a real "clean"
		// verdict — it needs a mandatory
		// rescreen on the address's first observed deposit. That trigger
		// (the vault watcher noticing a first deposit from a provisional
		// address) is Phase 5/6 wiring not built in this pass; for now
		// the provisional nature is visible to an operator via this
		// screening's own Verdict column on address_screenings — the
		// address status alone (Approved) does not distinguish it, which
		// is exactly why the raw screening history, not just current
		// status, is what the admin's screening detail view renders from.
		expires := time.Now().Add(s.screeningValidity)
		return status(models.AddressStatusApproved), &expires
	default:
		// VerdictPending, or anything not yet defined: leave the address's
		// current status untouched — see the doc comment above. A
		// screening row is still recorded by the caller; even a pending
		// result is worth keeping, since screenings are append-only.
		return nil, nil
	}
}
