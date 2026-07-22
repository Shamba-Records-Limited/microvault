package ussd

import (
	"testing"
	"unicode/utf8"
)

// runtimeRenderedMenus are the menus renderMenu builds itself rather than
// pulling from the registry, so they are legitimately absent from it.
var runtimeRenderedMenus = map[string]bool{
	"register_national_id": true,
	"loan_confirm":         true,
}

func TestNavBackTargetsAreRenderable(t *testing.T) {
	registry := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(registry)

	for menu, target := range navBackTargets {
		if runtimeRenderedMenus[target] {
			continue
		}
		if _, err := registry.Get(target); err != nil {
			t.Errorf("navBackTargets[%q] = %q, which is not a registered menu", menu, target)
		}
	}
}

// A menu that binds "0" as one of its own options must not also be in
// navBackTargets: the interceptor would swallow the input before the menu's
// handler ever saw it.
func TestNavBackDoesNotShadowMenuOptions(t *testing.T) {
	registry := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(registry)

	for menu := range navBackTargets {
		m, err := registry.Get(menu)
		if err != nil {
			continue
		}
		if _, err := m.GetOption(navBackInput); err == nil {
			t.Errorf("menu %q binds %q itself but is also in navBackTargets", menu, navBackInput)
		}
	}
}

func TestNavBackTerminatesAtRoot(t *testing.T) {
	for menu := range navBackTargets {
		seen := map[string]bool{menu: true}
		cur := menu
		for {
			next, ok := navBackTargets[cur]
			if !ok {
				break
			}
			if seen[next] {
				t.Fatalf("back chain from %q cycles at %q", menu, next)
			}
			seen[next] = true
			cur = next
		}
	}
}

// The hint is advisory, so it must never be what pushes a menu past the AT
// display limit. Menus that already overflow on their own are a separate
// problem and are left to render unchanged.
func TestNavHintNeverCausesOverflow(t *testing.T) {
	registry := NewMenuRegistry()
	NewStandardLoanMenuPreset().Initialize(registry)
	h := &USSDHandler{menuRegistry: registry}

	for _, lang := range []string{"en", "sw", "fr"} {
		for menu := range navBackTargets {
			m, err := registry.Get(menu)
			if err != nil {
				continue
			}
			body := m.Render(lang)
			if utf8.RuneCountInString(body) > navHintBudget {
				continue
			}
			session := &Session{Language: lang, UserID: "u1", CurrentMenu: menu, Data: map[string]any{}}
			got := utf8.RuneCountInString(h.withNavHint(session, menu, body))
			if got > navHintBudget {
				t.Errorf("menu %q (%s) fits at %d chars but the hint pushed it to %d, over the %d budget",
					menu, lang, utf8.RuneCountInString(body), got, navHintBudget)
			}
		}
	}
}

func TestCanGoBackAndHomeGates(t *testing.T) {
	tests := []struct {
		name     string
		session  *Session
		menu     string
		wantBack bool
		wantHome bool
	}{
		{
			name:     "registration step before a user exists",
			session:  &Session{Data: map[string]any{}},
			menu:     "register_national_id",
			wantBack: true,
			wantHome: false,
		},
		{
			name:     "loan amount roots at main, unreachable unregistered",
			session:  &Session{Data: map[string]any{}},
			menu:     "loan_amount",
			wantBack: false,
			wantHome: false,
		},
		{
			name:     "registered user mid loan flow",
			session:  &Session{UserID: "u1", Data: map[string]any{}},
			menu:     "payout_method",
			wantBack: true,
			wantHome: true,
		},
		{
			name:     "self-heal PIN set has no step behind it",
			session:  &Session{UserID: "u1", Data: map[string]any{"set_pin_only": true}},
			menu:     "pin_create",
			wantBack: false,
			wantHome: false,
		},
		{
			name:     "main menu is already home",
			session:  &Session{UserID: "u1", Data: map[string]any{}},
			menu:     "main",
			wantBack: false,
			wantHome: false,
		},
		{
			name:     "bio wizard keeps 0 as skip, home only",
			session:  &Session{UserID: "u1", Data: map[string]any{}},
			menu:     "register_bio",
			wantBack: false,
			wantHome: true,
		},
		{
			name:     "recovery challenge takes neither",
			session:  &Session{UserID: "u1", Data: map[string]any{}},
			menu:     "recover_sim_q1",
			wantBack: false,
			wantHome: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := canGoBack(tt.session, tt.menu); got != tt.wantBack {
				t.Errorf("canGoBack(%q) = %v, want %v", tt.menu, got, tt.wantBack)
			}
			if got := canGoHome(tt.session, tt.menu); got != tt.wantHome {
				t.Errorf("canGoHome(%q) = %v, want %v", tt.menu, got, tt.wantHome)
			}
		})
	}
}
