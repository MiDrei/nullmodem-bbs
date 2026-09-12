// Package menu loads the BBS's data-driven menu definitions: named
// menus, each with SL-gated items that trigger a builtin command, jump
// to another menu, or log the caller off. Menus live as YAML files
// under configs/menus/ so a sysop can add or rearrange them without
// touching Go code.
package menu

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Item is one selectable line in a menu.
type Item struct {
	Key    string `yaml:"key"`
	Label  string `yaml:"label"`
	Action string `yaml:"action"`
	MinSL  int    `yaml:"min_sl"`
}

// Menu is one named screen of selectable items.
type Menu struct {
	Name  string `yaml:"name"`
	Title string `yaml:"title"`
	Items []Item `yaml:"items"`
}

// Set is a collection of menus keyed by name, as loaded from disk.
type Set map[string]*Menu

// Get looks up a menu by name.
func (s Set) Get(name string) (*Menu, bool) {
	m, ok := s[name]
	return m, ok
}

// VisibleItems returns the items of m that a caller at securityLevel
// is allowed to see, in their defined order.
func (m *Menu) VisibleItems(securityLevel int) []Item {
	visible := make([]Item, 0, len(m.Items))
	for _, item := range m.Items {
		if securityLevel >= item.MinSL {
			visible = append(visible, item)
		}
	}
	return visible
}

// Find looks up an item by key (case-insensitive) among those visible
// to securityLevel. It returns false if the key doesn't exist or is
// gated above the caller's level.
func (m *Menu) Find(key string, securityLevel int) (Item, bool) {
	key = strings.ToUpper(strings.TrimSpace(key))
	for _, item := range m.VisibleItems(securityLevel) {
		if strings.ToUpper(item.Key) == key {
			return item, true
		}
	}
	return Item{}, false
}

// LoadDir reads every *.yaml file in dir as a Menu and returns them
// keyed by their Name field.
func LoadDir(dir string) (Set, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("menu: read dir %s: %w", dir, err)
	}

	set := make(Set)
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("menu: read %s: %w", path, err)
		}
		var m Menu
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("menu: parse %s: %w", path, err)
		}
		if m.Name == "" {
			return nil, fmt.Errorf("menu: %s: missing name", path)
		}
		if _, dup := set[m.Name]; dup {
			return nil, fmt.Errorf("menu: %s: duplicate menu name %q", path, m.Name)
		}
		for _, item := range m.Items {
			if item.Key == "" || item.Action == "" {
				return nil, fmt.Errorf("menu: %s: item with empty key or action", path)
			}
		}
		set[m.Name] = &m
	}
	return set, nil
}
