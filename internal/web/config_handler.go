package web

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/binkp"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/services"
	"git.maik.ch/nullmodem/bbs/internal/tosser"
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
	// NoCRAM -- see config.BinkpUplink.NoCRAM.
	NoCRAM bool `json:"no_cram"`
	// AKAAddresses -- see config.BinkpUplink.AKAAddresses' own doc
	// comment: which of this system's own FTN addresses this uplink
	// is restricted to (M_ADR presentation and Crash routing alike).
	// Empty means unrestricted (every configured address applies).
	AKAAddresses []string `json:"aka_addresses"`
	// Downlink -- see config.BinkpUplink.Downlink's own doc comment:
	// purely a UI grouping (Hubs vs. Nodes/Points), no behavioral
	// effect.
	Downlink bool `json:"downlink"`
	// PostAs -- see config.BinkpUplink.PostAs: the local user a point
	// (the sysop's reader app) reads and writes as. Points only.
	PostAs string `json:"post_as"`
}

// networkDTO is one config.Network. OriginalName is the name it was
// loaded under, sent back unchanged by the UI, so a rename can be
// carried over to the uplinks and areas that use the network.
type networkDTO struct {
	Name         string `json:"name"`
	Domain       string `json:"domain"`
	OriginalName string `json:"original_name,omitempty"`
}

type configDTO struct {
	Name                            string           `json:"name"`
	Sysop                           string           `json:"sysop"`
	Location                        string           `json:"location"`
	NewUserSL                       int              `json:"new_user_sl"`
	PublicFeeds                     bool             `json:"public_feeds"`
	FTNAddresses                    []string         `json:"ftn_addresses"`
	Networks                        []networkDTO     `json:"networks"`
	TelnetEnabled                   bool             `json:"telnet_enabled"`
	TelnetAddr                      string           `json:"telnet_addr"`
	SSHEnabled                      bool             `json:"ssh_enabled"`
	SSHAddr                         string           `json:"ssh_addr"`
	BinkpUplinks                    []binkpUplinkDTO `json:"binkp_uplinks"`
	BinkpDefaultPollIntervalSeconds int              `json:"binkp_default_poll_interval_seconds"`
	LastCallers                     lastCallersDTO   `json:"last_callers"`
}

// lastCallersDTO is config.LastCallersConfig.
type lastCallersDTO struct {
	Enabled     bool   `json:"enabled"`
	Area        string `json:"area"`
	Address     string `json:"address"`
	System      string `json:"system"`
	ShowAtLogin bool   `json:"show_at_login"`
}

