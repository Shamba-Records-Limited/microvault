package ussd

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
)

// Session keys for the borrower's product choice.
const (
	productKey        = "product"
	productChoicesKey = "product_choices"
)

// maxProductChoices is how many products fit on one screen.
const maxProductChoices = 5

// startBorrow begins a loan request. With a ProductCatalog the borrower sees
// only the products they may take: none ends the session, one goes straight
// to the amount, several are listed to choose from.
func (h *USSDHandler) startBorrow(ctx context.Context, session *Session) (string, error) {
	delete(session.Data, productKey)
	delete(session.Data, productChoicesKey)
	catalog, ok := h.loanService.(ProductCatalog)
	if !ok {
		return h.toLoanAmount(ctx, session)
	}
	products, err := catalog.ProductsFor(ctx, session.UserID)
	if err != nil {
		slog.ErrorContext(ctx, "loan products unavailable", slog.String("user_id", session.UserID), slog.Any("error", err))
		return h.formatError(session.Language, "error"), nil
	}
	switch len(products) {
	case 0:
		return h.formatResponse(session.Language, "END", "loan_no_offers"), nil
	case 1:
		if err := setProduct(session, products[0]); err != nil {
			return "", err
		}
		return h.toLoanAmount(ctx, session)
	}
	if len(products) > maxProductChoices {
		products = products[:maxProductChoices]
	}
	raw, err := json.Marshal(products)
	if err != nil {
		return "", fmt.Errorf("ussd: encode product choices: %w", err)
	}
	session.Data[productChoicesKey] = string(raw)
	session.CurrentMenu = "loan_product"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", sessionSaveErr(session, err)
	}
	return h.showProductMenu(session)
}

// handleLoanProduct stores the chosen product and asks for the amount.
func (h *USSDHandler) handleLoanProduct(ctx context.Context, session *Session, input string) (string, error) {
	choices := productChoices(session)
	i, err := strconv.Atoi(input)
	if err != nil || i < 1 || i > len(choices) {
		return h.showProductMenu(session)
	}
	if err := setProduct(session, choices[i-1]); err != nil {
		return "", err
	}
	return h.toLoanAmount(ctx, session)
}

func (h *USSDHandler) toLoanAmount(ctx context.Context, session *Session) (string, error) {
	session.CurrentMenu = "request_loan"
	if err := h.sessionManager.SaveSession(ctx, session); err != nil {
		return "", sessionSaveErr(session, err)
	}
	return h.showLoanAmountMenu(session)
}

func (h *USSDHandler) showProductMenu(session *Session) (string, error) {
	choices := productChoices(session)
	if len(choices) == 0 {
		return h.formatError(session.Language, "error"), nil
	}
	var b strings.Builder
	b.WriteString(GetLocalizedMessage(session.Language, "loan_choose_product"))
	for i, p := range choices {
		fmt.Fprintf(&b, "\n%d. %s %s %.0f-%.0f", i+1, p.Name, p.Currency, float64(p.MinAmountCents)/100, float64(p.MaxAmountCents)/100)
	}
	return "CON " + h.withNavHint(session, "loan_product", b.String()), nil
}

// productFor returns the product the borrower chose this session, or the
// loan service's single product when it offers no choice.
func (h *USSDHandler) productFor(session *Session) *LoanProductConfig {
	if raw, _ := session.Data[productKey].(string); raw != "" {
		var p LoanProductConfig
		if err := json.Unmarshal([]byte(raw), &p); err == nil {
			return &p
		}
	}
	if _, ok := h.loanService.(ProductCatalog); ok {
		return nil
	}
	return h.loanService.GetProductConfig()
}

func setProduct(session *Session, p LoanProductConfig) error {
	raw, err := json.Marshal(p)
	if err != nil {
		return fmt.Errorf("ussd: encode product: %w", err)
	}
	session.Data[productKey] = string(raw)
	delete(session.Data, productChoicesKey)
	return nil
}

func productChoices(session *Session) []LoanProductConfig {
	raw, _ := session.Data[productChoicesKey].(string)
	var out []LoanProductConfig
	if raw != "" {
		_ = json.Unmarshal([]byte(raw), &out)
	}
	return out
}
