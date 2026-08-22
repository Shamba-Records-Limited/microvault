package ussd

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

// bioUserSvc records what UpdateBio received so a test can assert that editing
// one field leaves the others alone.
type bioUserSvc struct {
	*fakeUserSvc
	got      []BioUpdate
	failWith error
}

func (b *bioUserSvc) UpdateBio(_ context.Context, _ string, bio BioUpdate) error {
	if b.failWith != nil {
		return b.failWith
	}
	b.got = append(b.got, bio)
	return nil
}

func newDetailsHarness(t *testing.T, user UserService) *USSDHandler {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mr.Close)
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	sm := NewSessionManager(client, 5*time.Minute)

	reg := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(reg)

	return NewUSSDHandler(sm, reg, user, fakeLoanSvc{}, fakeRateSvc{}, &fakePINSvc{hasPIN: true}, nil)
}

func detailsUser(stored map[string]any) *bioUserSvc {
	u := map[string]any{"id": "u1"}
	for k, v := range stored {
		u[k] = v
	}
	return &bioUserSvc{fakeUserSvc: &fakeUserSvc{user: u, accounts: []any{map[string]any{}}}}
}

func detailsSession() *Session {
	return &Session{
		SessionID:   "sess-details",
		PhoneNumber: "254711000111",
		UserID:      "u1",
		CurrentMenu: "my_details",
		Language:    "en",
		Data:        map[string]any{},
	}
}

// The picker's whole point: show what is on file before asking what to change.
func TestMyDetails_ListsStoredValuesAndMarksUnsetOnes(t *testing.T) {
	svc := detailsUser(map[string]any{"city": "Nakuru", "birth_date": "1990-04-12"})
	h := newDetailsHarness(t, svc)

	resp, err := h.showMyDetails(context.Background(), detailsSession(), "")
	if err != nil {
		t.Fatalf("showMyDetails: %v", err)
	}
	for _, want := range []string{"Nakuru", "1990-04-12", "1. DOB", "3. City", "0. Back"} {
		if !strings.Contains(resp, want) {
			t.Errorf("picker missing %q:\n%s", want, resp)
		}
	}
	// Address and postal code were never set, so both must read as unset.
	if n := strings.Count(resp, "not set"); n != 2 {
		t.Errorf("expected 2 unset fields, found %d:\n%s", n, resp)
	}
}

// Editing one field must send only that field. UpdateBio ignores empty values,
// so sending the whole struct would be harmless today but would silently
// rewrite the other three the moment that changes.
func TestMyDetails_EditsExactlyOneField(t *testing.T) {
	svc := detailsUser(map[string]any{"city": "Nakuru", "address": "12 Kenyatta Ave"})
	h := newDetailsHarness(t, svc)
	ctx := context.Background()

	session := detailsSession()
	if _, err := h.handleMyDetails(ctx, session, "3"); err != nil { // City
		t.Fatalf("handleMyDetails: %v", err)
	}
	if session.CurrentMenu != "bio_edit" || session.Data["bio_field"] != "city" {
		t.Fatalf("expected the city editor, got menu=%q field=%v", session.CurrentMenu, session.Data["bio_field"])
	}

	if _, err := h.handleBioEdit(ctx, session, "Eldoret"); err != nil {
		t.Fatalf("handleBioEdit: %v", err)
	}
	if len(svc.got) != 1 {
		t.Fatalf("expected one UpdateBio call, got %d", len(svc.got))
	}
	got := svc.got[0]
	if got.City != "Eldoret" {
		t.Errorf("City = %q, want Eldoret", got.City)
	}
	if got.Address != "" || got.BirthDate != "" || got.PostalCode != "" {
		t.Errorf("editing City sent other fields too: %+v", got)
	}
}

// After saving, the user lands back on the picker showing the new value.
func TestMyDetails_ReturnsToPickerAfterSave(t *testing.T) {
	svc := detailsUser(nil)
	h := newDetailsHarness(t, svc)
	ctx := context.Background()

	session := detailsSession()
	session.Data["bio_field"] = "city"
	session.CurrentMenu = "bio_edit"

	// The stub user map is fixed, so assert the navigation and the notice
	// rather than the refreshed value.
	resp, err := h.handleBioEdit(ctx, session, "Eldoret")
	if err != nil {
		t.Fatalf("handleBioEdit: %v", err)
	}
	if session.CurrentMenu != "my_details" {
		t.Errorf("expected a return to the picker, got %q", session.CurrentMenu)
	}
	if _, still := session.Data["bio_field"]; still {
		t.Error("bio_field should be cleared once the edit completes")
	}
	if !strings.Contains(resp, "Saved.") || !strings.Contains(resp, "1. DOB") {
		t.Errorf("expected the picker with a saved notice:\n%s", resp)
	}
}

