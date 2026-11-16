package iso3166

import (
	"sort"
	"testing"
)

func TestName(t *testing.T) {
	for code, want := range map[string]string{"KE": "Kenya", "ke": "Kenya", "US": "United States"} {
		if got, ok := Name(code); !ok || got != want {
			t.Errorf("Name(%q) = %q, %v; want %q", code, got, ok, want)
		}
	}
	for _, code := range []string{"", "XX", "KEN", "Kenya"} {
		if Valid(code) {
			t.Errorf("Valid(%q) = true", code)
		}
	}
}

func TestCountriesAreUniqueAndSorted(t *testing.T) {
	if len(byCode) != len(Countries) {
		t.Fatalf("%d codes for %d countries", len(byCode), len(Countries))
	}
	if !sort.SliceIsSorted(Countries, func(i, j int) bool { return Countries[i].Name < Countries[j].Name }) {
		t.Fatal("Countries is not sorted by name")
	}
}

func TestSubdivisions(t *testing.T) {
	if n := len(SubdivisionsOf("KE")); n != 47 {
		t.Errorf("KE has %d counties, want 47", n)
	}
	if n := len(SubdivisionsOf("us")); n < 50 {
		t.Errorf("US has %d subdivisions, want at least 50", n)
	}
	if name, ok := SubdivisionName("us-de"); !ok || name != "Delaware" {
		t.Errorf("SubdivisionName(us-de) = %q, %v", name, ok)
	}
	if !ValidSubdivision("US", "US-DE") || ValidSubdivision("KE", "US-DE") || ValidSubdivision("US", "US") {
		t.Error("ValidSubdivision accepted a code outside its country")
	}
	for cc := range subdivisions {
		if !Valid(cc) {
			t.Errorf("subdivisions keyed by unknown country %q", cc)
		}
	}
}
