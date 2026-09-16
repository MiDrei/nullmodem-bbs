package message

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// NetNode formats an FTN net/node pair the way FTS-0004's SEEN-BY
// lines conventionally write one -- zone and point are deliberately
// dropped: SEEN-BY entries are assumed to share the message's own
// zone (a known FTN limitation essentially every real tosser accepts
// rather than solves, since a hub/downlink relationship is almost
// always within a single network/zone in practice), and SEEN-BY
// tracks boss nodes, never individual points.
func NetNode(net, node int) string {
	return fmt.Sprintf("%d/%d", net, node)
}

// SeenByNetNodes returns every net/node pair listed across all of
// body's "SEEN-BY:" lines (case-insensitive keyword, FTS-0004) --
// used both to reject a genuine inbound duplicate (not implemented
// here; see internal/tosser) and, going the other direction, to
// decide whether a downlink already has a copy of this message before
// forwarding it again (see internal/tosser's RoutedOutboundEchoForward).
func SeenByNetNodes(body string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(strings.TrimRight(line, "\r"))
		upper := strings.ToUpper(line)
		if !strings.HasPrefix(upper, "SEEN-BY:") {
			continue
		}
		rest := strings.TrimSpace(line[len("SEEN-BY:"):])
		for _, tok := range strings.Fields(rest) {
			if looksLikeNetNode(tok) {
				out[tok] = true
			}
		}
	}
	return out
}

// looksLikeNetNode reports whether tok is shaped like "net/node" (two
// non-negative integers separated by a single '/') -- guards against
// a stray non-address token on a SEEN-BY line being treated as one.
func looksLikeNetNode(tok string) bool {
	net, node, ok := strings.Cut(tok, "/")
	if !ok {
		return false
	}
	if _, err := strconv.Atoi(net); err != nil {
		return false
	}
	if _, err := strconv.Atoi(node); err != nil {
		return false
	}
	return true
}

// addSeenByLine returns body with a new "SEEN-BY: ..." line appended,
// containing every net/node in netNodes not already listed anywhere
// in body's existing SEEN-BY lines (see SeenByNetNodes) -- appended
// at the very end rather than consolidated into an existing line,
// mirroring how a real message naturally accumulates one new SEEN-BY
// line per hop as it passes through a network rather than rewriting
// what's already there. Returns body unchanged if every net/node in
// netNodes is already listed.
func addSeenByLine(body string, netNodes []string) string {
	existing := SeenByNetNodes(body)
	var toAdd []string
	for _, nn := range netNodes {
		if !existing[nn] {
			toAdd = append(toAdd, nn)
		}
	}
	if len(toAdd) == 0 {
		return body
	}
	sort.Strings(toAdd)
	line := "SEEN-BY: " + strings.Join(toAdd, " ")
	if body == "" {
		return line
	}
	if strings.HasSuffix(body, "\n") {
		return body + line + "\n"
	}
	return body + "\n" + line + "\n"
}

// MarkSeenBy records that netNode (see NetNode) has now received
// messageID -- called after internal/tosser confirms a forwarded
// message was actually delivered (BinkP's M_GOT) to a downlink whose
// own address is netNode, so a later routing decision (for this or
// another downlink) doesn't send it again. A no-op if netNode is
// already listed in the message's SEEN-BY.
func (s *Store) MarkSeenBy(messageID int64, netNode string) error {
	var body string
	if err := s.db.QueryRow(`SELECT body FROM messages WHERE id = ?`, messageID).Scan(&body); err != nil {
		return fmt.Errorf("message: mark %d seen-by %s: loading body: %w", messageID, netNode, err)
	}
	updated := addSeenByLine(body, []string{netNode})
	if updated == body {
		return nil
	}
	if _, err := s.db.Exec(`UPDATE messages SET body = ? WHERE id = ?`, updated, messageID); err != nil {
		return fmt.Errorf("message: mark %d seen-by %s: %w", messageID, netNode, err)
	}
	return nil
}
