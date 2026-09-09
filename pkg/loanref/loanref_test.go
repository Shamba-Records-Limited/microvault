package loanref

import (
	"testing"
)

func TestValidatePrefix(t *testing.T) {
	valid := []string{"MV", "AA", "ZZ", "00", "7K"}
	for _, p := range valid {
		if err := ValidatePrefix(p); err != nil {
			t.Errorf("ValidatePrefix(%q) rejected a valid prefix: %v", p, err)
		}
	}

	invalid := map[string]string{
		"":    "empty",
		"M":   "too short",
		"MVO": "too long",
		"mv":  "lowercase not in alphabet",
		"MI":  "contains I",
		"ML":  "contains L",
		"MO":  "contains O",
		"MU":  "contains U",
		"M-":  "not alphanumeric",
	}
	for p, why := range invalid {
		if err := ValidatePrefix(p); err == nil {
			t.Errorf("ValidatePrefix(%q) accepted an invalid prefix (%s)", p, why)
		}
	}
}

func TestGenerate(t *testing.T) {
	ref, err := Generate(DefaultPrefix)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(ref) != 9 {
		t.Errorf("length = %d, want 9", len(ref))
	}
	if ref[:2] != DefaultPrefix {
		t.Errorf("prefix = %q", ref[:2])
	}
	if !Validate(DefaultPrefix, ref) {
		t.Errorf("Generate produced a reference that fails Validate: %q", ref)
	}
}

func TestGenerate_CustomPrefix(t *testing.T) {
	for _, prefix := range []string{"AA", "00", "ZZ"} {
		ref, err := Generate(prefix)
		if err != nil {
			t.Fatalf("Generate(%q): %v", prefix, err)
		}
		if ref[:2] != prefix {
			t.Errorf("Generate(%q) = %q", prefix, ref)
		}
		if !Validate(prefix, ref) {
			t.Errorf("Generate(%q) produced %q, which fails Validate", prefix, ref)
		}
	}
}

// A reference is valid only under the prefix it was minted for: the check
// character binds it to the configured namespace.
func TestValidate_PrefixScoped(t *testing.T) {
	ref, err := Generate("MV")
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if Validate("AA", ref) {
		t.Errorf("a reference minted under MV validated under AA")
	}
}

// The check character catches every single-character substitution, which is the
// class of error a borrower typing on a feature phone actually makes.
func TestValidate_CheckCharCatchesMutations(t *testing.T) {
	prefix := "MV"
	ref, err := Generate(prefix)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	for pos := 0; pos < len(ref); pos++ {
		for i := range len(alphabet) {
			c := alphabet[i]
			if c == ref[pos] {
				continue
			}
			mutated := ref[:pos] + string(c) + ref[pos+1:]
			// Mutations in the prefix break the prefix check; mutations
			// elsewhere break the check character. Either way they must not
			// validate.
			if Validate(prefix, mutated) {
				t.Errorf("mutation %q of %q validated", mutated, ref)
			}
		}
	}
}

func TestValidate_RejectsWrongShape(t *testing.T) {
	bad := []string{
		"",                   // empty
		"MV7K3QA9",           // too short (no check char)
		"MV7K3QA9FF",         // too long
		"MV7K3QA9i",          // lowercase
		"MV7K3QA9I",          // I in check position is fine as a char but I is excluded
		"LR-018F3A2B1C-A7F2", // the legacy format
	}
	for _, ref := range bad {
		if Validate(DefaultPrefix, ref) {
			t.Errorf("Validate accepted %q", ref)
		}
	}
}

func TestGenerateDeterministic_Rerunnable(t *testing.T) {
	seed := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
	first, err := GenerateDeterministic(DefaultPrefix, seed)
	if err != nil {
		t.Fatalf("GenerateDeterministic: %v", err)
	}
	second, err := GenerateDeterministic(DefaultPrefix, seed)
	if err != nil {
		t.Fatalf("GenerateDeterministic: %v", err)
	}
	if first != second {
		t.Errorf("same seed produced %q and %q; the migration must be rerunnable", first, second)
	}
	if !Validate(DefaultPrefix, first) {
		t.Errorf("deterministic reference %q fails Validate", first)
	}

	other := [16]byte{16, 15, 14, 13, 12, 11, 10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	different, _ := GenerateDeterministic(DefaultPrefix, other)
	if different == first {
		t.Error("different seeds produced the same reference")
	}
}

// References are not enumerable: two calls to Generate must differ.
func TestGenerate_NotEnumerable(t *testing.T) {
	seen := make(map[string]bool)
	for range 1000 {
		ref, err := Generate(DefaultPrefix)
		if err != nil {
			t.Fatalf("Generate: %v", err)
		}
		if seen[ref] {
			t.Fatalf("duplicate reference generated: %q", ref)
		}
		seen[ref] = true
	}
}
