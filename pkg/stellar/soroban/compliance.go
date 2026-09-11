package soroban

import (
	"context"
	"log"

	"github.com/samber/oops"
	"github.com/stellar/go-stellar-sdk/keypair"
	"github.com/stellar/go-stellar-sdk/xdr"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// complianceErr mirrors adminErr's shape (admin.go) but uses
// DomainCompliance so an on-call engineer can filter these separately from
// ordinary vault-admin failures — the same reasoning pkg/errors.vocabulary.go
// already gives for keeping compliance out of the payment domains.
func complianceErr(fnName string) oops.OopsErrorBuilder {
	return oops.
		In(pkgErrors.DomainCompliance).
		Tags("soroban", "compliance").
		With(pkgErrors.AttrContractFunction, fnName)
}

// requireComplianceRole returns a parsed keypair for the compliance role's
// signing key, or an error if the Service was never given one via
// WithComplianceRole — a Service used only for treasury/admin operations
// never needs this key, so failing loudly here (rather than silently
// falling back to the admin key, which the vault contract would reject
// anyway since allow_depositor checks the caller against the stored
// compliance_role, not the owner) is the correct default.
func (s *service) requireComplianceRole(fnName string) (*keypair.Full, error) {
	if s.complianceRolePrivateKey == "" {
		return nil, complianceErr(fnName).Code(pkgErrors.CodeMissingAccount).
			Errorf("no compliance role key configured — call WithComplianceRole first")
	}
	return keypair.MustParseFull(s.complianceRolePrivateKey), nil
}

// AllowDepositor calls the vault's allow_depositor, signed by the
// compliance role. See the vault contract's own doc comment: this is the
// fast path (no timelock), matching the guardian's pause() precedent —
// see elliptic-compliance-integration.md §17 Q4 on why revocation (and,
// symmetrically, approval) cannot wait on a multi-day delay.
func (s *service) AllowDepositor(ctx context.Context, address string) error {
	const fnName = "allow_depositor"

	roleKP, err := s.requireComplianceRole(fnName)
	if err != nil {
		return err
	}
	callerAddr, _ := addressToScVal(roleKP.Address())
	depositorAddr, err := addressToScVal(address)
	if err != nil {
		return complianceErr(fnName).Code(pkgErrors.CodeInvalidAddress).
			With(pkgErrors.AttrAddress, address).
			Wrapf(err, "invalid depositor address")
	}

	txResp, err := s.invokeSigned(ctx, roleKP, fnName, []xdr.ScVal{callerAddr, depositorAddr}, complianceErr(fnName).With(pkgErrors.AttrAddress, address))
	if err != nil {
		return err
	}

	log.Printf("AllowDepositor: %s allowed (tx: %s)", address, txResp.TransactionHash)
	return nil
}

// DisallowDepositor calls the vault's disallow_depositor, signed by the
// compliance role. See AllowDepositor's doc comment.
func (s *service) DisallowDepositor(ctx context.Context, address string) error {
	const fnName = "disallow_depositor"

	roleKP, err := s.requireComplianceRole(fnName)
	if err != nil {
		return err
	}
	callerAddr, _ := addressToScVal(roleKP.Address())
	depositorAddr, err := addressToScVal(address)
	if err != nil {
		return complianceErr(fnName).Code(pkgErrors.CodeInvalidAddress).
			With(pkgErrors.AttrAddress, address).
			Wrapf(err, "invalid depositor address")
	}

	txResp, err := s.invokeSigned(ctx, roleKP, fnName, []xdr.ScVal{callerAddr, depositorAddr}, complianceErr(fnName).With(pkgErrors.AttrAddress, address))
	if err != nil {
		return err
	}

	log.Printf("DisallowDepositor: %s disallowed (tx: %s)", address, txResp.TransactionHash)
	return nil
}
