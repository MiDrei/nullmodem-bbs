package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/menu"
)

// The menus: the SL matrix's overview, and the menu editor -- each
// menu's title, screen and items, a preview as callers see it, and
// what the defaults of this version have that a menu lacks. The bbs
// daemon picks saved menus up by itself (menu.Watcher).

type menuItemDTO struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Action string `json:"action"`
	MinSL  int    `json:"min_sl"`
	// Labels are the label in other languages, as the sysop wrote them;
	// Shown is what callers in each language see (read only: the
	// catalog's translation of a stock label where there's none).
	Labels map[string]string `json:"labels"`
	Shown  map[string]string `json:"shown,omitempty"`
}

type menuDTO struct {
	Name        string            `json:"name"`
	Title       string            `json:"title"`
	Titles      map[string]string `json:"titles"`
	TitlesShown map[string]string `json:"titles_shown,omitempty"`
	Screen      string            `json:"screen"`
	Items       []menuItemDTO     `json:"items"`
}

func toMenuDTO(m *menu.Menu) menuDTO {
	items := make([]menuItemDTO, len(m.Items))
	for i, it := range m.Items {
		items[i] = menuItemDTO{Key: it.Key, Label: it.Label, Action: it.Action, MinSL: it.MinSL, Labels: nonNilMap(it.Labels), Shown: map[string]string{}}
	}
	d := menuDTO{Name: m.Name, Title: m.Title, Titles: nonNilMap(m.Titles), TitlesShown: map[string]string{}, Screen: m.Screen, Items: items}
	for _, l := range i18n.Languages {
		if l.Code == i18n.Fallback {
			continue
		}
		in := m.In(l.Code)
		d.TitlesShown[l.Code] = in.Title
		for i := range in.Items {
			d.Items[i].Shown[l.Code] = in.Items[i].Label
		}
	}
	return d
}

func nonNilMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}

// cleanTexts keeps the translations into a language there is.
func cleanTexts(m map[string]string) map[string]string {
	var out map[string]string
	for code, v := range m {
		if v = strings.TrimSpace(v); v != "" && i18n.Valid(code) && code != i18n.Fallback {
			if out == nil {
				out = map[string]string{}
			}
			out[code] = v
		}
	}
	return out
}

func (d menuDTO) toMenu() *menu.Menu {
	m := &menu.Menu{Name: strings.TrimSpace(d.Name), Title: strings.TrimSpace(d.Title), Titles: cleanTexts(d.Titles), Screen: strings.TrimSpace(d.Screen)}
	for _, it := range d.Items {
		m.Items = append(m.Items, menu.Item{
			Key: strings.ToUpper(strings.TrimSpace(it.Key)), Label: strings.TrimSpace(it.Label),
			Action: strings.TrimSpace(it.Action), MinSL: it.MinSL, Labels: cleanTexts(it.Labels),
		})
	}
	return m
}

func (s *Server) loadMenus() (menu.Set, string, error) {
	c, err := s.loadBBSConfig()
	if err != nil {
		return nil, "", err
	}
	set, err := menu.LoadDir(c.BBS.MenusDir)
	return set, c.BBS.MenusDir, err
}

// handleListMenus: GET /api/menus, every menu by name.
func (s *Server) handleListMenus(w http.ResponseWriter, r *http.Request) {
	menus, _, err := s.loadMenus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	dtos := make([]menuDTO, 0, len(menus))
	for _, m := range menus {
		dtos = append(dtos, toMenuDTO(m))
	}
	sort.Slice(dtos, func(i, j int) bool {
		if (dtos[i].Name == "main") != (dtos[j].Name == "main") {
			return dtos[i].Name == "main"
		}
		return dtos[i].Name < dtos[j].Name
	})
	writeJSON(w, http.StatusOK, dtos)
}

