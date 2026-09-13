package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/message"
)

// areaTagPattern restricts area tags to a small, predictable
// character set. File area tags double as an on-disk directory
// component (see internal/file), so this also guards against path
// traversal or otherwise awkward filesystem names; message areas
// don't strictly need it but are held to the same convention.
var areaTagPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

type messageAreaDTO struct {
	ID          int64  `json:"id"`
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Network     string `json:"network"`
	MinSLRead   int    `json:"min_sl_read"`
	MinSLWrite  int    `json:"min_sl_write"`
	SortOrder   int    `json:"sort_order"`
}

func toMessageAreaDTO(a message.Area) messageAreaDTO {
	return messageAreaDTO{
		ID:          a.ID,
		Tag:         a.Tag,
		Name:        a.Name,
		Description: a.Description,
		Network:     a.Network,
		MinSLRead:   a.MinSLRead,
		MinSLWrite:  a.MinSLWrite,
		SortOrder:   a.SortOrder,
	}
}

func (s *Server) handleListMessageAreas(w http.ResponseWriter, r *http.Request) {
	areas, err := s.Messages.AllAreas()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list message areas")
		return
	}
	dtos := make([]messageAreaDTO, len(areas))
	for i, a := range areas {
		dtos[i] = toMessageAreaDTO(a)
	}
	writeJSON(w, http.StatusOK, dtos)
}

func (s *Server) handleCreateMessageArea(w http.ResponseWriter, r *http.Request) {
	var dto messageAreaDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateAreaDTO(true, dto.Tag, dto.Name, dto.MinSLRead, dto.MinSLWrite); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	area, err := s.Messages.CreateArea(dto.Tag, dto.Name, dto.Description, dto.Network, dto.MinSLRead, dto.MinSLWrite)
	if err != nil {
		if errors.Is(err, message.ErrTagTaken) {
			writeError(w, http.StatusConflict, "that tag is already in use")
			return
		}
		writeError(w, http.StatusInternalServerError, "could not create message area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s created message area %q (%s) via web", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusCreated, toMessageAreaDTO(*area))
}

func (s *Server) handleUpdateMessageArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	var dto messageAreaDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateAreaDTO(false, dto.Tag, dto.Name, dto.MinSLRead, dto.MinSLWrite); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	area, err := s.Messages.UpdateArea(id, dto.Name, dto.Description, dto.Network, dto.MinSLRead, dto.MinSLWrite, dto.SortOrder)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not update message area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated message area %q (%s)", claims.Subject, area.Name, area.Tag)
	}
	writeJSON(w, http.StatusOK, toMessageAreaDTO(*area))
}

func (s *Server) handleDeleteMessageArea(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid area id")
		return
	}
	if err := s.Messages.DeleteArea(id); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete message area")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted message area %d", claims.Subject, id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// validateAreaDTO is shared by message and file area create/update
// handlers. checkTag is false for updates, which don't accept a tag
// change (the tag is a stable identifier fixed at creation).
func validateAreaDTO(checkTag bool, tag, name string, minSLA, minSLB int) string {
	if checkTag {
		if strings.TrimSpace(tag) == "" {
			return "tag must not be empty"
		}
		if !areaTagPattern.MatchString(tag) {
			return "tag may only contain letters, digits, - and _"
		}
	}
	if strings.TrimSpace(name) == "" {
		return "name must not be empty"
	}
	if minSLA < 0 || minSLA > 255 || minSLB < 0 || minSLB > 255 {
		return "security levels must be between 0 and 255"
	}
	return ""
}
