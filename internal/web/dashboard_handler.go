package web

import (
	"net/http"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/version"
)

type nodeDTO struct {
	Node        int    `json:"node"`
	RemoteIP    string `json:"remote_ip"`
	TermType    string `json:"term_type"`
	Username    string `json:"username"`
	ConnectedAt string `json:"connected_at"`
}

type dashboardDTO struct {
	BBSName          string    `json:"bbs_name"`
	Version          string    `json:"version"`
	UserCount        int       `json:"user_count"`
	MessageAreaCount int       `json:"message_area_count"`
	FileAreaCount    int       `json:"file_area_count"`
	Nodes            []nodeDTO `json:"nodes"`
}

func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}

	userCount, err := s.Users.Count()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not count users")
		return
	}
	messageAreaCount, err := s.Messages.CountAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not count message areas")
		return
	}
	fileAreaCount, err := s.Files.CountAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not count file areas")
		return
	}
	nodes, err := s.Nodes.List()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list active sessions")
		return
	}

	nodeDTOs := make([]nodeDTO, len(nodes))
	for i, n := range nodes {
		nodeDTOs[i] = nodeDTO{
			Node:        n.Node,
			RemoteIP:    n.RemoteIP,
			TermType:    n.TermType,
			Username:    n.Username,
			ConnectedAt: n.ConnectedAt.Format(time.RFC3339),
		}
	}

	writeJSON(w, http.StatusOK, dashboardDTO{
		BBSName:          cfg.BBS.Name,
		Version:          version.Version,
		UserCount:        userCount,
		MessageAreaCount: messageAreaCount,
		FileAreaCount:    fileAreaCount,
		Nodes:            nodeDTOs,
	})
}
