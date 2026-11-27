package chipper

import (
	"encoding/json"
	"strings"

	"github.com/govalues/decimal"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// FeeRule estimates a fee in the asset being moved: Pct percent of the amount
// plus Flat, clamped to Min and Max when set.
type FeeRule struct {
	Pct  decimal.Decimal  `json:"pct"`
	Flat decimal.Decimal  `json:"flat"`
	Min  *decimal.Decimal `json:"min,omitempty"`
	Max  *decimal.Decimal `json:"max,omitempty"`
}

// FeeSchedule estimates Chipper's fees, which are set per organization and
// published nowhere but the dashboard. It prices routing only; recorded fees
// always come from the payout.
type FeeSchedule struct {
	Deposit FeeRule            `json:"deposit"`
	Payouts map[string]FeeRule `json:"payouts"`
}

// ParseFeeSchedule decodes and validates a schedule such as
// {"deposit":{"pct":"1.5"},"payouts":{"ke_mpesa":{"pct":"1.5"}}}.
func ParseFeeSchedule(raw string) (*FeeSchedule, error) {
	errb := chipperErr("parse_fee_schedule")
	var s FeeSchedule
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Wrapf(err, "fee schedule is not valid json")
	}
	if len(s.Payouts) == 0 {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).Errorf("fee schedule names no payout methods")
	}
	if err := s.Deposit.validate(); err != nil {
		return nil, errb.Code(pkgErrors.CodeInvalidConfig).With("rule", "deposit").Wrapf(err, "invalid fee rule")
	}
	normalized := make(map[string]FeeRule, len(s.Payouts))
	for code, rule := range s.Payouts {
		if err := rule.validate(); err != nil {
			return nil, errb.Code(pkgErrors.CodeInvalidConfig).With("rule", code).Wrapf(err, "invalid fee rule")
		}
		normalized[strings.ToLower(code)] = rule
	}
	s.Payouts = normalized
	return &s, nil
}

// PayoutRule returns the rule for a payout method.
func (s FeeSchedule) PayoutRule(code string) (FeeRule, bool) {
	rule, ok := s.Payouts[strings.ToLower(code)]
	return rule, ok
}

// Fee applies the rule to amount.
func (r FeeRule) Fee(amount decimal.Decimal) (decimal.Decimal, error) {
	pct, err := amount.Mul(r.Pct)
	if err != nil {
		return decimal.Zero, err
	}
	pct, err = pct.Quo(decimal.Hundred)
	if err != nil {
		return decimal.Zero, err
	}
	fee, err := pct.Add(r.Flat)
	if err != nil {
		return decimal.Zero, err
	}
	if r.Min != nil && fee.Cmp(*r.Min) < 0 {
		fee = *r.Min
	}
	if r.Max != nil && fee.Cmp(*r.Max) > 0 {
		fee = *r.Max
	}
	return fee, nil
}

func (r FeeRule) validate() error {
	errb := chipperErr("validate_fee_rule")
	switch {
	case r.Pct.IsNeg() || r.Flat.IsNeg():
		return errb.Errorf("fee rule cannot be negative")
	case r.Pct.Cmp(decimal.Hundred) >= 0:
		return errb.Errorf("fee percentage must be below 100")
	case r.Min != nil && r.Min.IsNeg(), r.Max != nil && r.Max.IsNeg():
		return errb.Errorf("fee bounds cannot be negative")
	case r.Min != nil && r.Max != nil && r.Min.Cmp(*r.Max) > 0:
		return errb.Errorf("fee minimum exceeds the maximum")
	}
	return nil
}
