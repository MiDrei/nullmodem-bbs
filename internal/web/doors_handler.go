package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/i18n"
	"git.maik.ch/nullmodem/bbs/internal/services"
)

// doorInstallTimeout bounds downloading and unpacking one door.
const doorInstallTimeout = 3 * time.Minute

// doorDTO is one configured door as the web admin edits it -- the
// same fields as config.DoorConfig.
type doorDTO struct {
	Name              string   `json:"name"`
	Kind              string   `json:"kind"`
	MinSL             int      `json:"min_sl"`
	Exe               string   `json:"exe"`
	Dir               string   `json:"dir"`
	Args              []string `json:"args"`
	DOSBoxDir         string   `json:"dosbox_dir"`
	DOSBoxLaunchCmd   string   `json:"dosbox_launch_cmd"`
	DropFile          string   `json:"dropfile"`
	DropFileInDoorDir bool     `json:"dropfile_in_door_dir"`
	LockFiles         []string `json:"lock_files"`
	Stdio             bool     `json:"stdio"`
	ANSI16            bool     `json:"ansi16"`
	// Remote is a door of kind "rlogin": where and as whom.
	Remote   config.RemoteDoor `json:"remote"`
	Template string            `json:"template"`
	// Daily is the daily maintenance command, DailyAt its time;
	// DailyState how its last run went (read-only).
	Daily      string            `json:"daily"`
	DailyAt    string            `json:"daily_at"`
	DailyState *doors.DailyState `json:"daily_state"`
	// Program is the door's background program -- see
	// config.DoorConfig.Program.
	Program []string `json:"program"`
	// Bulletins are the door's score and news files.
	Bulletins []config.DoorBulletin `json:"bulletins"`
	// TemplateBulletins: what the door's template offers, when the
	// door has none yet.
	TemplateBulletins []config.DoorBulletin `json:"template_bulletins,omitempty"`
	// Installed reports whether the door's directory exists and has
	// files in it. Read-only.
	Installed bool `json:"installed"`
}

type doorsDTO struct {
	Doors           []doorDTO `json:"doors"`
	DropFileFormats []string  `json:"dropfile_formats"`
	DoorsDir        string    `json:"doors_dir"`
}

// doorTemplateDTO is a doors.Template plus its state on this system.
type doorTemplateDTO struct {
	doors.Template
	// Downloadable reports whether Install can fetch it.
	Downloadable bool `json:"downloadable"`
	// Installed reports whether its directory already has files.
	Installed bool `json:"installed"`
	// Configured reports whether a door created from it exists.
	Configured bool `json:"configured"`
}

func nonEmptyDir(dir string) bool {
	if dir == "" {
		return false
	}
	entries, err := os.ReadDir(dir)
	return err == nil && len(entries) > 0
}

func toDoorDTO(d config.DoorConfig) doorDTO {
	dir := d.DOSBoxDir
	if d.Kind != "dosbox" {
		dir = d.Dir
	}
	orEmpty := func(v []string) []string {
		if v == nil {
			return []string{}
		}
		return v
	}
	return doorDTO{
		Name:              d.Name,
		Kind:              d.Kind,
		MinSL:             d.MinSL,
		Exe:               d.Exe,
		Dir:               d.Dir,
		Args:              orEmpty(d.Args),
		DOSBoxDir:         d.DOSBoxDir,
		DOSBoxLaunchCmd:   d.DOSBoxLaunchCmd,
		DropFile:          d.DropFile,
		DropFileInDoorDir: d.DropFileInDoorDir,
		LockFiles:         orEmpty(d.LockFiles),
		Stdio:             d.Stdio,
		ANSI16:            d.ANSI16,
		Remote:            d.Remote,
		Template:          d.Template,
		Daily:             d.Daily,
		DailyAt:           d.DailyAt,
		Program:           orEmpty(d.Program),
		Bulletins:         bulletinsOrEmpty(d.Bulletins),
		TemplateBulletins: offeredBulletins(d),
		Installed:         nonEmptyDir(dir),
	}
}

