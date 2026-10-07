package bbs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// The board speaks several languages (internal/i18n): a caller reads
// it in the one they chose, else in the board's own. A screen may come
// in each one too: main.de-du.ans, main.de.ans, then main.ans, as the
// language's chain goes.

// boardLang is the board's own language.
func (s *Server) boardLang() string {
	if s.Language != nil {
		if l := s.Language(); i18n.Valid(l) {
			return l
		}
	}
	return i18n.Fallback
}

// userLang is the language u reads the board in.
func (s *Server) userLang(u *user.User) string {
	if i18n.Valid(u.Language) {
		return u.Language
	}
	return s.boardLang()
}

// loadScreen is screen name in lang (see i18n.ScreenNames), its
// {T:key} placeholders filled in that language (i18n.FillScreen).
func (s *Server) loadScreen(lang, name string) (string, error) {
	var firstErr error
	for _, n := range i18n.ScreenNames(name, lang) {
		raw, err := ansi.LoadScreen(filepath.Join(s.ScreensDir, n))
		if err == nil {
			return i18n.FillScreen(lang, raw), nil
		}
		if firstErr == nil || !os.IsNotExist(err) {
			firstErr = err
		}
	}
	return "", firstErr
}

// isYes reports whether answer (upper case) means yes: Y, or the
// caller's language's own yes key.
func isYes(term *Terminal, answer string) bool {
	return answer == "Y" || answer == strings.ToUpper(term.T("common.yes_key"))
}

// askLoginLanguage lets a caller pick a language before logging in,
// so the handle prompt and a new account's questions come in one they
// read -- a key from the list, Enter keeps the board's. The choice is a
// new account's language; an existing one's own takes over at login.
func (s *Server) askLoginLanguage(term *Terminal) error {
	key := func(n int) string {
		return ansi.FG(ansi.Black, true) + "[" + ansi.FG(ansi.Cyan, true) + fmt.Sprint(n) + ansi.FG(ansi.Black, true) + "] "
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n\r\n  " + ansi.FG(ansi.White, true) + term.T("lang.title"))
	for i, l := range i18n.Languages {
		b.WriteString("  " + key(i+1) + ansi.FG(ansi.White, true) + toCP437(l.Name))
		if l.Code == term.Lang {
			b.WriteString(ansi.FG(ansi.Cyan, true) + " *")
		}
	}
	b.WriteString("\r\n  " + keyHints(term.T("lang.enter_keeps", "LANGUAGE", toCP437(i18n.NameOf(term.Lang)))) +
		ansi.FG(ansi.Blue, true) + " " + toCP437("\u00bb") + " " + ansi.Reset)
	if err := term.Print(b.String()); err != nil {
		return err
	}
	for {
		k, err := term.ReadKey()
		if err != nil {
			return err
		}
		if k.Type == KeyEnter {
			break
		}
		if n := int(k.Rune - '0'); k.Type == KeyChar && n >= 1 && n <= len(i18n.Languages) {
			term.Lang = i18n.Languages[n-1].Code
			break
		}
	}
	term.LangChosen = true
	return term.Print(toCP437(i18n.NameOf(term.Lang)) + "\r\n\r\n")
}

// chooseLanguage asks for a language; Enter keeps the current one.
func (s *Server) chooseLanguage(term *Terminal) (string, error) {
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n" + ansi.FG(ansi.Cyan, true) + term.T("lang.title") + ansi.Reset + "\r\n")
	for i, l := range i18n.Languages {
		mark := ""
		if l.Code == term.Lang {
			mark = ansi.FG(ansi.Green, false) + " *"
		}
		fmt.Fprintf(&b, "  %s[%d]%s %s%s%s\r\n", ansi.FG(ansi.Yellow, true), i+1, ansi.FG(ansi.White, true), toCP437(l.Name), mark, ansi.Reset)
	}
	b.WriteString(term.T("lang.prompt") + " " + ansi.FG(ansi.Yellow, true))
	if err := term.Print(b.String()); err != nil {
		return "", err
	}
	answer, err := term.ReadLine(false)
	if err != nil {
		return "", err
	}
	term.Print(ansi.Reset)
	answer = strings.TrimSpace(answer)
	for i, l := range i18n.Languages {
		if answer == fmt.Sprint(i+1) || strings.EqualFold(answer, l.Code) {
			return l.Code, nil
		}
	}
	return term.Lang, nil
}

// changeLanguage is the profile's language option.
func (s *Server) changeLanguage(term *Terminal, u *user.User) error {
	lang, err := s.chooseLanguage(term)
	if err != nil {
		return err
	}
	if lang == term.Lang && u.Language != "" {
		return nil
	}
	if err := s.Users.SetLanguage(u.ID, lang); err != nil {
		return err
	}
	u.Language, term.Lang = lang, lang
	return term.Println(ansi.FG(ansi.Green, true) + term.T("lang.changed", "LANGUAGE", toCP437(i18n.NameOf(lang))) + ansi.Reset)
}
