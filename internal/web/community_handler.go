package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/midrei/nullmodem-bbs/internal/community"
	"github.com/midrei/nullmodem-bbs/internal/nodelist"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

// Nodelists, polls and the BBS list: for callers in the portal (and
// reader), and for the sysop in the admin.

func pathID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return 0, false
	}
	return id, true
}

func (s *Server) communityReady(w http.ResponseWriter) bool {
	if s.Community == nil || s.Nodelist == nil {
		writeError(w, http.StatusServiceUnavailable, "not available")
		return false
	}
	return true
}

// ---- Nodelist ----

type nodelistEntryDTO struct {
	nodelist.Entry
	Address string `json:"address"`
	Host    string `json:"host"`
}

func toNodelistDTOs(es []nodelist.Entry) []nodelistEntryDTO {
	out := make([]nodelistEntryDTO, len(es))
	for i, e := range es {
		out[i] = nodelistEntryDTO{Entry: e, Address: e.Address(), Host: e.Host()}
	}
	return out
}

// handleSearchNodelist: GET /api/bbs/nodelist?q=&network=.
func (s *Server) handleSearchNodelist(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	q := r.URL.Query()
	found, err := s.Nodelist.Search(q.Get("network"), q.Get("q"), 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not search the nodelist")
		return
	}
	imports, _ := s.Nodelist.Imports()
	writeJSON(w, http.StatusOK, map[string]any{"entries": toNodelistDTOs(found), "imports": imports})
}

// handleLookupNodelist: GET /api/bbs/nodelist/lookup?addr= -- 404 if no
// nodelist has it.
func (s *Server) handleLookupNodelist(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	e, ok, err := s.Nodelist.LookupAddress(r.URL.Query().Get("addr"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not look it up")
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not in the nodelists")
		return
	}
	writeJSON(w, http.StatusOK, toNodelistDTOs([]nodelist.Entry{e})[0])
}

// handleNodelistStatus: GET /api/nodelists (admin).
func (s *Server) handleNodelistStatus(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	imports, err := s.Nodelist.Imports()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the nodelists")
		return
	}
	writeJSON(w, http.StatusOK, imports)
}

// handleNodelistSync: POST /api/nodelists/sync (admin) -- imports every
// network's newest nodelist file again.
func (s *Server) handleNodelistSync(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	n, err := s.Nodelist.Sync(true, s.Logger)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "the import failed (see the log)")
		return
	}
	imports, _ := s.Nodelist.Imports()
	writeJSON(w, http.StatusOK, map[string]any{"imported": n, "imports": imports})
}

// ---- Polls ----

// handleListBBSPolls: GET /api/bbs/polls -- open and closed, with the
// caller's votes.
func (s *Server) handleListBBSPolls(w http.ResponseWriter, r *http.Request) {
	claims, _ := claimsFromContext(r.Context())
	if !s.communityReady(w) {
		return
	}
	polls, err := s.Community.Polls(claims.UserID, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the polls")
		return
	}
	writeJSON(w, http.StatusOK, polls)
}

