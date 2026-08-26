package soroban

import (
	"context"
	"log"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
	"github.com/Shamba-Records-Limited/microvault/pkg/stellar/types"
)

// errDomain is the oops domain for every treasury-signed vault call. Errors
// leaving this file carry it, so an on-call engineer can filter to vault
// operations without matching on message text.
const errDomain = pkgErrors.DomainStellarVault

// treasuryErr starts an error builder scoped to one contract invocation.
// Attributes go here rather than into the message, so APM tools group on the
// message and filter on the attributes.
func treasuryErr(fnName string) oops.OopsErrorBuilder {
	return oops.
		In(errDomain).
		Tags("soroban", "contract").
		With(pkgErrors.AttrContractFunction, fnName)
}

// ============================================================================
// Treasury Operations (Mutative)
// ============================================================================

// BorrowFromVault allows treasury to borrow funds and send to a recipient
func (s *service) BorrowFromVault(ctx context.Context, req types.BorrowRequest) (*types.BorrowResponse, error) {
	const fnName = "borrow"

	errb := treasuryErr(fnName).
		With(pkgErrors.AttrAmountStroops, req.Amount).
		With(pkgErrors.AttrRecipient, req.RecipientAddress)

	if req.Amount <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(types.ErrInvalidTransactionAmount, "borrow amount must be positive")
	}

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	treasuryAddr, _ := addressToScVal(treasuryKP.Address())
	recipientAddr, err := addressToScVal(req.RecipientAddress)
	if err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidAddress).Wrapf(err, "recipient address is not a valid Stellar address")
	}
	amountVal := i128ToScVal(req.Amount)

	args := []xdr.ScVal{treasuryAddr, recipientAddr, amountVal}

	txResp, err := s.invokeSigned(ctx, treasuryKP, fnName, args, errb)
	if err != nil {
		return nil, err
	}

	// Extract the Borrowed event's recipient field from the transaction result metadata.
	// This serves as the on-chain memo linking the borrow to the child account.
	var eventRecipient string
	if txResp.ResultMetaXDR != "" {
		eventRecipient, _ = extractBorrowedRecipient(txResp.ResultMetaXDR, s.contractID)
	}

	// Fallback protection: if event_recipient is not found or empty, fall back to the recipient address from request
	if eventRecipient == "" {
		log.Printf("BorrowFromVault: event recipient not found in on-chain events, using request fallback: %s", req.RecipientAddress)
		eventRecipient = req.RecipientAddress
	} else {
		log.Printf("BorrowFromVault: event recipient successfully extracted from on-chain event: %s", eventRecipient)
	}

	// Fetch current borrow index after the borrow has been processed.
	borrowIndex, err := s.GetBorrowIndex(ctx)
	if err != nil {
		log.Printf("BorrowFromVault: failed to fetch borrow index: %v", err)
		// Non-fatal: borrow succeeded, index is supplementary data.
	}

	log.Printf("BorrowFromVault: %d to %s (tx: %s, event_recipient: %s, borrow_index: %d)",
		req.Amount, req.RecipientAddress, txResp.TransactionHash, eventRecipient, borrowIndex)

	contractID, functionName := s.contractInfoOrFallback(txResp.EnvelopeXDR, fnName)

	return &types.BorrowResponse{
		TxHash:           txResp.TransactionHash,
		AmountBorrowed:   req.Amount,
		RecipientAddress: req.RecipientAddress,
		EventRecipient:   eventRecipient,
		BorrowIndex:      borrowIndex,
		Ledger:           int64(txResp.Ledger),
		Status:           txResp.Status,
		ContractID:       contractID,
		ContractFunction: functionName,
	}, nil
}

// RepayToVault allows treasury to repay borrowed funds
func (s *service) RepayToVault(ctx context.Context, req types.RepayRequest) (*types.RepayResponse, error) {
	return s.repay(ctx, "", req.Amount)
}

// RepayForVault repays borrowed funds on behalf of a named borrower. Behaves
// exactly as RepayToVault, except the borrower is carried onto the on-chain
// Repaid event, giving the repayment the same attribution the Borrowed event
// gives disbursement. The treasury remains the payer; the borrower authorizes
// nothing.
func (s *service) RepayForVault(ctx context.Context, req types.RepayForRequest) (*types.RepayResponse, error) {
	if req.BorrowerAddress == "" {
		return nil, treasuryErr("repay_for").
			Code(pkgErrors.CodeInvalidAddress).
			Wrapf(types.ErrInvalidStellarAddress, "borrower address is required for an attributed repay")
	}
	return s.repay(ctx, req.BorrowerAddress, req.Amount)
}

