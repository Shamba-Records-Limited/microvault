package controllers

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/Shamba-Records-Limited/microvault/pkg/config"
	"github.com/Shamba-Records-Limited/microvault/pkg/models"
	"github.com/Shamba-Records-Limited/microvault/pkg/payment/airtel"
	"github.com/Shamba-Records-Limited/microvault/pkg/repository"
)

type fakeAirtelRepo struct {
	recorded  []*models.AirtelTransaction
	recordErr error
}

func (f *fakeAirtelRepo) RecordCallback(_ context.Context, tx *models.AirtelTransaction) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	f.recorded = append(f.recorded, tx)
	return nil
}

func (f *fakeAirtelRepo) GetByPartnerID(context.Context, string) (*models.AirtelTransaction, error) {
	return nil, repository.ErrAirtelNotFound
}

func (f *fakeAirtelRepo) GetByAirtelMoneyID(context.Context, string) (*models.AirtelTransaction, error) {
	return nil, repository.ErrAirtelNotFound
}

func (f *fakeAirtelRepo) DuePoll(context.Context, int) ([]*models.AirtelTransaction, error) {
	return nil, nil
}

func (f *fakeAirtelRepo) Confirm(context.Context, string, models.AirtelTransactionConfirmVia, string, string, string) error {
	return nil
}

func (f *fakeAirtelRepo) UpdatePoll(context.Context, string, time.Time) error { return nil }
func (f *fakeAirtelRepo) StopPoll(context.Context, string) error              { return nil }

func (f *fakeAirtelRepo) UpsertFromSummary(_ context.Context, tx *models.AirtelTransaction) error {
	f.recorded = append(f.recorded, tx)
	return nil
}

func (f *fakeAirtelRepo) ListUnappliedConfirmed(context.Context, int) ([]*models.AirtelTransaction, error) {
	return nil, nil
}

func (f *fakeAirtelRepo) SetAppliedStroops(context.Context, string, int64) error { return nil }

func (f *fakeAirtelRepo) SumAppliedStroopsByLoan(context.Context, string) (int64, error) {
	return 0, nil
}

const airtelSlug = "unguessable"

func airtelApp(t *testing.T, repo repository.AirtelTransactionRepository, cfg config.AirtelConfig, env string) *fiber.App {
	t.Helper()

	cfg.CallbackSlug = airtelSlug
	ctrl := NewAirtelCallbackController(repo, cfg, env)
	app := fiber.New()
	ctrl.Register(app)
	return app
}

const airtelCallbackPath = "/callbacks/airtel/" + airtelSlug + "/collection"

// signedCallback renders a body with a hash Airtel would have produced under
// the without-hash reading.
func signedCallback(t *testing.T, key, id, status, receipt string) string {
	t.Helper()

	transaction := map[string]any{
		"id":          id,
		"message":     "Transaction " + status,
		"status_code": status,
	}
	if receipt != "" {
		transaction["airtel_money_id"] = receipt
	}

	body := map[string]any{"transaction": transaction}
	unsigned, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	body["hash"] = airtel.SignCallback(unsigned, key)
	signed, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(signed)
}

func TestAirtelCallback_RecordsSettled(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{CallbackHMACKey: "key"}, "development")

	resp := postJSON(t, app, airtelCallbackPath, signedCallback(t, "key", "mv-1", "TS", "AM1"))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded %d observations, want 1", len(repo.recorded))
	}

	tx := repo.recorded[0]
	if tx.PartnerTxnID != "mv-1" {
		t.Fatalf("partner txn id = %q", tx.PartnerTxnID)
	}
	if tx.AirtelMoneyID == nil || *tx.AirtelMoneyID != "AM1" {
		t.Fatalf("receipt = %v", tx.AirtelMoneyID)
	}
	if !tx.HashVerified {
		t.Fatal("a correctly signed callback was recorded unverified")
	}
	if tx.HashVariant == nil || *tx.HashVariant != string(airtel.VariantWithoutHash) {
		t.Fatalf("hash variant = %v, want the matching rendering to be recorded", tx.HashVariant)
	}
	if tx.Confirmed {
		t.Fatal("a callback confirmed the row; only an enquiry may do that")
	}
	if tx.NextPollAt == nil {
		t.Fatal("a recorded callback was not staged for an enquiry")
	}
}

