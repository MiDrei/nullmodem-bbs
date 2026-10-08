package emailgw

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"database/sql"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"blitiri.com.ar/go/spf"
	"github.com/emersion/go-smtp"

	"github.com/midrei/nullmodem-bbs/internal/config"
)

// The mail server's limits: a mail is taken up to maxReceiveSize (more
// than the gateway keeps: bigger mail arrives as its headers and a
// note, see Receive), to maxRcpts callers, from maxPerIP connections
// at a time per address.
const (
	maxReceiveSize = 25 << 20
	maxRcpts       = 50
	maxPerIP       = 3
)

// Greylisting: a sender, recipient and network not seen before is told
// to come back; from greyDelay on its retry gets through, and the trio
// stays known for greyKeep.
var (
	greyDelay = 5 * time.Minute
	greyKeep  = 36 * 24 * time.Hour
	greyRetry = 24 * time.Hour
)

// Resolver is the DNS the mail server's checks use (block lists, SPF);
// net.DefaultResolver in production.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
	LookupTXT(ctx context.Context, name string) ([]string, error)
	LookupMX(ctx context.Context, name string) ([]*net.MX, error)
	LookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error)
	LookupAddr(ctx context.Context, addr string) ([]string, error)
}

// Receiver is the gateway's own mail server: it takes the domain's mail
// directly (the domain's MX points here) instead of the gateway
// fetching it from a mailbox. It follows the settings
// (config.MailReceive), starting and stopping as they change.
type Receiver struct {
	Gateway *Gateway
	// DataDir holds the server's TLS key and certificate (made on first
	// start, for STARTTLS).
	DataDir  string
	Resolver Resolver
	// Now is time.Now; tests set it.
	Now func() time.Time

	mu     sync.Mutex
	status ReceiveStatus
	conns  map[string]int
}

// ReceiveStatus is how the mail server is doing, for the admin.
type ReceiveStatus struct {
	Listening bool      `json:"listening"`
	Addr      string    `json:"addr"`
	Error     string    `json:"error"`
	Since     time.Time `json:"since"`
	Taken     int       `json:"taken"`
	Refused   int       `json:"refused"`
	LastAt    time.Time `json:"last_at"`
	LastFrom  string    `json:"last_from"`
}

// Status is the mail server's state now.
func (r *Receiver) Status() ReceiveStatus {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.status
}

func (r *Receiver) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Receiver) resolver() Resolver {
	if r.Resolver != nil {
		return r.Resolver
	}
	return net.DefaultResolver
}

func (r *Receiver) count(taken bool, from string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if taken {
		r.status.Taken++
		r.status.LastAt, r.status.LastFrom = r.now(), from
	} else {
		r.status.Refused++
	}
}

