package web

import (
	"net/http"
	"sort"
)

// handleListGroups returns every distinct "network" value already in
// use across message and file areas combined, sorted -- what the web
// admin's area forms offer as suggestions for that field (labeled
// "Group" in the UI: grouping areas isn't limited to a strict FTN
// network the way the underlying field name suggests).
func (s *Server) handleListGroups(w http.ResponseWriter, r *http.Request) {
	msgNetworks, err := s.Messages.Networks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list message area groups")
		return
	}
	fileNetworks, err := s.Files.Networks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list file area groups")
		return
	}

	seen := make(map[string]bool, len(msgNetworks)+len(fileNetworks))
	var groups []string
	for _, n := range msgNetworks {
		if !seen[n] {
			seen[n] = true
			groups = append(groups, n)
		}
	}
	for _, n := range fileNetworks {
		if !seen[n] {
			seen[n] = true
			groups = append(groups, n)
		}
	}
	sort.Strings(groups)
	if groups == nil {
		groups = []string{}
	}

	writeJSON(w, http.StatusOK, groups)
}
