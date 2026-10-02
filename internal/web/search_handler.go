package web

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/message"
)

// Message search for the portal and the reader.

type searchHitDTO struct {
	ID       int64  `json:"id"`
	AreaID   int64  `json:"area_id"`
	AreaTag  string `json:"area_tag"`
	AreaName string `json:"area_name"`
	Subject  string `json:"subject"`
	FromName string `json:"from_name"`
	ToName   string `json:"to_name"`
	PostedAt string `json:"posted_at"`
	// Snippet is the text around the first match (or its beginning).
	Snippet string `json:"snippet"`
}

var ansiSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// snippet is about 140 characters of body around q.
func snippet(body, q string) string {
	var lines []string
	for _, l := range strings.Split(message.StripSeenByAndPathForDisplay(body), "\n") {
		l = strings.TrimSpace(ansiSeq.ReplaceAllString(strings.TrimRight(l, "\r"), ""))
		// Not the kludges, tearline and origin a tosser adds.
		if l == "" || strings.HasPrefix(l, "\x01") || l == "---" || strings.HasPrefix(l, "--- ") || strings.HasPrefix(l, "* Origin:") {
			continue
		}
		lines = append(lines, l)
	}
	text := []rune(strings.Join(lines, " "))
	at := strings.Index(strings.ToLower(string(text)), strings.ToLower(q))
	if at < 0 {
		at = 0
	} else {
		at = len([]rune(string(text)[:at]))
	}
	from := max(0, at-50)
	to := min(len(text), from+140)
	out := string(text[from:to])
	if from > 0 {
		out = "…" + out
	}
	if to < len(text) {
		out += "…"
	}
	return out
}

// handleSearchBBSMessages: GET /api/bbs/messages/search?q=&area_id=.
func (s *Server) handleSearchBBSMessages(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len([]rune(q)) < 2 {
		writeError(w, http.StatusBadRequest, "search for at least two characters")
		return
	}
	areaID, _ := strconv.ParseInt(r.URL.Query().Get("area_id"), 10, 64)
	// Stored text is CP437: so is what's searched for.
	qCP := string(ansi.EncodeCP437(q))
	msgs, err := s.Messages.Search(claims.SecurityLevel, qCP, areaID, 100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not search")
		return
	}
	areas := map[int64]*message.Area{}
	out := make([]searchHitDTO, 0, len(msgs))
	for _, m := range msgs {
		a, ok := areas[m.AreaID]
		if !ok {
			if a, _ = s.Messages.AreaByID(m.AreaID); a == nil {
				a = &message.Area{ID: m.AreaID}
			}
			areas[m.AreaID] = a
		}
		dec := func(v string) string { return ansi.DecodeCP437([]byte(v)) }
		out = append(out, searchHitDTO{
			ID: m.ID, AreaID: m.AreaID, AreaTag: a.Tag, AreaName: a.Name,
			Subject: dec(m.Subject), FromName: dec(m.FromName), ToName: dec(m.ToName),
			PostedAt: m.PostedAt.Format(time.RFC3339), Snippet: snippet(dec(m.Body), q),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
