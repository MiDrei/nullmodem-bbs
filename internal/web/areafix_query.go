package web

import (
	"encoding/json"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/tosser"
)

// Asking a hub what it has linked to us (%QUERY), and taking its word
// over into our own record of the subscriptions. The reply is the
// hub's richest-parsing netmail after the request (see
// handleGetAreafixListReply for why "richest").

// queryKey is where the last %QUERY request to address (for kind) is
// remembered: its netmail id, so only replies after it count.
func queryKey(kind, address string) string {
	if kind != "file" {
		kind = "echo"
	}
	return "areafix_query:" + kind + ":" + address
}

// handleRequestAreafixQuery: POST /api/binkp/areafix/query {uplink, kind}.
func (s *Server) handleRequestAreafixQuery(w http.ResponseWriter, r *http.Request) {
	var req areafixListRequestDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Uplink.Host) == "" || strings.TrimSpace(req.Uplink.Address) == "" {
		writeError(w, http.StatusBadRequest, "uplink host and address must not be empty")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
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
	uplink := areafixUplinkFromDTO(req.Uplink)
	var msg *netmail.Message
	if req.Kind == "file" {
		msg, err = tosser.RequestFileAreaQuery(s.Netmail, c.BBS.FTNAddresses, c.BBS.Name, uplink)
	} else {
		msg, err = tosser.RequestEchoAreaQuery(s.Netmail, c.BBS.FTNAddresses, c.BBS.Name, uplink)
	}
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Sprintf("areafix query failed: %v", err))
		return
	}
	s.DB.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		queryKey(req.Kind, uplink.Address), strconv.FormatInt(msg.ID, 10)+" "+strconv.FormatInt(time.Now().Unix(), 10))
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s asked %s what it has linked (%s)", claims.Subject, req.Uplink.Host, req.Kind)
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued_message_id": msg.ID})
}

// queryReplyDTO is the hub's answer to the last %QUERY, if it came.
type queryReplyDTO struct {
	// Asked is when the last %QUERY went out (zero: never asked).
	Asked    time.Time `json:"asked"`
	Found    bool      `json:"found"`
	PostedAt time.Time `json:"posted_at"`
	Subject  string    `json:"subject"`
	RawBody  string    `json:"raw_body"`
	// Tags are the areas the hub says are linked to us.
	Tags []string `json:"tags"`
}

// handleGetAreafixQueryReply: GET /api/binkp/areafix/query-reply?address=&kind=.
// known (comma-separated, optional) are area tags the page knows
// (the hub's list, our record): a line of the reply counts as an area
// when its tag is one of them or written like one (all capitals) --
// not "Following" from "Following areas are linked to you".
func (s *Server) handleGetAreafixQueryReply(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	if address == "" {
		writeError(w, http.StatusBadRequest, "address query parameter is required")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}
	out := queryReplyDTO{Tags: []string{}}
	var raw string
	if s.DB.QueryRow(`SELECT value FROM meta WHERE key = ?`, queryKey(r.URL.Query().Get("kind"), address)).Scan(&raw) != nil {
		writeJSON(w, http.StatusOK, out)
		return
	}
	var afterID, at int64
	fmt.Sscanf(raw, "%d %d", &afterID, &at)
	out.Asked = time.Unix(at, 0)
	msgs, err := s.Netmail.InboxFromAddress(address, listReplyLookbackLimit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read inbound netmail")
		return
	}
	known := map[string]bool{}
	for _, k := range strings.Split(r.URL.Query().Get("known"), ",") {
		if k = strings.TrimSpace(k); k != "" {
			known[strings.ToUpper(k)] = true
		}
	}
	msgs = listCandidates(msgs, r.URL.Query().Get("kind"))
	// A reply that says it's about what's linked wins over a richer
	// one that may be the %LIST answer arriving meanwhile.
	var best *netmail.Message
	var bestTags []string
	bestSays := false
	for i := range msgs {
		m := &msgs[i]
		if m.ID <= afterID {
			continue
		}
		tags := linkedTags(m.Body, known)
		says := queryWords.MatchString(m.Subject + "\n" + m.Body)
		if best == nil || (says && !bestSays) || (says == bestSays && len(tags) > len(bestTags)) {
			best, bestTags, bestSays = m, tags, says
		}
	}
	if best != nil {
		out.Found, out.PostedAt, out.Subject, out.RawBody = true, best.PostedAt, best.Subject, best.Body
		out.Tags = append(out.Tags, bestTags...)
	}
	writeJSON(w, http.StatusOK, out)
}

