package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/offsite"
)

// The off-site copy of the backups (internal/offsite): where to, the
// encryption key, a test, "copy now". Secrets never leave the server.

const sshKeyComment = "nullmodem-bbs-backup"

type offsiteDTO struct {
	Enabled    bool   `json:"enabled"`
	Kind       string `json:"kind"`
	Recipient  string `json:"recipient"`
	KeepDaily  int    `json:"keep_daily"`
	KeepWeekly int    `json:"keep_weekly"`

	SFTPHost        string `json:"sftp_host"`
	SFTPPort        int    `json:"sftp_port"`
	SFTPUser        string `json:"sftp_user"`
	SFTPDir         string `json:"sftp_dir"`
	SFTPHasPassword bool   `json:"sftp_has_password"`
	SFTPPublicKey   string `json:"sftp_public_key"` // of the key the board logs in with
	SFTPHostKey     string `json:"sftp_host_key"`

	SwiftAuthURL       string `json:"swift_auth_url"`
	SwiftUser          string `json:"swift_user"`
	SwiftHasPassword   bool   `json:"swift_has_password"`
	SwiftProject       string `json:"swift_project"`
	SwiftUserDomain    string `json:"swift_user_domain"`
	SwiftProjectDomain string `json:"swift_project_domain"`
	SwiftRegion        string `json:"swift_region"`
	SwiftContainer     string `json:"swift_container"`
	SwiftPrefix        string `json:"swift_prefix"`

	Status offsite.Status `json:"status"`
}

func (s *Server) offsiteState(c *config.Config) offsiteDTO {
	o := c.Backup.Offsite
	d := offsiteDTO{Enabled: o.Enabled, Kind: o.Kind, Recipient: o.Recipient, KeepDaily: o.Daily(), KeepWeekly: o.Weekly(),
		SFTPHost: o.SFTP.Host, SFTPPort: o.SFTP.Port, SFTPUser: o.SFTP.User, SFTPDir: o.SFTP.Dir,
		SFTPHasPassword: o.SFTP.Password != "", SFTPHostKey: o.SFTP.HostKey,
		SwiftAuthURL: o.Swift.AuthURL, SwiftUser: o.Swift.User, SwiftHasPassword: o.Swift.Password != "",
		SwiftProject: o.Swift.Project, SwiftUserDomain: o.Swift.UserDomain, SwiftProjectDomain: o.Swift.ProjectDomain,
		SwiftRegion: o.Swift.Region, SwiftContainer: o.Swift.Container, SwiftPrefix: o.Swift.Prefix,
		Status: offsite.LoadStatus(s.DB)}
	if o.SFTP.Key != "" {
		d.SFTPPublicKey = offsite.PublicKeyOf(o.SFTP.Key, sshKeyComment)
	}
	return d
}

// handleGetOffsite: GET /api/backups/offsite.
func (s *Server) handleGetOffsite(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.offsiteState(c))
}

