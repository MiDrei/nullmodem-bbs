package web

import (
	"encoding/json"
	"net/http"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/services"
)

// serviceDTO is one daemon on the admin's Services page.
type serviceDTO struct {
	Name           string   `json:"name"`
	Running        bool     `json:"running"`
	Version        string   `json:"version"`
	PID            int      `json:"pid"`
	StartedAt      string   `json:"started_at,omitempty"`
	HeartbeatAt    string   `json:"heartbeat_at,omitempty"`
	RestartPending bool     `json:"restart_pending"`
	RestartMode    string   `json:"restart_mode,omitempty"`
	RestartNeeded  []string `json:"restart_needed"`
	// Online is how many callers the bbs daemon has connected (bbs only).
	Online int `json:"online"`
}

func iso(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// markRestartNeeded records that daemon name only picks up a change
// (reason) after a restart. Nil-safe; a failure is logged, not fatal --
// the change itself is saved either way.
func (s *Server) markRestartNeeded(reason string, names ...string) {
	if s.Services == nil {
		return
	}
	for _, n := range names {
		if err := s.Services.MarkRestartNeeded(n, reason); err != nil {
			s.logWarn("marking %s for a restart: %v", n, err)
		}
	}
}

func (s *Server) handleListServices(w http.ResponseWriter, r *http.Request) {
	if s.Services == nil {
		writeJSON(w, http.StatusOK, []serviceDTO{})
		return
	}
	list, err := s.Services.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list services")
		return
	}
	online := 0
	if s.Nodes != nil {
		if nodes, err := s.Nodes.List(); err == nil {
			online = len(nodes)
		}
	}
	out := make([]serviceDTO, len(list))
	for i, st := range list {
		needed := st.RestartNeeded
		if needed == nil {
			needed = []string{}
		}
		out[i] = serviceDTO{
			Name:           st.Name,
			Running:        st.Running(),
			Version:        st.Version,
			PID:            st.PID,
			StartedAt:      iso(st.StartedAt),
			HeartbeatAt:    iso(st.HeartbeatAt),
			RestartPending: st.RestartPending(),
			RestartMode:    st.RestartMode,
			RestartNeeded:  needed,
		}
		if st.Name == services.BBS {
			out[i].Online = online
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleRestartService asks a daemon to restart -- it exits on its own
// and Docker starts it again (see internal/services).
func (s *Server) handleRestartService(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if s.Services == nil {
		writeError(w, http.StatusNotFound, "no such service")
		return
	}
	if known, err := s.Services.Known(name); err != nil || !known {
		writeError(w, http.StatusNotFound, "no such service")
		return
	}
	var body struct {
		Mode string `json:"mode"`
	}
	json.NewDecoder(r.Body).Decode(&body)
	mode := body.Mode
	if mode == "" {
		mode = services.ModeNow
	}
	if mode != services.ModeNow && !(mode == services.ModeIdle && name == services.BBS) {
		writeError(w, http.StatusBadRequest, "mode must be \"now\" (or \"idle\" for bbs)")
		return
	}
	if err := s.Services.RequestRestart(name, mode); err != nil {
		writeError(w, http.StatusInternalServerError, "could not request the restart")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s asked %s to restart (%s)", claims.Subject, name, mode)
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "requested"})
}
