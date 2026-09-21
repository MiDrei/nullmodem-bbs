package web

import "net/http"

// handleBBSInfo reports the BBS's own configured display name --
// deliberately unauthenticated (no requireAuth/requireBBSUser), since
// both the admin login page and the BBS portal login page need to
// show it before anyone has a token, and it's no more sensitive than
// what the Telnet/SSH welcome screen already shows any caller before
// they log in.
func (s *Server) handleBBSInfo(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"name": c.BBS.Name})
}
