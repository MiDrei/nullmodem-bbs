package web

import (
	"crypto/subtle"
	"io"
	"mime"
	"net/http"
	"strings"
)

// handleEmailInbound: POST /api/email/inbound -- a forwarding service
// (Cloudflare Email Routing with a worker, Mailgun, ...) hands over a
// mail for the gateway's domains, instead of the BBS being their MX
// itself. The secret (config.MailReceive.WebhookSecret) comes as
// "Authorization: Bearer <secret>" or ?key=<secret>.
//
// The mail is the raw message as the body (message/rfc822, text/plain
// or octet-stream), its envelope recipients in ?to= (comma-separated) or
// X-Envelope-To; or a form with "body-mime" and "recipient" (Mailgun).
// Without recipients, the mail's own headers say whom it's for.
func (s *Server) handleEmailInbound(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil || s.EmailGateway == nil {
		writeError(w, http.StatusServiceUnavailable, "not available")
		return
	}
	cfg := c.Email
	rc := cfg.Receive
	if !cfg.Enabled || !rc.Webhook || rc.WebhookSecret == "" {
		http.NotFound(w, r)
		return
	}
	key := r.URL.Query().Get("key")
	if auth, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		key = auth
	}
	if subtle.ConstantTimeCompare([]byte(key), []byte(rc.WebhookSecret)) != 1 {
		s.logWarn("email webhook: wrong secret from %s", clientIP(r))
		writeError(w, http.StatusUnauthorized, "wrong secret")
		return
	}

	var raw []byte
	var rcpts []string
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mt {
	case "multipart/form-data", "application/x-www-form-urlencoded":
		if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		raw = []byte(r.FormValue("body-mime"))
		rcpts = splitAddrs(r.FormValue("recipient"))
	default:
		raw, err = io.ReadAll(r.Body)
		if err != nil {
			writeError(w, http.StatusBadRequest, "could not read the mail")
			return
		}
	}
	if len(raw) == 0 {
		writeError(w, http.StatusBadRequest, "no mail in the request")
		return
	}
	if to := r.URL.Query().Get("to"); to != "" {
		rcpts = splitAddrs(to)
	} else if to := r.Header.Get("X-Envelope-To"); to != "" && rcpts == nil {
		rcpts = splitAddrs(to)
	}
	spam := r.URL.Query().Get("spam") == "1"
	if err := s.EmailGateway.Receive(cfg, raw, rcpts, spam); err != nil {
		s.logWarn("email webhook: %v", err)
		// The service tries again later.
		writeError(w, http.StatusServiceUnavailable, "could not take the mail now")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func splitAddrs(s string) []string {
	var out []string
	for _, a := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' }) {
		if a = strings.Trim(a, "<>"); a != "" {
			out = append(out, strings.ToLower(a))
		}
	}
	return out
}
