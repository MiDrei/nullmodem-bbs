package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/community"
)

// newsDTO is a news item as the admin edits it: expires_at a date
// (YYYY-MM-DD, through the end of that day) or empty for never.
type newsDTO struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`
	TitleEN   string    `json:"title_en"`
	TextEN    string    `json:"text_en"`
	TitleDE   string    `json:"title_de"`
	TextDE    string    `json:"text_de"`
	ExpiresAt string    `json:"expires_at"`
	Expired   bool      `json:"expired"`
}

func toNewsDTO(n community.News) newsDTO {
	d := newsDTO{ID: n.ID, CreatedAt: n.CreatedAt, Author: n.Author, TitleEN: n.TitleEN, TextEN: n.TextEN,
		TitleDE: n.TitleDE, TextDE: n.TextDE, Expired: n.Expired(time.Now())}
	if !n.ExpiresAt.IsZero() {
		d.ExpiresAt = n.ExpiresAt.Add(-time.Millisecond).Format("2006-01-02")
	}
	return d
}

// newsReady answers 503 when the board has no community store.
func (s *Server) newsReady(w http.ResponseWriter) bool {
	if s.Community == nil {
		writeError(w, http.StatusServiceUnavailable, "not available")
		return false
	}
	return true
}

// handleListNews: GET /api/news (admin) -- every news item, the
// expired ones too, newest first.
func (s *Server) handleListNews(w http.ResponseWriter, r *http.Request) {
	if !s.newsReady(w) {
		return
	}
	news, err := s.Community.AllNews()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the news")
		return
	}
	out := []newsDTO{}
	for _, n := range news {
		out = append(out, toNewsDTO(n))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSaveNews: POST /api/news (a new item) and PUT /api/news/{id}
// (admin).
func (s *Server) handleSaveNews(w http.ResponseWriter, r *http.Request) {
	if !s.newsReady(w) {
		return
	}
	var req newsDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	n := community.News{TitleEN: req.TitleEN, TextEN: req.TextEN, TitleDE: req.TitleDE, TextDE: req.TextDE}
	if r.Method == http.MethodPut {
		id, ok := pathID(w, r)
		if !ok {
			return
		}
		n.ID = id
	} else if claims, ok := claimsFromContext(r.Context()); ok {
		n.Author = claims.Subject
	}
	if d := strings.TrimSpace(req.ExpiresAt); d != "" {
		day, err := time.ParseInLocation("2006-01-02", d, time.Local)
		if err != nil {
			writeError(w, http.StatusBadRequest, "the expiry date must be YYYY-MM-DD")
			return
		}
		n.ExpiresAt = day.AddDate(0, 0, 1)
	}
	id, err := s.Community.SaveNews(n)
	switch {
	case errors.Is(err, community.ErrNewsEmpty):
		writeError(w, http.StatusBadRequest, "news needs a title and a text, in English or German")
		return
	case errors.Is(err, community.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such news")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not save the news")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s saved the news #%d", claims.Subject, id)
	}
	s.handleListNews(w, r)
}

// handleDeleteNews: DELETE /api/news/{id} (admin).
func (s *Server) handleDeleteNews(w http.ResponseWriter, r *http.Request) {
	if !s.newsReady(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Community.DeleteNews(id); err != nil {
		if errors.Is(err, community.ErrNotFound) {
			writeError(w, http.StatusNotFound, "no such news")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not delete the news")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted the news #%d", claims.Subject, id)
	}
	s.handleListNews(w, r)
}

// publicNewsDTO is a news item in the reader's language.
type publicNewsDTO struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Title     string    `json:"title"`
	Text      string    `json:"text"`
}

// handlePublicNews: GET /api/public/news (no login) -- the latest
// three news items, in the visitor's language.
func (s *Server) handlePublicNews(w http.ResponseWriter, r *http.Request) {
	out := []publicNewsDTO{}
	if s.Community != nil {
		news, err := s.Community.ActiveNews(time.Now(), 3)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not load the news")
			return
		}
		lang := s.requestLang(r)
		for _, n := range news {
			title, text := n.In(lang)
			out = append(out, publicNewsDTO{ID: n.ID, CreatedAt: n.CreatedAt, Title: title, Text: text})
		}
	}
	writeJSON(w, http.StatusOK, out)
}
