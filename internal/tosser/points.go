package tosser

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// Points of this system -- downlinks with a point address, 21:3/194.1
// under our 21:3/194 -- are readers rather than systems passing mail
// on: typically an offline reader app like FidoMail that calls in over
// BinkP. They differ from nodes in three ways:
//
//   - They only get what's theirs: netmail addressed to them and the
//     areas they subscribed to, never this system's own outgoing mail
//     (which a node entry with a Network would otherwise be handed,
//     and marked sent, on its next call).
//   - What they got is tracked per point (echo_point_deliveries):
//     SEEN-BY can't name points, and a point shares its net/node with
//     us, so every message that already went past us looks "seen".
//     A new subscription brings the last pointBacklog of an area.
//   - With config.BinkpUplink.PostAs set, the point is the sysop's own
//     reader: its echomail and netmail are stored as that local user's
//     posts and go out under this system's own address, MSGID and
//     origin line -- as if written on the BBS -- and netmail to that
//     user is copied to the point as well.
//
// A reader app that has addresses in several networks has one uplink
// entry per point address; entries with the same Host are one point
// (they share Areafix subscriptions, which are keyed by host).

// pointBacklog is how far back a point's newly subscribed area, or
// its "post as" user's inbox, is sent.
const pointBacklog = 14 * 24 * time.Hour

// isPoint reports whether u is a point of this system.
func isPoint(u config.BinkpUplink) bool {
	if !u.Downlink {
		return false
	}
	a, err := mail.ParseAddress(u.Address)
	return err == nil && a.Point > 0
}

// pointEntries returns the point entries among uplinks with host host.
func pointEntries(uplinks []config.BinkpUplink, host string) []config.BinkpUplink {
	var out []config.BinkpUplink
	for _, u := range uplinks {
		if isPoint(u) && u.Host == host {
			out = append(out, u)
		}
	}
	return out
}

// pointForAddress returns the point entry addr (a netmail destination)
// belongs to, if any.
func pointForAddress(uplinks []config.BinkpUplink, addr string) (config.BinkpUplink, bool) {
	a, err := mail.ParseAddress(addr)
	if err != nil || a.Point == 0 {
		return config.BinkpUplink{}, false
	}
	for _, u := range uplinks {
		if !isPoint(u) {
			continue
		}
		if ua, err := mail.ParseAddress(u.Address); err == nil && ua == a {
			return u, true
		}
	}
	return config.BinkpUplink{}, false
}

// pointPoster posts a "post as" point's mail as its local user.
type pointPoster struct {
	user         *user.User
	host         string
	entries      []config.BinkpUplink
	ourAddresses []string
}

// newPointPoster returns the poster for a session with the point
// entry matched, or nil if it isn't a point with PostAs set. Several
// entries of the same point should name the same user; the first one
// that names one is used.
func newPointPoster(matched config.BinkpUplink, uplinks []config.BinkpUplink, users *user.Store, ourAddresses []string) (*pointPoster, error) {
	if !isPoint(matched) || users == nil {
		return nil, nil
	}
	entries := pointEntries(uplinks, matched.Host)
	if len(entries) == 0 {
		entries = []config.BinkpUplink{matched}
	}
	name := matched.PostAs
	for _, e := range entries {
		if name == "" {
			name = e.PostAs
		}
	}
	if name == "" {
		return nil, nil
	}
	u, err := users.ByUsername(name)
	if err != nil {
		// Never post the point's mail as the point instead: that is
		// exactly what the setting is meant to prevent.
		return nil, fmt.Errorf("tosser: point %s posts as %q: %w", matched.Address, name, err)
	}
	return &pointPoster{user: u, host: matched.Host, entries: entries, ourAddresses: ourAddresses}, nil
}

// tossEcho stores msg as the user's post in the area tagged tag, and
// records the point as having it (so it isn't sent back). handled is
// false when there's no such area here; the message is then tossed
// like any other.
func (p *pointPoster) tossEcho(tag string, msg *mail.Message, messages *message.Store) (handled, created bool, err error) {
	area, err := messages.AreaByTag(tag)
	if errors.Is(err, message.ErrAreaNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, fmt.Errorf("resolving area %q: %w", tag, err)
	}
	m, created, err := messages.PostEcho(area.ID, p.user.ID, msg.ToName, msg.Subject, readerText(msg.Body), echoMsgID(msg.Body), msg.Written)
	if err != nil {
		return false, false, fmt.Errorf("storing message in area %q: %w", tag, err)
	}
	if err := messages.MarkDeliveredToPoint(m.ID, p.host); err != nil {
		return false, false, err
	}
	return true, created, nil
}

