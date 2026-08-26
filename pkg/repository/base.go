package repository

import (
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

type Repositories struct {
	// Core repositories
	User        UserRepository
	Account     AccountRepository
	Transaction TransactionRepository
}

func NewRepositories(db *gorm.DB) (*Repositories, error) {
	if db == nil {
		return nil, pkgErrors.ErrNilDB
	}

	user, err := NewUserRepository(db)
	if err != nil {
		return nil, err
	}

	account, err := NewAccountRepository(db)
	if err != nil {
		return nil, err
	}

	transaction, err := NewTransactionRepository(db)
	if err != nil {
		return nil, err
	}

	return &Repositories{
		// Core repositories
		Transaction: transaction,
		User:        user,
		Account:     account,
	}, nil
}
