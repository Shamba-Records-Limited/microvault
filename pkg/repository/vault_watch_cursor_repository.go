package repository

import (
	"context"

	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// VaultWatchCursorRepository tracks how far the compliance watcher has
// scanned the vault's on-chain events. One row (id=1); Get and Advance both
// operate on it.
type VaultWatchCursorRepository interface {
	// Get returns the last ledger sequence the watcher has scanned through.
	Get(ctx context.Context) (uint32, error)
	// Advance moves the cursor forward to ledger.
	Advance(ctx context.Context, ledger uint32) error
}

type vaultWatchCursorRepository struct {
	db *gorm.DB
}

// NewVaultWatchCursorRepository builds the repository.
func NewVaultWatchCursorRepository(db *gorm.DB) (VaultWatchCursorRepository, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}
	return &vaultWatchCursorRepository{db: db}, nil
}

func (r *vaultWatchCursorRepository) Get(ctx context.Context) (uint32, error) {
	var lastLedger uint32
	result := r.db.WithContext(ctx).
		Raw("SELECT last_ledger FROM vault_watch_cursor WHERE id = 1").
		Scan(&lastLedger)
	if result.Error != nil {
		return 0, result.Error
	}
	return lastLedger, nil
}

func (r *vaultWatchCursorRepository) Advance(ctx context.Context, ledger uint32) error {
	return r.db.WithContext(ctx).
		Exec("UPDATE vault_watch_cursor SET last_ledger = ? WHERE id = 1", ledger).
		Error
}
