package web

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/emailgw"
	"git.maik.ch/nullmodem/kit/ansi"
)

// The email gateway's settings and state in the admin (System -> Email
// gateway). Passwords never leave the server.

type mailServerDTO struct {
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Security    string `json:"security"`
	User        string `json:"user"`
	HasPassword bool   `json:"has_password"`
	Password    string `json:"password,omitempty"` // in: "" keeps it
	Folder      string `json:"folder,omitempty"`
}

type emailMailDTO struct {
	ID       int64  `json:"id"`
	Incoming bool   `json:"incoming"`
	User     string `json:"user"`
	Address  string `json:"address"`
	Subject  string `json:"subject"`
	At       string `json:"at"`
	Status   string `json:"status"` // "received", "queued", "sent", "failed"
	Error    string `json:"error,omitempty"`
}

type emailDTO struct {
	Enabled       bool          `json:"enabled"`
	Domain        string        `json:"domain"`
	IMAP          mailServerDTO `json:"imap"`
	SMTP          mailServerDTO `json:"smtp"`
	MinSL         int           `json:"min_sl"`
	DailyLimit    int           `json:"daily_limit"`
	DeleteFetched bool          `json:"delete_fetched"`
	DeliverSpam   bool          `json:"deliver_spam"`

	Status  emailgw.Status `json:"status"`
	Waiting int            `json:"waiting"`
	Failed  int            `json:"failed"` // in the last day
	// Example is the sysop's own address, to show how addresses look.
	Example string         `json:"example"`
	Recent  []emailMailDTO `json:"recent"`
}

func toMailServerDTO(m config.MailServer) mailServerDTO {
	return mailServerDTO{Host: m.Host, Port: m.Port, Security: orDefault(m.Security, "tls"), User: m.User, HasPassword: m.Password != "", Folder: m.Folder}
}

func orDefault(v, d string) string {
	if v == "" {
		return d
	}
	return v
}

func (s *Server) emailState(r *http.Request, c *config.Config) emailDTO {
	e := c.Email
	d := emailDTO{Enabled: e.Enabled, Domain: e.Domain, IMAP: toMailServerDTO(e.IMAP), SMTP: toMailServerDTO(e.SMTP),
		MinSL: e.MinSL, DailyLimit: e.Limit(), DeleteFetched: e.DeleteFetched, DeliverSpam: e.DeliverSpam,
		Status: emailgw.LoadStatus(s.DB), Recent: []emailMailDTO{}}
	d.Waiting, _, d.Failed, _ = s.Netmail.EmailQueue(time.Now())
	if claims, ok := claimsFromContext(r.Context()); ok {
		d.Example = emailgw.Address(e, claims.Subject)
	}
	rows, err := s.DB.Query(`SELECT m.id, m.from_user_id IS NULL, COALESCE(u.username, m.to_name), m.email, m.subject, m.posted_at,
			m.sent_at IS NOT NULL, COALESCE(e.failed, 0), COALESCE(e.last_error, '')
		FROM netmail_messages m
		LEFT JOIN email_meta e ON e.netmail_id = m.id
		LEFT JOIN users u ON u.id = CASE WHEN m.from_user_id IS NULL THEN m.to_user_id ELSE m.from_user_id END
		WHERE m.email != '' ORDER BY m.id DESC LIMIT 30`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var m emailMailDTO
			var subject string
			var at time.Time
			var sent, failed bool
			if rows.Scan(&m.ID, &m.Incoming, &m.User, &m.Address, &subject, &at, &sent, &failed, &m.Error) != nil {
				continue
			}
			m.Subject = ansi.DecodeCP437([]byte(subject))
			m.At = at.Format(time.RFC3339)
			switch {
			case m.Incoming:
				m.Status, m.Error = "received", ""
			case sent:
				m.Status, m.Error = "sent", ""
			case failed:
				m.Status = "failed"
			default:
				m.Status = "queued"
			}
			d.Recent = append(d.Recent, m)
		}
	}
	return d
}

// handleGetEmail: GET /api/email.
func (s *Server) handleGetEmail(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	writeJSON(w, http.StatusOK, s.emailState(r, c))
}

func applyMailServer(dst *config.MailServer, in mailServerDTO) {
	dst.Host, dst.Port, dst.User, dst.Folder = strings.TrimSpace(in.Host), in.Port, strings.TrimSpace(in.User), strings.TrimSpace(in.Folder)
	dst.Security = in.Security
	if dst.Security == "tls" {
		dst.Security = "" // the default
	}
	if in.Password != "" {
		dst.Password = in.Password
	}
}

func validSecurity(v string) bool {
	return v == "" || v == "tls" || v == "starttls" || v == "none"
}

// readEmailSettings applies the request's settings to c.
func readEmailSettings(w http.ResponseWriter, r *http.Request, c *config.Config) bool {
	var in emailDTO
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return false
	}
	in.Domain = strings.ToLower(strings.Trim(strings.TrimSpace(in.Domain), "@."))
	if in.Domain != "" && (!strings.Contains(in.Domain, ".") || strings.ContainsAny(in.Domain, " @/:")) {
		writeError(w, http.StatusBadRequest, "the domain looks wrong (e.g. bbs.example.com)")
		return false
	}
	if !validSecurity(in.IMAP.Security) || !validSecurity(in.SMTP.Security) {
		writeError(w, http.StatusBadRequest, "security is tls, starttls or none")
		return false
	}
	if in.DailyLimit < 1 || in.MinSL < 0 || in.MinSL > 255 {
		writeError(w, http.StatusBadRequest, "a daily limit of at least 1 and a level from 0 to 255, please")
		return false
	}
	e := &c.Email
	e.Domain, e.MinSL, e.DeleteFetched, e.DeliverSpam = in.Domain, in.MinSL, in.DeleteFetched, in.DeliverSpam
	limit := in.DailyLimit
	e.DailyLimit = &limit
	applyMailServer(&e.IMAP, in.IMAP)
	applyMailServer(&e.SMTP, in.SMTP)
	e.Enabled = in.Enabled
	if e.Enabled && (e.Domain == "" || e.IMAP.Host == "" || e.SMTP.Host == "") {
		writeError(w, http.StatusBadRequest, "the gateway needs a domain, an IMAP and an SMTP server")
		return false
	}
	return true
}

// handlePutEmail: PUT /api/email -- passwords only when they change.
func (s *Server) handlePutEmail(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if !readEmailSettings(w, r, c) {
		return
	}
	if err := config.Save(s.BBSConfigPath, c); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save config")
		return
	}
	s.logInfo("email gateway %s (%s)", map[bool]string{true: "on", false: "off"}[c.Email.Enabled], c.Email.Domain)
	if s.EmailGateway != nil {
		s.EmailGateway.Wake()
	}
	writeJSON(w, http.StatusOK, s.emailState(r, c))
}

// handleTestEmail: POST /api/email/test -- logs in to both servers
// with the settings sent (not saved).
func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load config")
		return
	}
	if !readEmailSettings(w, r, c) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	if err := emailgw.Test(ctx, c.Email); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFetchEmail: POST /api/email/fetch -- a round now.
func (s *Server) handleFetchEmail(w http.ResponseWriter, r *http.Request) {
	if s.EmailGateway != nil {
		s.EmailGateway.Wake()
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}
