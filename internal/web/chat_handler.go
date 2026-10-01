package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"git.maik.ch/nullmodem/bbs/internal/chat"
)

// The web admin's Chat page: the rooms with something going on (a
// caller paging comes first), talking in one -- with the Telnet/SSH
// callers in it, through the shared internal/chat store -- and the
// one-liners wall to tidy up.

const chatSource = "web"

func (s *Server) chatReady(w http.ResponseWriter) bool {
	if s.Chat == nil {
		writeError(w, http.StatusServiceUnavailable, "chat is not available")
		return false
	}
	return true
}

func (s *Server) chatRoomParam(w http.ResponseWriter, r *http.Request) (string, bool) {
	room := r.PathValue("room")
	if !chat.ValidRoom(room) {
		writeError(w, http.StatusBadRequest, "not a room")
		return "", false
	}
	return room, true
}

// handleListChatRooms: GET /api/chat/rooms.
func (s *Server) handleListChatRooms(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	rooms, err := s.Chat.Rooms()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the rooms")
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

type chatRoomState struct {
	Lines   []chat.Line     `json:"lines"`
	Present []chat.Presence `json:"present"`
}

// handleGetChatRoom: GET /api/chat/rooms/{room}?after=ID -- the lines
// after ID (the newest ones without), who's there; polling it keeps
// the sysop in the room.
func (s *Server) handleGetChatRoom(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	room, ok := s.chatRoomParam(w, r)
	if !ok {
		return
	}
	claims, _ := claimsFromContext(r.Context())
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	lines, err := s.Chat.Lines(room, after, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the room")
		return
	}
	if r.URL.Query().Get("watch") != "1" {
		s.Chat.Touch(room, claims.Subject, chatSource)
	}
	present, err := s.Chat.Present(room)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the room")
		return
	}
	writeJSON(w, http.StatusOK, chatRoomState{Lines: lines, Present: present})
}

// handleChatAction: POST /api/chat/rooms/{room} {action: enter|say|leave, text}.
func (s *Server) handleChatAction(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	room, ok := s.chatRoomParam(w, r)
	if !ok {
		return
	}
	claims, _ := claimsFromContext(r.Context())
	var req struct {
		Action string `json:"action"`
		Text   string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	var err error
	switch req.Action {
	case "enter":
		err = s.Chat.Enter(room, claims.Subject, chatSource)
	case "leave":
		err = s.Chat.Exit(room, claims.Subject, chatSource)
	case "say":
		if req.Text == "" {
			writeError(w, http.StatusBadRequest, "nothing to say")
			return
		}
		s.Chat.Touch(room, claims.Subject, chatSource)
		_, err = s.Chat.Post(room, claims.Subject, chatSource, chat.Say, req.Text)
	default:
		writeError(w, http.StatusBadRequest, "action must be enter, say or leave")
		return
	}
	if errors.Is(err, chat.ErrBadRoom) {
		writeError(w, http.StatusBadRequest, "not a room")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not do that")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListOneliners: GET /api/oneliners.
func (s *Server) handleListOneliners(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	list, err := s.Chat.Oneliners(100)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the one-liners")
		return
	}
	writeJSON(w, http.StatusOK, list)
}

// handleDeleteOneliner: DELETE /api/oneliners/{id}.
func (s *Server) handleDeleteOneliner(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid id")
		return
	}
	if err := s.Chat.DeleteOneliner(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete it")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted a one-liner", claims.Subject)
	}
	w.WriteHeader(http.StatusNoContent)
}