func fromDoorDTO(d doorDTO) config.DoorConfig {
	trimmed := func(v []string) []string {
		var out []string
		for _, s := range v {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
		return out
	}
	kind := d.Kind
	if kind == "native" {
		kind = ""
	}
	return config.DoorConfig{
		Name:              strings.TrimSpace(d.Name),
		Kind:              kind,
		MinSL:             d.MinSL,
		Exe:               strings.TrimSpace(d.Exe),
		Dir:               strings.TrimSpace(d.Dir),
		Args:              trimmed(d.Args),
		DOSBoxDir:         strings.TrimSpace(d.DOSBoxDir),
		DOSBoxLaunchCmd:   strings.TrimSpace(d.DOSBoxLaunchCmd),
		DropFile:          d.DropFile,
		DropFileInDoorDir: d.DropFileInDoorDir,
		LockFiles:         trimmed(d.LockFiles),
		Stdio:             d.Stdio && kind == "",
		ANSI16:            d.ANSI16,
		Remote:            trimRemote(d.Remote, kind),
		Template:          d.Template,
		Daily:             strings.TrimSpace(d.Daily),
		DailyAt:           strings.TrimSpace(d.DailyAt),
		Program:           trimmed(d.Program),
		Bulletins:         cleanBulletins(d.Bulletins),
	}
}

func bulletinsOrEmpty(b []config.DoorBulletin) []config.DoorBulletin {
	if b == nil {
		return []config.DoorBulletin{}
	}
	return b
}

// cleanBulletins drops empty rows and trims.
func cleanBulletins(in []config.DoorBulletin) []config.DoorBulletin {
	var out []config.DoorBulletin
	for _, b := range in {
		b.Title, b.File = strings.TrimSpace(b.Title), strings.TrimSpace(b.File)
		if b.File == "" {
			continue
		}
		if b.Title == "" {
			b.Title = b.File
		}
		out = append(out, b)
	}
	return out
}

// validateDoors checks a whole door list before it's saved.
func validateDoors(list []config.DoorConfig) string {
	seen := map[string]bool{}
	for _, d := range list {
		if d.Name == "" {
			return "every door needs a name"
		}
		key := strings.ToLower(d.Name)
		if seen[key] {
			return fmt.Sprintf("door name %q is used twice", d.Name)
		}
		seen[key] = true
		for _, b := range d.Bulletins {
			if _, err := doors.BulletinPath("door", b.File); err != nil {
				return fmt.Sprintf("%s: bulletin %q -- a file inside the door's directory, like data/bull/scores.ans", d.Name, b.File)
			}
		}
		if d.MinSL < 0 || d.MinSL > 255 {
			return fmt.Sprintf("%s: security level must be 0-255", d.Name)
		}
		switch d.Kind {
		case "":
			if d.Exe == "" || d.Dir == "" {
				return fmt.Sprintf("%s: a native door needs an executable and a directory", d.Name)
			}
		case "dosbox":
			if d.DOSBoxDir == "" || d.DOSBoxLaunchCmd == "" {
				return fmt.Sprintf("%s: a DOSBox door needs a directory and a launch command", d.Name)
			}
		case "rlogin":
			if d.Remote.Host == "" || d.Remote.ClientUser == "" {
				return fmt.Sprintf("%s: an RLogin door needs a host and a user name", d.Name)
			}
			if d.Remote.Port < 0 || d.Remote.Port > 65535 {
				return fmt.Sprintf("%s: port must be 1-65535", d.Name)
			}
		default:
			return fmt.Sprintf("%s: unknown kind %q", d.Name, d.Kind)
		}
		if d.DropFile != "" {
			ok := false
			for _, f := range doors.DropFileFormats {
				ok = ok || d.DropFile == f
			}
			if !ok {
				return fmt.Sprintf("%s: unknown drop file format %q", d.Name, d.DropFile)
			}
		}
		if d.DailyAt != "" {
			if _, err := time.Parse("15:04", d.DailyAt); err != nil {
				return fmt.Sprintf("%s: the daily maintenance time must be HH:MM", d.Name)
			}
		}
		if d.Daily != "" && d.Kind == "rlogin" {
			return fmt.Sprintf("%s: a remote door is maintained by its own system", d.Name)
		}
		if len(d.Program) > 0 && d.Kind != "" {
			return fmt.Sprintf("%s: only a native door can have a background program", d.Name)
		}
		for _, lf := range d.LockFiles {
			if !filepath.IsLocal(lf) {
				return fmt.Sprintf("%s: lock file %q must be a path inside the door's directory", d.Name, lf)
			}
		}
	}
	return ""
}

func (s *Server) doorsResponse(c *config.Config) doorsDTO {
	states := map[string]doors.DailyState{}
	if s.DB != nil {
		states, _ = doors.DailyStates(s.DB)
	}
	list := make([]doorDTO, len(c.Doors))
	for i, d := range c.Doors {
		list[i] = toDoorDTO(d)
		if st, ok := states[d.Name]; ok && !st.LastAt.IsZero() {
			st := st
			list[i].DailyState = &st
		}
	}
	return doorsDTO{Doors: list, DropFileFormats: doors.DropFileFormats, DoorsDir: c.BBS.DoorsDir}
}

func (s *Server) handleListDoors(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.doorsResponse(c))
}