func toDTO(c *config.Config) configDTO {
	uplinks := make([]binkpUplinkDTO, len(c.Binkp.Uplinks))
	for i, u := range c.Binkp.Uplinks {
		akaAddrs := u.AKAAddresses
		if akaAddrs == nil {
			akaAddrs = []string{}
		}
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
			NoCRAM:              u.NoCRAM,
			AKAAddresses:        akaAddrs,
			Downlink:            u.Downlink,
			PostAs:              strings.TrimSpace(u.PostAs),
		}
	}
	addrs := c.BBS.FTNAddresses
	if addrs == nil {
		addrs = []string{}
	}
	networks := make([]networkDTO, len(c.Networks))
	for i, n := range c.Networks {
		networks[i] = networkDTO{Name: n.Name, Domain: n.Domain, OriginalName: n.Name}
	}
	return configDTO{
		Networks:                        networks,
		Name:                            c.BBS.Name,
		Sysop:                           c.BBS.Sysop,
		Location:                        c.BBS.Location,
		NewUserSL:                       c.BBS.NewUserSL,
		PublicFeeds:                     c.BBS.PublicFeeds,
		FTNAddresses:                    addrs,
		TelnetEnabled:                   c.Telnet.Enabled,
		TelnetAddr:                      c.Telnet.Addr,
		SSHEnabled:                      c.SSH.Enabled,
		SSHAddr:                         c.SSH.Addr,
		BinkpUplinks:                    uplinks,
		BinkpDefaultPollIntervalSeconds: c.Binkp.PollIntervalSeconds,
		LastCallers: lastCallersDTO{
			Enabled:     c.InterBBS.LastCallers.Enabled,
			Area:        c.InterBBS.LastCallers.AreaTag(),
			Address:     c.InterBBS.LastCallers.Address,
			System:      c.InterBBS.LastCallers.SystemName(),
			ShowAtLogin: c.InterBBS.LastCallers.ShowAtLogin,
		},
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
	renames := networkRenames(dto)
	for i, u := range dto.BinkpUplinks {
		for from, to := range renames {
			if strings.EqualFold(u.Network, from) {
				dto.BinkpUplinks[i].Network = to
			}
		}
	}
	if msg := validateConfigDTO(dto); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	if msg := s.validatePostAs(dto.BinkpUplinks); msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}

	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	before := toDTO(c)

	c.BBS.Name = dto.Name
	c.BBS.Sysop = dto.Sysop
	c.BBS.Location = strings.TrimSpace(dto.Location)
	c.BBS.NewUserSL = dto.NewUserSL
	c.BBS.PublicFeeds = dto.PublicFeeds
	c.BBS.FTNAddresses = dto.FTNAddresses
	c.Networks = make([]config.Network, len(dto.Networks))
	for i, n := range dto.Networks {
		c.Networks[i] = config.Network{Name: strings.TrimSpace(n.Name), Domain: strings.ToLower(strings.TrimSpace(n.Domain))}
	}
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
			NoCRAM:              u.NoCRAM,
			AKAAddresses:        u.AKAAddresses,
			Downlink:            u.Downlink,
			PostAs:              strings.TrimSpace(u.PostAs),
		}
	}
	c.Binkp.PollIntervalSeconds = dto.BinkpDefaultPollIntervalSeconds
	c.InterBBS.LastCallers = config.LastCallersConfig{
		Enabled:     dto.LastCallers.Enabled,
		Area:        strings.ToUpper(strings.TrimSpace(dto.LastCallers.Area)),
		Address:     strings.TrimSpace(dto.LastCallers.Address),
		System:      strings.TrimSpace(dto.LastCallers.System),
		ShowAtLogin: dto.LastCallers.ShowAtLogin,
	}

	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	// The areas follow a renamed network only once the config naming it
	// is saved.
	for from, to := range renames {
		for _, st := range []config.NetworkRenamer{s.Messages, s.Files} {
			if _, err := st.RenameNetwork(from, to); err != nil {
				writeError(w, http.StatusInternalServerError, "could not rename the network's areas")
				return
			}
		}
	}

	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s updated the BBS configuration", claims.Subject)
		for from, to := range renames {
			s.logInfo("%s renamed network %s to %s", claims.Subject, from, to)
		}
	}

	s.markConfigRestarts(before, toDTO(c))

	writeJSON(w, http.StatusOK, map[string]any{
		"config": toDTO(c),
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

	// Mirror tosser.effectiveAKAAddresses: a restricted AKAAddresses
	// list, if set, is what would actually be presented on a real dial
	// -- testing with the full unrestricted list instead would give a
	// falsely reassuring result for an uplink that's deliberately
	// restricted.
	presentedAddresses := c.BBS.FTNAddresses
	if len(req.AKAAddresses) > 0 {
		presentedAddresses = req.AKAAddresses
	}

	result, err := binkp.Dial(ctx, req.Host, binkp.Config{
		OurAddresses: presentedAddresses,
		Password:     req.Password,
		NoCRAM:       req.NoCRAM,
		SysName:      c.BBS.Name,
		Sysop:        c.BBS.Sysop,
		Location:     c.BBS.Location,
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
		BBSName:      c.BBS.Name,
		Sysop:        c.BBS.Sysop,
		Location:     c.BBS.Location,
		Uplinks:      c.Binkp.Uplinks,
		EchoStore:    s.EchoAreafix,
		FileStore:    s.FileAreafix,
		Files:        s.Files,
		Archive:      s.Archive,
	}
	ticCfg := &tosser.TICConfig{Files: s.Files}
	result, err := tosser.Poll(ctx, c.BBS.FTNAddresses, c.BBS.Name, config.BinkpUplink{
		Address:        req.Address,
		Host:           req.Host,
		Password:       req.Password,
		NoCRAM:         req.NoCRAM,
		PacketPassword: req.PacketPassword,
		Network:        req.Network,
		AKAAddresses:   req.AKAAddresses,
	}, c.Binkp.Uplinks, s.Netmail, s.Messages, s.Users, robot, ticCfg, s.BinkpLog)
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
	seen := map[string]bool{}
	for i, n := range dto.Networks {
		name := strings.TrimSpace(n.Name)
		if name == "" {
			return fmt.Sprintf("network %d: name must not be empty", i+1)
		}
		if seen[strings.ToLower(name)] {
			return fmt.Sprintf("network %q is defined twice", name)
		}
		seen[strings.ToLower(name)] = true
		if !domainPattern.MatchString(strings.ToLower(strings.TrimSpace(n.Domain))) {
			return fmt.Sprintf("network %s: domain must be 1-20 letters, digits, - or _ (as after the @ in 21:3/100@fsxnet)", name)
		}
	}
	for i, u := range dto.BinkpUplinks {
		if u.Network != "" && len(dto.Networks) > 0 && !seen[strings.ToLower(u.Network)] {
			return fmt.Sprintf("binkp uplink %d: network %q is not defined", i+1, u.Network)
		}
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

// domainPattern is what an FTN domain may look like.
var domainPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,19}$`)

// networkRenames maps each network the UI renamed from its original
// name to its new one.
func networkRenames(dto configDTO) map[string]string {
	renames := map[string]string{}
	for _, n := range dto.Networks {
		from, to := strings.TrimSpace(n.OriginalName), strings.TrimSpace(n.Name)
		if from != "" && to != "" && from != to {
			renames[from] = to
		}
	}
	return renames
}

// markConfigRestarts records which daemons need a restart to pick up
// the difference between two saved configs -- each reads its part of
// the config only at startup.
func (s *Server) markConfigRestarts(before, after configDTO) {
	same := func(a, b any) bool {
		x, _ := json.Marshal(a)
		y, _ := json.Marshal(b)
		return string(x) == string(y)
	}
	if before.Name != after.Name || before.Sysop != after.Sysop {
		s.markRestartNeeded("BBS name or sysop changed", services.BBS, services.Mailer)
	}
	if before.Location != after.Location {
		s.markRestartNeeded("Location changed", services.Mailer)
	}
	if before.LastCallers != after.LastCallers {
		s.markRestartNeeded("InterBBS Last Callers changed", services.BBS)
	}
	if before.NewUserSL != after.NewUserSL {
		s.markRestartNeeded("New-user security level changed", services.BBS)
	}
	if before.TelnetEnabled != after.TelnetEnabled || before.TelnetAddr != after.TelnetAddr ||
		before.SSHEnabled != after.SSHEnabled || before.SSHAddr != after.SSHAddr {
		s.markRestartNeeded("Telnet/SSH settings changed", services.BBS)
	}
	if !same(before.FTNAddresses, after.FTNAddresses) {
		s.markRestartNeeded("FTN addresses changed", services.BBS, services.Mailer, services.Web)
	}
	if !same(before.BinkpUplinks, after.BinkpUplinks) || !same(before.Networks, after.Networks) ||
		before.BinkpDefaultPollIntervalSeconds != after.BinkpDefaultPollIntervalSeconds {
		s.markRestartNeeded("BinkP settings changed", services.Mailer)
	}
}

// validatePostAs checks the uplinks' "post as" settings: only on a
// point (a downlink with a point address), and naming a local user.
func (s *Server) validatePostAs(uplinks []binkpUplinkDTO) string {
	for _, u := range uplinks {
		name := strings.TrimSpace(u.PostAs)
		if name == "" {
			continue
		}
		a, err := mail.ParseAddress(u.Address)
		if !u.Downlink || err != nil || a.Point == 0 {
			return fmt.Sprintf("%s: \"post as\" is only for a point of this system (a downlink with a point address like 21:3/194.1)", u.Address)
		}
		if s.Users != nil {
			if _, err := s.Users.ByUsername(name); err != nil {
				return fmt.Sprintf("%s: there is no user %q to post as", u.Address, name)
			}
		}
	}
	return ""
}
