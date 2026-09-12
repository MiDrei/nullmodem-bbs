package web

import (
	"encoding/json"
	"net/http"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/menu"
)

type menuItemDTO struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Action string `json:"action"`
	MinSL  int    `json:"min_sl"`
}

type menuDTO struct {
	Name  string        `json:"name"`
	Title string        `json:"title"`
	Items []menuItemDTO `json:"items"`
}

func toMenuDTO(m *menu.Menu) menuDTO {
	items := make([]menuItemDTO, len(m.Items))
	for i, it := range m.Items {
		items[i] = menuItemDTO{Key: it.Key, Label: it.Label, Action: it.Action, MinSL: it.MinSL}
	}
	return menuDTO{Name: m.Name, Title: m.Title, Items: items}
}

// handleListMenus serves the menu part of the SL permission matrix:
// every menu with its items and their SL gates, so the web UI can
// build a single overview across menus, message areas and file areas.
func (s *Server) handleListMenus(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	menus, err := menu.LoadDir(c.BBS.MenusDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	dtos := make([]menuDTO, 0, len(menus))
	for _, m := range menus {
		dtos = append(dtos, toMenuDTO(m))
	}
	writeJSON(w, http.StatusOK, dtos)
}

// handleSetMenuItemSL updates the min_sl of a single item in a single
// menu and persists it back to that menu's YAML file. The running bbs
// daemon only reads menus at startup, so this needs a daemon restart
// to take effect -- the same caveat as bbs.yaml config edits.
func (s *Server) handleSetMenuItemSL(w http.ResponseWriter, r *http.Request) {
	menuName := r.PathValue("name")
	itemKey := r.PathValue("key")

	var body struct {
		MinSL int `json:"min_sl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if body.MinSL < 0 || body.MinSL > 255 {
		writeError(w, http.StatusBadRequest, "min_sl must be between 0 and 255")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	menus, err := menu.LoadDir(c.BBS.MenusDir)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	m, ok := menus.Get(menuName)
	if !ok {
		writeError(w, http.StatusNotFound, "menu not found")
		return
	}

	found := false
	for i := range m.Items {
		if strings.EqualFold(m.Items[i].Key, itemKey) {
			m.Items[i].MinSL = body.MinSL
			found = true
			break
		}
	}
	if !found {
		writeError(w, http.StatusNotFound, "menu item not found")
		return
	}

	if err := menu.Save(c.BBS.MenusDir, m); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save menu")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s set %s menu item %q's minimum SL to %d", claims.Subject, menuName, itemKey, body.MinSL)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"menu": toMenuDTO(m),
		"note": "Restart the bbs daemon for changes to take effect.",
	})
}
