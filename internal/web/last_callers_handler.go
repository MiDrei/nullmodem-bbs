package web

import (
	"net/http"

	"github.com/midrei/nullmodem-bbs/internal/lastcallers"
	"github.com/midrei/nullmodem-kit/ansi"
)

// lastCallerDTO is one InterBBS last caller, for the portal.
type lastCallerDTO struct {
	Alias    string `json:"alias"`
	BBS      string `json:"bbs"`
	Date     string `json:"date"`
	Time     string `json:"time"`
	Location string `json:"location"`
	System   string `json:"system"`
	Address  string `json:"address"`
}

// handleListLastCallers returns the newest InterBBS last callers from
// the configured data echo (see internal/lastcallers) -- empty when
// there's no such area yet.
func (s *Server) handleListLastCallers(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	recs, err := lastcallers.Recent(s.Messages, c.InterBBS.LastCallers.AreaTag(), 40)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the last callers")
		return
	}
	// Stored bodies are CP437, like every message (see toBBSMessageDTO).
	dec := func(v string) string { return ansi.DecodeCP437([]byte(v)) }
	out := make([]lastCallerDTO, 0, len(recs))
	for _, rec := range recs {
		out = append(out, lastCallerDTO{
			Alias: dec(rec.Alias), BBS: dec(rec.BBS), Date: rec.Date, Time: rec.Time,
			Location: dec(rec.Location), System: dec(rec.System), Address: dec(rec.Address),
		})
	}
	writeJSON(w, http.StatusOK, out)
}
