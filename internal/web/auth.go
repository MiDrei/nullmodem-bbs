package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

const tokenTTL = 12 * time.Hour

// claims is shared by both JWT-issuing login paths (handleLogin for
// the sysop admin panel, handleBBSLogin for the BBS user portal) --
// UserID is unset (0) on an admin token, since every existing admin
// handler already resolves the account via Subject (the username)
// instead and nothing there needs to change.
type claims struct {
	SecurityLevel int   `json:"sl"`
	UserID        int64 `json:"uid,omitempty"`
	jwt.RegisteredClaims
}

type ctxKey int

const claimsCtxKey ctxKey = iota

// handleLogin authenticates against the shared user store and, for
// accounts at sysop level, issues a JWT for use against the rest of
// the admin API.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u, err := s.Users.Authenticate(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrInvalidCredentials) {
			s.logWarn("failed admin login attempt for %q", req.Username)
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "authentication failed")
		return
	}
	if u.SecurityLevel < user.SLSysop {
		s.logWarn("admin login rejected for %s: not sysop-level", u.Username)
		writeError(w, http.StatusForbidden, "sysop access required")
		return
	}
	s.logInfo("%s logged into the admin UI", u.Username)

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		SecurityLevel: u.SecurityLevel,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.Username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	})
	signed, err := token.SignedString(s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":          signed,
		"username":       u.Username,
		"security_level": u.SecurityLevel,
		"expires_at":     now.Add(tokenTTL),
	})
}

// requireAuth validates the Bearer token on protected routes and
// makes its claims available to the handler via the request context.
func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(authHeader) <= len(prefix) || authHeader[:len(prefix)] != prefix {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tokenString := authHeader[len(prefix):]

		var c claims
		token, err := jwt.ParseWithClaims(tokenString, &c, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return s.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}
		if c.SecurityLevel < user.SLSysop {
			writeError(w, http.StatusForbidden, "sysop access required")
			return
		}

		ctx := context.WithValue(r.Context(), claimsCtxKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// claimsFromContext retrieves the JWT claims requireAuth attaches to
// the request context, for handlers (e.g. file upload) that need to
// know which sysop account is making the request.
func claimsFromContext(ctx context.Context) (claims, bool) {
	c, ok := ctx.Value(claimsCtxKey).(claims)
	return c, ok
}

// handleBBSLogin is handleLogin's counterpart for the BBS user portal
// (routes/(portal)/* in web/src): any registered account may log in
// here, not just sysop-level ones -- what a request is then allowed
// to do is gated per-endpoint (see requireBBSUser and each
// bbs_*_handler.go's own Area.CanRead/CanWrite/CanDownload/CanUpload
// checks), the same way a Telnet/SSH session is gated, rather than by
// a blanket SL floor on login itself.
func (s *Server) handleBBSLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	u, err := s.Users.Authenticate(req.Username, req.Password)
	if err != nil {
		if errors.Is(err, user.ErrInvalidCredentials) {
			s.logWarn("failed BBS portal login attempt for %q", req.Username)
			writeError(w, http.StatusUnauthorized, "invalid username or password")
			return
		}
		writeError(w, http.StatusInternalServerError, "authentication failed")
		return
	}
	s.logInfo("%s logged into the BBS web portal", u.Username)

	now := time.Now()
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims{
		SecurityLevel: u.SecurityLevel,
		UserID:        u.ID,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   u.Username,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	})
	signed, err := token.SignedString(s.JWTSecret)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not issue token")
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"token":          signed,
		"username":       u.Username,
		"security_level": u.SecurityLevel,
		"timezone":       u.Timezone,
		"expires_at":     now.Add(tokenTTL),
	})
}

// requireBBSUser is requireAuth without the sysop SL floor: it only
// validates the JWT itself (signature, expiry), since the BBS portal
// is open to any registered account. Individual bbs_*_handler.go
// handlers enforce their own per-area SL gates.
func (s *Server) requireBBSUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeader := r.Header.Get("Authorization")
		const prefix = "Bearer "
		if len(authHeader) <= len(prefix) || authHeader[:len(prefix)] != prefix {
			writeError(w, http.StatusUnauthorized, "missing bearer token")
			return
		}
		tokenString := authHeader[len(prefix):]

		var c claims
		token, err := jwt.ParseWithClaims(tokenString, &c, func(t *jwt.Token) (any, error) {
			if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
				return nil, errors.New("unexpected signing method")
			}
			return s.JWTSecret, nil
		})
		if err != nil || !token.Valid {
			writeError(w, http.StatusUnauthorized, "invalid or expired token")
			return
		}

		ctx := context.WithValue(r.Context(), claimsCtxKey, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
