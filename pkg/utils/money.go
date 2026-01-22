package utils

import (
	"fmt"

	"github.com/govalues/decimal"
	"github.com/govalues/money"
)

// BasisPoints represents interest rates, fees, and percentages as basis points
// 1 basis point = 0.01% = 0.0001
// Example: 15.00% = 1500 bps, 3.75% = 375 bps, 178.00% = 17800 bps
type BasisPoints int32

// ToBps converts a percentage (as float) to basis points
// Example: ToBps(15.0) = 1500 (15.00%)
func ToBps(percentage float64) BasisPoints {
	return BasisPoints(percentage * 100)
}

// ToPercent converts basis points to percentage as float
// Example: BasisPoints(1500).ToPercent() = 15.0
func (bp BasisPoints) ToPercent() float64 {
	return float64(bp) / 100.0
}

// ToDecimal converts basis points to decimal representation for multiplication
// Example: BasisPoints(1500).ToDecimal() = 0.15
func (bp BasisPoints) ToDecimal() float64 {
	return float64(bp) / 10000.0
}

// String returns a formatted percentage string
// Example: BasisPoints(1500).String() = "15.00%"
func (bp BasisPoints) String() string {
	return fmt.Sprintf("%.2f%%", bp.ToPercent())
}

// Int64ToAmount converts an amount in smallest unit (int64) to money.Amount
// Uses minor units (cents, satoshis, etc.) for the specified currency
// Example: Int64ToAmount(100050, "KES") converts 100050 cents to 1000.50 KES
func Int64ToAmount(smallestUnit int64, currency string) (money.Amount, error) {
	// Use NewAmountFromMinorUnits which handles the currency's natural subdivision
	amount, err := money.NewAmountFromMinorUnits(currency, smallestUnit)
	if err != nil {
		return money.Amount{}, fmt.Errorf("failed to create amount: %w", err)
	}
	return amount, nil
}

// AmountToInt64 converts money.Amount to int64 in smallest unit (minor units)
// Example: AmountToInt64(1000.50 KES) returns 100050 (cents)
func AmountToInt64(amount money.Amount) (int64, error) {
	// Use MinorUnits to extract the value in smallest unit
	units, ok := amount.MinorUnits()
	if !ok {
		return 0, fmt.Errorf("failed to convert amount to minor units")
	}
	return units, nil
}

// FormatAmount formats an amount in smallest unit to a readable string
// Example: FormatAmount(100050, "KES") = "KES 1000.50"
func FormatAmount(smallestUnit int64, currency string) string {
	amount, err := Int64ToAmount(smallestUnit, currency)
	if err != nil {
		return fmt.Sprintf("%d %s (smallest unit)", smallestUnit, currency)
	}
	return amount.String()
}

// ParseAmount parses a string amount to int64 in smallest unit
// Example: ParseAmount("1000.50", "KES") = 100050 (cents)
func ParseAmount(amountStr string, currency string) (int64, error) {
	amount, err := money.ParseAmount(currency, amountStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse amount: %w", err)
	}
	return AmountToInt64(amount)
}

// AddAmounts adds two amounts in smallest unit (must be same currency)
// Returns the sum in smallest unit
func AddAmounts(a, b int64, currency string) (int64, error) {
	amountA, err := Int64ToAmount(a, currency)
	if err != nil {
		return 0, err
	}

	amountB, err := Int64ToAmount(b, currency)
	if err != nil {
		return 0, err
	}

	sum, err := amountA.Add(amountB)
	if err != nil {
		return 0, fmt.Errorf("failed to add amounts: %w", err)
	}

	return AmountToInt64(sum)
}

// SubtractAmounts subtracts two amounts in smallest unit (must be same currency)
// Returns a - b in smallest unit
func SubtractAmounts(a, b int64, currency string) (int64, error) {
	amountA, err := Int64ToAmount(a, currency)
	if err != nil {
		return 0, err
	}

	amountB, err := Int64ToAmount(b, currency)
	if err != nil {
		return 0, err
	}

	diff, err := amountA.Sub(amountB)
	if err != nil {
		return 0, fmt.Errorf("failed to subtract amounts: %w", err)
	}

	return AmountToInt64(diff)
}

// MultiplyAmount multiplies an amount by a factor
// Example: MultiplyAmount(100000, 2.5, "KES") = 250000 (1000.00 * 2.5 = 2500.00)
func MultiplyAmount(smallestUnit int64, multiplier float64, currency string) (int64, error) {
	amount, err := Int64ToAmount(smallestUnit, currency)
	if err != nil {
		return 0, err
	}

	// Convert float to decimal
	multiplierStr := fmt.Sprintf("%.8f", multiplier)
	multiplierDec, err := decimal.Parse(multiplierStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse multiplier: %w", err)
	}

	result, err := amount.Mul(multiplierDec)
	if err != nil {
		return 0, fmt.Errorf("failed to multiply amount: %w", err)
	}

	return AmountToInt64(result)
}

