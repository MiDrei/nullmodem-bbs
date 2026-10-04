// Package i18n holds the board's texts in every language it speaks:
// the built-in catalogs (lang/<code>.yaml, compiled in) and the
// sysop's changes to them (<dir>/<code>.yaml, only the texts that
// differ), edited in the web admin's language editor.
//
// A text may carry placeholders like {COUNT}, filled from the
// arguments of T. A language that lacks a text falls back along its
// Chain, and English -- the fallback language -- has them all.
package i18n

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

//go:embed lang/*.yaml
var builtin embed.FS

// Language is one language a caller may choose.
type Language struct {
	Code string `json:"code"`
	Name string `json:"name"`
}

// Fallback is the language every other one falls back to, and the
// one with every text.
const Fallback = "en"

// Languages are the board's languages, the fallback first.
var Languages = []Language{
	{Code: "en", Name: "English"},
	{Code: "de", Name: "Deutsch (Sie)"},
	{Code: "de-du", Name: "Deutsch (Du)"},
}

// Valid reports whether code is one of Languages.
func Valid(code string) bool {
	for _, l := range Languages {
		if l.Code == code {
			return true
		}
	}
	return false
}

// NameOf is a language's own name ("Deutsch (Du)"), or code itself.
func NameOf(code string) string {
	for _, l := range Languages {
		if l.Code == code {
			return l.Name
		}
	}
	return code
}

// Chain is where a text or screen for lang is looked for, in order:
// "de-du" -> de-du, de, en. An unknown or empty code is English.
func Chain(lang string) []string {
	if !Valid(lang) || lang == Fallback {
		return []string{Fallback}
	}
	var out []string
	for code := lang; ; {
		out = append(out, code)
		i := strings.LastIndex(code, "-")
		if i < 0 {
			break
		}
		code = code[:i]
	}
	if out[len(out)-1] != Fallback {
		out = append(out, Fallback)
	}
	return out
}

// Catalog is the texts: the built-in ones plus the sysop's changes,
// read again when their file changes.
type Catalog struct {
	dir string

	mu        sync.Mutex
	defaults  map[string]map[string]string
	keys      []string // English's keys, in file order
	overrides map[string]map[string]string
	stamps    map[string]time.Time
	checked   time.Time
}

// New is the catalog with the sysop's changes in dir ("" for none).
func New(dir string) *Catalog {
	c := &Catalog{dir: dir, defaults: map[string]map[string]string{}, overrides: map[string]map[string]string{}, stamps: map[string]time.Time{}}
	for _, l := range Languages {
		data, err := builtin.ReadFile("lang/" + l.Code + ".yaml")
		if err != nil {
			continue
		}
		m, keys, err := parse(data)
		if err != nil {
			panic(fmt.Sprintf("i18n: lang/%s.yaml: %v", l.Code, err))
		}
		c.defaults[l.Code] = m
		if l.Code == Fallback {
			c.keys = keys
		}
	}
	return c
}

// parse reads a flat key: text map, keeping the keys' order.
func parse(data []byte) (map[string]string, []string, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, nil, err
	}
	m := map[string]string{}
	var keys []string
	if len(doc.Content) == 0 {
		return m, nil, nil
	}
	root := doc.Content[0]
	if root.Kind != yaml.MappingNode {
		return nil, nil, fmt.Errorf("not a map of key: text")
	}
	for i := 0; i+1 < len(root.Content); i += 2 {
		k, v := root.Content[i].Value, root.Content[i+1].Value
		if _, dup := m[k]; !dup {
			keys = append(keys, k)
		}
		m[k] = v
	}
	return m, keys, nil
}

// Dir is where the sysop's changes are kept.
func (c *Catalog) Dir() string { return c.dir }

// refresh reads the changes again if their files changed (looked at
// once a second at most).
func (c *Catalog) refresh() {
	if c.dir == "" || time.Since(c.checked) < time.Second {
		return
	}
	c.checked = time.Now()
	for _, l := range Languages {
		path := filepath.Join(c.dir, l.Code+".yaml")
		info, err := os.Stat(path)
		if err != nil {
			delete(c.overrides, l.Code)
			delete(c.stamps, l.Code)
			continue
		}
		if c.stamps[l.Code].Equal(info.ModTime()) && c.overrides[l.Code] != nil {
			continue
		}
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		m, _, err := parse(data)
		if err != nil {
			continue // a broken file: the built-in texts until it's fixed
		}
		c.overrides[l.Code] = m
		c.stamps[l.Code] = info.ModTime()
	}
}