// menuDefaults are the stock menus of this version (the image's
// configs-defaults), nil when there are none to compare with.
func (s *Server) menuDefaults(menusDir string) menu.Set {
	if s.MenuDefaultsDir == "" {
		return nil
	}
	a, errA := filepath.Abs(s.MenuDefaultsDir)
	b, errB := filepath.Abs(menusDir)
	if errA != nil || errB != nil || a == b {
		return nil
	}
	set, err := menu.LoadDir(s.MenuDefaultsDir)
	if err != nil {
		return nil
	}
	return set
}

type menuEditDTO struct {
	Menu menuDTO `json:"menu"`
	// MissingDefaults: items of this version's stock menu whose action
	// this menu doesn't have (a new feature, say).
	MissingDefaults []menuItemDTO `json:"missing_defaults"`
	// UsedBy: the menus with an item going here.
	UsedBy []string `json:"used_by"`
}

func (s *Server) menuEdit(set menu.Set, dir string, m *menu.Menu) menuEditDTO {
	out := menuEditDTO{Menu: toMenuDTO(m), MissingDefaults: []menuItemDTO{}, UsedBy: []string{}}
	if def, ok := s.menuDefaults(dir).Get(m.Name); ok {
		have := map[string]bool{}
		for _, it := range m.Items {
			have[it.Action] = true
		}
		for _, it := range def.Items {
			if !have[it.Action] {
				out.MissingDefaults = append(out.MissingDefaults, menuItemDTO{Key: it.Key, Label: it.Label, Action: it.Action, MinSL: it.MinSL})
			}
		}
	}
	for _, o := range set {
		for _, it := range o.Items {
			if it.Action == "goto:"+m.Name && o.Name != m.Name {
				out.UsedBy = append(out.UsedBy, o.Name)
				break
			}
		}
	}
	sort.Strings(out.UsedBy)
	return out
}

// handleGetMenu: GET /api/menus/{name}.
func (s *Server) handleGetMenu(w http.ResponseWriter, r *http.Request) {
	set, dir, err := s.loadMenus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	m, ok := set.Get(r.PathValue("name"))
	if !ok {
		writeError(w, http.StatusNotFound, "menu not found")
		return
	}
	writeJSON(w, http.StatusOK, s.menuEdit(set, dir, m))
}

// handleSaveMenu: PUT /api/menus/{name} -- the whole menu; a new name
// makes a new menu.
func (s *Server) handleSaveMenu(w http.ResponseWriter, r *http.Request) {
	var in menuDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	in.Name = r.PathValue("name")
	set, dir, err := s.loadMenus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	m := in.toMenu()
	if err := menu.Check(m, set); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if m.Screen != "" {
		c, _ := s.loadBBSConfig()
		if _, err := os.Stat(filepath.Join(c.BBS.ScreensDir, filepath.Base(m.Screen))); err != nil {
			writeError(w, http.StatusBadRequest, "there's no screen "+m.Screen)
			return
		}
		m.Screen = filepath.Base(m.Screen)
	}
	_, existed := set[m.Name]
	if err := menu.Save(dir, m); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the menu")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		verb := "changed"
		if !existed {
			verb = "created"
		}
		s.logInfo("%s %s the %s menu", claims.Subject, verb, m.Name)
	}
	set[m.Name] = m
	writeJSON(w, http.StatusOK, s.menuEdit(set, dir, m))
}

// handleDeleteMenu: DELETE /api/menus/{name} -- not main, and not one
// another menu still leads to.
func (s *Server) handleDeleteMenu(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if name == "main" {
		writeError(w, http.StatusBadRequest, "the main menu stays")
		return
	}
	set, dir, err := s.loadMenus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	m, ok := set.Get(name)
	if !ok {
		writeError(w, http.StatusNotFound, "menu not found")
		return
	}
	if used := s.menuEdit(set, dir, m).UsedBy; len(used) > 0 {
		writeError(w, http.StatusBadRequest, "the "+strings.Join(used, ", ")+" menu still leads here -- remove that item first")
		return
	}
	if err := menu.Delete(dir, name); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the menu")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted the %s menu", claims.Subject, name)
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleMenuActions: GET /api/menu-actions -- what an item can do.
func (s *Server) handleMenuActions(w http.ResponseWriter, r *http.Request) {
	set, _, err := s.loadMenus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load menus")
		return
	}
	names := []string{}
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	writeJSON(w, http.StatusOK, map[string]any{"builtins": menu.Builtins, "menus": names})
}

