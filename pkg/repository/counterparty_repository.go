package repository

import (
	"context"
	"errors"
	"log"
	"time"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
)

// Common errors for CounterpartyRepository.
var (
	ErrCounterpartyNotFound        = errors.New("counterparty not found")
	ErrFailedToCreateCounterparty  = errors.New("failed to create counterparty")
	ErrFailedToGetCounterparty     = errors.New("failed to get counterparty")
	ErrFailedToGetCounterparties   = errors.New("failed to get counterparties")
	ErrFailedToCountCounterparties = errors.New("failed to count counterparties")
	ErrFailedToUpdateCounterparty  = errors.New("failed to update counterparty")

	ErrCounterpartyAddressNotFound        = errors.New("counterparty address not found")
	ErrFailedToAddCounterpartyAddress     = errors.New("failed to add counterparty address")
	ErrFailedToGetCounterpartyAddress     = errors.New("failed to get counterparty address")
	ErrFailedToGetCounterpartyAddresses   = errors.New("failed to get counterparty addresses")
	ErrFailedToCountCounterpartyAddresses = errors.New("failed to count counterparty addresses")
	ErrFailedToUpdateCounterpartyAddress  = errors.New("failed to update counterparty address")

	ErrFailedToRecordScreening = errors.New("failed to record screening")
)

// CounterpartyRepository spans all three compliance tables
// (counterparties, counterparty_addresses, address_screenings) — one
// bounded context, mirroring how UserRepository spans everything
// user-related in one file. Actor parameters throughout are the admin's
// Stellar public key as text (see the source design doc §14 and
// counterparty.go's KYBApprovedBy doc comment).
type CounterpartyRepository interface {
	Create(ctx context.Context, cp *models.Counterparty) error
	GetByID(ctx context.Context, id string) (*models.Counterparty, error) // preloads Addresses
	List(ctx context.Context, kybStatus string, limit, offset int) ([]*models.Counterparty, error)
	Count(ctx context.Context, kybStatus string) (int64, error)
	ApproveKYB(ctx context.Context, id, actor string) error
	RejectKYB(ctx context.Context, id, actor string) error

	AddAddress(ctx context.Context, addr *models.CounterpartyAddress) error
	GetAddressByID(ctx context.Context, id string) (*models.CounterpartyAddress, error) // preloads Counterparty + LatestScreening
	ListAddressesByCounterparty(ctx context.Context, counterpartyID string) ([]*models.CounterpartyAddress, error)
	ListAddressesByStatus(ctx context.Context, statuses []string, limit, offset int) ([]*models.CounterpartyAddress, error)
	CountAddressesByStatus(ctx context.Context, statuses []string) (int64, error)
	ApproveAddress(ctx context.Context, id, actor, reason string) error
	RejectAddress(ctx context.Context, id, actor, reason string) error
	RevokeAddress(ctx context.Context, id, actor, reason string) error

	// ListScreeningsByAddress returns an address's full screening history,
	// newest first — the audit artefact the screening detail screen renders
	// from raw_payload, per the source design doc §14.
	ListScreeningsByAddress(ctx context.Context, addressID string) ([]*models.AddressScreening, error)

	// RecordScreening inserts the append-only screening row and updates the
	// parent address's last_screening_id/screened_at/expires_at in one
	// transaction. newAddressStatus and newOnchainState are each optional —
	// nil leaves that column untouched (a screening whose verdict didn't
	// resolve to anything new, e.g. still pending, must not silently
	// downgrade an address that was already approved).
	RecordScreening(ctx context.Context, screening *models.AddressScreening, newAddressStatus *models.CounterpartyAddressStatus, newOnchainState *models.CounterpartyAddressOnchainState, expiresAt *time.Time) error

	// ListAddressesNeedingOnchainAllow returns approved addresses whose
	// onchain_state is still "pending" — the on-chain writer's queue for
	// allow_depositor. See the source design doc §14 on why this write
	// stays a separate worker rather than something the admin web process
	// signs itself.
	ListAddressesNeedingOnchainAllow(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error)

	// ListAddressesNeedingOnchainRevoke returns addresses with a recorded
	// revocation intent (RevokedAt set) whose onchain_state has not yet
	// been confirmed as revoked — the on-chain writer's queue for
	// disallow_depositor.
	ListAddressesNeedingOnchainRevoke(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error)

	// SetOnchainState records what the on-chain writer actually observed
	// after submitting a transaction — the "observed state" half of the
	// intent/observed split described in RevokeAddress's doc comment.
	SetOnchainState(ctx context.Context, id string, state models.CounterpartyAddressOnchainState) error

	// MarkExpiredAddresses flips every approved address whose expires_at
	// has passed to AddressStatusExpired, and returns how many it touched.
	// One bulk statement rather than a per-row round trip, since this runs
	// on every rescreening-sweep tick. Per the source design doc §10:
	// "expired must actually be reachable — a status that can only ever be
	// set by a sweep that nobody scheduled is a status that never fires."
	MarkExpiredAddresses(ctx context.Context) (int64, error)
}

