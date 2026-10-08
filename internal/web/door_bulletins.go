package web

import (
	"net/http"
	"sort"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/doors"
)

// The doors' bulletins (scoreboards, news) for the portal, and the
// public ones for the front page.

type doorBulletinDTO struct {
	Door    string    `json:"door"`
	Title   string    `json:"title"`
	Updated time.Time `json:"updated"`
	Grid    ansi.Grid `json:"grid"`
	Public  bool      `json:"public"`
}

// doorBulletins are the bulletins the doors have written, of the doors
// minSL may play (-1: every door), only public ones if publicOnly.
func (s *Server) doorBulletins(c *config.Config, sl int, publicOnly bool) []doorBulletinDTO {
	out := []doorBulletinDTO{}
	for _, d := range c.Doors {
		if sl >= 0 && d.MinSL > sl {
			continue
		}
		dir := d.Dir
		if d.Kind == "dosbox" {
			dir = d.DOSBoxDir
		}
		for _, b := range d.Bulletins {
			if publicOnly && !b.Public {
				continue
			}
			data, at, err := doors.ReadBulletin(dir, b.File)
			if err != nil {
				continue // not written yet
			}
			out = append(out, doorBulletinDTO{Door: d.Name, Title: b.Title, Updated: at, Public: b.Public,
				Grid: ansi.ParseGrid(doors.BulletinText(data, artWidth), artWidth)})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Public && !out[j].Public })
	return out
}

// normalizeNewlines makes bare LF (Linux doors) CR LF, as ANSI expects.
func normalizeNewlines(s string) string {
	out := make([]byte, 0, len(s)+len(s)/40)
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' && (i == 0 || s[i-1] != '\r') {
			out = append(out, '\r')
		}
		out = append(out, s[i])
	}
	return string(out)
}

// handleBBSDoorBulletins: GET /api/bbs/door-bulletins.
func (s *Server) handleBBSDoorBulletins(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	claims, _ := claimsFromContext(r.Context())
	writeJSON(w, http.StatusOK, s.doorBulletins(c, claims.SecurityLevel, false))
}

// handlePublicDoorBulletins: GET /api/public/door-bulletins.
func (s *Server) handlePublicDoorBulletins(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, s.doorBulletins(c, -1, true))
}