// repay is the shared body of RepayToVault and RepayForVault. An empty borrower
// invokes the contract's unattributed "repay"; a non-empty one invokes
// "repay_for".
func (s *service) repay(ctx context.Context, borrowerAddress string, amount int64) (*types.RepayResponse, error) {
	fnName := "repay"
	if borrowerAddress != "" {
		fnName = "repay_for"
	}

	errb := treasuryErr(fnName).
		With(pkgErrors.AttrAmountStroops, amount).
		With(pkgErrors.AttrBorrower, borrowerAddress)

	if amount <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(types.ErrInvalidTransactionAmount, "repay amount must be positive")
	}

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	treasuryAddr, _ := addressToScVal(treasuryKP.Address())
	amountVal := i128ToScVal(amount)

	args := []xdr.ScVal{treasuryAddr, amountVal}
	if borrowerAddress != "" {
		borrowerAddr, err := addressToScVal(borrowerAddress)
		if err != nil {
			return nil, errb.Code(pkgErrors.CodeInvalidAddress).Wrapf(err, "borrower address is not a valid Stellar address")
		}
		args = []xdr.ScVal{treasuryAddr, borrowerAddr, amountVal}
	}

	txResp, err := s.invokeSigned(ctx, treasuryKP, fnName, args, errb)
	if err != nil {
		return nil, err
	}

	// Extract the Repaid event's borrower field from the transaction result
	// metadata. This is the on-chain record attributing the repayment.
	var eventBorrower string
	if borrowerAddress != "" {
		if txResp.ResultMetaXDR != "" {
			eventBorrower, _ = extractRepaidBorrower(txResp.ResultMetaXDR, s.contractID)
		}

		// Fallback protection: if the event borrower is missing or empty, fall
		// back to the borrower address from the request.
		if eventBorrower == "" {
			log.Printf("RepayForVault: event borrower not found in on-chain events, using request fallback: %s", borrowerAddress)
			eventBorrower = borrowerAddress
		}
	}

	log.Printf("RepayToVault: %d repaid (tx: %s, borrower: %q, event_borrower: %q)",
		amount, txResp.TransactionHash, borrowerAddress, eventBorrower)

	contractID, functionName := s.contractInfoOrFallback(txResp.EnvelopeXDR, fnName)

	return &types.RepayResponse{
		TxHash:           txResp.TransactionHash,
		AmountRepaid:     amount,
		BorrowerAddress:  borrowerAddress,
		EventBorrower:    eventBorrower,
		Ledger:           int64(txResp.Ledger),
		Status:           txResp.Status,
		ContractID:       contractID,
		ContractFunction: functionName,
	}, nil
}

// BumpYield contributes treasury-held assets to the vault without minting
// shares, raising the value of every existing share. Used to return interest
// earned on loans the vault stopped counting as borrowed at disbursement.
func (s *service) BumpYield(ctx context.Context, req types.BumpYieldRequest) (*types.BumpYieldResponse, error) {
	const fnName = "bump_yield"

	errb := treasuryErr(fnName).With(pkgErrors.AttrAmountStroops, req.Amount)

	if req.Amount <= 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidAmount).Wrapf(types.ErrInvalidTransactionAmount, "contribution amount must be positive")
	}

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	// The treasury is both the authorizer and the source of the funds, so
	// "from" is the treasury address.
	treasuryAddr, _ := addressToScVal(treasuryKP.Address())
	amountVal := i128ToScVal(req.Amount)

	args := []xdr.ScVal{treasuryAddr, amountVal}

	txResp, err := s.invokeSigned(ctx, treasuryKP, fnName, args, errb)
	if err != nil {
		return nil, err
	}

	// Read the post-contribution managed assets off the YieldBumped event.
	var totalManaged int64
	if txResp.ResultMetaXDR != "" {
		totalManaged, err = extractYieldBumpedTotalManaged(txResp.ResultMetaXDR, s.contractID)
		if err != nil {
			log.Printf("BumpYield: failed to read total_managed from YieldBumped event: %v", err)
			// Non-fatal: the contribution succeeded, this figure is supplementary.
		}
	}

	log.Printf("BumpYield: %d contributed (tx: %s, total_managed: %d)",
		req.Amount, txResp.TransactionHash, totalManaged)

	contractID, functionName := s.contractInfoOrFallback(txResp.EnvelopeXDR, fnName)

	return &types.BumpYieldResponse{
		TxHash:             txResp.TransactionHash,
		AmountContributed:  req.Amount,
		TotalManagedAssets: totalManaged,
		Ledger:             int64(txResp.Ledger),
		Status:             txResp.Status,
		ContractID:         contractID,
		ContractFunction:   functionName,
	}, nil
}

// AccrueInterest triggers interest accrual on the vault
func (s *service) AccrueInterest(ctx context.Context) error {
	const fnName = "accrue"

	treasuryKP := keypair.MustParseFull(s.treasuryPrivateKey)

	txResp, err := s.invokeSigned(ctx, treasuryKP, fnName, nil, treasuryErr(fnName))
	if err != nil {
		return err
	}

	log.Printf("AccrueInterest: interest accrued (tx: %s)", txResp.TransactionHash)
	return nil
}

// contractInfoOrFallback reads the contract ID and function name off the
// submitted envelope, falling back to what we asked for when the envelope
// cannot be decoded. The call already succeeded at this point, so a decode
// failure must not fail it.
func (s *service) contractInfoOrFallback(envelopeXDR, fnName string) (string, string) {
	contractID, functionName, err := ExtractContractInfo(envelopeXDR)
	if err != nil {
		log.Printf("%s: failed to extract contract info: %v", fnName, err)
		return s.contractID, fnName
	}
	return contractID, functionName
}
