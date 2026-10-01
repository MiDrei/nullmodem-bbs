package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/config"
)

// The web admin's Backups page (internal/backup): settings, the
// backups on disk, writing one now, downloading and deleting.

type backupSettingsDTO struct {
	Enabled      bool   `json:"enabled"`
	Hour         int    `json:"hour"`
	KeepDaily    int    `json:"keep_daily"`
	KeepWeekly   int    `json:"keep_weekly"`
	Dir          string `json:"dir"`
	IncludeFiles bool   `json:"include_files"`
}

type backupResponse struct {
	Settings backupSettingsDTO `json:"settings"`
	Backups  []backup.Info     `json:"backups"`
	// FreeBytes is the free space where the backups go, 0 if unknown.
	FreeBytes uint64 `json:"free_bytes"`
}

// BackupSources is what a backup holds on this server: the database,
// both config files, menus and screens, the data directory's keys --
// and the file areas and doors, if asked for.
func (s *Server) BackupSources(c *config.Config) backup.Sources {
	return backup.Sources{
		DB: s.DB, DBPath: s.DBPath, DataDir: filepath.Dir(s.DBPath),
		ConfigFiles: []string{s.BBSConfigPath, s.WebConfigPath},
		ConfigDirs:  []string{c.BBS.MenusDir, c.BBS.ScreensDir},
		BulkDirs:    []string{c.BBS.FilesDir, c.BBS.DoorsDir},
	}
}

// BackupOptions is how backups are kept, by c.
func BackupOptions(c *config.Config) backup.Options {
	b := c.Backup
	return backup.Options{Dir: b.Directory(), KeepDaily: b.Daily(), KeepWeekly: b.Weekly(), IncludeFiles: b.IncludeFiles}
}

func (s *Server) backupResponse(c *config.Config) (backupResponse, error) {
	b := c.Backup
	list, err := backup.List(b.Directory())
	if err != nil {
		return backupResponse{}, err
	}
	return backupResponse{
		Settings: backupSettingsDTO{
			Enabled: b.On(), Hour: b.RunHour(), KeepDaily: b.Daily(), KeepWeekly: b.Weekly(),
			Dir: b.Directory(), IncludeFiles: b.IncludeFiles,
		},
		Backups:   list,
		FreeBytes: freeBytes(b.Directory()),
	}, nil
}

func (s *Server) handleGetBackups(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	resp, err := s.backupResponse(c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the backups")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handlePutBackupSettings saves the settings; the nightly backup reads
// them each time, so no restart is needed.
func (s *Server) handlePutBackupSettings(w http.ResponseWriter, r *http.Request) {
	var d backupSettingsDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch {
	case d.Hour < 0 || d.Hour > 23:
		writeError(w, http.StatusBadRequest, "hour must be 0-23")
		return
	case d.KeepDaily < 1 || d.KeepWeekly < 0:
		writeError(w, http.StatusBadRequest, "keep at least one daily backup")
		return
	case d.Dir == "":
		writeError(w, http.StatusBadRequest, "the backup directory must be set")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	enabled, hour, daily, weekly := d.Enabled, d.Hour, d.KeepDaily, d.KeepWeekly
	c.Backup = config.BackupConfig{Enabled: &enabled, Hour: &hour, KeepDaily: &daily, KeepWeekly: &weekly, Dir: d.Dir, IncludeFiles: d.IncludeFiles}
	if c.Backup.Dir == "data/backups" {
		c.Backup.Dir = ""
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s changed the backup settings", claims.Subject)
	}
	resp, err := s.backupResponse(c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the backups")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// handleRunBackup writes a backup now.
func (s *Server) handleRunBackup(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	info, err := backup.Run(r.Context(), s.BackupSources(c), BackupOptions(c), time.Now())
	if errors.Is(err, backup.ErrRunning) {
		writeError(w, http.StatusConflict, "a backup is already being written")
		return
	}
	if err != nil {
		s.logWarn("backup: %v", err)
		writeError(w, http.StatusInternalServerError, "the backup failed (see the log)")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s wrote a backup: %s (%.1f MB)", claims.Subject, info.Name, float64(info.Size)/(1<<20))
	}
	resp, err := s.backupResponse(c)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list the backups")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) backupPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := r.PathValue("name")
	if !backup.ValidName(name) {
		writeError(w, http.StatusBadRequest, "not a backup")
		return "", false
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return "", false
	}
	path := filepath.Join(c.Backup.Directory(), name)
	if _, err := os.Stat(path); err != nil {
		writeError(w, http.StatusNotFound, "no such backup")
		return "", false
	}
	return path, true
}

// handleDownloadBackup: GET /api/backups/{name}.
func (s *Server) handleDownloadBackup(w http.ResponseWriter, r *http.Request) {
	path, ok := s.backupPath(w, r)
	if !ok {
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Content-Disposition", `attachment; filename="`+filepath.Base(path)+`"`)
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s downloaded the backup %s", claims.Subject, filepath.Base(path))
	}
	http.ServeFile(w, r, path)
}

// handleDeleteBackup: DELETE /api/backups/{name}.
func (s *Server) handleDeleteBackup(w http.ResponseWriter, r *http.Request) {
	path, ok := s.backupPath(w, r)
	if !ok {
		return
	}
	if err := os.Remove(path); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the backup")
		return
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s deleted the backup %s", claims.Subject, filepath.Base(path))
	}
	w.WriteHeader(http.StatusNoContent)
}
