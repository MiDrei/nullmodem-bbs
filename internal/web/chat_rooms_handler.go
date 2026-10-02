package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/discord"
	"git.maik.ch/nullmodem/bbs/internal/matrix"
)

// The rooms callers may enter (the teleconference and the sysop's
// own), each optionally bridged to a Discord channel, and the bridge's
// bot itself.

// handleListChatRoomSettings: GET /api/chat/room-settings.
func (s *Server) handleListChatRoomSettings(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	rooms, err := s.Chat.ListRooms()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the rooms")
		return
	}
	writeJSON(w, http.StatusOK, rooms)
}

// handleSaveChatRoom: PUT /api/chat/room-settings/{room}.
func (s *Server) handleSaveChatRoom(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	var in chat.RoomInfo
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Name = strings.ToLower(r.PathValue("room"))
	if !chat.ValidRoom(in.Name) || strings.HasPrefix(in.Name, chat.PagePrefix) {
		writeError(w, http.StatusBadRequest, "a room name is lower case letters, digits, - _ . (up to 64), and not page-...")
		return
	}
	if in.MinSL < 0 || in.MinSL > 255 {
		writeError(w, http.StatusBadRequest, "the security level is 0..255")
		return
	}
	if in.DiscordChannel != "" {
		if strings.Trim(in.DiscordChannel, "0123456789") != "" {
			writeError(w, http.StatusBadRequest, "a Discord channel is its numeric ID")
			return
		}
		if rooms, err := s.Chat.ListRooms(); err == nil {
			for _, o := range rooms {
				if o.Name != in.Name && o.DiscordChannel == in.DiscordChannel {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("that channel is already bridged to %s", o.Name))
					return
				}
			}
		}
	}
	if in.MatrixRoom = strings.TrimSpace(in.MatrixRoom); in.MatrixRoom != "" {
		if !strings.HasPrefix(in.MatrixRoom, "!") && !strings.HasPrefix(in.MatrixRoom, "#") {
			writeError(w, http.StatusBadRequest, "a Matrix room is its ID (!abc:server) or an address (#room:server)")
			return
		}
		// The bot joins it (and an address becomes the room's ID).
		var c *matrix.Client
		if s.Matrix != nil {
			c = s.Matrix.Client()
		}
		if c != nil {
			id, err := c.Join(r.Context(), in.MatrixRoom)
			if err != nil {
				writeError(w, http.StatusBadRequest, "the bot couldn't join "+in.MatrixRoom+" -- invite it there first ("+err.Error()+")")
				return
			}
			in.MatrixRoom = id
		} else if strings.HasPrefix(in.MatrixRoom, "#") {
			writeError(w, http.StatusBadRequest, "connect the Matrix bot first, or give the room's ID (!abc:server)")
			return
		}
		if rooms, err := s.Chat.ListRooms(); err == nil {
			for _, o := range rooms {
				if o.Name != in.Name && o.MatrixRoom == in.MatrixRoom {
					writeError(w, http.StatusBadRequest, fmt.Sprintf("that Matrix room is already bridged to %s", o.Name))
					return
				}
			}
		}
	}
	if err := s.Chat.SaveRoom(in); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the room")
		return
	}
	s.logInfo("chat room %s saved", in.Name)
	if s.Discord != nil {
		s.Discord.Reload()
	}
	if s.Matrix != nil {
		s.Matrix.Reload()
	}
	saved, _ := s.Chat.RoomByName(in.Name)
	writeJSON(w, http.StatusOK, saved)
}

// handleDeleteChatRoom: DELETE /api/chat/room-settings/{room}.
func (s *Server) handleDeleteChatRoom(w http.ResponseWriter, r *http.Request) {
	if !s.chatReady(w) {
		return
	}
	switch err := s.Chat.DeleteRoom(r.PathValue("room")); {
	case errors.Is(err, chat.ErrMainRoom):
		writeError(w, http.StatusBadRequest, "the teleconference stays")
		return
	case errors.Is(err, chat.ErrNoRoom):
		writeError(w, http.StatusNotFound, "no such room")
		return
	case err != nil:
		writeError(w, http.StatusInternalServerError, "could not remove the room")
		return
	}
	s.logInfo("chat room %s removed", r.PathValue("room"))
	w.WriteHeader(http.StatusNoContent)
}

type discordDTO struct {
	Enabled  bool              `json:"enabled"`
	HasToken bool              `json:"has_token"`
	Quiet    bool              `json:"quiet"`
	Status   discord.Status    `json:"status"`
	Channels []discord.Channel `json:"channels"`
	// InviteURL adds the bot to a server, with the permissions it needs
	// (known once it has connected).
	InviteURL string `json:"invite_url,omitempty"`
}

