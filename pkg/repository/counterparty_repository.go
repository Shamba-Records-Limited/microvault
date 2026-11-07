package repository

import (
	"context"
	"errors"
	"log/slog"
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
	// ErrCounterpartyKYBDecided is a KYB approve or reject refused because the
	// counterparty is no longer pending.
	ErrCounterpartyKYBDecided = errors.New("counterparty KYB is no longer pending")

	ErrCounterpartyAddressNotFound        = errors.New("counterparty address not found")
	ErrFailedToAddCounterpartyAddress     = errors.New("failed to add counterparty address")
	ErrFailedToGetCounterpartyAddress     = errors.New("failed to get counterparty address")
	ErrFailedToGetCounterpartyAddresses   = errors.New("failed to get counterparty addresses")
	ErrFailedToCountCounterpartyAddresses = errors.New("failed to count counterparty addresses")
	ErrFailedToUpdateCounterpartyAddress  = errors.New("failed to update counterparty address")
	// ErrCounterpartyAddressDecided is an approve or reject refused because
	// the address is not awaiting a decision, or is frozen on-chain.
	ErrCounterpartyAddressDecided = errors.New("counterparty address is not awaiting a decision")

	ErrFailedToRecordScreening = errors.New("failed to record screening")
)

// CounterpartyRepository spans all three compliance tables
// (counterparties, counterparty_addresses, address_screenings) — one
// bounded context, mirroring how UserRepository spans everything
// user-related in one file. Actor parameters throughout are the admin's
// Stellar public key as text (see counterparty.go's KYBApprovedBy doc
// comment).
type CounterpartyRepository interface {
	Create(ctx context.Context, cp *models.Counterparty) error
	GetByID(ctx context.Context, id string) (*models.Counterparty, error) // preloads Addresses
	List(ctx context.Context, kybStatus string, limit, offset int) ([]*models.Counterparty, error)
	Count(ctx context.Context, kybStatus string) (int64, error)
	// ApproveKYB and RejectKYB decide a pending counterparty only; once
	// decided, ErrCounterpartyKYBDecided.
	ApproveKYB(ctx context.Context, id, actor string) error
	RejectKYB(ctx context.Context, id, actor string) error

	AddAddress(ctx context.Context, addr *models.CounterpartyAddress) error
	GetAddressByID(ctx context.Context, id string) (*models.CounterpartyAddress, error) // preloads Counterparty + LatestScreening
	ListAddressesByCounterparty(ctx context.Context, counterpartyID string) ([]*models.CounterpartyAddress, error)
	ListAddressesByStatus(ctx context.Context, statuses []string, limit, offset int) ([]*models.CounterpartyAddress, error)
	CountAddressesByStatus(ctx context.Context, statuses []string) (int64, error)
	// ApproveAddress and RejectAddress decide an address in one of
	// models.AddressDecisionStatuses that is not frozen on-chain; otherwise
	// ErrCounterpartyAddressDecided.
	ApproveAddress(ctx context.Context, id, actor, reason string) error
	RejectAddress(ctx context.Context, id, actor, reason string) error
	RevokeAddress(ctx context.Context, id, actor, reason string) error
	// FreezeAddress records a freeze intent: revoked too, and the on-chain
	// writer calls freeze_depositor rather than disallow_depositor.
	FreezeAddress(ctx context.Context, id, actor, reason string) error

	// ListScreeningsByAddress returns an address's full screening history,
	// newest first — the audit artefact the screening detail screen renders
	// from raw_payload.
	ListScreeningsByAddress(ctx context.Context, addressID string) ([]*models.AddressScreening, error)

	// RecordScreening inserts the append-only screening row and updates the
	// parent address's last_screening_id/screened_at/expires_at in one
	// transaction. expires_at changes only with the status. newAddressStatus
	// and newOnchainState are each optional —
	// nil leaves that column untouched (a screening whose verdict didn't
	// resolve to anything new, e.g. still pending, must not silently
	// downgrade an address that was already approved).
	RecordScreening(ctx context.Context, screening *models.AddressScreening, newAddressStatus *models.CounterpartyAddressStatus, newOnchainState *models.CounterpartyAddressOnchainState, expiresAt *time.Time) error

	// ListAddressesNeedingOnchainAllow returns approved addresses whose
	// onchain_state is still "pending" — the on-chain writer's queue for
	// allow_depositor. This write stays a separate worker rather than something the admin web process
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

	// ListAddressesNeedingOnchainFreeze returns addresses with a freeze
	// intent the chain has not yet confirmed.
	ListAddressesNeedingOnchainFreeze(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error)

	// SetExitDeadline mirrors the vault's exit deadline for address; nil
	// clears it. A changed deadline resets exit_warning_level. An address
	// that is not a counterparty is not an error.
	SetExitDeadline(ctx context.Context, address string, deadline *time.Time) error

	// ListExitWindowsClosing returns addresses whose deadline is still ahead
	// but no later than before, and whose warning level is below level.
	ListExitWindowsClosing(ctx context.Context, before time.Time, level, limit int) ([]*models.CounterpartyAddress, error)

	// SetExitWarningLevel records which closing alert was sent.
	SetExitWarningLevel(ctx context.Context, id string, level int) error

	// MarkExpiredAddresses flips every approved address whose expires_at
	// has passed to AddressStatusExpired, and returns how many it touched.
	// One bulk statement rather than a per-row round trip, since this runs
	// on every rescreening-sweep tick, so expired is actually reachable.
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
		slog.ErrorContext(ctx, "Create: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "GetByID: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "List: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "Count: database error", slog.Any("error", err))
		return 0, ErrFailedToCountCounterparties
	}
	return count, nil
}

