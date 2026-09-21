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
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/tosser"
)

// binkpRequestTimeout bounds how long a single BinkP operation
// (a connection test, or a manual "send now" poll) waits on a real
// uplink before giving up.
const binkpRequestTimeout = 60 * time.Second

// configDTO is the subset of the BBS config the web UI can view and
// edit. Fields like the database path and SSH host key path stay
// internal and are preserved as-is on save.
type binkpUplinkDTO struct {
	Address             string `json:"address"`
	Host                string `json:"host"`
	Password            string `json:"password"`
	PollDisabled        bool   `json:"poll_disabled"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	PacketPassword      string `json:"packet_password"`
	// TICPassword is stored and round-tripped but not used yet -- see
	// config.BinkpUplink's doc comment. AreafixPassword/
	// FilefixPassword authenticate outbound subscription requests --
	// see internal/tosser's RequestEchoAreaSubscription/
	// RequestFileAreaSubscription.
	TICPassword     string `json:"tic_password"`
	AreafixPassword string `json:"areafix_password"`
	FilefixPassword string `json:"filefix_password"`
	// Network labels which FTN network this uplink carries echomail
	// for -- see config.BinkpUplink.Network's doc comment.
	Network string `json:"network"`
	// Hold -- see config.BinkpUplink.Hold's own doc comment: never
	// dialed automatically at all, not even for pending/Crash mail,
	// only via "Send Now".
	Hold bool `json:"hold"`
}

type configDTO struct {
	Name                            string           `json:"name"`
	Sysop                           string           `json:"sysop"`
	NewUserSL                       int              `json:"new_user_sl"`
	FTNAddresses                    []string         `json:"ftn_addresses"`
	TelnetEnabled                   bool             `json:"telnet_enabled"`
	TelnetAddr                      string           `json:"telnet_addr"`
	SSHEnabled                      bool             `json:"ssh_enabled"`
	SSHAddr                         string           `json:"ssh_addr"`
	BinkpUplinks                    []binkpUplinkDTO `json:"binkp_uplinks"`
	BinkpDefaultPollIntervalSeconds int              `json:"binkp_default_poll_interval_seconds"`
}

func toDTO(c *config.Config) configDTO {
	uplinks := make([]binkpUplinkDTO, len(c.Binkp.Uplinks))
	for i, u := range c.Binkp.Uplinks {
		uplinks[i] = binkpUplinkDTO{
			Address:             u.Address,
			Host:                u.Host,
			Password:            u.Password,
			PollDisabled:        u.PollDisabled,
			PollIntervalSeconds: u.PollIntervalSeconds,
			PacketPassword:      u.PacketPassword,
			TICPassword:         u.TICPassword,
			AreafixPassword:     u.AreafixPassword,
			FilefixPassword:     u.FilefixPassword,
			Network:             u.Network,
			Hold:                u.Hold,
		}
	}
	addrs := c.BBS.FTNAddresses
	if addrs == nil {
		addrs = []string{}
	}
	return configDTO{
		Name:                            c.BBS.Name,
		Sysop:                           c.BBS.Sysop,
		NewUserSL:                       c.BBS.NewUserSL,
		FTNAddresses:                    addrs,
		TelnetEnabled:                   c.Telnet.Enabled,
		TelnetAddr:                      c.Telnet.Addr,
		SSHEnabled:                      c.SSH.Enabled,
		SSHAddr:                         c.SSH.Addr,
		BinkpUplinks:                    uplinks,
		BinkpDefaultPollIntervalSeconds: c.Binkp.PollIntervalSeconds,
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
	c.BBS.FTNAddresses = dto.FTNAddresses
	c.Telnet.Enabled = dto.TelnetEnabled
	c.Telnet.Addr = dto.TelnetAddr
	c.SSH.Enabled = dto.SSHEnabled
	c.SSH.Addr = dto.SSHAddr
	c.Binkp.Uplinks = make([]config.BinkpUplink, len(dto.BinkpUplinks))
	for i, u := range dto.BinkpUplinks {
		c.Binkp.Uplinks[i] = config.BinkpUplink{
			Address:             u.Address,
			Host:                u.Host,
			Password:            u.Password,
			PollDisabled:        u.PollDisabled,
			PollIntervalSeconds: u.PollIntervalSeconds,
			PacketPassword:      u.PacketPassword,
			TICPassword:         u.TICPassword,
			AreafixPassword:     u.AreafixPassword,
			FilefixPassword:     u.FilefixPassword,
			Network:             u.Network,
			Hold:                u.Hold,
		}
	}
	c.Binkp.PollIntervalSeconds = dto.BinkpDefaultPollIntervalSeconds

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
	if len(c.BBS.FTNAddresses) == 0 {
		writeError(w, http.StatusBadRequest, "set this system's own FTN address above before testing an uplink")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), binkpRequestTimeout)
	defer cancel()

	result, err := binkp.Dial(ctx, req.Host, binkp.Config{
		OurAddresses: c.BBS.FTNAddresses,
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

// handleSendNowBinkp polls one configured uplink immediately (see
// internal/tosser), instead of waiting for the mailer daemon's next
// scheduled poll -- sending any queued netmail and filing away
// whatever the uplink sends back, all in one BinkP session.
func (s *Server) handleSendNowBinkp(w http.ResponseWriter, r *http.Request) {
	var req binkpUplinkDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Host) == "" {
		writeError(w, http.StatusBadRequest, "host must not be empty")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "netmail store is not configured")
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if len(c.BBS.FTNAddresses) == 0 {
		writeError(w, http.StatusBadRequest, "set this system's own FTN address above before sending")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), binkpRequestTimeout)
	defer cancel()

	robot := &tosser.RobotConfig{
		OurAddresses: c.BBS.FTNAddresses,
		Uplinks:      c.Binkp.Uplinks,
		EchoStore:    s.EchoAreafix,
		FileStore:    s.FileAreafix,
		Files:        s.Files,
	}
	ticCfg := &tosser.TICConfig{Files: s.Files}
	result, err := tosser.Poll(ctx, c.BBS.FTNAddresses, c.BBS.Name, config.BinkpUplink{
		Address:        req.Address,
		Host:           req.Host,
		Password:       req.Password,
		PacketPassword: req.PacketPassword,
		Network:        req.Network,
	}, c.Binkp.Uplinks, s.Netmail, s.Messages, s.Users, robot, ticCfg)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("poll failed: %v", err))
		return
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		skippedNote := ""
		if len(result.SkippedFiles) > 0 {
			skippedNote = fmt.Sprintf(", skipped %d unsupported file(s): %s", len(result.SkippedFiles), strings.Join(result.SkippedFiles, ", "))
		}
		s.logInfo("%s manually polled BinkP uplink %s (sent %d netmail, %d echomail, forwarded %d echomail, %d file(s), received %d netmail, %d echomail, %d file(s)%s)", claims.Subject, req.Host, result.Sent, result.SentEcho, result.ForwardedEcho, result.ForwardedFiles, result.Received, result.ReceivedEcho, result.ReceivedFiles, skippedNote)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"sent":             result.Sent,
		"sent_echo":        result.SentEcho,
		"forwarded_echo":   result.ForwardedEcho,
		"forwarded_files":  result.ForwardedFiles,
		"received":         result.Received,
		"received_echo":    result.ReceivedEcho,
		"received_files":   result.ReceivedFiles,
		"remote_addresses": result.RemoteAddresses,
		"skipped_files":    result.SkippedFiles,
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
	for i, addr := range dto.FTNAddresses {
		if strings.TrimSpace(addr) == "" {
			return fmt.Sprintf("ftn address %d: must not be empty", i+1)
		}
		if _, err := mail.ParseAddress(addr); err != nil {
			return fmt.Sprintf("ftn address %d: %v", i+1, err)
		}
	}
	if dto.BinkpDefaultPollIntervalSeconds < 0 {
		return "binkp_default_poll_interval_seconds must not be negative"
	}
	for i, u := range dto.BinkpUplinks {
		if strings.TrimSpace(u.Host) == "" {
			return fmt.Sprintf("binkp uplink %d: host must not be empty", i+1)
		}
		if u.PollIntervalSeconds < 0 {
			return fmt.Sprintf("binkp uplink %d: poll_interval_seconds must not be negative", i+1)
		}
		if len(u.PacketPassword) > 8 {
			return fmt.Sprintf("binkp uplink %d: packet_password must be at most 8 characters (FTS-0001's packet header field)", i+1)
		}
	}
	return ""
}