// handlePutDoors replaces the whole door list. The bbs daemon re-reads
// it whenever a caller opens the doors menu, so no restart is needed.
func (s *Server) handlePutDoors(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Doors []doorDTO `json:"doors"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	list := make([]config.DoorConfig, len(req.Doors))
	for i, d := range req.Doors {
		list[i] = fromDoorDTO(d)
	}
	if msg := validateDoors(list); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	c.Doors = list
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated the doors", claims.Subject)
	}
	writeJSON(w, http.StatusOK, s.doorsResponse(c))
}

func (s *Server) handleListDoorTemplates(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	out := make([]doorTemplateDTO, len(doors.Templates))
	lang := s.requestLang(r)
	for i, t := range doors.Templates {
		// In the admin's language, where the catalog has them.
		t.Description = i18n.ByEnglish(lang, "doortpl.", t.Description)
		t.Setup = i18n.ByEnglish(lang, "doortpl.", t.Setup)
		t.License = i18n.ByEnglish(lang, "doortpl.", t.License)
		configured := false
		for _, d := range c.Doors {
			configured = configured || d.Template == t.ID || strings.EqualFold(d.Name, t.Name)
		}
		out[i] = doorTemplateDTO{
			Template:     t,
			Downloadable: t.Download != nil && t.Download.Supports(runtime.GOARCH),
			Installed:    nonEmptyDir(filepath.Join(c.BBS.DoorsDir, t.Dir)),
			Configured:   configured,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddDoorFromTemplate sets up a door from a template: downloads
// and unpacks it when the template allows (and it isn't there yet),
// otherwise just creates its empty directory for the sysop to fill,
// then adds the door to the list. It never overwrites an existing
// door directory or door entry.
func (s *Server) handleAddDoorFromTemplate(w http.ResponseWriter, r *http.Request) {
	t, ok := doors.TemplateByID(r.PathValue("id"))
	if !ok {
		writeError(w, http.StatusNotFound, "no such door template")
		return
	}
	// uMRC's settings come with the request (see handlePutMRCConfig).
	var req struct {
		MRC *doors.MRCConfig `json:"mrc"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
	}
	if t.MRC && req.MRC != nil {
		if err := req.MRC.Validate(); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	for _, d := range c.Doors {
		if d.Template == t.ID || strings.EqualFold(d.Name, t.Name) {
			writeError(w, http.StatusConflict, fmt.Sprintf("%s is already set up as door %q", t.Name, d.Name))
			return
		}
	}

	dir := filepath.Join(c.BBS.DoorsDir, t.Dir)
	if t.Kind == "native" && !nonEmptyDir(dir) && !t.Download.Supports(runtime.GOARCH) {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("%s has no build for this system (%s)", t.Name, runtime.GOARCH))
		return
	}
	installed := false
	switch {
	case nonEmptyDir(dir):
		// Already there (unpacked by hand, or installed before and
		// removed from the list) -- keep it as it is.
	case t.Download != nil && t.Download.Supports(runtime.GOARCH):
		client := &http.Client{Timeout: doorInstallTimeout}
		if _, err := doors.Install(r.Context(), client, t, c.BBS.DoorsDir, c.BBS.Name, c.BBS.Sysop); err != nil {
			if errors.Is(err, doors.ErrAlreadyInstalled) {
				writeError(w, http.StatusConflict, err.Error())
				return
			}
			s.logWarn("installing door %s: %v", t.Name, err)
			writeError(w, http.StatusBadGateway, "could not install "+t.Name+": "+err.Error())
			return
		}
		installed = true
	default:
		if err := os.MkdirAll(dir, 0o755); err != nil {
			writeError(w, http.StatusInternalServerError, "could not create "+dir)
			return
		}
	}

	// A door directory kept from before keeps its mrc.cfg, unless new
	// settings were given.
	if t.MRC && nonEmptyDir(dir) {
		if _, err := doors.ReadMRCConfig(dir); err != nil || req.MRC != nil {
			mrc := doors.DefaultMRCConfig(c.BBS.Name, c.BBS.Sysop)
			if req.MRC != nil {
				mrc = *req.MRC
			}
			if err := doors.WriteMRCConfig(dir, mrc); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
	}

	entry := config.DoorConfig{
		Name:              t.Name,
		MinSL:             0,
		DropFile:          t.DropFile,
		DropFileInDoorDir: t.DropFileInDoorDir,
		LockFiles:         t.LockFiles,
		Template:          t.ID,
		Bulletins:         templateBulletins(t),
		Daily:             t.Daily,
	}
	if t.Kind == "native" {
		entry.Exe = filepath.Join(dir, t.Exe)
		entry.Dir = dir
		entry.Args = t.Args
		entry.Stdio = t.Stdio
		entry.ANSI16 = t.ANSI16
		entry.Program = t.Program
	} else {
		entry.Kind = "dosbox"
		entry.DOSBoxDir = dir
		entry.DOSBoxLaunchCmd = t.DOSBoxLaunchCmd
	}
	if err := enableTemplateBulletins(entry, c.BBS.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c.Doors = append(c.Doors, entry)
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		if installed {
			s.logInfo("%s installed door %s into %s", claims.Subject, t.Name, dir)
		} else {
			s.logInfo("%s added door %s (%s)", claims.Subject, t.Name, dir)
		}
	}
	writeJSON(w, http.StatusOK, s.doorsResponse(c))
}