// handleVoteBBSPoll: POST /api/bbs/polls/{id}/vote {option_id}.
func (s *Server) handleVoteBBSPoll(w http.ResponseWriter, r *http.Request) {
	claims, _ := claimsFromContext(r.Context())
	if !s.communityReady(w) || s.pendingApproval(w, claims.UserID) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		OptionID int64 `json:"option_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch err := s.Community.Vote(id, claims.UserID, req.OptionID); {
	case errors.Is(err, community.ErrNotFound):
		writeError(w, http.StatusNotFound, "no such poll or option")
		return
	case errors.Is(err, community.ErrClosed):
		writeError(w, http.StatusConflict, "the poll is closed")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not vote")
		return
	}
	p, err := s.Community.Poll(id, claims.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the poll")
		return
	}
	writeJSON(w, http.StatusOK, p)
}

// handleListPolls: GET /api/polls (admin).
func (s *Server) handleListPolls(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	polls, err := s.Community.Polls(0, true)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the polls")
		return
	}
	writeJSON(w, http.StatusOK, polls)
}

// handleCreatePoll: POST /api/polls {question, options} (admin).
func (s *Server) handleCreatePoll(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	var req struct {
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	id, err := s.Community.CreatePoll(req.Question, req.Options)
	if err != nil {
		writeError(w, http.StatusBadRequest, "a poll needs a question and 2 to 10 options")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s started the poll %q", claims.Subject, req.Question)
	}
	p, _ := s.Community.Poll(id, 0)
	writeJSON(w, http.StatusOK, p)
}

// handleClosePoll: POST /api/polls/{id}/close {closed} (admin).
func (s *Server) handleClosePoll(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	var req struct {
		Closed bool `json:"closed"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if err := s.Community.ClosePoll(id, req.Closed); err != nil {
		writeError(w, http.StatusInternalServerError, "could not change the poll")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeletePoll: DELETE /api/polls/{id} (admin).
func (s *Server) handleDeletePoll(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Community.DeletePoll(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the poll")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ---- BBS list ----

// handleListBBSList: GET /api/bbs/bbslist (portal) and /api/bbslist (admin).
func (s *Server) handleListBBSList(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	list, err := s.Community.BBSList()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the BBS list")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// mayChangeBBS: the caller added it, or is the sysop.
func (s *Server) mayChangeBBS(w http.ResponseWriter, r *http.Request) (community.BBS, *user.User, bool) {
	claims, _ := claimsFromContext(r.Context())
	id, ok := pathID(w, r)
	if !ok {
		return community.BBS{}, nil, false
	}
	e, err := s.Community.GetBBS(id)
	if err != nil {
		writeError(w, http.StatusNotFound, "no such entry")
		return community.BBS{}, nil, false
	}
	u, err := s.Users.ByID(claims.UserID)
	if err != nil || (e.AddedByID != u.ID && u.SecurityLevel < user.SLSysop) {
		writeError(w, http.StatusForbidden, "only who added it (or the sysop) may change it")
		return community.BBS{}, nil, false
	}
	return e, u, true
}

// handleSaveBBSListEntry: POST /api/bbs/bbslist (new) and PUT
// /api/bbs/bbslist/{id}.
func (s *Server) handleSaveBBSListEntry(w http.ResponseWriter, r *http.Request) {
	claims, _ := claimsFromContext(r.Context())
	if !s.communityReady(w) || s.pendingApproval(w, claims.UserID) {
		return
	}
	var req community.BBS
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	entry := community.BBS{Name: req.Name, Address: req.Address, Sysop: req.Sysop, Software: req.Software, Description: req.Description}
	if r.Method == http.MethodPut {
		cur, _, ok := s.mayChangeBBS(w, r)
		if !ok {
			return
		}
		entry.ID = cur.ID
	} else {
		entry.AddedByID, entry.AddedBy = claims.UserID, claims.Subject
	}
	id, err := s.Community.SaveBBS(entry)
	if err != nil {
		writeError(w, http.StatusBadRequest, "a BBS needs a name and an address")
		return
	}
	s.logInfo("%s saved %s in the BBS list", claims.Subject, entry.Name)
	saved, _ := s.Community.GetBBS(id)
	writeJSON(w, http.StatusOK, saved)
}

// handleDeleteBBSListEntry: DELETE /api/bbs/bbslist/{id} (own or sysop).
func (s *Server) handleDeleteBBSListEntry(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	e, u, ok := s.mayChangeBBS(w, r)
	if !ok {
		return
	}
	if err := s.Community.DeleteBBS(e.ID); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete it")
		return
	}
	s.logInfo("%s removed %s from the BBS list", u.Username, e.Name)
	w.WriteHeader(http.StatusNoContent)
}

// handleAdminDeleteBBSListEntry: DELETE /api/bbslist/{id} (admin).
func (s *Server) handleAdminDeleteBBSListEntry(w http.ResponseWriter, r *http.Request) {
	if !s.communityReady(w) {
		return
	}
	id, ok := pathID(w, r)
	if !ok {
		return
	}
	if err := s.Community.DeleteBBS(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete it")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
