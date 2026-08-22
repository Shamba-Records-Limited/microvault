package notifications

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// mergeInto copies every non-nil func field of override onto a copy of base and
// returns it. base and override must be pointers to the same struct type whose
// fields are all funcs. A nil override yields base unchanged.
//
// Reflection keeps this to one loop rather than a nil check per event, and it
// runs once per language at construction, never on a send path.
func mergeInto[T any](base, override *T) *T {
	if override == nil {
		return base
	}
	merged := *base
	dst := reflect.ValueOf(&merged).Elem()
	src := reflect.ValueOf(override).Elem()
	for i := range src.NumField() {
		if !src.Field(i).IsNil() {
			dst.Field(i).Set(src.Field(i))
		}
	}
	return &merged
}

// validateSet renders every field of a template set against the sentinel and
// reports the fields that are unset or produce a message no handset can carry
// in GSM-7. call invokes one field and returns the rendered message.
func validateSet(lang string, set any, call func(field reflect.Value) string) error {
	v := reflect.ValueOf(set).Elem()
	t := v.Type()

	var problems []string
	for i := range v.NumField() {
		name := t.Field(i).Name
		field := v.Field(i)
		if field.IsNil() {
			problems = append(problems, fmt.Sprintf("%s is not set", name))
			continue
		}
		msg := call(field)
		if msg == "" {
			problems = append(problems, fmt.Sprintf("%s renders an empty message", name))
			continue
		}
		if _, bad, ok := GSM7Len(msg); !ok {
			problems = append(problems, fmt.Sprintf("%s contains %q, outside GSM 03.38", name, bad))
		}
	}
	if len(problems) == 0 {
		return nil
	}
	sort.Strings(problems)
	return fmt.Errorf("%s templates: %s", lang, strings.Join(problems, "; "))
}

// validateLoanTemplates checks one language's merged loan set.
func validateLoanTemplates(lang string, set *LoanTemplates) error {
	n := SentinelLoanNotification()
	return validateSet(lang, set, func(field reflect.Value) string {
		return field.Interface().(LoanMessage)(n)
	})
}

// validateAccountTemplates checks one language's merged account set.
func validateAccountTemplates(lang string, set *AccountTemplates) error {
	n := SentinelAccountNotification()
	return validateSet(lang, set, func(field reflect.Value) string {
		return field.Interface().(AccountMessage)(n)
	})
}
