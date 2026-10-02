package web

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/chat"
)

// The chat rooms for callers in the portal and the reader app: the
// same rooms (and bridges) as the teleconference on Telnet, those
// their level allows. Polling a room keeps them in it.

type bbsChatRoomDTO struct {
	chat.RoomInfo
	Present  []chat.Presence `json:"present"`
	LastLine *chat.Line      `json:"last_line"`
	Bridges  []string        `json:"bridges"`
}

// bbsChatRoom is the room in the path, if the caller may enter it.
func (s *Server) bbsChatRoom(w http.ResponseWriter, r *http.Request) (*chat.RoomInfo, string, bool) {
	if !s.chatReady(w) {
		return nil, "", false
	}
	claims, _ := claimsFromContext(r.Context())
	info, err := s.Chat.RoomByName(r.PathValue("room"))
	if err != nil || claims.SecurityLevel < info.MinSL {
		writeError(w, http.StatusNotFound, "no such room")
		return nil, "", false
	}
	return info, claims.Subject, true
}

// handleBBSChatRooms: GET /api/bbs/chat/rooms.
func (s *Server) handleBBSChatRooms(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	claims, _ := claimsFromContext(r.Context())
	rooms, err := s.Chat.RoomsFor(claims.SecurityLevel)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the rooms")
		return
	}
	out := make([]bbsChatRoomDTO, 0, len(rooms))
	for _, room := range rooms {
		d := bbsChatRoomDTO{RoomInfo: room, Present: []chat.Presence{}, Bridges: []string{}}
		// Callers see which networks a room reaches, not the IDs.
		d.DiscordChannel, d.MatrixRoom = "", ""
		if room.DiscordChannel != "" {
			d.Bridges = append(d.Bridges, "Discord")
		}
		if room.MatrixRoom != "" {
			d.Bridges = append(d.Bridges, "Matrix")
		}
		if p, err := s.Chat.Present(room.Name); err == nil {
			d.Present = p
		}
		if lines, err := s.Chat.Lines(room.Name, 0, 1); err == nil && len(lines) > 0 {
			d.LastLine = &lines[0]
		}
		out = append(out, d)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleBBSChatLines: GET /api/bbs/chat/rooms/{room}?after=ID.
func (s *Server) handleBBSChatLines(w http.ResponseWriter, r *http.Request) {
	info, who, ok := s.bbsChatRoom(w, r)
	if !ok {
		return
	}
	after, _ := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	lines, err := s.Chat.Lines(info.Name, after, 200)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the room")
		return
	}
	s.Chat.Touch(info.Name, who, chatSource)
	present, err := s.Chat.Present(info.Name)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the room")
		return
	}
	writeJSON(w, http.StatusOK, chatRoomState{Lines: lines, Present: present})
}

// handleBBSChatAction: POST /api/bbs/chat/rooms/{room} {action, text}.
func (s *Server) handleBBSChatAction(w http.ResponseWriter, r *http.Request) {
	info, who, ok := s.bbsChatRoom(w, r)
	if !ok {
		return
	}
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
		err = s.Chat.Enter(info.Name, who, chatSource)
	case "leave":
		err = s.Chat.Exit(info.Name, who, chatSource)
	case "say":
		claims, _ := claimsFromContext(r.Context())
		if s.pendingApproval(w, claims.UserID) {
			return
		}
		text := strings.TrimSpace(req.Text)
		if text == "" {
			writeError(w, http.StatusBadRequest, "nothing to say")
			return
		}
		s.Chat.Touch(info.Name, who, chatSource)
		_, err = s.Chat.Post(info.Name, who, chatSource, chat.Say, text)
	default:
		writeError(w, http.StatusBadRequest, "action must be enter, say or leave")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not do that")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
