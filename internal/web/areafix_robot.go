package web

import (
	"net/http"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/netmail"
)

// A hub has two robots, Areafix (echomail) and Filefix (file echos),
// both answering from the hub's address. A reply belongs to the one
// its sender name or subject names; failing that, to what its text
// talks about (as BinktermPHP tells them apart). Receipts and help
// texts are no area list.

var (
	fileWords = regexp.MustCompile(`(?i)filefix|file ?areas?|file ?echo|fileareas|\bTIC\b`)
	echoWords = regexp.MustCompile(`(?i)areafix|echo ?areas?|echomail|area list for|areas linked at`)
	receipt   = regexp.MustCompile(`(?i)command processed|\[ begin message \]|here are the list of commands|original message text|rescanned|invalid password`)
)

// robotOf is "file", "echo", or "" when the reply doesn't say.
func robotOf(m netmail.Message) string {
	head := m.FromName + " " + m.Subject
	switch {
	case strings.Contains(strings.ToLower(head), "filefix"):
		return "file"
	case strings.Contains(strings.ToLower(head), "areafix"):
		return "echo"
	case fileWords.MatchString(m.Body):
		return "file"
	case echoWords.MatchString(m.Body):
		return "echo"
	}
	return ""
}

// forRobot keeps the replies of kind's robot (and those that don't
// say whose they are), never the other robot's.
func forRobot(msgs []netmail.Message, kind string) []netmail.Message {
	if kind != "file" {
		kind = "echo"
	}
	var out []netmail.Message
	for _, m := range msgs {
		if r := robotOf(m); r == "" || r == kind {
			out = append(out, m)
		}
	}
	return out
}

// listCandidates are the replies of kind's robot that may be an area
// list: not a receipt or help text.
func listCandidates(msgs []netmail.Message, kind string) []netmail.Message {
	var out []netmail.Message
	for _, m := range forRobot(msgs, kind) {
		if !receipt.MatchString(m.Body) {
			out = append(out, m)
		}
	}
	return out
}

func robotName(kind string) string {
	if kind == "file" {
		return "Filefix"
	}
	return "Areafix"
}

type areafixHistoryDTO struct {
	ID       int64     `json:"id"`
	Outgoing bool      `json:"outgoing"`
	At       time.Time `json:"at"`
	// Subject of a reply; a request's is its password and stays here.
	Subject string `json:"subject,omitempty"`
	Body    string `json:"body"`
	Sent    bool   `json:"sent,omitempty"` // a request handed to the hub
}

// handleAreafixHistory: GET /api/binkp/areafix/history?address=&kind=
// -- the requests to that robot at the hub and its replies, newest
// first.
func (s *Server) handleAreafixHistory(w http.ResponseWriter, r *http.Request) {
	address := strings.TrimSpace(r.URL.Query().Get("address"))
	kind := r.URL.Query().Get("kind")
	if address == "" || s.Netmail == nil {
		writeError(w, http.StatusBadRequest, "address query parameter is required")
		return
	}
	out := []areafixHistoryDTO{}
	rows, err := s.DB.Query(`SELECT id FROM netmail_messages
		WHERE to_address = ? AND from_user_id IS NULL AND to_user_id IS NULL AND lower(to_name) = lower(?)
		ORDER BY id DESC LIMIT 15`, address, robotName(kind))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read netmail")
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		ids = append(ids, id)
	}
	rows.Close()
	for _, id := range ids {
		if m, err := s.Netmail.MessageByID(id); err == nil {
			out = append(out, areafixHistoryDTO{ID: m.ID, Outgoing: true, At: m.PostedAt, Body: m.Body, Sent: m.IsSent()})
		}
	}
	msgs, err := s.Netmail.InboxFromAddress(address, 30)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "could not read netmail")
		return
	}
	for _, m := range forRobot(msgs, kind) {
		out = append(out, areafixHistoryDTO{ID: m.ID, At: m.PostedAt, Subject: m.Subject, Body: m.Body})
	}
	// The hub's receipts quote our request, password and all: not here.
	var secrets []string
	if c, err := s.loadBBSConfig(); err == nil {
		for _, u := range c.Binkp.Uplinks {
			if u.Address == address {
				for _, p := range []string{u.AreafixPassword, u.FilefixPassword, u.Password, u.PacketPassword} {
					if len(p) >= 3 {
						secrets = append(secrets, p)
					}
				}
			}
		}
	}
	for i := range out {
		for _, p := range secrets {
			out[i].Body = strings.ReplaceAll(out[i].Body, p, "••••••")
			out[i].Subject = strings.ReplaceAll(out[i].Subject, p, "••••••")
		}
	}
	// In the order they came and went (ids grow as mail is stored).
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	if len(out) > 20 {
		out = out[:20]
	}
	writeJSON(w, http.StatusOK, out)
}
