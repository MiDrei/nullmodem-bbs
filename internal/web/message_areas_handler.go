package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/midrei/nullmodem-bbs/internal/message"
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
	// Pending is read-only here (see handleApprovePendingMessageArea):
	// an auto-created area internal/tosser is still awaiting sysop
	// review never appears in this endpoint's own listing in the
	// first place (see AllAreas), but the field rides along on a
	// single Area anyway so /api/pending-areas can reuse this DTO.
	Pending bool `json:"pending"`
	// Hidden marks a data area, left out of callers' area lists (see
	// message.Area.Hidden). On update, absent means unchanged.
	Hidden *bool `json:"hidden,omitempty"`
	// KeepDays and KeepMax are the area's own cleanup limits (see
	// message.Area.KeepDays): 0 default, -1 keep all. On update, absent
	// means unchanged.
	KeepDays *int `json:"keep_days,omitempty"`
	KeepMax  *int `json:"keep_max,omitempty"`
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
		Pending:     a.Pending,
		Hidden:      &a.Hidden,
		KeepDays:    &a.KeepDays,
		KeepMax:     &a.KeepMax,
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
	if dto.Hidden != nil && *dto.Hidden != area.Hidden {
		if err := s.Messages.SetAreaHidden(id, *dto.Hidden); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update message area")
			return
		}
		area.Hidden = *dto.Hidden
	}
	if dto.KeepDays != nil || dto.KeepMax != nil {
		days, max := area.KeepDays, area.KeepMax
		if dto.KeepDays != nil {
			days = *dto.KeepDays
		}
		if dto.KeepMax != nil {
			max = *dto.KeepMax
		}
		if days < -1 || max < -1 {
			writeError(w, http.StatusBadRequest, "keep limits: -1 keeps all, 0 is the default")
			return
		}
		if err := s.Messages.SetAreaKeep(id, days, max); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update message area")
			return
		}
		area.KeepDays, area.KeepMax = days, max
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
