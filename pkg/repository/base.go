package repository

import (
	"gorm.io/gorm"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

type Repositories struct {
	// Core repositories
	User            UserRepository
	Account         AccountRepository
	Transaction     TransactionRepository
	Mpesa           MpesaTransactionRepository
	MpesaValidation MpesaNumberValidationRepository
	MpesaPullCursor MpesaPullCursorRepository
	MpesaBalance    MpesaBalanceRepository
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

	mpesa, err := NewMpesaTransactionRepository(db)
	if err != nil {
		return nil, err
	}

	mpesaValidation, err := NewMpesaNumberValidationRepository(db)
	if err != nil {
		return nil, err
	}

	mpesaPullCursor, err := NewMpesaPullCursorRepository(db)
	if err != nil {
		return nil, err
	}

	mpesaBalance, err := NewMpesaBalanceRepository(db)
	if err != nil {
		return nil, err
	}

	return &Repositories{
		// Core repositories
		Transaction:     transaction,
		User:            user,
		Account:         account,
		Mpesa:           mpesa,
		MpesaValidation: mpesaValidation,
		MpesaPullCursor: mpesaPullCursor,
		MpesaBalance:    mpesaBalance,
	}, nil
}
