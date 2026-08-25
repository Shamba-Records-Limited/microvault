package ussd

import (
	"fmt"
	"strings"

	pkgErrors "github.com/Shamba-Records-Limited/microvault/pkg/errors"
)

// NewMenuRegistry creates a new menu registry
func NewMenuRegistry() *MenuRegistry {
	return &MenuRegistry{
		menus: make(map[string]*Menu),
	}
}

// Register registers a menu
func (mr *MenuRegistry) Register(menu *Menu) {
	mr.menus[menu.ID] = menu
}

// Get retrieves a menu by ID
func (mr *MenuRegistry) Get(menuID string) (*Menu, error) {
	menu, ok := mr.menus[menuID]
	if !ok {
		return nil, ussdErr("get_menu", nil).With("menu", menuID).Code(pkgErrors.CodeNotFound).Errorf("menu is not registered")
	}
	return menu, nil
}

// GetAll returns all registered menus
func (mr *MenuRegistry) GetAll() map[string]*Menu {
	return mr.menus
}

// Unregister removes a menu from the registry
func (mr *MenuRegistry) Unregister(menuID string) {
	delete(mr.menus, menuID)
}

// Clear removes all menus from the registry
func (mr *MenuRegistry) Clear() {
	mr.menus = make(map[string]*Menu)
}

// Render renders a menu for display
func (m *Menu) Render(language string) string {
	var sb strings.Builder

	// Add title
	title := m.Title[language]
	if title == "" {
		title = m.Title["en"] // Fallback to English
	}
	sb.WriteString(title)
	sb.WriteString("\n")

	// Add options
	for _, option := range m.Options {
		label := option.Label[language]
		if label == "" {
			label = option.Label["en"]
		}
		fmt.Fprintf(&sb, "%s. %s\n", option.Key, label)
	}

	return strings.TrimRight(sb.String(), "\n")
}

// GetOption retrieves an option by key
func (m *Menu) GetOption(key string) (*MenuOption, error) {
	for _, option := range m.Options {
		if option.Key == key {
			return &option, nil
		}
	}
	return nil, ussdErr("get_option", nil).With("option", key).Code(pkgErrors.CodeNotFound).Errorf("menu has no such option")
}

// NewMenuBuilder creates a new menu builder
func NewMenuBuilder(id string) *MenuBuilder {
	return &MenuBuilder{
		menu: &Menu{
			ID:      id,
			Title:   make(map[string]string),
			Options: []MenuOption{},
		},
	}
}

// WithTitle sets the menu title for a language
func (mb *MenuBuilder) WithTitle(language, title string) *MenuBuilder {
	mb.menu.Title[language] = title
	return mb
}

// WithOption adds an option to the menu
func (mb *MenuBuilder) WithOption(key string, labels map[string]string, targetMenu string) *MenuBuilder {
	mb.menu.Options = append(mb.menu.Options, MenuOption{
		Key:        key,
		Label:      labels,
		TargetMenu: targetMenu,
	})
	return mb
}

// WithParentMenu sets the parent menu
func (mb *MenuBuilder) WithParentMenu(parentMenu string) *MenuBuilder {
	mb.menu.ParentMenu = parentMenu
	return mb
}

// Build returns the built menu
func (mb *MenuBuilder) Build() *Menu {
	return mb.menu
}
