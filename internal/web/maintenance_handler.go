package web

import (
	"encoding/json"
	"net/http"
	"sync"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/maintenance"
)

// maintenanceDTO is config.MaintenanceConfig with its defaults filled
// in; 0 means "keep everything" where a limit is optional.
type maintenanceDTO struct {
	Enabled            bool `json:"enabled"`
	Hour               int  `json:"hour"`
	MessageKeepDays    int  `json:"message_keep_days"`
	MessageKeepMax     int  `json:"message_keep_max"`
	DataAreaKeepDays   int  `json:"data_area_keep_days"`
	FileKeepDays       int  `json:"file_keep_days"`
	NetmailKeepDays    int  `json:"netmail_keep_days"`
	LogKeepRows        int  `json:"log_keep_rows"`
	TranscriptKeepDays int  `json:"transcript_keep_days"`
	ArchiveKeepDays    int  `json:"archive_keep_days"`
	Vacuum             bool `json:"vacuum"`
	PendingUserDays    int  `json:"pending_user_days"`
}

func toMaintenanceDTO(m config.MaintenanceConfig) maintenanceDTO {
	return maintenanceDTO{
		Enabled: m.Enabled, Hour: m.RunHour(),
		MessageKeepDays: m.MessageDays(), MessageKeepMax: m.MessageMax(), DataAreaKeepDays: m.DataAreaDays(),
		FileKeepDays: m.FileDays(), NetmailKeepDays: m.NetmailDays(), LogKeepRows: m.LogRows(),
		TranscriptKeepDays: m.TranscriptDays(), ArchiveKeepDays: m.ArchiveDays(), Vacuum: m.VacuumAfter(),
		PendingUserDays: m.PendingDays(),
	}
}

func fromMaintenanceDTO(d maintenanceDTO) config.MaintenanceConfig {
	p := func(v int) *int { return &v }
	vac := d.Vacuum
	return config.MaintenanceConfig{
		Enabled: d.Enabled, Hour: p(d.Hour),
		MessageKeepDays: p(d.MessageKeepDays), MessageKeepMax: p(d.MessageKeepMax), DataAreaKeepDays: p(d.DataAreaKeepDays),
		FileKeepDays: p(d.FileKeepDays), NetmailKeepDays: p(d.NetmailKeepDays), LogKeepRows: p(d.LogKeepRows),
		TranscriptKeepDays: p(d.TranscriptKeepDays), ArchiveKeepDays: p(d.ArchiveKeepDays), Vacuum: &vac,
		PendingUserDays: p(d.PendingUserDays),
	}
}

type maintenanceResponse struct {
	Settings maintenanceDTO      `json:"settings"`
	Last     *maintenance.Report `json:"last"`
}

func (s *Server) maintenanceResponse(c *config.Config) maintenanceResponse {
	resp := maintenanceResponse{Settings: toMaintenanceDTO(c.Maintenance)}
	if s.DB != nil {
		resp.Last, _ = maintenance.Last(s.DB)
	}
	return resp
}

func (s *Server) handleGetMaintenance(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.maintenanceResponse(c))
}

// handlePutMaintenance saves the cleanup settings. The mailer reads
// them each time it checks, so no restart is needed.
func (s *Server) handlePutMaintenance(w http.ResponseWriter, r *http.Request) {
	var d maintenanceDTO
	if err := json.NewDecoder(r.Body).Decode(&d); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	switch {
	case d.Hour < 0 || d.Hour > 23:
		writeError(w, http.StatusBadRequest, "hour must be 0-23")
		return
	case d.MessageKeepDays < 0 || d.MessageKeepMax < 0 || d.DataAreaKeepDays < 0 || d.FileKeepDays < 0 ||
		d.NetmailKeepDays < 0 || d.TranscriptKeepDays < 0 || d.ArchiveKeepDays < 0 || d.PendingUserDays < 0:
		writeError(w, http.StatusBadRequest, "limits must not be negative (0 keeps everything)")
		return
	case d.LogKeepRows < 100:
		writeError(w, http.StatusBadRequest, "keep at least 100 log entries")
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	c.Maintenance = fromMaintenanceDTO(d)
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	maintenance.ApplyLimits(c.Maintenance)
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s changed the maintenance settings", claims.Subject)
	}
	writeJSON(w, http.StatusOK, s.maintenanceResponse(c))
}

// maintenanceRunning keeps two runs from overlapping.
var maintenanceRunning sync.Mutex

// handleRunMaintenance runs the cleanup now -- or with {"dry": true}
// only counts what it would delete (the preview) -- by the saved
// settings, and returns the report.
func (s *Server) handleRunMaintenance(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Dry bool `json:"dry"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	if s.DB == nil {
		writeError(w, http.StatusInternalServerError, "maintenance is not available")
		return
	}
	if !maintenanceRunning.TryLock() {
		writeError(w, http.StatusConflict, "a cleanup is already running")
		return
	}
	defer maintenanceRunning.Unlock()
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	rep := maintenance.Run(r.Context(), maintenance.Deps{
		DB: s.DB, Files: s.Files, Logs: s.Logs, SessionLog: s.BinkpLog, Archive: s.Archive, DBPath: s.DBPath,
	}, c.Maintenance, req.Dry)
	if !req.Dry {
		if err := maintenance.Save(s.DB, rep); err != nil {
			s.logWarn("recording the maintenance run: %v", err)
		}
		if claims, ok := claimsFromContext(r.Context()); ok {
			s.logInfo("%s ran the maintenance: deleted %s", claims.Subject, rep.Summary())
		}
	}
	writeJSON(w, http.StatusOK, rep)
}
