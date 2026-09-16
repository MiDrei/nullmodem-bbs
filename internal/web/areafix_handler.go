package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/tosser"
)

// areaChangeDTO is one area a POST /api/binkp/areafix/changes request
// wants added (subscribe: true) or dropped (false).
type areaChangeDTO struct {
	AreaTag   string `json:"area_tag"`
	Subscribe bool   `json:"subscribe"`
}

// areafixChangesRequestDTO is the request body for POST
// /api/binkp/areafix/changes: the calling uplink's connection details
// (mirroring binkpUplinkDTO -- enough to compose and route the
// request whether or not this uplink has already been saved to
// config, the same allowance handleSendNowBinkp makes), every area to
// (un)subscribe in one batch, and which robot to address ("echo" for
// Areafix, "file" for Filefix).
type areafixChangesRequestDTO struct {
	Uplink  binkpUplinkDTO  `json:"uplink"`
	Changes []areaChangeDTO `json:"changes"`
	Kind    string          `json:"kind"`
}

func areafixUplinkFromDTO(dto binkpUplinkDTO) config.BinkpUplink {
	return config.BinkpUplink{
		Address:         dto.Address,
		Host:            dto.Host,
		AreafixPassword: dto.AreafixPassword,
		FilefixPassword: dto.FilefixPassword,
	}
}

// handleRequestAreafixChanges queues a single Crash-priority netmail
// asking the given uplink's Areafix (echo) or Filefix (file-echo)
// robot to add/drop every area in the batch, and records each request
// (see internal/tosser's RequestEchoAreaChanges/
// RequestFileAreaChanges) -- sent the same way any other queued
// netmail is, via cmd/mailer's universal crash-trigger, typically
// within its 5-minute throttle window rather than immediately from
// this request.
func (s *Server) handleRequestAreafixChanges(w http.ResponseWriter, r *http.Request) {
	var req areafixChangesRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Uplink.Host) == "" || strings.TrimSpace(req.Uplink.Address) == "" {
		writeError(w, http.StatusBadRequest, "uplink host and address must not be empty")
		return
	}
	if len(req.Changes) == 0 {
		writeError(w, http.StatusBadRequest, "changes must not be empty")
		return
	}
	changes := make([]tosser.AreaChange, len(req.Changes))
	for i, c := range req.Changes {
		tag := strings.TrimSpace(c.AreaTag)
		if tag == "" {
			writeError(w, http.StatusBadRequest, "every change needs a non-empty area_tag")
			return
		}
		changes[i] = tosser.AreaChange{Tag: tag, Subscribe: c.Subscribe}
	}
	if s.Netmail == nil || s.EchoAreafix == nil || s.FileAreafix == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if len(c.BBS.FTNAddresses) == 0 {
		writeError(w, http.StatusBadRequest, "set this system's own FTN address above before sending")
		return
	}

	uplink := areafixUplinkFromDTO(req.Uplink)

	var msg *netmail.Message
	if req.Kind == "file" {
		msg, err = tosser.RequestFileAreaChanges(s.Netmail, s.FileAreafix, c.BBS.FTNAddresses, uplink, changes)
	} else {
		msg, err = tosser.RequestEchoAreaChanges(s.Netmail, s.EchoAreafix, c.BBS.FTNAddresses, uplink, changes)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("areafix request failed: %v", err))
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s requested %d area change(s) on %s via %s", claims.Subject, len(changes), req.Uplink.Host, req.Kind)
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued_message_id": msg.ID})
}

// areafixListRequestDTO is the request body for POST
// /api/binkp/areafix/list.
type areafixListRequestDTO struct {
	Uplink binkpUplinkDTO `json:"uplink"`
	Kind   string         `json:"kind"`
}

