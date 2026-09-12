package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/config"
)

// configDTO is the subset of the BBS config the web UI can view and
// edit. Fields like the database path and SSH host key path stay
// internal and are preserved as-is on save.
type configDTO struct {
	Name          string `json:"name"`
	Sysop         string `json:"sysop"`
	NewUserSL     int    `json:"new_user_sl"`
	TelnetEnabled bool   `json:"telnet_enabled"`
	TelnetAddr    string `json:"telnet_addr"`
	SSHEnabled    bool   `json:"ssh_enabled"`
	SSHAddr       string `json:"ssh_addr"`
}

func toDTO(c *config.Config) configDTO {
	return configDTO{
		Name:          c.BBS.Name,
		Sysop:         c.BBS.Sysop,
		NewUserSL:     c.BBS.NewUserSL,
		TelnetEnabled: c.Telnet.Enabled,
		TelnetAddr:    c.Telnet.Addr,
		SSHEnabled:    c.SSH.Enabled,
		SSHAddr:       c.SSH.Addr,
	}
}

func (s *Server) loadBBSConfig() (*config.Config, error) {
	c, err := config.Load(s.BBSConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return config.Default(), nil
		}
		return nil, err
	}
	return c, nil
}

func (s *Server) handleGetConfig(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, toDTO(c))
}

func (s *Server) handlePutConfig(w http.ResponseWriter, r *http.Request) {
	var dto configDTO
	if err := json.NewDecoder(r.Body).Decode(&dto); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if msg := validateConfigDTO(dto); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}

	c.BBS.Name = dto.Name
	c.BBS.Sysop = dto.Sysop
	c.BBS.NewUserSL = dto.NewUserSL
	c.Telnet.Enabled = dto.TelnetEnabled
	c.Telnet.Addr = dto.TelnetAddr
	c.SSH.Enabled = dto.SSHEnabled
	c.SSH.Addr = dto.SSHAddr

	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config": toDTO(c),
		"note":   "Restart the bbs daemon for changes to take effect.",
	})
}

func validateConfigDTO(dto configDTO) string {
	if strings.TrimSpace(dto.Name) == "" {
		return "name must not be empty"
	}
	if strings.TrimSpace(dto.Sysop) == "" {
		return "sysop must not be empty"
	}
	if dto.NewUserSL < 0 || dto.NewUserSL > 255 {
		return "new_user_sl must be between 0 and 255"
	}
	if dto.TelnetEnabled && strings.TrimSpace(dto.TelnetAddr) == "" {
		return "telnet_addr must not be empty when telnet is enabled"
	}
	if dto.SSHEnabled && strings.TrimSpace(dto.SSHAddr) == "" {
		return "ssh_addr must not be empty when ssh is enabled"
	}
	if !dto.TelnetEnabled && !dto.SSHEnabled {
		return "at least one of telnet or ssh must be enabled"
	}
	return ""
}
