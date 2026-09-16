package file

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// NetNode mirrors message.NetNode exactly (net/node pair, zone/point
// dropped -- see that function's doc comment for why) -- duplicated
// here rather than imported since internal/file and internal/message
// are otherwise independent peer packages, and this is small enough
// not to be worth a cross-dependency for.
func NetNode(net, node int) string {
	return fmt.Sprintf("%d/%d", net, node)
}

// SeenByNetNodes parses seenBy (the raw seen_by column -- a plain
// space-separated list of net/node pairs, unlike message's embedded
// SEEN-BY lines, since a file has no body to embed them in) into a
// set, for internal/tosser's file-echo hub-forwarding routing
// decision (see RoutedOutboundFileForward): has target already
// received this file?
func SeenByNetNodes(seenBy string) map[string]bool {
	out := map[string]bool{}
	for _, tok := range strings.Fields(seenBy) {
		if looksLikeNetNode(tok) {
			out[tok] = true
		}
	}
	return out
}

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

// MarkSeenBy records that netNode (see NetNode) has now received
// fileID -- called after internal/tosser confirms a forwarded file
// was actually delivered (BinkP's M_GOT on both its TIC descriptor
// and its payload) to a downlink whose own address is netNode, so a
// later routing decision (for this or another downlink) doesn't send
// it again. A no-op if netNode is already listed.
func (s *Store) MarkSeenBy(fileID int64, netNode string) error {
	var seenBy string
	if err := s.db.QueryRow(`SELECT seen_by FROM files WHERE id = ?`, fileID).Scan(&seenBy); err != nil {
		return fmt.Errorf("file: mark %d seen-by %s: loading seen_by: %w", fileID, netNode, err)
	}
	existing := SeenByNetNodes(seenBy)
	if existing[netNode] {
		return nil
	}
	entries := make([]string, 0, len(existing)+1)
	for nn := range existing {
		entries = append(entries, nn)
	}
	entries = append(entries, netNode)
	sort.Strings(entries)
	updated := strings.Join(entries, " ")
	if _, err := s.db.Exec(`UPDATE files SET seen_by = ? WHERE id = ?`, updated, fileID); err != nil {
		return fmt.Errorf("file: mark %d seen-by %s: %w", fileID, netNode, err)
	}
	return nil
}