func (s *Server) discordState(c *config.Config) discordDTO {
	out := discordDTO{Enabled: c.Discord.Enabled, HasToken: c.Discord.Token != "", Quiet: c.Discord.Quiet, Channels: []discord.Channel{}}
	if s.Discord != nil {
		out.Status = s.Discord.Status()
		out.Channels = s.Discord.Channels()
		if out.Status.BotID != "" {
			out.InviteURL = fmt.Sprintf("https://discord.com/oauth2/authorize?client_id=%s&scope=bot&permissions=%d", out.Status.BotID, discord.Permissions)
		}
	}
	return out
}

// handleGetDiscord: GET /api/chat/discord -- never the token itself.
func (s *Server) handleGetDiscord(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.discordState(c))
}

// handlePutDiscord: PUT /api/chat/discord {enabled, quiet, token?} --
// a token only when it changes ("" keeps it); clear_token removes it.
func (s *Server) handlePutDiscord(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled    bool   `json:"enabled"`
		Quiet      bool   `json:"quiet"`
		Token      string `json:"token"`
		ClearToken bool   `json:"clear_token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	token := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(in.Token), "Bot "))
	switch {
	case in.ClearToken:
		c.Discord.Token = ""
	case token != "":
		if strings.ContainsAny(token, " \t\r\n") || len(token) < 50 {
			writeError(w, http.StatusBadRequest, "that doesn't look like a bot token -- copy it from the Developer Portal (Bot -> Reset Token)")
			return
		}
		c.Discord.Token = token
	}
	c.Discord.Enabled, c.Discord.Quiet = in.Enabled, in.Quiet
	if c.Discord.Enabled && c.Discord.Token == "" {
		writeError(w, http.StatusBadRequest, "the bridge needs the bot's token")
		return
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	s.logInfo("Discord bridge %s", map[bool]string{true: "on", false: "off"}[c.Discord.Enabled])
	if s.Discord != nil {
		s.Discord.Reload()
	}
	writeJSON(w, http.StatusOK, s.discordState(c))
}

type matrixDTO struct {
	Enabled    bool          `json:"enabled"`
	Homeserver string        `json:"homeserver"`
	UserID     string        `json:"user_id"`
	HasToken   bool          `json:"has_token"`
	Quiet      bool          `json:"quiet"`
	Status     matrix.Status `json:"status"`
	Rooms      []matrix.Room `json:"rooms"`
}

func (s *Server) matrixState(r *http.Request, c *config.Config) matrixDTO {
	out := matrixDTO{Enabled: c.Matrix.Enabled, Homeserver: c.Matrix.Homeserver, UserID: c.Matrix.UserID,
		HasToken: c.Matrix.Token != "", Quiet: c.Matrix.Quiet, Rooms: []matrix.Room{}}
	if s.Matrix != nil {
		out.Status = s.Matrix.Status()
		if cl := s.Matrix.Client(); cl != nil && out.Status.Connected {
			if rooms, err := cl.JoinedRooms(r.Context()); err == nil {
				out.Rooms = rooms
			}
		}
	}
	return out
}

// handleGetMatrix: GET /api/chat/matrix -- never the token.
func (s *Server) handleGetMatrix(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.matrixState(r, c))
}

// handlePutMatrix: PUT /api/chat/matrix {enabled, quiet, homeserver,
// user, password, forget} -- with a password the bot logs in and only
// its access token is kept.
func (s *Server) handlePutMatrix(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Enabled    bool   `json:"enabled"`
		Quiet      bool   `json:"quiet"`
		Homeserver string `json:"homeserver"`
		User       string `json:"user"`
		Password   string `json:"password"`
		Forget     bool   `json:"forget"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	hs := strings.TrimRight(strings.TrimSpace(in.Homeserver), "/")
	if hs != "" && !strings.HasPrefix(hs, "https://") && !strings.HasPrefix(hs, "http://") {
		hs = "https://" + hs
	}
	switch {
	case in.Forget:
		c.Matrix.Token, c.Matrix.UserID = "", ""
	case in.Password != "":
		if hs == "" || strings.TrimSpace(in.User) == "" {
			writeError(w, http.StatusBadRequest, "the homeserver and the bot's user name, please")
			return
		}
		userID, token, err := matrix.Login(r.Context(), hs, strings.TrimSpace(in.User), in.Password)
		if err != nil {
			writeError(w, http.StatusBadRequest, "the login failed: "+err.Error())
			return
		}
		c.Matrix.Homeserver, c.Matrix.UserID, c.Matrix.Token = hs, userID, token
	case hs != "" && hs != c.Matrix.Homeserver:
		writeError(w, http.StatusBadRequest, "a new homeserver needs a login (user and password)")
		return
	}
	c.Matrix.Enabled, c.Matrix.Quiet = in.Enabled, in.Quiet
	if c.Matrix.Enabled && c.Matrix.Token == "" {
		writeError(w, http.StatusBadRequest, "the bridge needs the bot to log in first")
		return
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	s.logInfo("Matrix bridge %s (%s)", map[bool]string{true: "on", false: "off"}[c.Matrix.Enabled], c.Matrix.UserID)
	if s.Matrix != nil {
		s.Matrix.Reload()
	}
	writeJSON(w, http.StatusOK, s.matrixState(r, c))
}
