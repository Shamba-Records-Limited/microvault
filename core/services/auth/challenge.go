package auth

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"time"

	"github.com/Shamba-Records-Limited/Microvault/pkg/config"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"
)

// ChallengeService defines the interface for Stellar transaction-based challenge-response authentication.
// This implements a cryptographic proof-of-ownership flow where users must sign a server-generated
// Stellar transaction with their private key to prove they control a specific public key.
type ChallengeService interface {
	// GenerateChallenge creates a new authentication challenge containing a Stellar transaction
	// that must be signed by the user. The challenge expires after a configured duration.
	GenerateChallenge() (*Challenge, error)

	// VerifySignedChallenge validates that the provided transaction was signed by the correct
	// keypair and matches the original challenge. Challenges are single-use and deleted after verification.
	VerifySignedChallenge(challengeID, signedTxB64 string) error
}

type challengeService struct {
	authConfig    *config.AuthConfig
	stellarConfig *config.StellarConfig
	store         ChallengeStore
	serverKP      *keypair.Full
}

// NewChallengeService creates a new ChallengeService with the provided configuration.
// The serverSecret is the Stellar private key used to sign challenges, proving they originated
// from the server. Returns an error if the serverSecret is not a valid Stellar private key.
func NewChallengeService(authConfig *config.AuthConfig, stellarConfig *config.StellarConfig, store ChallengeStore) (ChallengeService, error) {
	serverKP, err := keypair.ParseFull(stellarConfig.AdminSecretKey)
	if err != nil {
		return nil, fmt.Errorf("invalid server secret: %w", err)
	}

	return &challengeService{
		authConfig:    authConfig,
		stellarConfig: stellarConfig,
		store:         store,
		serverKP:      serverKP,
	}, nil
}

// GenerateChallenge creates a new Stellar transaction challenge for authentication.
// The generated transaction contains a random nonce and must be signed by the admin's
// private key to prove ownership. The transaction is never submitted to the network.
//
// Returns a Challenge containing the transaction envelope and metadata, or an error
// if challenge generation or storage fails.
func (s *challengeService) GenerateChallenge() (*Challenge, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	nonceB64 := base64.StdEncoding.EncodeToString(nonce)

	challengeID := make([]byte, 16)
	if _, err := rand.Read(challengeID); err != nil {
		return nil, fmt.Errorf("failed to generate challenge ID: %w", err)
	}
	challengeIDStr := base64.URLEncoding.EncodeToString(challengeID)

	now := time.Now()

	// Stellar MemoText has a max length of 28 bytes, truncate if needed
	memoText := challengeIDStr
	if len(memoText) > 28 {
		memoText = memoText[:28]
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount: &txnbuild.SimpleAccount{
				AccountID: s.stellarConfig.AdminPublicKey,
				Sequence:  0,
			},
			IncrementSequenceNum: false,
			Operations: []txnbuild.Operation{
				&txnbuild.ManageData{
					SourceAccount: s.stellarConfig.AdminPublicKey,
					Name:          "auth",
					Value:         []byte(nonceB64),
				},
			},
			BaseFee: txnbuild.MinBaseFee,
			Memo:    txnbuild.MemoText(memoText),
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewTimebounds(
					now.Unix(),
					now.Add(s.authConfig.ChallengeExpiration).Unix(),
				),
			},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("failed to build challenge transaction: %w", err)
	}

	tx, err = tx.Sign(s.stellarConfig.NetworkPassphrase, s.serverKP)
	if err != nil {
		return nil, fmt.Errorf("failed to sign challenge: %w", err)
	}

	txeB64, err := tx.Base64()
	if err != nil {
		return nil, fmt.Errorf("failed to serialize challenge: %w", err)
	}

	challenge := &Challenge{
		ID:          challengeIDStr,
		Transaction: txeB64,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.authConfig.ChallengeExpiration),
	}

	if err := s.store.Store(challengeIDStr, challenge); err != nil {
		return nil, fmt.Errorf("failed to store challenge: %w", err)
	}

	return challenge, nil
}

// VerifySignedChallenge validates a user-signed challenge transaction.
// This method ensures that:
//  1. The challenge exists and hasn't expired
//  2. The transaction matches the original challenge
//  3. The transaction is signed by the admin's private key
//
// The challenge is deleted after verification to prevent reuse (replay attacks).
// Returns an error if any validation step fails.
func (s *challengeService) VerifySignedChallenge(challengeID, signedTxB64 string) error {
	challenge, err := s.store.Get(challengeID)
	if err != nil {
		return ErrChallengeNotFound
	}

	if time.Now().After(challenge.ExpiresAt) {
		_ = s.store.Delete(challengeID) // Ignore error on cleanup
		return ErrChallengeExpired
	}

	var envelope xdr.TransactionEnvelope
	err = xdr.SafeUnmarshalBase64(signedTxB64, &envelope)
	if err != nil {
		return ErrInvalidTransaction
	}

	genericTx, err := txnbuild.TransactionFromXDR(signedTxB64)
	if err != nil {
		return ErrInvalidTransaction
	}

	tx, ok := genericTx.Transaction()
	if !ok {
		return ErrInvalidTransaction
	}

	// Verify the transaction matches our original challenge
	originalTxB64 := challenge.Transaction
	var originalEnvelope xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshalBase64(originalTxB64, &originalEnvelope); err != nil {
		return fmt.Errorf("failed to parse original challenge: %w", err)
	}

	// Compare transaction hashes (excluding signatures)
	signedHash, err := tx.Hash(s.stellarConfig.NetworkPassphrase)
	if err != nil {
		return fmt.Errorf("failed to hash signed tx: %w", err)
	}

	originalTx, _ := txnbuild.TransactionFromXDR(originalTxB64)
	originalTransaction, _ := originalTx.Transaction()
	originalHash, err := originalTransaction.Hash(s.stellarConfig.NetworkPassphrase)
	if err != nil {
		return fmt.Errorf("failed to hash original tx: %w", err)
	}

	if signedHash != originalHash {
		return ErrTransactionMismatch
	}

	// Verify the admin's signature is present
	adminKP, err := keypair.ParseAddress(s.stellarConfig.AdminPublicKey)
	if err != nil {
		return fmt.Errorf("invalid admin public key: %w", err)
	}

	signatures := envelope.Signatures()
	adminSigned := false

	for _, sig := range signatures {
		err := adminKP.Verify(signedHash[:], sig.Signature)
		if err == nil {
			adminSigned = true
			break
		}
	}

	if !adminSigned {
		return ErrInvalidSignature
	}

	// Delete challenge to prevent reuse ignore error on cleanup
	_ = s.store.Delete(challengeID)

	return nil
}
