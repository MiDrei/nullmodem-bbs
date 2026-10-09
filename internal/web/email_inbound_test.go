package web

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/emailgw"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestEmailInboundWebhook(t *testing.T) {
	srv, users, cfgPath := newTestServer(t)
	maik, err := users.Register("SwissMaik", "password123", 255)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := config.Load(cfgPath)
	c.Email = config.EmailConfig{Enabled: true, Domain: "bbs.example.ch",
		Receive: config.MailReceive{Webhook: true, WebhookSecret: "s3cret", ExtraDomains: []string{"other.example"}}}
	config.Save(cfgPath, c)
	srv.EmailGateway = &emailgw.Gateway{DB: srv.DB, Netmail: srv.Netmail, Users: users,
		Config: func() config.EmailConfig { c, _ := config.Load(cfgPath); return c.Email },
		Lang:   func(*user.User) string { return "en" }, BBSName: func() string { return "Test BBS" }}
	h := srv.Routes()
	mail := "From: Jurg <jm@sender.example>\r\nTo: list@sender.example\r\nSubject: Via webhook\r\n\r\nHello.\r\n"

	post := func(path, contentType string, body []byte, bearer string) int {
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
		req.Header.Set("Content-Type", contentType)
		if bearer != "" {
			req.Header.Set("Authorization", "Bearer "+bearer)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post("/api/email/inbound?key=wrong&to=swissmaik@bbs.example.ch", "message/rfc822", []byte(mail), ""); code != http.StatusUnauthorized {
		t.Fatalf("wrong secret: %d", code)
	}
	// The raw mail, the envelope recipient in ?to=.
	if code := post("/api/email/inbound?to=swissmaik@other.example", "message/rfc822", []byte(mail), "s3cret"); code != http.StatusOK {
		t.Fatalf("raw mail: %d", code)
	}
	// Mailgun's form: body-mime and recipient.
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	mw.WriteField("recipient", "SwissMaik@bbs.example.ch")
	mw.WriteField("body-mime", strings.Replace(mail, "Via webhook", "Via Mailgun", 1))
	mw.Close()
	if code := post("/api/email/inbound?key=s3cret", mw.FormDataContentType(), b.Bytes(), ""); code != http.StatusOK {
		t.Fatalf("form: %d", code)
	}
	// Forward Email's JSON, signed once a signing key is set.
	c.Email.Receive.WebhookSigningKey = "sign-key"
	config.Save(cfgPath, c)
	payload, _ := json.Marshal(map[string]any{
		"raw":        strings.Replace(mail, "Via webhook", "Via Forward Email", 1),
		"recipients": []string{"swissmaik@bbs.example.ch"},
		"session":    map[string]any{"recipient": "swissmaik@bbs.example.ch"},
		"subject":    "Via Forward Email",
	})
	signed := func(body []byte, sig string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/email/inbound?key=s3cret", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if sig != "" {
			req.Header.Set("X-Webhook-Signature", sig)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	m := hmac.New(sha256.New, []byte("sign-key"))
	m.Write(payload)
	if code := signed(payload, ""); code != http.StatusUnauthorized {
		t.Errorf("unsigned: %d", code)
	}
	if code := signed(payload, hex.EncodeToString(make([]byte, 32))); code != http.StatusUnauthorized {
		t.Errorf("wrong signature: %d", code)
	}
	if code := signed(payload, hex.EncodeToString(m.Sum(nil))); code != http.StatusOK {
		t.Fatalf("Forward Email: %d", code)
	}
	inbox, _ := srv.Netmail.Inbox(maik.ID)
	if len(inbox) != 3 {
		t.Fatalf("inbox has %d mails, want 3", len(inbox))
	}

	// Off: not there at all.
	c.Email.Receive.Webhook = false
	config.Save(cfgPath, c)
	if code := post("/api/email/inbound?key=s3cret", "message/rfc822", []byte(mail), ""); code != http.StatusNotFound {
		t.Errorf("webhook off: %d", code)
	}
}
