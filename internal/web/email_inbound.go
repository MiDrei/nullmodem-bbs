package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strings"
)

// maxInboundMailBytes bounds a webhook request: a mail of the 25 MB the
// own mail server takes, as JSON (Forward Email) with room to spare.
const maxInboundMailBytes = 64 << 20

// forwardEmailPayload is the part of Forward Email's webhook JSON the
// gateway needs: the raw mail and whom it was for.
type forwardEmailPayload struct {
	Raw        string   `json:"raw"`
	Recipients []string `json:"recipients"`
	Session    struct {
		Recipient string `json:"recipient"`
	} `json:"session"`
}

// validSignature checks a hex HMAC-SHA256 of body under key, as
// Forward Email sends it in X-Webhook-Signature.
func validSignature(key string, body []byte, sig string) bool {
	got, err := hex.DecodeString(strings.TrimSpace(sig))
	if err != nil || len(got) == 0 {
		return false
	}
	m := hmac.New(sha256.New, []byte(key))
	m.Write(body)
	return hmac.Equal(got, m.Sum(nil))
}

// handleEmailInbound: POST /api/email/inbound -- a forwarding service
// (Cloudflare Email Routing with a worker, Mailgun, ...) hands over a
// mail for the gateway's domains, instead of the BBS being their MX
// itself. The secret (config.MailReceive.WebhookSecret) comes as
// "Authorization: Bearer <secret>" or ?key=<secret>.
//
// The mail is the raw message as the body (message/rfc822, text/plain
// or octet-stream), its envelope recipients in ?to= (comma-separated) or
// X-Envelope-To; or a form with "body-mime" and "recipient" (Mailgun);
// or Forward Email's JSON with "raw" and "recipients", signed in
// X-Webhook-Signature when a signing key is set. Without recipients,
// the mail's own headers say whom it's for. A taken mail is answered
// 200 (Forward Email counts nothing else as delivered).
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

	body, err := io.ReadAll(r.Body)
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			// Refusing would only have the service try again for days.
			s.logWarn("email webhook: a mail over %d MB from %s dropped", maxInboundMailBytes>>20, clientIP(r))
			w.Write([]byte("OK"))
			return
		}
		writeError(w, http.StatusBadRequest, "could not read the mail")
		return
	}
	if rc.WebhookSigningKey != "" && !validSignature(rc.WebhookSigningKey, body, r.Header.Get("X-Webhook-Signature")) {
		s.logWarn("email webhook: wrong signature from %s", clientIP(r))
		writeError(w, http.StatusUnauthorized, "wrong signature")
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	var raw []byte
	var rcpts []string
	mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type"))
	switch mt {
	case "application/json":
		var p forwardEmailPayload
		if err := json.Unmarshal(body, &p); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body")
			return
		}
		raw = []byte(p.Raw)
		rcpts = splitAddrs(strings.Join(p.Recipients, ","))
		if rcpts == nil && p.Session.Recipient != "" {
			rcpts = splitAddrs(p.Session.Recipient)
		}
	case "multipart/form-data", "application/x-www-form-urlencoded":
		if err := r.ParseMultipartForm(32 << 20); err != nil && err != http.ErrNotMultipart {
			writeError(w, http.StatusBadRequest, "invalid form")
			return
		}
		raw = []byte(r.FormValue("body-mime"))
		rcpts = splitAddrs(r.FormValue("recipient"))
	default:
		raw = body
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
	w.Write([]byte("OK"))
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
