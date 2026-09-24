package web

import (
	"net/http"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/version"
)

type nodeDTO struct {
	Node        int    `json:"node"`
	RemoteIP    string `json:"remote_ip"`
	TermType    string `json:"term_type"`
	Username    string `json:"username"`
	ConnectedAt string `json:"connected_at"`
}

// binkpStatusDTO summarizes BinkP/netmail state for the dashboard --
// counts only, not full uplink details (see /binkp for those), so
// this stays cheap to compute on every dashboard poll.
type binkpStatusDTO struct {
	OwnFTNAddresses      []string `json:"own_ftn_addresses"`
	UplinkCount          int      `json:"uplink_count"`
	CrashOnlyUplinkCount int      `json:"crash_only_uplink_count"`
	// HoldUplinkCount counts config.BinkpUplink.Hold entries --
	// distinct from CrashOnlyUplinkCount (PollDisabled): a Hold uplink
	// is never auto-dialed for any reason, while a crash-only one
	// still is, for pending mail.
	HoldUplinkCount int `json:"hold_uplink_count"`
	PendingOutbound int `json:"pending_outbound"`
	PendingCrash    int `json:"pending_crash"`
}

type dashboardDTO struct {
	BBSName          string         `json:"bbs_name"`
	Version          string         `json:"version"`
	UserCount        int            `json:"user_count"`
	MessageAreaCount int            `json:"message_area_count"`
	FileAreaCount    int            `json:"file_area_count"`
	Nodes            []nodeDTO      `json:"nodes"`
	Binkp            binkpStatusDTO `json:"binkp"`
	// PendingMessageAreaCount/PendingFileAreaCount are areas an
	// inbound echomail/file-echo toss created that are still awaiting
	// a sysop's approval (see message.Area.Pending's doc comment) --
	// invisible anywhere in the BBS until then.
	PendingMessageAreaCount int `json:"pending_message_area_count"`
	PendingFileAreaCount    int `json:"pending_file_area_count"`
	// UnresolvedNetmailCount is the true total (see
	// netmail.Store.CountUnresolvedInbox), not capped at what the
	// admin's own list page displays.
	UnresolvedNetmailCount int `json:"unresolved_netmail_count"`
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

	binkp := binkpStatusDTO{OwnFTNAddresses: cfg.BBS.FTNAddresses}
	if binkp.OwnFTNAddresses == nil {
		binkp.OwnFTNAddresses = []string{}
	}
	for _, u := range cfg.Binkp.Uplinks {
		binkp.UplinkCount++
		if u.PollDisabled {
			binkp.CrashOnlyUplinkCount++
		}
		if u.Hold {
			binkp.HoldUplinkCount++
		}
	}
	if s.Netmail != nil {
		pending, err := s.Netmail.PendingOutbound()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not count pending netmail")
			return
		}
		binkp.PendingOutbound = len(pending)
		for _, m := range pending {
			if m.Crash {
				binkp.PendingCrash++
			}
		}
	}

	pendingMsgAreas, err := s.Messages.PendingAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not count pending message areas")
		return
	}
	pendingFileAreas, err := s.Files.PendingAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not count pending file areas")
		return
	}
	var unresolvedCount int
	if s.Netmail != nil {
		unresolvedCount, err = s.Netmail.CountUnresolvedInbox()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "could not count unresolved netmail")
			return
		}
	}

	writeJSON(w, http.StatusOK, dashboardDTO{
		BBSName:                 cfg.BBS.Name,
		Version:                 version.Version,
		UserCount:               userCount,
		MessageAreaCount:        messageAreaCount,
		FileAreaCount:           fileAreaCount,
		Nodes:                   nodeDTOs,
		Binkp:                   binkp,
		PendingMessageAreaCount: len(pendingMsgAreas),
		PendingFileAreaCount:    len(pendingFileAreas),
		UnresolvedNetmailCount:  unresolvedCount,
	})
}