// handleRequestAreafixList queues a "%LIST" request to the given
// uplink's Areafix/Filefix robot -- see tosser.RequestEchoAreaList/
// RequestFileAreaList. The reply arrives later as ordinary inbound
// netmail; poll handleGetAreafixListReply for it.
func (s *Server) handleRequestAreafixList(w http.ResponseWriter, r *http.Request) {
	var req areafixListRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Uplink.Host) == "" || strings.TrimSpace(req.Uplink.Address) == "" {
		writeError(w, http.StatusBadRequest, "uplink host and address must not be empty")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if len(c.BBS.FTNAddresses) == 0 {
		writeError(w, http.StatusBadRequest, "set this system's own FTN address above before sending")
		return
	}

	uplink := areafixUplinkFromDTO(req.Uplink)

	var msg *netmail.Message
	if req.Kind == "file" {
		msg, err = tosser.RequestFileAreaList(s.Netmail, c.BBS.FTNAddresses, uplink)
	} else {
		msg, err = tosser.RequestEchoAreaList(s.Netmail, c.BBS.FTNAddresses, uplink)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("areafix list request failed: %v", err))
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s requested the area list from %s via %s", claims.Subject, req.Uplink.Host, req.Kind)
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued_message_id": msg.ID})
}

// listReplyLookbackLimit bounds how many of an uplink's most recent
// inbound netmail messages handleGetAreafixListReply scans for the
// latest one that looks like a "%LIST" reply -- generous, since
// there's no reliable marker distinguishing a list reply from any
// other netmail this uplink's address happens to send.
const listReplyLookbackLimit = 20

// handleGetAreafixListReply returns whichever of the uplink's (host+
// address, via the "host" query parameter to identify which uplink,
// matched against its FTN address) most recent inbound netmail
// messages parses as the richest Areafix/Filefix "%LIST" reply (see
// areafix.ParseAreaListReply) -- both the parsed entries (for the web
// UI's checkbox list) and the raw body (since the parse is best-
// effort and hub software varies, so the sysop can sanity-check or
// fall back to typing tags by hand).
//
// Picking by parsed-area count rather than just the newest message
// matters because a real hub (confirmed live, "Clearing Houz") sends
// a "%LIST" reply as TWO separate netmail messages with the identical
// timestamp -- one actually listing the areas, one just confirming
// the command was received ("COMMAND PROCESSED") -- so "most recent"
// alone picked the empty confirmation as often as the real list.
func (s *Server) handleGetAreafixListReply(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" {
		writeError(w, http.StatusBadRequest, "address query parameter is required")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}

	msgs, err := s.Netmail.InboxFromAddress(address, listReplyLookbackLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read inbound netmail")
		return
	}
	if len(msgs) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"found": false})
		return
	}

	best := msgs[0]
	bestAreas := areafix.ParseAreaListReply(best.Body)
	for _, m := range msgs[1:] {
		if areas := areafix.ParseAreaListReply(m.Body); len(areas) > len(bestAreas) {
			best, bestAreas = m, areas
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"found":     true,
		"posted_at": best.PostedAt,
		"subject":   best.Subject,
		"raw_body":  best.Body,
		"areas":     bestAreas,
	})
}

// handleListAreafixSubscriptions returns the area tags this system
// has itself outbound-requested (see areafix.Outbound) from the
// uplink named by the "host" query parameter -- this system's own
// record of what it's asked for, distinct from (and not dependent on)
// handleGetAreafixListReply's best-effort parse of the hub's actual
// reply. "kind" (echo, the default, or file) selects Areafix vs
// Filefix subscriptions.
func (s *Server) handleListAreafixSubscriptions(w http.ResponseWriter, r *http.Request) {
	host := strings.TrimSpace(r.URL.Query().Get("host"))
	if host == "" {
		writeError(w, http.StatusBadRequest, "host query parameter is required")
		return
	}
	if s.EchoAreafix == nil || s.FileAreafix == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}

	var subs []areafix.Subscription
	var err error
	if r.URL.Query().Get("kind") == "file" {
		subs, err = s.FileAreafix.ListForUplink(host, areafix.Outbound)
	} else {
		subs, err = s.EchoAreafix.ListForUplink(host, areafix.Outbound)
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list subscriptions")
		return
	}

	tags := make([]string, len(subs))
	for i, sub := range subs {
		tags[i] = sub.AreaTag
	}
	writeJSON(w, http.StatusOK, map[string]any{"area_tags": tags})
}
