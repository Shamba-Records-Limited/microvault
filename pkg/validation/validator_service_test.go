package validation

import (
	"testing"

	"github.com/go-playground/validator/v10"
)

type sample struct {
	Email    string `validate:"required,email"`
	UserName string `validate:"required,min=3"`
}

func TestValidate_Passing(t *testing.T) {
	vs := NewValidatorService()
	fieldErrs, err := vs.Validate(sample{Email: "a@b.com", UserName: "alice"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fieldErrs != nil {
		t.Errorf("expected no field errors, got %v", fieldErrs)
	}
}

func TestValidate_FieldErrors(t *testing.T) {
	vs := NewValidatorService()
	fieldErrs, err := vs.Validate(sample{Email: "not-an-email", UserName: "x"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Keys are snake_cased; messages come from generateDefaultMessage.
	if got := fieldErrs["email"]; got != "Email must be a valid email address" {
		t.Errorf("email error = %q", got)
	}
	if got := fieldErrs["user_name"]; got != "UserName must be at least 3 characters" {
		t.Errorf("user_name error = %q", got)
	}
}

func TestValidate_NonStruct(t *testing.T) {
	vs := NewValidatorService()
	// A non-struct input is not a ValidationErrors — surfaced as a raw error.
	if _, err := vs.Validate(123); err == nil {
		t.Error("expected error validating a non-struct, got nil")
	}
}

func TestStellarXDRValidator(t *testing.T) {
	vs := NewValidatorService()
	type envelope struct {
		XDR string `validate:"stellar_xdr"`
	}
	fieldErrs, err := vs.Validate(envelope{XDR: "not-valid-xdr"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := fieldErrs["xdr"]; got != "XDR must be a valid Stellar XDR transaction" {
		t.Errorf("stellar_xdr error = %q", got)
	}
	// Empty is also invalid.
	if fieldErrs, _ := vs.Validate(envelope{XDR: ""}); fieldErrs["xdr"] == "" {
		t.Error("empty XDR should fail validation")
	}
}

func TestRegisterCustomValidator(t *testing.T) {
	vs := NewValidatorService()
	if err := vs.RegisterValidator("is42", func(fl validator.FieldLevel) bool {
		return fl.Field().String() == "42"
	}, func(field, _ string) string {
		return field + " must be 42"
	}); err != nil {
		t.Fatalf("RegisterValidator error: %v", err)
	}
	type box struct {
		V string `validate:"is42"`
	}
	fieldErrs, _ := vs.Validate(box{V: "7"})
	if got := fieldErrs["v"]; got != "V must be 42" {
		t.Errorf("custom validator message = %q, want 'V must be 42'", got)
	}
}

func TestDefaultMessages(t *testing.T) {
	vs := NewValidatorService()
	type tags struct {
		A string `validate:"required"`
		B int    `validate:"gte=10"`
		C string `validate:"uuid4"`
	}
	fieldErrs, _ := vs.Validate(tags{A: "", B: 5, C: "nope"})
	wants := map[string]string{
		"a": "A is required",
		"b": "B must be greater than or equal to 10",
		"c": "C must be a valid UUID v4",
	}
	for k, want := range wants {
		if fieldErrs[k] != want {
			t.Errorf("%s = %q, want %q", k, fieldErrs[k], want)
		}
	}
}
