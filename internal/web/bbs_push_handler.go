package web

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/midrei/nullmodem-bbs/internal/i18n"
	"github.com/midrei/nullmodem-bbs/internal/push"
)

// The mobile reader's notifications (internal/push): the device asks
// for the public key, subscribes with its browser, and registers that
// subscription here with what it wants to hear about.

type pushSubscriptionRequest struct {
	Endpoint string `json:"endpoint"`
	Keys     struct {
		P256dh string `json:"p256dh"`
		Auth   string `json:"auth"`
	} `json:"keys"`
	Netmail  bool `json:"netmail"`
	Echomail bool `json:"echomail"`
}

func (s *Server) pushReady(w http.ResponseWriter) bool {
	if s.Push == nil {
		writeError(w, http.StatusServiceUnavailable, "notifications are not available on this BBS")
		return false
	}
	return true
}

// handleGetPushKey: GET /api/bbs/push/key.
func (s *Server) handleGetPushKey(w http.ResponseWriter, r *http.Request) {
	if !s.pushReady(w) {
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"public_key": s.Push.Keys.Public})
}

// handleGetPushSubscription: GET /api/bbs/push/subscription?endpoint=
// -- this device's settings, 404 if it isn't subscribed.
func (s *Server) handleGetPushSubscription(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if !s.pushReady(w) {
		return
	}
	sub, err := s.Push.Store.Get(claims.UserID, r.URL.Query().Get("endpoint"))
	if errors.Is(err, push.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not subscribed")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not load the subscription")
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"netmail": sub.Netmail, "echomail": sub.Echomail})
}

// handlePutPushSubscription: PUT /api/bbs/push/subscription.
func (s *Server) handlePutPushSubscription(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if !s.pushReady(w) {
		return
	}
	var req pushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if !strings.HasPrefix(req.Endpoint, "https://") || req.Keys.P256dh == "" || req.Keys.Auth == "" {
		writeError(w, http.StatusBadRequest, "not a push subscription")
		return
	}
	sub := push.Subscription{
		UserID: claims.UserID, Endpoint: req.Endpoint, P256dh: req.Keys.P256dh, Auth: req.Keys.Auth,
		Origin: requestOrigin(r), Netmail: req.Netmail, Echomail: req.Echomail,
	}
	if err := s.Push.Store.Save(sub); err != nil {
		writeError(w, http.StatusInternalServerError, "could not save the subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDeletePushSubscription: DELETE /api/bbs/push/subscription
// {endpoint}.
func (s *Server) handleDeletePushSubscription(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if !s.pushReady(w) {
		return
	}
	var req pushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if err := s.Push.Store.Delete(claims.UserID, req.Endpoint); err != nil {
		writeError(w, http.StatusInternalServerError, "could not delete the subscription")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestPush: POST /api/bbs/push/test {endpoint} -- a test
// notification to this device.
func (s *Server) handleTestPush(w http.ResponseWriter, r *http.Request) {
	claims, ok := claimsFromContext(r.Context())
	if !ok {
		writeError(w, http.StatusUnauthorized, "missing auth claims")
		return
	}
	if !s.pushReady(w) {
		return
	}
	var req pushSubscriptionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	sub, err := s.Push.Store.Get(claims.UserID, req.Endpoint)
	if err != nil {
		writeError(w, http.StatusNotFound, "not subscribed")
		return
	}
	lang := s.requestLang(r)
	n := push.Notification{Title: i18n.T(lang, "push.test_title"), Body: i18n.T(lang, "push.test_body"), URL: "/reader/", Tag: "test"}
	if err := s.Push.Send(r.Context(), sub, n); err != nil {
		s.Logger.Warn("test notification: %v", err)
		writeError(w, http.StatusBadGateway, "the push service did not take the notification")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// requestOrigin is the site the request came to, e.g.
// https://bbs.example.org -- the VAPID contact push services ask for.
func requestOrigin(r *http.Request) string {
	if o := r.Header.Get("Origin"); strings.HasPrefix(o, "https://") {
		return o
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return "https://" + host
}
