package web

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/doors"
	"git.maik.ch/nullmodem/bbs/internal/services"
)

// DoorUpdateCheckEvery is how often RunDoorUpdateCheck asks the doors'
// projects for new releases.
const DoorUpdateCheckEvery = 12 * time.Hour

// doorUpdateTimeout bounds downloading and unpacking one update.
const doorUpdateTimeout = 5 * time.Minute

// doorUpdating lets one door update run at a time (two at once would
// overwrite each other's backup).
var doorUpdating sync.Mutex

func doorDir(d config.DoorConfig) string {
	if d.Kind == "dosbox" {
		return d.DOSBoxDir
	}
	return d.Dir
}

// doorTemplate is the template d was installed from, if it has a
// release feed.
func doorTemplate(d config.DoorConfig) (doors.Template, bool) {
	t, ok := doors.TemplateFor(d.Template, doorDir(d))
	if !ok || t.Download == nil || t.Download.GitHub == "" {
		return doors.Template{}, false
	}
	return t, true
}

// CheckDoorReleases asks for the newest release of each configured
// door that has a release feed and records it.
func (s *Server) CheckDoorReleases(ctx context.Context) {
	c, err := s.loadBBSConfig()
	if err != nil || s.DB == nil {
		return
	}
	client := &http.Client{Timeout: 30 * time.Second}
	seen := map[string]bool{}
	for _, d := range c.Doors {
		t, ok := doorTemplate(d)
		if !ok || seen[t.ID] {
			continue
		}
		seen[t.ID] = true
		rel, err := doors.LatestRelease(ctx, client, t)
		if err != nil {
			s.logWarn("door update check: %v", err)
		}
		if err := doors.SaveRelease(s.DB, t.ID, rel, err, time.Now()); err != nil {
			s.logWarn("door update check: %v", err)
		}
	}
}

// RunDoorUpdateCheck checks for door releases a minute after it starts
// and then every DoorUpdateCheckEvery, until ctx ends.
func (s *Server) RunDoorUpdateCheck(ctx context.Context) {
	timer := time.NewTimer(time.Minute)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		s.CheckDoorReleases(ctx)
		timer.Reset(DoorUpdateCheckEvery)
	}
}

// latestDoorVersion is the newest known release of t, "" if none was
// found yet (Install then takes the template's own).
func (s *Server) latestDoorVersion(t doors.Template) string {
	if s.DB == nil || t.Download == nil || t.Download.GitHub == "" {
		return ""
	}
	known, _ := doors.LatestReleases(s.DB)
	return known[t.ID].Version
}

// addDoorVersions fills in the doors' installed and newest versions.
func (s *Server) addDoorVersions(c *config.Config, list []doorDTO) {
	known := map[string]doors.KnownRelease{}
	if s.DB != nil {
		known, _ = doors.LatestReleases(s.DB)
	}
	for i, d := range c.Doors {
		t, ok := doorTemplate(d)
		if !ok {
			continue
		}
		list[i].Version = doors.InstalledVersion(t, doorDir(d))
		if k, ok := known[t.ID]; ok {
			list[i].LatestVersion = k.Version
			list[i].ReleaseURL = k.URL
			list[i].UpdateAvailable = doors.UpdateAvailable(list[i].Version, k.Version)
		}
	}
}

// handleCheckDoorUpdates: POST /api/doors/check-updates -- asks for
// new releases now.
func (s *Server) handleCheckDoorUpdates(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	s.CheckDoorReleases(ctx)
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.doorsResponse(c))
}

// handleUpdateDoor: POST /api/doors/update/{name} -- brings the door to
// its newest release (see doors.Update) and restarts its background
// program, if it has one.
func (s *Server) handleUpdateDoor(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	var door *config.DoorConfig
	for i := range c.Doors {
		if c.Doors[i].Name == r.PathValue("name") {
			door = &c.Doors[i]
		}
	}
	if door == nil {
		writeError(w, http.StatusNotFound, "no such door")
		return
	}
	t, ok := doorTemplate(*door)
	if !ok {
		writeError(w, http.StatusBadRequest, "this door can't be updated from here")
		return
	}
	latest := s.latestDoorVersion(t)
	dir := doorDir(*door)
	from := doors.InstalledVersion(t, dir)
	if !doors.UpdateAvailable(from, latest) {
		writeError(w, http.StatusConflict, "the door is up to date")
		return
	}
	if !doorUpdating.TryLock() {
		writeError(w, http.StatusConflict, "a door update is running already")
		return
	}
	defer doorUpdating.Unlock()
	ctx, cancel := context.WithTimeout(r.Context(), doorUpdateTimeout)
	defer cancel()
	if err := doors.Update(ctx, &http.Client{Timeout: doorUpdateTimeout}, t, dir, latest); err != nil {
		s.logWarn("updating door %s to %s: %v", door.Name, latest, err)
		status := http.StatusBadGateway
		if errors.Is(err, context.DeadlineExceeded) {
			status = http.StatusGatewayTimeout
		}
		writeError(w, status, "could not update "+door.Name+": "+err.Error())
		return
	}
	if s.Services != nil && len(door.Program) > 0 {
		name := doors.Program{Door: door.Name}.ServiceName()
		if err := s.Services.RequestRestart(name, services.ModeNow); err != nil {
			s.logWarn("restarting %s: %v", name, err)
		}
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated door %s from %s to %s", claims.Subject, door.Name, from, latest)
	}
	writeJSON(w, http.StatusOK, s.doorsResponse(c))
}