// queryWords mark a reply as the answer to %QUERY.
var queryWords = regexp.MustCompile(`(?i)query|linked|subscribed|connected to`)

// linkedTags are the areas in a %QUERY reply: every listed one is
// linked, whatever marker the line has.
func linkedTags(body string, known map[string]bool) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range areafix.ParseAreaListReply(body) {
		key := strings.ToUpper(a.Tag)
		if seen[key] || !(known[key] || a.Tag == key) {
			continue
		}
		seen[key] = true
		out = append(out, a.Tag)
	}
	return out
}

// handleAdoptAreafixQuery: PUT /api/binkp/areafix/subscriptions
// {host, kind, area_tags} -- our record of what we're subscribed to
// at host becomes exactly area_tags (the hub's word); nothing is sent.
func (s *Server) handleAdoptAreafixQuery(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Host     string   `json:"host"`
		Kind     string   `json:"kind"`
		AreaTags []string `json:"area_tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Host) == "" {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if s.EchoAreafix == nil || s.FileAreafix == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
		return
	}
	type store interface {
		ListForUplink(string, areafix.Direction) ([]areafix.Subscription, error)
		Request(string, string, areafix.Direction) error
		Withdraw(string, string, areafix.Direction) error
	}
	var st store = s.EchoAreafix
	if req.Kind == "file" {
		st = s.FileAreafix
	}
	want := map[string]string{}
	for _, t := range req.AreaTags {
		if t = strings.TrimSpace(t); t != "" {
			want[strings.ToUpper(t)] = t
		}
	}
	have, err := st.ListForUplink(req.Host, areafix.Outbound)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not list subscriptions")
		return
	}
	added, removed := 0, 0
	for _, sub := range have {
		key := strings.ToUpper(sub.AreaTag)
		if _, ok := want[key]; ok {
			delete(want, key)
			continue
		}
		if err := st.Withdraw(req.Host, sub.AreaTag, areafix.Outbound); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update subscriptions")
			return
		}
		removed++
	}
	for _, tag := range want {
		if err := st.Request(req.Host, tag, areafix.Outbound); err != nil {
			writeError(w, http.StatusInternalServerError, "could not update subscriptions")
			return
		}
		added++
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s took over %s's own list of linked areas (%s): %d added, %d removed", claims.Subject, req.Host, req.Kind, added, removed)
	}
	writeJSON(w, http.StatusOK, map[string]int{"added": added, "removed": removed})
}

// handleAreafixCommand: POST /api/binkp/areafix/command {uplink, kind,
// command} -- one of tosser.RobotCommands to the robot. %QUERY and
// %LINKED count as asking what's linked (see the query reply).
func (s *Server) handleAreafixCommand(w http.ResponseWriter, r *http.Request) {
	var req struct {
		areafixListRequestDTO
		Command string `json:"command"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if strings.TrimSpace(req.Uplink.Host) == "" || strings.TrimSpace(req.Uplink.Address) == "" {
		writeError(w, http.StatusBadRequest, "uplink host and address must not be empty")
		return
	}
	if s.Netmail == nil {
		writeError(w, http.StatusInternalServerError, "areafix is not configured")
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
	cmd := strings.ToUpper(strings.TrimSpace(req.Command))
	uplink := areafixUplinkFromDTO(req.Uplink)
	msg, err := tosser.RequestRobotCommand(s.Netmail, c.BBS.FTNAddresses, c.BBS.Name, uplink, req.Kind == "file", cmd)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unknown robot command")
		return
	}
	if cmd == "%QUERY" || cmd == "%LINKED" {
		s.DB.Exec(`INSERT INTO meta (key, value) VALUES (?, ?) ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
			queryKey(req.Kind, uplink.Address), strconv.FormatInt(msg.ID, 10)+" "+strconv.FormatInt(time.Now().Unix(), 10))
	}
	if claims, ok := claimsFromContext(r.Context()); ok {
		s.logInfo("%s sent %s to %s's %s robot", claims.Subject, cmd, req.Uplink.Host, robotName(req.Kind))
	}
	writeJSON(w, http.StatusOK, map[string]any{"queued_message_id": msg.ID})
}