// ApplyBasisPoints applies a basis points percentage to an amount
// Example: ApplyBasisPoints(100000, 350, "KES") = 3500 (3.5% of 1000.00)
func ApplyBasisPoints(smallestUnit int64, bps BasisPoints, currency string) (int64, error) {
	amount, err := Int64ToAmount(smallestUnit, currency)
	if err != nil {
		return 0, err
	}

	// Convert basis points to decimal (bps / 10000)
	bpsDecimal := decimal.MustNew(int64(bps), 4) // 4 scale means divide by 10000
	result, err := amount.Mul(bpsDecimal)
	if err != nil {
		return 0, fmt.Errorf("failed to apply basis points: %w", err)
	}

	return AmountToInt64(result)
}

// CalculateSimpleInterest calculates simple interest: I = P * r * t
// principal: amount in smallest unit
// rateBps: annual interest rate in basis points
// durationDays: loan duration in days
// Returns interest amount in smallest unit
func CalculateSimpleInterest(principal int64, rateBps BasisPoints, durationDays int, currency string) (int64, error) {
	amount, err := Int64ToAmount(principal, currency)
	if err != nil {
		return 0, err
	}

	// I = P * r * (days/365)
	// Convert rate from basis points to decimal
	rateDec := decimal.MustNew(int64(rateBps), 4) // bps/10000

	// Multiply principal by rate
	interestAnnual, err := amount.Mul(rateDec)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate annual interest: %w", err)
	}

	// Multiply by days and divide by 365
	daysFactor := decimal.MustNew(int64(durationDays), 0)
	daysInYear := decimal.MustNew(365, 0)
	timeFactor, err := daysFactor.Quo(daysInYear)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate time factor: %w", err)
	}

	interest, err := interestAnnual.Mul(timeFactor)
	if err != nil {
		return 0, fmt.Errorf("failed to calculate interest for duration: %w", err)
	}

	return AmountToInt64(interest)
}

// CalculateCompoundInterest calculates compound interest with daily compounding
// Returns the total amount (principal + interest) in smallest unit
func CalculateCompoundInterest(principal int64, rateBps BasisPoints, durationDays int, currency string) (int64, error) {
	amount, err := Int64ToAmount(principal, currency)
	if err != nil {
		return 0, err
	}

	// Daily rate = annual rate / 365
	dailyRateBps := int64(rateBps) / 365
	dailyRateDec := decimal.MustNew(dailyRateBps, 4) // basis points to decimal

	// Apply daily compounding
	result := amount
	for i := 0; i < durationDays; i++ {
		dailyInterest, err := result.Mul(dailyRateDec)
		if err != nil {
			return 0, fmt.Errorf("failed to calculate compound interest: %w", err)
		}

		result, err = result.Add(dailyInterest)
		if err != nil {
			return 0, fmt.Errorf("failed to add compound interest: %w", err)
		}
	}

	return AmountToInt64(result)
}

// ConvertCurrency converts an amount from one currency to another using an exchange rate
// exchangeRateBps: exchange rate in basis points (e.g., 7700 = 0.77 USD per KES)
func ConvertCurrency(amount int64, fromCurrency, toCurrency string, exchangeRateBps int64) (int64, error) {
	fromAmount, err := Int64ToAmount(amount, fromCurrency)
	if err != nil {
		return 0, err
	}

	// Create exchange rate string (basis points to decimal: divide by 10000)
	rateDecimal := float64(exchangeRateBps) / 10000.0
	rateStr := fmt.Sprintf("%.8f", rateDecimal)

	exchRate, err := money.ParseExchRate(fromCurrency, toCurrency, rateStr)
	if err != nil {
		return 0, fmt.Errorf("failed to parse exchange rate: %w", err)
	}

	toAmount, err := exchRate.Conv(fromAmount)
	if err != nil {
		return 0, fmt.Errorf("failed to convert currency: %w", err)
	}

	return AmountToInt64(toAmount)
}

// CompareAmounts compares two amounts in smallest unit (same currency)
// Returns: -1 if a < b, 0 if a == b, 1 if a > b
func CompareAmounts(a, b int64, currency string) (int, error) {
	amountA, err := Int64ToAmount(a, currency)
	if err != nil {
		return 0, err
	}

	amountB, err := Int64ToAmount(b, currency)
	if err != nil {
		return 0, err
	}

	cmp, err := amountA.Cmp(amountB)
	if err != nil {
		return 0, fmt.Errorf("failed to compare amounts: %w", err)
	}
	return cmp, nil
}

// IsPositive checks if an amount is positive
func IsPositive(amount int64) bool {
	return amount > 0
}

// IsNegative checks if an amount is negative
func IsNegative(amount int64) bool {
	return amount < 0
}

// IsZero checks if an amount is zero
func IsZero(amount int64) bool {
	return amount == 0
}

// ValidateCurrency checks if a currency code is supported
func ValidateCurrency(currencyCode string) bool {
	_, err := money.ParseCurr(currencyCode)
	return err == nil
}