// Airtel's TF may be intermediate, so unlike the Daraja rail — which drops a
// failed prompt — a failure is recorded and still queued for an enquiry.
func TestAirtelCallback_RecordsFailure(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{CallbackHMACKey: "key"}, "development")

	resp := postJSON(t, app, airtelCallbackPath, signedCallback(t, "key", "mv-2", "TF", ""))
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("a failed callback was dropped; Airtel's TF is not necessarily terminal")
	}

	tx := repo.recorded[0]
	if tx.StatusCode != "TF" {
		t.Fatalf("status code = %q", tx.StatusCode)
	}
	if tx.AirtelMoneyID != nil {
		t.Fatalf("a failed callback carried a receipt: %v", tx.AirtelMoneyID)
	}
	if tx.NextPollAt == nil {
		t.Fatal("a failed callback was not staged for an enquiry")
	}
}

// The enquiry is not scheduled before Airtel's documented floor.
func TestAirtelCallback_SchedulesBeyondTheEnquiryFloor(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{}, "development")

	before := time.Now()
	if resp := postJSON(t, app, airtelCallbackPath, `{"transaction":{"id":"mv-3","status_code":"TS","airtel_money_id":"AM3"}}`); resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	tx := repo.recorded[0]
	if tx.NextPollAt.Before(before.Add(config.EnquiryDelayFloor)) {
		t.Fatalf("first enquiry scheduled at %s, sooner than the %s floor", tx.NextPollAt, config.EnquiryDelayFloor)
	}
}

// A hash that does not verify is a forgery or a key mismatch. Neither should
// land a row.
func TestAirtelCallback_RejectsBadHash(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{CallbackHMACKey: "the-real-key"}, "development")

	resp := postJSON(t, app, airtelCallbackPath, signedCallback(t, "a-forger's-key", "mv-4", "TS", "AM4"))
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("a callback with an unverifiable hash was recorded")
	}
}

// Callback authentication is optional at Airtel's end. A deployment that has
// not enabled it is not misconfigured, and its callbacks are recorded
// unverified rather than refused.
func TestAirtelCallback_UnsignedIsRecordedUnverified(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{CallbackHMACKey: "key"}, "development")

	resp := postJSON(t, app, airtelCallbackPath, `{"transaction":{"id":"mv-5","status_code":"TS","airtel_money_id":"AM5"}}`)
	if resp.StatusCode != fiber.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if len(repo.recorded) != 1 {
		t.Fatalf("recorded %d observations, want 1", len(repo.recorded))
	}
	if repo.recorded[0].HashVerified {
		t.Fatal("an unsigned callback was recorded as verified")
	}
}

func TestAirtelCallback_RejectsUndecodable(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{}, "development")

	for name, body := range map[string]string{
		"garbage":        `not json`,
		"no transaction": `{"hash":"abc"}`,
		"no id":          `{"transaction":{"status_code":"TS"}}`,
	} {
		t.Run(name, func(t *testing.T) {
			resp := postJSON(t, app, airtelCallbackPath, body)
			if resp.StatusCode != fiber.StatusBadRequest {
				t.Fatalf("status = %d, want 400", resp.StatusCode)
			}
		})
	}
	if len(repo.recorded) != 0 {
		t.Fatal("an undecodable callback was recorded")
	}
}

// Airtel does not publish an egress list, so production with no allowlist is
// a misconfiguration and must fail closed rather than default permissive.
func TestAirtelCallback_ProductionRequiresAllowlist(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{}, "production")

	resp := postJSON(t, app, airtelCallbackPath, `{"transaction":{"id":"mv-6","status_code":"TS"}}`)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403", resp.StatusCode)
	}
	if len(repo.recorded) != 0 {
		t.Fatal("a production callback was recorded with no allowlist configured")
	}
}

func TestAirtelCallback_AllowlistAdmitsAndRejects(t *testing.T) {
	repo := &fakeAirtelRepo{}
	app := airtelApp(t, repo, config.AirtelConfig{CallbackAllowedCIDRs: []string{"10.0.0.0/8"}}, "production")

	// fiber's test driver reports 0.0.0.0 as the client address, which is
	// outside the range.
	resp := postJSON(t, app, airtelCallbackPath, `{"transaction":{"id":"mv-7","status_code":"TS"}}`)
	if resp.StatusCode != fiber.StatusForbidden {
		t.Fatalf("status = %d, want 403 for a source outside the allowlist", resp.StatusCode)
	}
}

func TestAirtelCallback_RecordFailureIs500(t *testing.T) {
	repo := &fakeAirtelRepo{recordErr: repository.ErrFailedToRecordAirtel}
	app := airtelApp(t, repo, config.AirtelConfig{}, "development")

	resp := postJSON(t, app, airtelCallbackPath, `{"transaction":{"id":"mv-8","status_code":"TS"}}`)
	if resp.StatusCode != fiber.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}
