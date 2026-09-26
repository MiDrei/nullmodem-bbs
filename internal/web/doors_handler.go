package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/doors"
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
	Template          string   `json:"template"`
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
		Template:          d.Template,
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
		Template:          d.Template,
	}
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
		for _, lf := range d.LockFiles {
			if !filepath.IsLocal(lf) {
				return fmt.Sprintf("%s: lock file %q must be a path inside the door's directory", d.Name, lf)
			}
		}
	}
	return ""
}

func (s *Server) doorsResponse(c *config.Config) doorsDTO {
	list := make([]doorDTO, len(c.Doors))
	for i, d := range c.Doors {
		list[i] = toDoorDTO(d)
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
	for i, t := range doors.Templates {
		configured := false
		for _, d := range c.Doors {
			configured = configured || d.Template == t.ID
		}
		out[i] = doorTemplateDTO{
			Template:     t,
			Downloadable: t.Download != nil,
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
	installed := false
	switch {
	case nonEmptyDir(dir):
		// Already there (unpacked by hand, or installed before and
		// removed from the list) -- keep it as it is.
	case t.Download != nil:
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

	c.Doors = append(c.Doors, config.DoorConfig{
		Name:              t.Name,
		Kind:              "dosbox",
		MinSL:             0,
		DOSBoxDir:         dir,
		DOSBoxLaunchCmd:   t.DOSBoxLaunchCmd,
		DropFile:          t.DropFile,
		DropFileInDoorDir: t.DropFileInDoorDir,
		LockFiles:         t.LockFiles,
		Template:          t.ID,
	})
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
