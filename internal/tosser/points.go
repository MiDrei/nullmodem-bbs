package tosser

import (
	"fmt"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/message"
)

// Points of this system -- downlinks with a point address, 21:3/194.1
// under our 21:3/194 -- are readers rather than systems passing mail
// on: typically an offline reader app like FidoMail that calls in over
// BinkP. They differ from nodes in two ways:
//
//   - They only get what's theirs: netmail addressed to them and the
//     areas they subscribed to, never this system's own outgoing mail
//     (which a node entry with a Network would otherwise be handed,
//     and marked sent, on its next call).
//   - What they got is tracked per point (echo_point_deliveries):
//     SEEN-BY can't name points, and a point shares its net/node with
//     us, so every message that already went past us looks "seen".
//     A new subscription brings the last pointBacklog of an area.
//
// A reader app that has addresses in several networks has one uplink
// entry per point address; entries with the same Host are one point
// (they share Areafix subscriptions, which are keyed by host).

// pointBacklog is how far back a point's newly subscribed area is
// sent.
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