// mrcDoor finds the uMRC door and its directory.
func (s *Server) mrcDoor(c *config.Config) (config.DoorConfig, bool) {
	for _, d := range c.Doors {
		if d.Template == "umrc" && d.Dir != "" {
			return d, true
		}
	}
	return config.DoorConfig{}, false
}

// handleGetMRCConfig returns the uMRC door's mrc.cfg (404 without the
// door).
func (s *Server) handleGetMRCConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	d, ok := s.mrcDoor(c)
	if !ok {
		writeError(w, http.StatusNotFound, "the MRC Chat door is not set up")
		return
	}
	mrc, err := doors.ReadMRCConfig(d.Dir)
	if err != nil {
		// Not written yet: offer what installing would write.
		mrc = doors.DefaultMRCConfig(c.BBS.Name, c.BBS.Sysop)
	}
	writeJSON(w, http.StatusOK, mrc)
}

// handlePutMRCConfig saves the uMRC door's mrc.cfg and restarts its
// umrc-bridge, which only reads it when starting.
func (s *Server) handlePutMRCConfig(w http.ResponseWriter, r *http.Request) {
	var mrc doors.MRCConfig
	if err := json.NewDecoder(r.Body).Decode(&mrc); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	d, ok := s.mrcDoor(c)
	if !ok {
		writeError(w, http.StatusNotFound, "the MRC Chat door is not set up")
		return
	}
	if err := doors.WriteMRCConfig(d.Dir, mrc); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if s.Services != nil && len(d.Program) > 0 {
		name := doors.Program{Door: d.Name}.ServiceName()
		if err := s.Services.RequestRestart(name, services.ModeNow); err != nil {
			s.logWarn("restarting %s: %v", name, err)
		}
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s changed the MRC Chat settings", claims.Subject)
	}
	writeJSON(w, http.StatusOK, mrc)
}