func (r *counterpartyRepository) ApproveKYB(ctx context.Context, id, actor string) error {
	now := time.Now()
	return r.decideKYB(ctx, "ApproveKYB", id, map[string]interface{}{
		"kyb_status":      models.CounterpartyKYBApproved,
		"kyb_approved_at": now,
		"kyb_approved_by": actor,
		"updated_at":      now,
	})
}

func (r *counterpartyRepository) decideKYB(ctx context.Context, op, id string, fields map[string]interface{}) error {
	result := r.db.WithContext(ctx).
		Model(&models.Counterparty{}).
		Where("id = ? AND kyb_status = ?", id, models.CounterpartyKYBPending).
		Updates(fields)
	if result.Error != nil {
		slog.ErrorContext(ctx, "counterparty decision: database error", slog.String("op", op), slog.Any("error", result.Error))
		return ErrFailedToUpdateCounterparty
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(&models.Counterparty{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return ErrFailedToUpdateCounterparty
	}
	if n == 0 {
		return ErrCounterpartyNotFound
	}
	return ErrCounterpartyKYBDecided
}

func (r *counterpartyRepository) RejectKYB(ctx context.Context, id, actor string) error {
	return r.decideKYB(ctx, "RejectKYB", id, map[string]interface{}{
		"kyb_status": models.CounterpartyKYBRejected,
		"updated_at": time.Now(),
	})
}

// --- CounterpartyAddress ---

func (r *counterpartyRepository) AddAddress(ctx context.Context, addr *models.CounterpartyAddress) error {
	result := r.db.WithContext(ctx).Create(addr)
	if result.Error != nil {
		slog.ErrorContext(ctx, "AddAddress: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "GetAddressByID: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "ListAddressesByCounterparty: database error", slog.Any("error", result.Error))
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

// ListAddressesByStatus is the screening queue's source: everything in
// review, plus pending/expired.
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
		slog.ErrorContext(ctx, "ListAddressesByStatus: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "CountAddressesByStatus: database error", slog.Any("error", result.Error))
		return 0, ErrFailedToCountCounterpartyAddresses
	}
	return count, nil
}

// ApproveAddress is the human override path from the screening queue —
// distinct from RecordScreening's automated verdict application.
func (r *counterpartyRepository) ApproveAddress(ctx context.Context, id, actor, reason string) error {
	now := time.Now()
	return r.decideAddress(ctx, "ApproveAddress", id, map[string]interface{}{
		"status":          models.AddressStatusApproved,
		"onchain_state":   models.OnchainStatePending,
		"approved_by":     actor,
		"approved_at":     now,
		"revoked_by":      nil,
		"revoked_at":      nil,
		"frozen_by":       nil,
		"frozen_at":       nil,
		"override_reason": reason,
		"updated_at":      now,
	})
}

func (r *counterpartyRepository) decideAddress(ctx context.Context, op, id string, fields map[string]interface{}) error {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ? AND status IN ? AND onchain_state <> ?", id, models.AddressDecisionStatuses, models.OnchainStateFrozen).
		Updates(fields)
	if result.Error != nil {
		slog.ErrorContext(ctx, "counterparty decision: database error", slog.String("op", op), slog.Any("error", result.Error))
		return ErrFailedToUpdateCounterpartyAddress
	}
	if result.RowsAffected > 0 {
		return nil
	}
	var n int64
	if err := r.db.WithContext(ctx).Model(&models.CounterpartyAddress{}).Where("id = ?", id).Count(&n).Error; err != nil {
		return ErrFailedToUpdateCounterpartyAddress
	}
	if n == 0 {
		return ErrCounterpartyAddressNotFound
	}
	return ErrCounterpartyAddressDecided
}

func (r *counterpartyRepository) RejectAddress(ctx context.Context, id, actor, reason string) error {
	return r.decideAddress(ctx, "RejectAddress", id, map[string]interface{}{
		"status":          models.AddressStatusRejected,
		"override_reason": reason,
		"updated_at":      time.Now(),
	})
}

// RevokeAddress records revocation *intent* only — it does not set
// onchain_state to revoked, because that would claim a fact ("the chain
// has confirmed this") the database cannot know yet. onchain_state stays
// whatever it currently observes (typically "approved") until the on-chain
// writer worker actually submits disallow_depositor and confirms it, at
// which point it advances to revoked. Intent and observed state are
// recorded separately because the decision and the contract write can
// fail independently.
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
		slog.ErrorContext(ctx, "RevokeAddress: database error", slog.Any("error", result.Error))
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) FreezeAddress(ctx context.Context, id, actor, reason string) error {
	now := time.Now()
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"frozen_by":       actor,
			"frozen_at":       now,
			"revoked_by":      gorm.Expr("COALESCE(revoked_by, ?)", actor),
			"revoked_at":      gorm.Expr("COALESCE(revoked_at, ?)", now),
			"override_reason": reason,
			"updated_at":      now,
		})
	if result.Error != nil {
		slog.ErrorContext(ctx, "FreezeAddress: database error", slog.Any("error", result.Error))
		return ErrFailedToUpdateCounterpartyAddress
	}
	if result.RowsAffected == 0 {
		return ErrCounterpartyAddressNotFound
	}
	return nil
}

