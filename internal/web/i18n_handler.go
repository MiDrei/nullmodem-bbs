package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
)

// The language editor: the board's texts in every language, the
// sysop's changes to them, and the board's own language.

type languagesDTO struct {
	Languages []i18n.Language `json:"languages"`
	// Board is the board's language: what callers read before they log
	// in, and after if they never chose one.
	Board    string `json:"board"`
	Fallback string `json:"fallback"`
	// Changed is how many texts the sysop changed, per language.
	Changed map[string]int `json:"changed"`
}

func boardLanguage(c *config.Config) string {
	if i18n.Valid(c.BBS.Language) {
		return c.BBS.Language
	}
	return i18n.Fallback
}

// handleGetLanguages: GET /api/i18n
func (s *Server) handleGetLanguages(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	out := languagesDTO{Languages: i18n.Languages, Board: boardLanguage(c), Fallback: i18n.Fallback, Changed: map[string]int{}}
	for _, l := range i18n.Languages {
		out.Changed[l.Code] = len(i18n.Global().Overrides(l.Code))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetBoardLanguage: PUT /api/i18n/board {"language": "de"}
func (s *Server) handleSetBoardLanguage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Language string `json:"language"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || !i18n.Valid(body.Language) {
		writeError(w, http.StatusBadRequest, "no such language")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	c.BBS.Language = body.Language
	if body.Language == i18n.Fallback {
		c.BBS.Language = ""
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	s.logInfo("board language set to %s", body.Language)
	w.WriteHeader(http.StatusNoContent)
}

type textDTO struct {
	Key string `json:"key"`
	// English is the English built-in text (the fallback's).
	English string `json:"english"`
	// Builtin is the built-in text in this language -- or what it falls
	// back to (From says from which language) when it has none.
	Builtin string `json:"builtin"`
	From    string `json:"from,omitempty"`
	// Text is the sysop's own text, "" for the built-in one.
	Text         string   `json:"text"`
	Placeholders []string `json:"placeholders"`
	// Was are the keys merged into this one: where the text is used
	// besides (the editor shows it in their place).
	Was []string `json:"was,omitempty"`
}

// handleGetTexts: GET /api/i18n/{lang} -- every text in that language.
func (s *Server) handleGetTexts(w http.ResponseWriter, r *http.Request) {
	lang := r.PathValue("lang")
	if !i18n.Valid(lang) {
		writeError(w, http.StatusNotFound, "no such language")
		return
	}
	cat := i18n.Global()
	own := cat.Overrides(lang)
	merged := cat.MergedInto()
	out := make([]textDTO, 0, len(cat.Keys()))
	for _, k := range cat.Keys() {
		en, _ := cat.Default(i18n.Fallback, k)
		d := textDTO{Key: k, English: en, Text: own[k], Placeholders: i18n.Placeholders(en), Was: merged[k]}
		for _, code := range i18n.Chain(lang) {
			if v, ok := cat.Default(code, k); ok {
				d.Builtin = v
				if code != lang {
					d.From = code
				}
				break
			}
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// handlePutTexts: PUT /api/i18n/{lang} {"texts": {key: text}} -- the
// sysop's changes for that language, all of them (a text left out or
// equal to the built-in one goes back to it).
func (s *Server) handlePutTexts(w http.ResponseWriter, r *http.Request) {
	lang := r.PathValue("lang")
	if !i18n.Valid(lang) {
		writeError(w, http.StatusNotFound, "no such language")
		return
	}
	var body struct {
		Texts map[string]string `json:"texts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	cat := i18n.Global()
	for k, v := range body.Texts {
		en, ok := cat.Default(i18n.Fallback, k)
		if !ok {
			writeError(w, http.StatusBadRequest, "no text "+k)
			return
		}
		// A text may leave a placeholder out, not make one up: nothing
		// would fill it.
		have := map[string]bool{}
		for _, p := range i18n.Placeholders(en) {
			have[p] = true
		}
		if strings.HasPrefix(k, "screen.") || strings.HasPrefix(k, "menu.") {
			// Screens and menus fill in their placeholders too.
			for _, p := range screenPlaceholders {
				have[p] = true
			}
		}
		for _, p := range i18n.Placeholders(v) {
			if !have[p] {
				writeError(w, http.StatusBadRequest, k+": {"+p+"} isn't filled in here -- it can use "+placeholderList(en))
				return
			}
		}
		body.Texts[k] = strings.TrimRight(v, "\r\n")
	}
	if err := cat.SetOverrides(lang, body.Texts); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logInfo("texts in %s changed (%d of their own)", lang, len(cat.Overrides(lang)))
	w.WriteHeader(http.StatusNoContent)
}

// screenPlaceholders are what a screen or menu fills in (see
// bbs.Server.userVars), so their texts may use them.
var screenPlaceholders = []string{"BBSNAME", "SYSOP", "VERSION", "NODE", "DATE", "TIME", "USERNAME", "SL", "TOTALCALLS"}

func placeholderList(text string) string {
	ps := i18n.Placeholders(text)
	if len(ps) == 0 {
		return "none"
	}
	return "{" + strings.Join(ps, "}, {") + "}"
}