// trimRemote keeps a remote door's settings only for kind "rlogin".
func trimRemote(r config.RemoteDoor, kind string) config.RemoteDoor {
	if kind != "rlogin" {
		return config.RemoteDoor{}
	}
	r.Host = strings.TrimSpace(r.Host)
	r.ClientUser = strings.TrimSpace(r.ClientUser)
	r.ServerUser = strings.TrimSpace(r.ServerUser)
	r.TermType = strings.TrimSpace(r.TermType)
	return r
}

// handleRunDoorDaily: POST /api/door-daily/{name} -- the bbs daemon
// runs the door's daily maintenance within half a minute.
func (s *Server) handleRunDoorDaily(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	var door *config.DoorConfig
	for i := range c.Doors {
		if c.Doors[i].Name == name && c.Doors[i].Daily != "" {
			door = &c.Doors[i]
		}
	}
	if door == nil {
		writeError(w, http.StatusNotFound, "no such door with a daily maintenance")
		return
	}
	// The maintenance is what writes the bulletins: make sure the game
	// was told to (a door added from its template before v0.83.5 wasn't).
	if err := enableTemplateBulletins(*door, c.BBS.Name); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := doors.RequestDaily(s.DB, name); err != nil {
		writeError(w, http.StatusInternalServerError, "could not ask for it")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s asked for %s's daily maintenance now", claims.Subject, name)
	}
	w.WriteHeader(http.StatusAccepted)
}

// enableTemplateBulletins tells a door's game to write its bulletins,
// if the door has bulletins and its template knows how (see
// doors.Template.EnableBulletins); done already, it changes nothing.
func enableTemplateBulletins(d config.DoorConfig, bbsName string) error {
	if len(d.Bulletins) == 0 {
		return nil
	}
	dir := d.Dir
	if d.Kind == "dosbox" {
		dir = d.DOSBoxDir
	}
	if t, ok := doors.TemplateFor(d.Template, dir); ok && t.EnableBulletins != nil {
		return t.EnableBulletins(dir, bbsName)
	}
	return nil
}

func templateBulletins(t doors.Template) []config.DoorBulletin {
	var out []config.DoorBulletin
	for _, b := range t.Bulletins {
		out = append(out, config.DoorBulletin{Title: b.Title, File: b.File, Public: b.Public})
	}
	return out
}

// offeredBulletins: the template's bulletins, for a door without any.
func offeredBulletins(d config.DoorConfig) []config.DoorBulletin {
	if len(d.Bulletins) > 0 {
		return nil
	}
	dir := d.Dir
	if d.Kind == "dosbox" {
		dir = d.DOSBoxDir
	}
	if t, ok := doors.TemplateFor(d.Template, dir); ok {
		return templateBulletins(t)
	}
	return nil
}

// handleDoorTemplateBulletins: POST /api/door-bulletins/{name} -- the
// door gets its template's bulletins (and the game is told to write
// them, and the daily maintenance, if it has none, that writes them).
func (s *Server) handleDoorTemplateBulletins(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	name := r.PathValue("name")
	for i := range c.Doors {
		d := &c.Doors[i]
		if d.Name != name {
			continue
		}
		dir := d.Dir
		if d.Kind == "dosbox" {
			dir = d.DOSBoxDir
		}
		t, ok := doors.TemplateFor(d.Template, dir)
		if !ok || len(t.Bulletins) == 0 {
			writeError(w, http.StatusBadRequest, "no bulletins known for this door -- add them by hand")
			return
		}
		if t.EnableBulletins != nil {
			if err := t.EnableBulletins(dir, c.BBS.Name); err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
		}
		d.Bulletins = templateBulletins(t)
		if d.Daily == "" && t.Daily != "" {
			d.Daily = t.Daily
		}
		if err := config.Save(s.BBSConfigPath, c); err != nil {
			writeError(w, http.StatusInternalServerError, "could not save config")
			return
		}
		s.logInfo("door %s: the template's bulletins added", name)
		writeJSON(w, http.StatusOK, s.doorsResponse(c))
		return
	}
	writeError(w, http.StatusNotFound, "no such door")
}