func (r *counterpartyRepository) ListAddressesNeedingOnchainFreeze(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("frozen_at IS NOT NULL AND onchain_state != ?", models.OnchainStateFrozen).
		Order("frozen_at ASC").
		Limit(limit).
		Find(&addrs)
	if result.Error != nil {
		slog.ErrorContext(ctx, "ListAddressesNeedingOnchainFreeze: database error", slog.Any("error", result.Error))
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) SetExitDeadline(ctx context.Context, address string, deadline *time.Time) error {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("address = ? AND exit_deadline IS DISTINCT FROM ?", address, deadline).
		Updates(map[string]interface{}{
			"exit_deadline":      deadline,
			"exit_warning_level": 0,
			"updated_at":         time.Now(),
		})
	if result.Error != nil {
		slog.ErrorContext(ctx, "SetExitDeadline: database error", slog.Any("error", result.Error))
		return ErrFailedToUpdateCounterpartyAddress
	}
	return nil
}

func (r *counterpartyRepository) ListExitWindowsClosing(ctx context.Context, before time.Time, level, limit int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("exit_deadline > ? AND exit_deadline <= ? AND exit_warning_level < ?", time.Now(), before, level).
		Order("exit_deadline ASC").
		Limit(limit).
		Find(&addrs)
	if result.Error != nil {
		slog.ErrorContext(ctx, "ListExitWindowsClosing: database error", slog.Any("error", result.Error))
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) SetExitWarningLevel(ctx context.Context, id string, level int) error {
	result := r.db.WithContext(ctx).
		Model(&models.CounterpartyAddress{}).
		Where("id = ?", id).
		Updates(map[string]interface{}{
			"exit_warning_level": level,
			"updated_at":         time.Now(),
		})
	if result.Error != nil {
		slog.ErrorContext(ctx, "SetExitWarningLevel: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "ListAddressesNeedingOnchainAllow: database error", slog.Any("error", result.Error))
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return addrs, nil
}

func (r *counterpartyRepository) ListAddressesNeedingOnchainRevoke(ctx context.Context, limit int) ([]*models.CounterpartyAddress, error) {
	var addrs []*models.CounterpartyAddress
	result := r.db.WithContext(ctx).
		Where("revoked_at IS NOT NULL AND frozen_at IS NULL AND onchain_state != ?", models.OnchainStateRevoked).
		Order("revoked_at ASC").
		Limit(limit).
		Find(&addrs)
	if result.Error != nil {
		slog.ErrorContext(ctx, "ListAddressesNeedingOnchainRevoke: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "SetOnchainState: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "MarkExpiredAddresses: database error", slog.Any("error", result.Error))
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
		slog.ErrorContext(ctx, "ListScreeningsByAddress: database error", slog.Any("error", result.Error))
		return nil, ErrFailedToGetCounterpartyAddresses
	}
	return screenings, nil
}

func (r *counterpartyRepository) RecordScreening(ctx context.Context, screening *models.AddressScreening, newAddressStatus *models.CounterpartyAddressStatus, newOnchainState *models.CounterpartyAddressOnchainState, expiresAt *time.Time) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(screening).Error; err != nil {
			slog.ErrorContext(ctx, "RecordScreening: could not insert screening", slog.Any("error", err))
			return ErrFailedToRecordScreening
		}

		updates := map[string]interface{}{
			"last_screening_id": screening.ID,
			"screened_at":       time.Now(),
			"updated_at":        time.Now(),
		}
		if newAddressStatus != nil {
			updates["status"] = *newAddressStatus
			updates["expires_at"] = expiresAt
		}
		if newOnchainState != nil {
			updates["onchain_state"] = *newOnchainState
		}
		result := tx.Model(&models.CounterpartyAddress{}).
			Where("id = ?", screening.AddressID).
			Updates(updates)
		if result.Error != nil {
			slog.ErrorContext(ctx, "RecordScreening: could not update address", slog.Any("error", result.Error))
			return ErrFailedToRecordScreening
		}
		if result.RowsAffected == 0 {
			return ErrCounterpartyAddressNotFound
		}
		return nil
	})
}
