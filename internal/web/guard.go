package web

import (
	"net"
	"net/http"
	"strings"

	"git.maik.ch/nullmodem/bbs/internal/guard"
)

// clientIP is the caller's address. Behind a reverse proxy on the same
// machine (Caddy on apollo), the request comes from loopback and the
// caller is the last X-Forwarded-For entry -- the one the proxy added.
// From anywhere else the header is ignored: anyone could send one.
func clientIP(r *http.Request) string {
	ip := guard.IP(r.RemoteAddr)
	if a := net.ParseIP(ip); a != nil && a.IsLoopback() {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if last := strings.TrimSpace(parts[len(parts)-1]); last != "" {
				return guard.IP(last)
			}
		}
	}
	return ip
}

// loginAllowed refuses a login from a locked-out or blocked address
// (429/403 with the reason); it returns the caller's IP.
func (s *Server) loginAllowed(w http.ResponseWriter, r *http.Request) (string, bool) {
	ip := clientIP(r)
	if s.Guard == nil {
		return ip, true
	}
	v, err := s.Guard.Check(ip)
	if err != nil || !v.Blocked {
		return ip, true
	}
	status := http.StatusTooManyRequests
	if v.Until.IsZero() {
		status = http.StatusForbidden
	}
	writeError(w, status, v.Message())
	return ip, false
}

// loginFailed records a failed login and answers it: 401, or 429 when
// it just locked the address out.
func (s *Server) loginFailed(w http.ResponseWriter, ip, handle, source string) {
	if s.Guard != nil {
		if v, err := s.Guard.Fail(ip, handle, source); err == nil && v.Blocked {
			writeError(w, http.StatusTooManyRequests, v.Message())
			return
		}
	}
	writeError(w, http.StatusUnauthorized, "invalid username or password")
}

func (s *Server) loginSucceeded(ip string) {
	if s.Guard != nil {
		s.Guard.Succeed(ip)
	}
}

// pendingApproval refuses (403) what an account waiting for the
// sysop's approval may not do yet -- posting, uploads, netmail to
// anyone but the sysop; true if it did.
func (s *Server) pendingApproval(w http.ResponseWriter, userID int64) bool {
	u, err := s.Users.ByID(userID)
	if err != nil || u.Validated {
		return false
	}
	writeError(w, http.StatusForbidden, "your account is waiting for the sysop's approval -- until then you can read, and write netmail to the sysop")
	return true
}