// Text is key's text in lang, along the fallbacks, with its
// placeholders as they are. ok is false when no language has it.
func (c *Catalog) Text(lang, key string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh()
	for _, code := range Chain(lang) {
		if v, ok := c.overrides[code][key]; ok && v != "" {
			return v, true
		}
		if v, ok := c.defaults[code][key]; ok {
			return v, true
		}
	}
	return "", false
}

// T is key's text in lang with its placeholders filled: args are
// pairs of placeholder name and value ("COUNT", 3). A key no catalog
// has comes back as itself, so a missing text shows.
func (c *Catalog) T(lang, key string, args ...any) string {
	text, ok := c.Text(lang, key)
	if !ok {
		return key
	}
	return Fill(text, args...)
}

// Fill puts args (name, value pairs) into text's {NAME} placeholders.
func Fill(text string, args ...any) string {
	if len(args) < 2 || !strings.Contains(text, "{") {
		return text
	}
	pairs := make([]string, 0, len(args))
	for i := 0; i+1 < len(args); i += 2 {
		pairs = append(pairs, "{"+fmt.Sprint(args[i])+"}", fmt.Sprint(args[i+1]))
	}
	return strings.NewReplacer(pairs...).Replace(text)
}

// Keys are all the texts' keys, in the English catalog's order.
func (c *Catalog) Keys() []string { return append([]string(nil), c.keys...) }

// Default is key's built-in text in exactly lang (no fallback).
func (c *Catalog) Default(lang, key string) (string, bool) {
	v, ok := c.defaults[lang][key]
	return v, ok
}

// Overrides are the sysop's changed texts for exactly lang.
func (c *Catalog) Overrides(lang string) map[string]string {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.refresh()
	out := map[string]string{}
	for k, v := range c.overrides[lang] {
		out[k] = v
	}
	return out
}

// SetOverrides replaces the sysop's changes for lang: texts equal to
// the built-in one or empty are dropped, unknown keys refused.
func (c *Catalog) SetOverrides(lang string, texts map[string]string) error {
	if !Valid(lang) {
		return fmt.Errorf("no language %q", lang)
	}
	if c.dir == "" {
		return fmt.Errorf("no directory for changed texts")
	}
	known := map[string]bool{}
	for _, k := range c.keys {
		known[k] = true
	}
	keep := map[string]string{}
	for k, v := range texts {
		if !known[k] {
			return fmt.Errorf("no text %q", k)
		}
		if def, ok := c.defaults[lang][k]; v == "" || (ok && v == def) {
			continue
		}
		keep[k] = v
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return err
	}
	path := filepath.Join(c.dir, lang+".yaml")
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(keep) == 0 {
		if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
			return err
		}
		delete(c.overrides, lang)
		delete(c.stamps, lang)
		return nil
	}
	data, err := marshal(keep, c.keys)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	c.overrides[lang] = keep
	if info, err := os.Stat(path); err == nil {
		c.stamps[lang] = info.ModTime()
	}
	return nil
}

// marshal writes texts in the catalog's key order.
func marshal(texts map[string]string, order []string) ([]byte, error) {
	idx := map[string]int{}
	for i, k := range order {
		idx[k] = i
	}
	keys := make([]string, 0, len(texts))
	for k := range texts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return idx[keys[i]] < idx[keys[j]] })
	root := &yaml.Node{Kind: yaml.MappingNode}
	for _, k := range keys {
		root.Content = append(root.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: texts[k], Style: yaml.DoubleQuotedStyle})
	}
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{root}}
	return yaml.Marshal(doc)
}

// Placeholders are the {NAME}s in text, sorted.
func Placeholders(text string) []string {
	seen := map[string]bool{}
	out := []string{}
	for {
		i := strings.Index(text, "{")
		if i < 0 {
			break
		}
		j := strings.Index(text[i:], "}")
		if j < 0 {
			break
		}
		name := text[i+1 : i+j]
		if name != "" && strings.ToUpper(name) == name && !strings.ContainsAny(name, " {") && !seen[name] {
			seen[name] = true
			out = append(out, name)
		}
		text = text[i+j+1:]
	}
	sort.Strings(out)
	return out
}

var (
	globalMu sync.RWMutex
	global   = New("")
)

// Use makes c the catalog T reads.
func Use(c *Catalog) {
	globalMu.Lock()
	global = c
	globalMu.Unlock()
}

// Global is the catalog T reads.
func Global() *Catalog {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return global
}

// T is key's text in lang from the global catalog (see Catalog.T).
func T(lang, key string, args ...any) string { return Global().T(lang, key, args...) }
