package menu

import (
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
)

// In is m as a caller reading lang sees it: each title and label in
// that language where the sysop wrote one (Titles, Labels), else --
// for a text still as shipped -- the catalog's translation of it,
// else as it is.
func (m *Menu) In(lang string) *Menu {
	if lang == "" || lang == i18n.Fallback {
		return m
	}
	out := *m
	out.Title = translate(m.Title, m.Titles, lang, "menu.title.")
	out.Items = make([]Item, len(m.Items))
	for i, it := range m.Items {
		it.Label = translate(it.Label, it.Labels, lang, "menu.item.")
		out.Items[i] = it
	}
	return &out
}

func translate(text string, own map[string]string, lang, prefix string) string {
	chain := i18n.Chain(lang)
	for _, code := range chain {
		if code == i18n.Fallback {
			break
		}
		if v := strings.TrimSpace(own[code]); v != "" {
			return v
		}
	}
	if key := shippedKey(text, prefix); key != "" {
		return i18n.T(lang, key)
	}
	return text
}

// shippedKey is the catalog key whose English built-in text is text
// (a stock menu's title or label), or "".
func shippedKey(text, prefix string) string {
	c := i18n.Global()
	for _, k := range c.Keys() {
		if !strings.HasPrefix(k, prefix) {
			continue
		}
		if en, ok := c.Default(i18n.Fallback, k); ok && en == text {
			return k
		}
	}
	return ""
}
