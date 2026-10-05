package web

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
)

// The portal, the reader app and the front page in the visitor's
// language: their texts are the catalog's web.* keys, put into the
// page itself (no flash of English) and served for a switch; the
// errors of the callers' API come back in that language too.

// langCookie remembers a visitor's choice (the front page's or the
// portal's language picker), and the account's language once logged in.
const langCookie = "nm_lang"

// requestLang is the language to answer r in: what the page says it
// shows (X-Lang), the visitor's choice (cookie), their browser's
// languages -- German formal or informal as the board speaks it --
// else the board's own.
func (s *Server) requestLang(r *http.Request) string {
	if l := r.Header.Get("X-Lang"); i18n.Valid(l) {
		return l
	}
	if c, err := r.Cookie(langCookie); err == nil && i18n.Valid(c.Value) {
		return c.Value
	}
	board := i18n.Fallback
	if c, err := s.loadBBSConfig(); err == nil {
		board = boardLanguage(c)
	}
	for _, part := range strings.Split(r.Header.Get("Accept-Language"), ",") {
		tag := strings.ToLower(strings.TrimSpace(strings.SplitN(part, ";", 2)[0]))
		base := strings.SplitN(tag, "-", 2)[0]
		switch {
		case base == "":
		case strings.HasPrefix(board, base):
			return board // the board's variant of it (de-du for a de-du board)
		case i18n.Valid(base):
			return base
		}
	}
	return board
}

// webTexts are the web's texts in lang, fallbacks and the sysop's
// changes applied: web.* and the ones shared with Telnet (common.*) --
// with the admin's (admin.*) too for its pages.
func webTexts(lang string, admin bool) map[string]string {
	cat := i18n.Global()
	out := map[string]string{}
	for _, k := range cat.Keys() {
		if strings.HasPrefix(k, "web.") || strings.HasPrefix(k, "common.") || admin && strings.HasPrefix(k, "admin.") {
			out[k], _ = cat.Text(lang, k)
		}
	}
	return out
}

type webTextsDTO struct {
	Lang  string            `json:"lang"`
	Texts map[string]string `json:"texts"`
	// Languages are the ones to choose from.
	Languages []i18n.Language `json:"languages"`
}

func webTextsFor(lang string, admin bool) webTextsDTO {
	return webTextsDTO{Lang: lang, Texts: webTexts(lang, admin), Languages: i18n.Languages}
}

// handlePublicTexts: GET /api/i18n-texts/{lang} -- the web's texts,
// for switching language without reloading ("auto": the request's).
func (s *Server) handlePublicTexts(w http.ResponseWriter, r *http.Request) {
	lang := r.PathValue("lang")
	if !i18n.Valid(lang) {
		lang = s.requestLang(r)
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, webTextsFor(lang, r.URL.Query().Get("admin") != ""))
}

// textsScript is the <script> that hands the page its texts.
func (s *Server) textsScript(r *http.Request) string {
	data, err := json.Marshal(webTextsFor(s.requestLang(r), strings.HasPrefix(r.URL.Path, "/admin")))
	if err != nil {
		return ""
	}
	// json.Marshal escapes <, > and &, so the data can't end the script.
	return `<script>window.__NM_I18N=` + string(data) + `;</script>`
}

// apiErrorKeys maps an API error's English text (lower case) to its
// catalog key (the api.* texts).
func apiErrorKeys() map[string]string {
	cat := i18n.Global()
	out := map[string]string{}
	for _, k := range cat.Keys() {
		if strings.HasPrefix(k, "api.") {
			if en, ok := cat.Default(i18n.Fallback, k); ok {
				out[strings.ToLower(en)] = k
			}
		}
	}
	return out
}

// localizeErrors translates the {"error": "..."} of the API into the
// request's language, where the catalog has the message.
func (s *Server) localizeErrors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// A WebSocket (the browser terminal) takes the connection over.
		if !strings.HasPrefix(r.URL.Path, "/api/") || r.Header.Get("Upgrade") != "" {
			next.ServeHTTP(w, r)
			return
		}
		lang := s.requestLang(r)
		if lang == i18n.Fallback {
			next.ServeHTTP(w, r)
			return
		}
		lw := &errorWriter{ResponseWriter: w}
		next.ServeHTTP(lw, r)
		lw.finish(lang)
	})
}

// errorWriter holds back an error response's body to translate it.
type errorWriter struct {
	http.ResponseWriter
	status int
	held   bytes.Buffer
}

func (e *errorWriter) WriteHeader(code int) {
	e.status = code
	if code < 400 {
		e.ResponseWriter.WriteHeader(code)
	}
}

func (e *errorWriter) Write(b []byte) (int, error) {
	if e.status == 0 {
		e.WriteHeader(http.StatusOK)
	}
	if e.status < 400 {
		return e.ResponseWriter.Write(b)
	}
	return e.held.Write(b)
}

// Unwrap lets http.ResponseController reach the connection.
func (e *errorWriter) Unwrap() http.ResponseWriter { return e.ResponseWriter }

// Flush lets a streamed answer through.
func (e *errorWriter) Flush() {
	if f, ok := e.ResponseWriter.(http.Flusher); ok && e.status < 400 {
		f.Flush()
	}
}

func (e *errorWriter) finish(lang string) {
	if e.status < 400 {
		return
	}
	body := e.held.Bytes()
	var msg map[string]any
	if json.Unmarshal(body, &msg) == nil {
		if text, ok := msg["error"].(string); ok {
			if key, ok := apiErrorKeys()[strings.ToLower(text)]; ok {
				msg["error"] = i18n.T(lang, key)
				if b, err := json.Marshal(msg); err == nil {
					body = append(b, '\n')
				}
			}
		}
	}
	e.ResponseWriter.Header().Del("Content-Length")
	e.ResponseWriter.WriteHeader(e.status)
	e.ResponseWriter.Write(body)
}