// tossNetmail stores netmail msg as sent by the user: to a local user
// if it's addressed to this system, otherwise queued for its
// destination under this system's address in that zone. handled is
// false for mail to this system for someone without an account here,
// which is then stored like any other unresolved netmail.
func (p *pointPoster) tossNetmail(msg *mail.Message, netmailStore *netmail.Store, users *user.Store) (handled bool, err error) {
	ours := ourAddressForUplink(p.ourAddresses, msg.DestAddr)
	text := readerText(msg.Body)
	if msg.DestAddr == ours {
		recipient, err := users.ByUsername(msg.ToName)
		if errors.Is(err, user.ErrNotFound) {
			return false, nil
		}
		if err != nil {
			return false, fmt.Errorf("resolving recipient %q: %w", msg.ToName, err)
		}
		_, err = netmailStore.Send(p.user.ID, ours.String(), recipient.ID, recipient.Username, "", msg.Subject, text, false)
		return err == nil, err
	}
	crash := msg.Attr&mail.AttrCrash != 0
	_, err = netmailStore.Send(p.user.ID, ours.String(), 0, msg.ToName, msg.DestAddr.String(), msg.Subject, text, crash)
	return err == nil, err
}

// readerText is a message as its writer typed it: without the leading
// AREA line and kludges, and without the tearline, origin line,
// SEEN-BY, PATH and trailing kludges a tosser adds -- this system adds
// its own on the way out.
func readerText(body string) string {
	lines := strings.Split(stripLeadingKludges(body), "\n")
	end := len(lines)
	for end > 0 {
		l := strings.TrimRight(lines[end-1], " \r")
		switch {
		case l == "", strings.HasPrefix(l, "\x01"), strings.HasPrefix(l, "SEEN-BY:"),
			strings.HasPrefix(l, " * Origin:"), l == "---", strings.HasPrefix(l, "--- "):
			end--
			continue
		}
		break
	}
	return strings.Join(lines[:end], "\n")
}

// pointEchoForward is RoutedOutboundEchoForward for a point: the
// messages in its subscribed areas it hasn't been sent yet, from the
// last pointBacklog before the subscription on.
func pointEchoForward(messages *message.Store, subs []subscribedArea, host string) ([]message.PendingEcho, error) {
	delivered, err := messages.DeliveredToPoint(host)
	if err != nil {
		return nil, err
	}
	var out []message.PendingEcho
	for _, sub := range subs {
		areaMsgs, err := messages.ListMessages(sub.area.ID)
		if err != nil {
			return nil, fmt.Errorf("tosser: loading messages for area %q: %w", sub.tag, err)
		}
		since := sub.requestedAt.Add(-pointBacklog)
		for _, m := range areaMsgs {
			if delivered[m.ID] || m.PostedAt.Before(since) {
				continue
			}
			out = append(out, message.PendingEcho{Message: m, AreaTag: sub.tag})
		}
	}
	return out, nil
}

type subscribedArea struct {
	tag         string
	area        *message.Area
	requestedAt time.Time
}

// pointNetmailCopies returns the netmail in the "post as" user's inbox
// the point hasn't been sent yet (from the last pointBacklog), each
// readdressed to the point: to the point address in the sender's
// zone, or target's own.
func (p *pointPoster) pointNetmailCopies(netmailStore *netmail.Store, target config.BinkpUplink) ([]netmail.Message, error) {
	inbox, err := netmailStore.Inbox(p.user.ID)
	if err != nil {
		return nil, fmt.Errorf("tosser: loading %s's netmail: %w", p.user.Username, err)
	}
	delivered, err := netmailStore.DeliveredToPoint(p.host)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-pointBacklog)
	var out []netmail.Message
	for _, m := range inbox {
		if delivered[m.ID] || m.PostedAt.Before(since) {
			continue
		}
		dest := target.Address
		if from, err := mail.ParseAddress(m.FromAddress); err == nil {
			for _, e := range p.entries {
				if a, err := mail.ParseAddress(e.Address); err == nil && a.Zone == from.Zone {
					dest = e.Address
					break
				}
			}
		}
		c := m
		c.ToAddress = dest
		c.ToName = p.user.Username
		c.Body = withoutAddressingKludges(m.Body)
		out = append(out, c)
	}
	return out, nil
}

// withoutAddressingKludges drops INTL/FMPT/TOPT kludges (the packet
// writer adds the copy's own) and Via lines from a stored netmail body.
func withoutAddressingKludges(body string) string {
	var keep []string
	for _, l := range strings.Split(body, "\n") {
		if strings.HasPrefix(l, "\x01INTL ") || strings.HasPrefix(l, "\x01FMPT ") ||
			strings.HasPrefix(l, "\x01TOPT ") || strings.HasPrefix(l, "\x01Via") {
			continue
		}
		keep = append(keep, l)
	}
	return strings.Join(keep, "\n")
}