type menuPreviewDTO struct {
	Grid      ansi.Grid     `json:"grid"`
	HasScreen bool          `json:"has_screen"`
	NotShown  []menuItemDTO `json:"not_shown"`
	// OnlyOnScreen: keys the screen shows that no item answers to.
	OnlyOnScreen []string `json:"only_on_screen"`
}

// gridText is what a grid says, row by row, without colours.
func gridText(g ansi.Grid) string {
	var b strings.Builder
	for row := 0; row < g.Height; row++ {
		line := make([]byte, 0, g.Width)
		for col := 0; col < g.Width; col++ {
			line = append(line, g.Cells[row*g.Width+col].Char)
		}
		b.WriteString(ansi.DecodeCP437(line))
		b.WriteByte('\n')
	}
	return b.String()
}

// handlePreviewMenu: POST /api/menu-preview {menu, sl} -- a menu (as
// edited, not yet saved) the way a caller at that level sees it, and
// the items its screen doesn't seem to show.
func (s *Server) handlePreviewMenu(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Menu menuDTO `json:"menu"`
		SL   int     `json:"sl"`
		// Lang is the language to show it in ("" English).
		Lang string `json:"lang"`
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
	lang := in.Lang
	if !i18n.Valid(lang) {
		lang = i18n.Fallback
	}
	m := in.Menu.toMenu().In(lang)
	vars := previewVars(c.BBS.Name, c.BBS.Sysop)
	vars["SL"] = strconv.Itoa(in.SL)
	vars["USERNAME"] = "caller"
	if in.SL >= menu.SysopMenuSL {
		vars["USERNAME"] = c.BBS.Sysop
	}
	vars["SYSOP_ITEM"] = menu.SysopItem(in.SL, string(ansi.EncodeCP437(i18n.T(lang, "menu.sysop_item"))))

	out := menuPreviewDTO{NotShown: []menuItemDTO{}, OnlyOnScreen: []string{}}
	var text string
	if m.Screen != "" {
		for _, name := range i18n.ScreenNames(m.Screen, lang) {
			raw, err := ansi.LoadScreen(filepath.Join(c.BBS.ScreensDir, name))
			if err == nil {
				text = ansi.Render(i18n.FillScreen(lang, raw), vars)
				out.HasScreen = true
				break
			} else if !errors.Is(err, os.ErrNotExist) {
				writeError(w, http.StatusInternalServerError, "could not read the screen")
				return
			}
		}
	}
	if !out.HasScreen {
		text = menu.RenderGenerated(m, in.SL, vars)
	}
	out.Grid = ansi.ParseGrid(ansi.Layout(text, previewWidth), previewWidth)
	if out.HasScreen {
		for _, it := range menu.NotShown(m, in.SL, gridText(out.Grid), vars) {
			out.NotShown = append(out.NotShown, menuItemDTO{Key: it.Key, Label: it.Label, Action: it.Action, MinSL: it.MinSL})
		}
		if keys := menu.OnlyOnScreen(m, in.SL, gridText(out.Grid)); keys != nil {
			out.OnlyOnScreen = keys
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleSetMenuItemSL updates the min_sl of a single item in a single
// menu (the SL matrix); the bbs daemon picks it up by itself.
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
	menus, dir, err := s.loadMenus()
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
	if err := menu.Save(dir, m); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save menu")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s set %s menu item %q's minimum SL to %d", claims.Subject, menuName, itemKey, body.MinSL)
	}
	writeJSON(w, http.StatusOK, map[string]any{"menu": toMenuDTO(m)})
}