// handlePutOffsite: PUT /api/backups/offsite -- passwords only when
// they change ("" keeps them).
func (s *Server) handlePutOffsite(w http.ResponseWriter, r *http.Request) {
	var in struct {
		offsiteDTO
		SFTPPassword  string `json:"sftp_password"`
		SwiftPassword string `json:"swift_password"`
		ForgetSFTPKey bool   `json:"forget_sftp_key"`
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
	o := &c.Backup.Offsite
	if in.Kind != "" && in.Kind != "sftp" && in.Kind != "swift" {
		writeError(w, http.StatusBadRequest, "kind is sftp or swift")
		return
	}
	in.Recipient = strings.TrimSpace(in.Recipient)
	if in.Recipient != "" {
		if err := offsite.CheckRecipient(in.Recipient); err != nil {
			writeError(w, http.StatusBadRequest, "the public key isn't an age key (age1...)")
			return
		}
	}
	if in.Enabled && in.Recipient == "" {
		writeError(w, http.StatusBadRequest, "copies go out encrypted only: make or enter a key first")
		return
	}
	if in.KeepDaily < 1 || in.KeepWeekly < 0 {
		writeError(w, http.StatusBadRequest, "keep at least one daily copy")
		return
	}
	hostChanged := strings.TrimSpace(in.SFTPHost) != o.SFTP.Host || in.SFTPPort != o.SFTP.Port
	o.Enabled, o.Kind, o.Recipient = in.Enabled, in.Kind, in.Recipient
	o.KeepDaily, o.KeepWeekly = &in.KeepDaily, &in.KeepWeekly
	o.SFTP.Host, o.SFTP.Port, o.SFTP.User, o.SFTP.Dir = strings.TrimSpace(in.SFTPHost), in.SFTPPort, strings.TrimSpace(in.SFTPUser), strings.TrimSpace(in.SFTPDir)
	if hostChanged {
		o.SFTP.HostKey = "" // a new server: its key is confirmed again
	} else if in.SFTPHostKey != "" {
		o.SFTP.HostKey = in.SFTPHostKey
	}
	if in.SFTPPassword != "" {
		o.SFTP.Password = in.SFTPPassword
	}
	if in.ForgetSFTPKey {
		o.SFTP.Key = ""
	}
	o.Swift = config.OffsiteSwift{AuthURL: strings.TrimSpace(in.SwiftAuthURL), User: strings.TrimSpace(in.SwiftUser), Password: o.Swift.Password,
		Project: strings.TrimSpace(in.SwiftProject), UserDomain: strings.TrimSpace(in.SwiftUserDomain), ProjectDomain: strings.TrimSpace(in.SwiftProjectDomain),
		Region: strings.TrimSpace(in.SwiftRegion), Container: strings.TrimSpace(in.SwiftContainer), Prefix: strings.TrimSpace(in.SwiftPrefix)}
	if in.SwiftPassword != "" {
		o.Swift.Password = in.SwiftPassword
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	s.logInfo("off-site backup copy settings saved (%s, %s)", o.Kind, map[bool]string{true: "on", false: "off"}[o.Enabled])
	writeJSON(w, http.StatusOK, s.offsiteState(c))
}

// handleOffsiteAgeKey: POST /api/backups/offsite/age-key -- a new key
// pair; the private key goes to the sysop once and isn't kept here.
func (s *Server) handleOffsiteAgeKey(w http.ResponseWriter, r *http.Request) {
	priv, pub, err := offsite.NewKey()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not make a key")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"private": priv, "public": pub})
}

// handleOffsiteSSHKey: POST /api/backups/offsite/ssh-key -- a new key
// the board logs in to the SFTP server with; its public line goes into
// the server's authorized_keys.
func (s *Server) handleOffsiteSSHKey(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	priv, _, err := offsite.NewSSHKey(sshKeyComment)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not make a key")
		return
	}
	c.Backup.Offsite.SFTP.Key = priv
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	writeJSON(w, http.StatusOK, s.offsiteState(c))
}

// handleOffsiteTest: POST /api/backups/offsite/test -- for an SFTP
// server whose key isn't confirmed yet, its fingerprint to confirm
// (host_key); else a write/read/delete there.
func (s *Server) handleOffsiteTest(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	o := c.Backup.Offsite
	if o.Kind == "sftp" && o.SFTP.HostKey == "" {
		fp, err := offsite.ScanHostKey(ctx, o.SFTP)
		if fp == "" && err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		resp := map[string]string{"host_key": fp}
		if err != nil {
			resp["error"] = err.Error()
		}
		writeJSON(w, http.StatusOK, resp)
		return
	}
	if err := offsite.Test(ctx, o); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	names, _ := offsite.Remote(ctx, o)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "remote": names})
}

// handleOffsiteRun: POST /api/backups/offsite/run -- the newest backup
// there, now.
func (s *Server) handleOffsiteRun(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if !c.Backup.Offsite.Enabled || c.Backup.Offsite.Recipient == "" {
		writeError(w, http.StatusBadRequest, "turn the off-site copy on first")
		return
	}
	list, err := backup.List(c.Backup.Directory())
	if err != nil || len(list) == 0 {
		writeError(w, http.StatusBadRequest, "there's no backup yet -- back up now first")
		return
	}
	cp := &offsite.Copier{DB: s.DB, Logger: s.Logger, Config: func() config.BackupConfig { return c.Backup }}
	if _, err := cp.Copy(r.Context(), c.Backup, list[0]); err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, s.offsiteState(c))
}
