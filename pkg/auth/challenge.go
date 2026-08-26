package auth

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"time"

	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/txnbuild"
	"github.com/stellar/go-stellar-sdk/xdr"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"

	"github.com/samber/oops"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// challengeErr starts an error builder for SEP-10 challenge issuance and
// verification.
func challengeErr(op string) oops.OopsErrorBuilder {
	return oops.In(pkgErrors.DomainIdentity).Tags("auth", "challenge").With(pkgErrors.AttrOperation, op)
}

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
// Challenges are signed with StellarConfig.ServerSecretKey, which must differ from the
// admin key it authenticates. Returns an error if that key is not a valid Stellar private key.
func NewChallengeService(authConfig *config.AuthConfig, stellarConfig *config.StellarConfig, store ChallengeStore) (ChallengeService, error) {
	serverKP, err := keypair.ParseFull(stellarConfig.ServerSecretKey)
	if err != nil {
		return nil, challengeErr("new").Code(pkgErrors.CodeInvalidAddress).Wrapf(err, "server secret is not a valid Stellar key")
	}

	if serverKP.Address() == stellarConfig.AdminPublicKey {
		return nil, challengeErr("new").Code(pkgErrors.CodePermissionDenied).
			Errorf("server signing key must differ from the admin key")
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
func (s *challengeService) GenerateChallenge() (*Challenge, error) {
	nonce := make([]byte, 32)
	if _, err := rand.Read(nonce); err != nil {
		return nil, challengeErr("generate").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not generate a nonce")
	}
	nonceB64 := base64.StdEncoding.EncodeToString(nonce)

	challengeID := make([]byte, 16)
	if _, err := rand.Read(challengeID); err != nil {
		return nil, challengeErr("generate").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not generate a challenge id")
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
				AccountID: s.serverKP.Address(),
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
		return nil, challengeErr("generate").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not build the challenge transaction")
	}

	tx, err = tx.Sign(s.stellarConfig.NetworkPassphrase, s.serverKP)
	if err != nil {
		return nil, challengeErr("generate").Code(pkgErrors.CodeBuildFailed).Wrapf(err, "could not sign the challenge")
	}

	txeB64, err := tx.Base64()
	if err != nil {
		return nil, challengeErr("generate").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not serialise the challenge")
	}

	challenge := &Challenge{
		ID:          challengeIDStr,
		Transaction: txeB64,
		CreatedAt:   now,
		ExpiresAt:   now.Add(s.authConfig.ChallengeExpiration),
	}

	if err := s.store.Store(challengeIDStr, challenge); err != nil {
		return nil, challengeErr("generate").Code(pkgErrors.CodeStateWriteFailed).Wrapf(err, "could not store the challenge")
	}

	return challenge, nil
}

// VerifySignedChallenge validates a user-signed challenge transaction.
// This method ensures that:
//  1. The challenge exists and hasn't expired
//  2. The transaction matches the original challenge
//  3. The transaction is signed by the admin's private key
func (s *challengeService) VerifySignedChallenge(challengeID, signedTxB64 string) error {
	challenge, err := s.store.Get(challengeID)
	if err != nil {
		// Only a genuinely absent challenge is a client error. A store that
		// cannot be read is ours, and collapsing the two reported every Redis
		// outage to the caller as a 404.
		if errors.Is(err, ErrChallengeNotFound) {
			return ErrChallengeNotFound
		}
		return challengeErr("verify").With("challenge_id", challengeID).
			Code(pkgErrors.CodeTransportFailed).Wrapf(err, "could not read the challenge")
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
		return challengeErr("verify").Code(pkgErrors.CodeDecodeFailed).Wrapf(ErrInvalidTransaction, "could not parse the original challenge")
	}

	// Compare transaction hashes (excluding signatures)
	signedHash, err := tx.Hash(s.stellarConfig.NetworkPassphrase)
	if err != nil {
		return challengeErr("verify").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash the signed transaction")
	}

	originalTx, _ := txnbuild.TransactionFromXDR(originalTxB64)
	originalTransaction, _ := originalTx.Transaction()
	originalHash, err := originalTransaction.Hash(s.stellarConfig.NetworkPassphrase)
	if err != nil {
		return challengeErr("verify").Code(pkgErrors.CodeEncodeFailed).Wrapf(err, "could not hash the original transaction")
	}

	if signedHash != originalHash {
		return ErrTransactionMismatch
	}

	// Verify the admin's signature is present, ignoring the server's own countersignature.
	adminKP, err := keypair.ParseAddress(s.stellarConfig.AdminPublicKey)
	if err != nil {
		return challengeErr("verify").Code(pkgErrors.CodeInvalidAddress).Wrapf(err, "admin public key is not a valid Stellar key")
	}

	signatures := envelope.Signatures()
	adminSigned := false

	for _, sig := range signatures {
		if s.serverKP.Verify(signedHash[:], sig.Signature) == nil {
			continue
		}
		if adminKP.Verify(signedHash[:], sig.Signature) == nil {
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
