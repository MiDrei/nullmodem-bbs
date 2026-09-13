// Package mail implements FTS-0001 FidoNet packet files (with the
// FSC-0039 "Type 2+" zone/point extension) -- the container format a
// BinkP session (see internal/binkp) transfers to move netmail and
// echomail messages between systems. It handles the packet/message
// wire format only; routing messages into/out of internal/message and
// internal/netmail is a future tosser's job.
package mail

import (
	"fmt"
	"strconv"
	"strings"
)

// Address is an FTN node address: zone:net/node[.point]. Point is 0
// when the address names a full node rather than a specific point.
type Address struct {
	Zone, Net, Node, Point int
}

// ParseAddress parses "zone:net/node" or "zone:net/node.point" (an
// "@domain" suffix, if present, is ignored -- this package doesn't
// need it to read or write packets).
func ParseAddress(s string) (Address, error) {
	s = strings.TrimSpace(s)
	if at := strings.IndexByte(s, '@'); at >= 0 {
		s = s[:at]
	}

	colon := strings.IndexByte(s, ':')
	if colon < 0 {
		return Address{}, fmt.Errorf("mail: address %q: missing zone (expected zone:net/node)", s)
	}
	zone, err := strconv.Atoi(s[:colon])
	if err != nil {
		return Address{}, fmt.Errorf("mail: address %q: invalid zone: %w", s, err)
	}
	rest := s[colon+1:]

	slash := strings.IndexByte(rest, '/')
	if slash < 0 {
		return Address{}, fmt.Errorf("mail: address %q: missing net/node separator", s)
	}
	net, err := strconv.Atoi(rest[:slash])
	if err != nil {
		return Address{}, fmt.Errorf("mail: address %q: invalid net: %w", s, err)
	}
	rest = rest[slash+1:]

	node := rest
	point := 0
	if dot := strings.IndexByte(rest, '.'); dot >= 0 {
		node = rest[:dot]
		point, err = strconv.Atoi(rest[dot+1:])
		if err != nil {
			return Address{}, fmt.Errorf("mail: address %q: invalid point: %w", s, err)
		}
	}
	nodeNum, err := strconv.Atoi(node)
	if err != nil {
		return Address{}, fmt.Errorf("mail: address %q: invalid node: %w", s, err)
	}

	return Address{Zone: zone, Net: net, Node: nodeNum, Point: point}, nil
}

// String renders the address as "zone:net/node", or "zone:net/node.point"
// when Point is non-zero.
func (a Address) String() string {
	if a.Point == 0 {
		return fmt.Sprintf("%d:%d/%d", a.Zone, a.Net, a.Node)
	}
	return fmt.Sprintf("%d:%d/%d.%d", a.Zone, a.Net, a.Node, a.Point)
}

// IsZero reports whether a is the zero Address (no zone/net/node/point
// set) -- used to detect an unset per-message address falling back to
// the packet header's.
func (a Address) IsZero() bool {
	return a == Address{}
}