// Preserve-only: an empty entry must not reach UpdateBio, because "" is
// indistinguishable from "leave alone" and would silently no-op while telling
// the user their change was saved.
func TestMyDetails_EmptyEntryDoesNotWrite(t *testing.T) {
	svc := detailsUser(map[string]any{"city": "Nakuru"})
	h := newDetailsHarness(t, svc)

	session := detailsSession()
	session.Data["bio_field"] = "city"
	session.CurrentMenu = "bio_edit"

	resp, err := h.handleBioEdit(context.Background(), session, "   ")
	if err != nil {
		t.Fatalf("handleBioEdit: %v", err)
	}
	if len(svc.got) != 0 {
		t.Errorf("an empty entry must not call UpdateBio, got %+v", svc.got)
	}
	if !strings.Contains(resp, "City/town") {
		t.Errorf("expected a re-prompt, got %q", resp)
	}
	if strings.Contains(resp, "Saved.") {
		t.Errorf("an empty entry must not report success: %q", resp)
	}
}

func TestMyDetails_RejectsMalformedBirthDate(t *testing.T) {
	svc := detailsUser(nil)
	h := newDetailsHarness(t, svc)

	session := detailsSession()
	session.Data["bio_field"] = "birth_date"
	session.CurrentMenu = "bio_edit"

	resp, err := h.handleBioEdit(context.Background(), session, "12/04/1990")
	if err != nil {
		t.Fatalf("handleBioEdit: %v", err)
	}
	if len(svc.got) != 0 {
		t.Errorf("a malformed date must not be written, got %+v", svc.got)
	}
	if !strings.Contains(resp, "Invalid date") {
		t.Errorf("expected the invalid-date prompt, got %q", resp)
	}
}

func TestMyDetails_InvalidSelectionRepromptsPicker(t *testing.T) {
	h := newDetailsHarness(t, detailsUser(nil))

	for _, input := range []string{"9", "abc", "-1"} {
		session := detailsSession()
		resp, err := h.handleMyDetails(context.Background(), session, input)
		if err != nil {
			t.Fatalf("input %q: %v", input, err)
		}
		if session.CurrentMenu != "my_details" {
			t.Errorf("input %q: should stay on the picker, got %q", input, session.CurrentMenu)
		}
		if !strings.Contains(resp, "1. DOB") {
			t.Errorf("input %q: expected the picker to be re-rendered, got %q", input, resp)
		}
	}
}

// A write failure must not claim success.
func TestMyDetails_WriteFailureSurfaces(t *testing.T) {
	svc := detailsUser(nil)
	svc.failWith = errors.New("db down")
	h := newDetailsHarness(t, svc)

	session := detailsSession()
	session.Data["bio_field"] = "city"
	session.CurrentMenu = "bio_edit"

	resp, err := h.handleBioEdit(context.Background(), session, "Eldoret")
	if err != nil {
		t.Fatalf("handleBioEdit: %v", err)
	}
	if strings.Contains(resp, "Saved.") {
		t.Errorf("a failed write must not report success: %q", resp)
	}
}

// The editor accepts an address and a birth date, so its input is PII.
func TestMyDetails_EditorInputIsRedactedInLogs(t *testing.T) {
	if got := safeInput("bio_edit", "12 Kenyatta Ave"); got != "[REDACTED]" {
		t.Errorf("safeInput(bio_edit) = %q, want [REDACTED]", got)
	}
}

// Africa's Talking truncates at 160 characters, and a long address is the most
// likely thing to blow the budget.
func TestMyDetails_FitsOneUSSDScreen(t *testing.T) {
	long := "Flat 14B, Riverside Court, 221 Ngong Road, Kilimani, Nairobi County"
	svc := detailsUser(map[string]any{
		"address":     long,
		"city":        "Nairobi",
		"birth_date":  "1990-04-12",
		"postal_code": "00100",
	})
	h := newDetailsHarness(t, svc)

	for _, lang := range []string{"en", "sw", "fr"} {
		session := detailsSession()
		session.Language = lang
		resp, err := h.showMyDetails(context.Background(), session, GetLocalizedMessage(lang, "bio_field_saved"))
		if err != nil {
			t.Fatalf("%s: %v", lang, err)
		}
		body := strings.TrimPrefix(resp, "CON ")
		if n := len([]rune(body)); n > 160 {
			t.Errorf("%s: picker is %d chars, over the 160 limit:\n%s", lang, n, body)
		}
		if strings.Contains(body, long) {
			t.Errorf("%s: long address was not elided:\n%s", lang, body)
		}
	}
}

// Every string the picker renders must survive GSM-7, or the confirmation SMS
// budget is not the only thing that suffers — the screen itself gets mangled.
func TestMyDetails_LabelsAreGSM7(t *testing.T) {
	keys := []string{"my_details_title", "my_details_not_set", "my_details_back", "bio_field_saved", "bio_invalid_date"}
	for _, f := range bioFields {
		keys = append(keys, f.labelKey, f.promptKey)
	}
	for _, lang := range []string{"en", "sw", "fr"} {
		for _, k := range keys {
			msg := GetLocalizedMessage(lang, k)
			for _, r := range msg {
				if r > 0x7F && !strings.ContainsRune("àäöñüèéùìòÇØøÅåÆæßÉÄÖÑÜ§¿¡£$¥¤…", r) {
					t.Errorf("%s %s: %q may not survive the USSD display:\n%s", lang, k, r, msg)
				}
			}
		}
	}
}
