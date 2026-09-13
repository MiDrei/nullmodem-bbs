package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
)

// binkpTestTimeout bounds how long handleTestBinkpConnection waits
// for a full handshake against a real uplink before giving up.
const binkpTestTimeout = 20 * time.Second

// configDTO is the subset of the BBS config the web UI can view and
// edit. Fields like the database path and SSH host key path stay
// internal and are preserved as-is on save.
type binkpUplinkDTO struct {
	Address  string `json:"address"`
	Host     string `json:"host"`
	Password string `json:"password"`
}

type configDTO struct {
	Name          string           `json:"name"`
	Sysop         string           `json:"sysop"`
	NewUserSL     int              `json:"new_user_sl"`
	FTNAddress    string           `json:"ftn_address"`
	TelnetEnabled bool             `json:"telnet_enabled"`
	TelnetAddr    string           `json:"telnet_addr"`
	SSHEnabled    bool             `json:"ssh_enabled"`
	SSHAddr       string           `json:"ssh_addr"`
	BinkpUplinks  []binkpUplinkDTO `json:"binkp_uplinks"`
}

func toDTO(c *config.Config) configDTO {
	uplinks := make([]binkpUplinkDTO, len(c.Binkp.Uplinks))
	for i, u := range c.Binkp.Uplinks {
		uplinks[i] = binkpUplinkDTO{Address: u.Address, Host: u.Host, Password: u.Password}
	}
	return configDTO{
		Name:          c.BBS.Name,
		Sysop:         c.BBS.Sysop,
		NewUserSL:     c.BBS.NewUserSL,
		FTNAddress:    c.BBS.FTNAddress,
		TelnetEnabled: c.Telnet.Enabled,
		TelnetAddr:    c.Telnet.Addr,
		SSHEnabled:    c.SSH.Enabled,
		SSHAddr:       c.SSH.Addr,
		BinkpUplinks:  uplinks,
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
	c.BBS.FTNAddress = dto.FTNAddress
	c.Telnet.Enabled = dto.TelnetEnabled
	c.Telnet.Addr = dto.TelnetAddr
	c.SSH.Enabled = dto.SSHEnabled
	c.SSH.Addr = dto.SSHAddr
	c.Binkp.Uplinks = make([]config.BinkpUplink, len(dto.BinkpUplinks))
	for i, u := range dto.BinkpUplinks {
		c.Binkp.Uplinks[i] = config.BinkpUplink{Address: u.Address, Host: u.Host, Password: u.Password}
	}

	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated the BBS configuration", claims.Subject)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"config": toDTO(c),
		"note":   "Restart the bbs daemon for changes to take effect.",
	})
}

// handleTestBinkpConnection dials a BinkP host with our own configured
// FTN address and reports whether the handshake (address exchange,
// and authentication if a password is given) succeeded -- no files
// are sent or requested, so this never touches mail content or the
// uplink's queue, just proves connectivity/credentials work.
func (s *Server) handleTestBinkpConnection(w http.ResponseWriter, r *http.Request) {
	var req binkpUplinkDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		writeError(w, http.StatusBadRequest, "host must not be empty")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if strings.TrimSpace(c.BBS.FTNAddress) == "" {
		writeError(w, http.StatusBadRequest, "set this system's own FTN address above before testing an uplink")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), binkpTestTimeout)
	defer cancel()

	result, err := binkp.Dial(ctx, req.Host, binkp.Config{
		OurAddresses: []string{c.BBS.FTNAddress},
		Password:     req.Password,
		SysName:      c.BBS.Name,
		Sysop:        c.BBS.Sysop,
	})
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("connection failed: %v", err))
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s tested a BinkP connection to %s", claims.Subject, req.Host)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"remote_addresses": result.RemoteAddresses,
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
	for i, u := range dto.BinkpUplinks {
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Sprintf("binkp uplink %d: host must not be empty", i+1)
		}
	}
	return ""
}