type counterpartyRepository struct {
	db *gorm.DB
}

// NewCounterpartyRepository builds the repository.
func NewCounterpartyRepository(db *gorm.DB) (CounterpartyRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &counterpartyRepository{db: db}, nil
}

// --- Counterparty ---

func (r *counterpartyRepository) Create(ctx context.Context, cp *models.Counterparty) error {
	result := r.db.WithContext(ctx).Create(cp)
	if result.Error != nil {
		log.Printf("Create: database error: %v", result.Error)
		return ErrFailedToCreateCounterparty
	}
	return nil
}

func (r *counterpartyRepository) GetByID(ctx context.Context, id string) (*models.Counterparty, error) {
	var cp models.Counterparty
	result := r.db.WithContext(ctx).
		Preload("Addresses").
		Where("id = ?", id).
		First(&cp)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrCounterpartyNotFound
	}
	if result.Error != nil {
		log.Printf("GetByID: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterparty
	}
	return &cp, nil
}

func (r *counterpartyRepository) List(ctx context.Context, kybStatus string, limit, offset int) ([]*models.Counterparty, error) {
	var cps []*models.Counterparty
	query := r.db.WithContext(ctx)
	if kybStatus != "" {
		query = query.Where("kyb_status = ?", kybStatus)
	}
	result := query.
		Order("created_at DESC").
		Limit(limit).
		Offset(offset).
		Find(&cps)
	if result.Error != nil {
		log.Printf("List: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterparties
	}
	return cps, nil
}

func (r *counterpartyRepository) Count(ctx context.Context, kybStatus string) (int64, error) {
	var count int64
	query := r.db.WithContext(ctx).Model(&models.Counterparty{})
	if kybStatus != "" {
		query = query.Where("kyb_status = ?", kybStatus)
	}
	if err := query.Count(&count).Error; err != nil {
		log.Printf("Count: database error: %v", err)
		return 0, ErrFailedToCountCounterparties
	}
	return count, nil
}

func (r *counterpartyRepository) ApproveKYB(ctx context.Context, id, actor string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&models.Counterparty{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"kyb_status":      models.CounterpartyKYBApproved,
			"kyb_approved_at": now,
			"kyb_approved_by": actor,
			"updated_at":      now,
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyNotFound
	}
	if result.Error != nil {
		log.Printf("ApproveKYB: database error: %v", result.Error)
		return ErrFailedToUpdateCounterparty
	}
	return nil
}

func (r *counterpartyRepository) RejectKYB(ctx context.Context, id, actor string) error {
	result := r.db.WithContext(ctx).
		Model(&models.Counterparty{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"kyb_status": models.CounterpartyKYBRejected,
			"updated_at": time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyNotFound
	}
	if result.Error != nil {
		log.Printf("RejectKYB: database error: %v", result.Error)
		return ErrFailedToUpdateCounterparty
	}
	return nil
}

// --- CounterpartyAddress ---

func (r *counterpartyRepository) AddAddress(ctx context.Context, addr *models.CounterpartyAddress) error {
	result := r.db.WithContext(ctx).Create(addr)
	if result.Error != nil {
		log.Printf("AddAddress: database error: %v", result.Error)
		return ErrFailedToAddCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) GetAddressByID(ctx context.Context, id string) (*models.CounterpartyAddress, error) {
	var addr models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Preload("Counterparty").
		Preload("LatestScreening").
		Where("id = ?", id).
		First(&addr)
	if errors.Is(result.Error, gorm.ErrRecordNotFound) {
		return nil, ErrCounterpartyAddressNotFound
	}
	if result.Error != nil {
		log.Printf("GetAddressByID: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddress
	}
	return &addr, nil
}

func (r *counterpartyRepository) ListAddressesByCounterparty(ctx context.Context, counterpartyID string) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("counterparty_id = ?", counterpartyID).
		Order("created_at DESC").
		Find(&addrs)
	if result.Error != nil {
		log.Printf("ListAddressesByCounterparty: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

// ListAddressesByStatus is the screening queue's source: everything in
// review, plus pending/expired, per the source design doc §14.
func (r *counterpartyRepository) ListAddressesByStatus(ctx context.Context, statuses []string, limit, offset int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Preload("Counterparty").
		Preload("LatestScreening").
		Where("status IN ?", statuses).
		Order("created_at ASC").
		Limit(limit).
		Offset(offset).
		Find(&addrs)
	if result.Error != nil {
		log.Printf("ListAddressesByStatus: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) CountAddressesByStatus(ctx context.Context, statuses []string) (int64, error) {
	var count int64
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("status IN ?", statuses).
		Count(&count)
	if result.Error != nil {
		log.Printf("CountAddressesByStatus: database error: %v", result.Error)
		return 0, ErrFailedToCountCounterpartyAddresses
	}
	return count, nil
}

// ApproveAddress is the human override path from the screening queue —
// distinct from RecordScreening's automated verdict application.
func (r *counterpartyRepository) ApproveAddress(ctx context.Context, id, actor, reason string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":          models.AddressStatusApproved,
			"onchain_state":   models.OnchainStatePending,
			"approved_by":     actor,
			"approved_at":     now,
			"override_reason": reason,
			"updated_at":      now,
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyAddressNotFound
	}
	if result.Error != nil {
		log.Printf("ApproveAddress: database error: %v", result.Error)
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) RejectAddress(ctx context.Context, id, actor, reason string) error {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"status":          models.AddressStatusRejected,
			"override_reason": reason,
			"updated_at":      time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyAddressNotFound
	}
	if result.Error != nil {
		log.Printf("RejectAddress: database error: %v", result.Error)
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

// RevokeAddress records revocation *intent* only — it does not set
// onchain_state to revoked, because that would claim a fact ("the chain
// has confirmed this") the database cannot know yet. onchain_state stays
// whatever it currently observes (typically "approved") until the on-chain
// writer worker actually submits disallow_depositor and confirms it, at
// which point it advances to revoked. See the source design doc §12 on why
// intent and observed state are recorded separately.
func (r *counterpartyRepository) RevokeAddress(ctx context.Context, id, actor, reason string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"revoked_by":      actor,
			"revoked_at":      now,
			"override_reason": reason,
			"updated_at":      now,
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyAddressNotFound
	}
	if result.Error != nil {
		log.Printf("RevokeAddress: database error: %v", result.Error)
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) ListAddressesNeedingOnchainAllow(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("status = ? AND onchain_state = ?", models.AddressStatusApproved, models.OnchainStatePending).
		Order("created_at ASC").
		Limit(limit).
		Find(&addrs)
	if result.Error != nil {
		log.Printf("ListAddressesNeedingOnchainAllow: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) ListAddressesNeedingOnchainRevoke(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("revoked_at IS NOT NULL AND onchain_state != ?", models.OnchainStateRevoked).
		Order("revoked_at ASC").
		Limit(limit).
		Find(&addrs)
	if result.Error != nil {
		log.Printf("ListAddressesNeedingOnchainRevoke: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) SetOnchainState(ctx context.Context, id string, state models.CounterpartyAddressOnchainState) error {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"onchain_state": state,
			"updated_at":    time.Now(),
		})
	if result.RowsAffected == 0 {
		return ErrCounterpartyAddressNotFound
	}
	if result.Error != nil {
		log.Printf("SetOnchainState: database error: %v", result.Error)
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) MarkExpiredAddresses(ctx context.Context) (int64, error) {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("status = ? AND expires_at IS NOT NULL AND expires_at < ?", models.AddressStatusApproved, time.Now()).
		Updates(map[string]interface{}{
			"status":     models.AddressStatusExpired,
			"updated_at": time.Now(),
		})
	if result.Error != nil {
		log.Printf("MarkExpiredAddresses: database error: %v", result.Error)
		return 0, ErrFailedToUpdateCounterpartyAddress
	}
	return result.RowsAffected, nil
}

func (r *counterpartyRepository) ListScreeningsByAddress(ctx context.Context, addressID string) ([]*models.AddressScreening, error) {
	var screenings []*models.AddressScreening
	result := r.db.WithContext(ctx).
		Where("address_id = ?", addressID).
		Order("created_at DESC").
		Find(&screenings)
	if result.Error != nil {
		log.Printf("ListScreeningsByAddress: database error: %v", result.Error)
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return screenings, nil
}

func (r *counterpartyRepository) RecordScreening(ctx context.Context, screening *models.AddressScreening, newAddressStatus *models.CounterpartyAddressStatus, newOnchainState *models.CounterpartyAddressOnchainState, expiresAt *time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(screening).Error; err != nil {
			log.Printf("RecordScreening: could not insert screening: %v", err)
			return ErrFailedToRecordScreening
		}

		updates := map[string]interface{}{
			"last_screening_id": screening.ID,
			"screened_at":       time.Now(),
			"expires_at":        expiresAt,
			"updated_at":        time.Now(),
		}
		if newAddressStatus != nil {
			updates["status"] = *newAddressStatus
		}
		if newOnchainState != nil {
			updates["onchain_state"] = *newOnchainState
		}
		result := tx.Model(&models.CounterpartyAddress{}).
			Where("id = ?", screening.AddressID).
			Updates(updates)
		if result.Error != nil {
			log.Printf("RecordScreening: could not update address: %v", result.Error)
			return ErrFailedToRecordScreening
		}
		if result.RowsAffected == 0 {
			return ErrCounterpartyAddressNotFound
		}
		return nil
	})
}