// Run keeps the mail server in line with the settings until ctx ends:
// listening while the gateway and its mail server are on, on the
// configured address.
func (r *Receiver) Run(ctx context.Context) {
	var srv *smtp.Server
	var addr, lastErr string
	stop := func() {
		if srv != nil {
			srv.Close()
			srv = nil
		}
		r.mu.Lock()
		r.status.Listening = false
		r.mu.Unlock()
	}
	defer stop()
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	for {
		cfg := r.Gateway.Config()
		want := cfg.Enabled && cfg.Domain != "" && cfg.Receive.SMTP
		if !want || cfg.Receive.ListenAddr() != addr {
			stop()
			addr = ""
		}
		if want && srv == nil {
			addr = cfg.Receive.ListenAddr()
			s, err := r.start(cfg, addr)
			r.mu.Lock()
			if err != nil {
				r.status.Error = err.Error()
				addr = ""
			} else {
				srv = s
				r.status = ReceiveStatus{Listening: true, Addr: addr, Since: r.now(), Taken: r.status.Taken, Refused: r.status.Refused,
					LastAt: r.status.LastAt, LastFrom: r.status.LastFrom}
			}
			r.mu.Unlock()
			if err != nil {
				// Tried again every round; said once.
				if err.Error() != lastErr {
					r.Gateway.logWarn("email gateway: mail server on %s: %v", cfg.Receive.ListenAddr(), err)
				}
				lastErr = err.Error()
			} else {
				lastErr = ""
				r.Gateway.logInfo("email gateway: mail server listening on %s for %s", addr, strings.Join(cfg.Domains(), ", "))
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// start listens on addr and serves the mail server there.
func (r *Receiver) start(cfg config.EmailConfig, addr string) (*smtp.Server, error) {
	cert, err := r.certificate(hostname(cfg))
	if err != nil {
		return nil, err
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	s := smtp.NewServer(&backend{r: r})
	s.Addr = ln.Addr().String()
	s.Domain = hostname(cfg)
	s.MaxMessageBytes = maxReceiveSize
	s.MaxRecipients = maxRcpts
	s.ReadTimeout = 5 * time.Minute
	s.WriteTimeout = time.Minute
	s.TLSConfig = &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12}
	s.ErrorLog = log.New(io.Discard, "", 0)
	go s.Serve(&checkedListener{Listener: ln, r: r})
	return s, nil
}

func hostname(cfg config.EmailConfig) string {
	if h := strings.TrimSpace(cfg.Receive.Hostname); h != "" {
		return h
	}
	return cfg.Domain
}

// certificate is the server's STARTTLS certificate: a self-signed one
// for host, made once and kept in DataDir. Mail servers delivering to
// an MX encrypt with it without checking who signed it.
func (r *Receiver) certificate(host string) (tls.Certificate, error) {
	certPath := filepath.Join(r.DataDir, "smtp-tls.crt")
	keyPath := filepath.Join(r.DataDir, "smtp-tls.key")
	if c, err := tls.LoadX509KeyPair(certPath, keyPath); err == nil {
		return c, nil
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, _ := rand.Int(rand.Reader, big.NewInt(1<<62))
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: host},
		DNSNames:     []string{host},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().AddDate(10, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return tls.Certificate{}, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	if r.DataDir != "" {
		os.WriteFile(certPath, certPEM, 0o644)
		os.WriteFile(keyPath, keyPEM, 0o600)
	}
	return tls.X509KeyPair(certPEM, keyPEM)
}

// checkedListener turns away, before the greeting, a server that has
// too many connections open or is on a block list.
type checkedListener struct {
	net.Listener
	r *Receiver
}

func (l *checkedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		ip := remoteIP(c)
		if reason := l.r.admit(ip); reason != "" {
			c.SetWriteDeadline(time.Now().Add(10 * time.Second))
			fmt.Fprintf(c, "554 5.7.1 %s\r\n", reason)
			c.Close()
			l.r.count(false, "")
			l.r.Gateway.logInfo("email gateway: refused a connection from %s: %s", ip, reason)
			continue
		}
		return &trackedConn{Conn: c, r: l.r, ip: ip}, nil
	}
}

// trackedConn gives its address's connection slot back on Close.
type trackedConn struct {
	net.Conn
	r    *Receiver
	ip   string
	once sync.Once
}

func (c *trackedConn) Close() error {
	c.once.Do(func() {
		c.r.mu.Lock()
		if c.r.conns[c.ip]--; c.r.conns[c.ip] <= 0 {
			delete(c.r.conns, c.ip)
		}
		c.r.mu.Unlock()
	})
	return c.Conn.Close()
}

func remoteIP(c net.Conn) string {
	host, _, err := net.SplitHostPort(c.RemoteAddr().String())
	if err != nil {
		return c.RemoteAddr().String()
	}
	return host
}

// admit takes a connection slot for ip, or says why not.
func (r *Receiver) admit(ip string) string {
	r.mu.Lock()
	if r.conns == nil {
		r.conns = map[string]int{}
	}
	if r.conns[ip] >= maxPerIP {
		r.mu.Unlock()
		return "too many connections from " + ip
	}
	r.conns[ip]++
	r.mu.Unlock()
	if list := r.listedAt(ip); list != "" {
		r.mu.Lock()
		if r.conns[ip]--; r.conns[ip] <= 0 {
			delete(r.conns, ip)
		}
		r.mu.Unlock()
		return ip + " is listed at " + list
	}
	return ""
}

// listedAt is the first block list ip is on, "" if none (or the lists
// couldn't be asked).
func (r *Receiver) listedAt(ip string) string {
	parsed := net.ParseIP(ip)
	if parsed == nil || parsed.IsLoopback() || parsed.IsPrivate() {
		return ""
	}
	v4 := parsed.To4()
	if v4 == nil {
		return "" // the lists here are for IPv4
	}
	rev := fmt.Sprintf("%d.%d.%d.%d", v4[3], v4[2], v4[1], v4[0])
	for _, list := range r.Gateway.Config().Receive.BlockLists() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		addrs, err := r.resolver().LookupHost(ctx, rev+"."+list)
		cancel()
		if err != nil {
			continue
		}
		for _, a := range addrs {
			// 127.0.0.x is a listing; 127.255.255.x is the list
			// refusing the query (e.g. through a public resolver).
			if strings.HasPrefix(a, "127.0.0.") {
				return list
			}
		}
	}
	return ""
}

type backend struct{ r *Receiver }

func (b *backend) NewSession(c *smtp.Conn) (smtp.Session, error) {
	return &session{r: b.r, conn: c, ip: remoteIP(c.Conn())}, nil
}

// session is one mail being taken: from MAIL FROM to DATA.
type session struct {
	r     *Receiver
	conn  *smtp.Conn
	ip    string
	from  string
	spam  bool
	rcpts []string
}

func smtpErr(code int, enh smtp.EnhancedCode, msg string) error {
	return &smtp.SMTPError{Code: code, EnhancedCode: enh, Message: msg}
}

func (s *session) Reset() {
	s.from, s.spam, s.rcpts = "", false, nil
}

func (s *session) Logout() error { return nil }

func (s *session) Mail(from string, _ *smtp.MailOptions) error {
	s.Reset()
	s.from = strings.ToLower(strings.TrimSpace(from))
	cfg := s.r.Gateway.Config()
	if !cfg.Receive.CheckSPF() || s.from == "" {
		return nil // a bounce (empty sender) has nothing to check
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, _ := spf.CheckHostWithSender(net.ParseIP(s.ip), s.conn.Hostname(), s.from,
		spf.WithContext(ctx), spf.WithResolver(s.r.resolver()))
	switch res {
	case spf.Fail:
		s.r.count(false, s.from)
		s.r.Gateway.logInfo("email gateway: refused mail from %s via %s: SPF fail", s.from, s.ip)
		return smtpErr(550, smtp.EnhancedCode{5, 7, 23}, "SPF check failed: "+s.ip+" may not send for this sender")
	case spf.SoftFail:
		s.spam = true
	case spf.TempError:
		return smtpErr(451, smtp.EnhancedCode{4, 4, 3}, "SPF check could not be done, try again later")
	}
	return nil
}

func (s *session) Rcpt(to string, _ *smtp.RcptOptions) error {
	cfg := s.r.Gateway.Config()
	u, err := s.r.Gateway.Recipient(cfg, to)
	if err != nil {
		return smtpErr(451, smtp.EnhancedCode{4, 3, 0}, "temporary failure, try again later")
	}
	if u == nil {
		s.r.count(false, s.from)
		return smtpErr(550, smtp.EnhancedCode{5, 1, 1}, "no such mailbox here")
	}
	if cfg.Receive.Greylisting() {
		ok, err := s.r.greylist(s.ip, s.from, strings.ToLower(to))
		if err != nil {
			s.r.Gateway.logWarn("email gateway: greylist: %v", err)
		} else if !ok {
			return smtpErr(451, smtp.EnhancedCode{4, 7, 1}, "greylisted, please try again in a few minutes")
		}
	}
	s.rcpts = append(s.rcpts, strings.ToLower(strings.Trim(to, "<> ")))
	return nil
}

func (s *session) Data(rd io.Reader) error {
	body, err := io.ReadAll(rd)
	if err != nil {
		return err
	}
	cfg := s.r.Gateway.Config()
	proto := "ESMTP"
	if _, ok := s.conn.TLSConnectionState(); ok {
		proto = "ESMTPS"
	}
	var b bytes.Buffer
	fmt.Fprintf(&b, "Received: from %s ([%s])\r\n\tby %s (NullModem BBS) with %s;\r\n\t%s\r\n",
		strings.NewReplacer("\r", "", "\n", "").Replace(s.conn.Hostname()), s.ip, hostname(cfg), proto,
		s.r.now().Format(time.RFC1123Z))
	if s.from != "" {
		fmt.Fprintf(&b, "Return-Path: <%s>\r\n", s.from)
	}
	b.Write(body)
	if err := s.r.Gateway.Receive(cfg, b.Bytes(), s.rcpts, s.spam); err != nil {
		s.r.Gateway.logWarn("email gateway: mail from %s: %v", s.from, err)
		return smtpErr(451, smtp.EnhancedCode{4, 3, 0}, "temporary failure, try again later")
	}
	s.r.count(true, s.from)
	return nil
}

// greylist reports whether mail from from to to via ip may pass now:
// a trio not seen before is noted and told to come back; its retry
// after greyDelay passes and keeps it known.
func (r *Receiver) greylist(ip, from, to string) (bool, error) {
	db := r.Gateway.DB
	if db == nil {
		return true, nil
	}
	key := network(ip) + "|" + from + "|" + to
	now := r.now()
	var first, passed int64
	err := db.QueryRow(`SELECT first_seen, passed FROM email_greylist WHERE key = ?`, key).Scan(&first, &passed)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err := db.Exec(`INSERT INTO email_greylist (key, first_seen, passed, last_seen) VALUES (?, ?, 0, ?)`,
			key, now.UnixMilli(), now.UnixMilli())
		r.pruneGreylist(now)
		return false, err
	case err != nil:
		return false, err
	}
	if passed == 0 && now.Sub(time.UnixMilli(first)) < greyDelay {
		return false, nil
	}
	_, err = db.Exec(`UPDATE email_greylist SET passed = 1, last_seen = ? WHERE key = ?`, now.UnixMilli(), key)
	return true, err
}

// pruneGreylist forgets trios not seen for greyKeep, and retries that
// never came within greyRetry.
func (r *Receiver) pruneGreylist(now time.Time) {
	r.Gateway.DB.Exec(`DELETE FROM email_greylist WHERE last_seen < ? OR (passed = 0 AND first_seen < ?)`,
		now.Add(-greyKeep).UnixMilli(), now.Add(-greyRetry).UnixMilli())
}

// network is ip's network for greylisting: a sender's retry may come
// from another server of the same farm (/24 for IPv4, /64 for IPv6).
func network(ip string) string {
	p := net.ParseIP(ip)
	if p == nil {
		return ip
	}
	if v4 := p.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String()
	}
	return p.Mask(net.CIDRMask(64, 128)).String()
}
