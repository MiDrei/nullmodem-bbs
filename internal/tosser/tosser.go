// Package tosser bridges the local netmail store (internal/netmail)
// and the FTS-0001 packet format (internal/mail) over a BinkP session
// (internal/binkp): it's the piece that actually moves netmail to and
// from a configured uplink, run either periodically by cmd/mailer or
// on demand from the web admin's "Send Now" button.
//
// Routing between more than one configured uplink is deliberately
// minimal, covering exactly one real-world shape: a "main" uplink
// that carries everything by default (ordinary mail for any
// destination, on the assumption that it can route it onward -- true
// for a simple leaf/point setup), plus zero or more additional
// uplinks each dedicated to one specific FTN network (identified by
// its own configured Address's zone) that only ever carry Crash-
// flagged mail for that network, dialed immediately rather than
// waiting for cmd/mailer's regular scheduled poll (see
// config.BinkpUplink.PollDisabled and routeOutbound). There's no
// general per-destination routing table beyond that.
package tosser

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// dialTimeout bounds one poll attempt against an uplink.
const dialTimeout = 60 * time.Second

// Result summarizes one poll of one uplink.
type Result struct {
	// Sent is how many queued netmail messages were bundled, handed
	// off, and acknowledged (M_GOT) by the uplink.
	Sent int
	// Received is how many netmail messages were filed away (or
	// queued locally, if the recipient didn't resolve to a local
	// user) out of whatever the uplink sent back in the same session.
	Received int
	// ReceivedEcho is how many echomail messages (an AREA-kludged
	// message, as opposed to netmail) were tossed into a message
	// area out of whatever the uplink sent back, whether or not the
	// area already existed (see tossEcho).
	ReceivedEcho int
	// RemoteAddresses are the FTN addresses the uplink identified
	// itself as, straight from binkp.Result.
	RemoteAddresses []string
}

// Poll connects to uplink, sends whatever netmail routes to it (see
// RoutedOutbound), and files away anything the uplink sends back --
// both in one BinkP session, matching how real FTN mailers exchange
// mail. ourAddresses are this system's own FTN addresses/AKAs
// (config.Config's BBS.FTNAddresses); at least one is required, since
// both the BinkP handshake and the outbound packet's header need one.
// All of them are presented to the uplink via BinkP's M_ADR (needed
// when the same uplink bridges more than one FTN network and expects
// us to identify under each); the first is used as the outbound
// packet's own header address. allUplinks is every uplink configured
// for this system (config.Config's Binkp.Uplinks), used only to
// resolve routing -- it need not include uplink itself (e.g. the web
// admin's "Send Now" button tries an address the sysop hasn't saved
// yet), and uplink need not be poll-disabled just because it's being
// dialed here (that flag only governs cmd/mailer's own scheduled
// loop, not a manual or crash-triggered dial).
func Poll(ctx context.Context, ourAddresses []string, uplink config.BinkpUplink, allUplinks []config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store) (*Result, error) {
	if len(ourAddresses) == 0 {
		return nil, fmt.Errorf("tosser: no FTN addresses configured for this system")
	}
	ourAddr, err := mail.ParseAddress(ourAddresses[0])
	if err != nil {
		return nil, fmt.Errorf("tosser: this system's FTN address %q: %w", ourAddresses[0], err)
	}

	routed, err := RoutedOutbound(netmailStore, uplink, allUplinks)
	if err != nil {
		return nil, err
	}

	var outFiles []binkp.OutboundFile
	var packetName string
	if len(routed) > 0 {
		uplinkAddr, err := mail.ParseAddress(uplink.Address)
		if err != nil {
			return nil, fmt.Errorf("tosser: uplink address %q: %w", uplink.Address, err)
		}
		buf, err := buildPacket(ourAddr, uplinkAddr, uplink.PacketPassword, routed)
		if err != nil {
			return nil, err
		}
		packetName = fmt.Sprintf("%08x.pkt", time.Now().Unix())
		outFiles = append(outFiles, binkp.OutboundFile{
			Name:    packetName,
			Size:    int64(buf.Len()),
			ModTime: time.Now(),
			Data:    buf,
		})
	}

	res := &Result{}
	var receiveErr error
	receiveFile := func(f binkp.InboundFile, r io.Reader) error {
		stats, err := tossInbound(r, uplink.PacketPassword, netmailStore, messages, users)
		res.Received += stats.netmail
		res.ReceivedEcho += stats.echo
		if err != nil {
			receiveErr = err
		}
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, dialTimeout)
	defer cancel()

	sessionResult, err := binkp.Dial(ctx, uplink.Host, binkp.Config{
		OurAddresses:  ourAddresses,
		Password:      uplink.Password,
		OutboundFiles: outFiles,
		ReceiveFile:   receiveFile,
	})
	if err != nil {
		return nil, fmt.Errorf("tosser: polling %s: %w", uplink.Host, err)
	}
	res.RemoteAddresses = sessionResult.RemoteAddresses

	if packetName != "" && containsString(sessionResult.FilesSent, packetName) {
		for _, m := range routed {
			if err := netmailStore.MarkSent(m.ID); err != nil {
				return res, fmt.Errorf("tosser: marking message %d sent: %w", m.ID, err)
			}
			res.Sent++
		}
	}

	return res, receiveErr
}

// RoutedOutbound returns the subset of currently pending netmail that
// Poll would bundle for target, without dialing anything -- so a
// caller can check whether dialing target is even worthwhile (e.g.
// cmd/mailer's crash-check loop skipping a crash-only uplink with
// nothing routed to it right now).
//
// The routing rule: an ordinary (non-Crash) message always defaults
// to whichever uplink is asked for it, matching a simple leaf/point
// setup with one real uplink. A Crash-flagged message instead goes
// only to the uplink whose own configured Address is on the same FTN
// zone as the message's destination, if any such uplink is
// configured -- reserving crash-only links (config.BinkpUplink.
// PollDisabled) for exactly the traffic they exist for, and never
// routing them ordinary mail. A Crash message whose zone matches no
// specific uplink falls back to the default case like ordinary mail.
func RoutedOutbound(netmailStore *netmail.Store, target config.BinkpUplink, allUplinks []config.BinkpUplink) ([]netmail.Message, error) {
	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		return nil, fmt.Errorf("tosser: loading pending netmail: %w", err)
	}
	return routeOutbound(pending, target, allUplinks), nil
}

func routeOutbound(pending []netmail.Message, target config.BinkpUplink, allUplinks []config.BinkpUplink) []netmail.Message {
	var routed []netmail.Message
	for _, m := range pending {
		matched, hasMatch := uplinkForDestination(allUplinks, m.ToAddress)
		isTarget := hasMatch && matched.Host == target.Host

		switch {
		case target.PollDisabled:
			// A crash-only uplink only ever carries Crash mail
			// specifically zoned to it -- never ordinary traffic, and
			// never Crash mail meant for a different uplink.
			if m.Crash && isTarget {
				routed = append(routed, m)
			}
		case m.Crash && hasMatch && !isTarget:
			// Reserved for a different, specifically configured
			// uplink (typically a crash-only one) -- skip here so it
			// waits for that uplink instead.
		default:
			// Ordinary mail, or Crash mail with no more specific
			// uplink to claim it, defaults to whichever non-disabled
			// uplink is being asked for it.
			routed = append(routed, m)
		}
	}
	return routed
}

// uplinkForDestination returns the configured uplink whose own
// Address is on the same FTN zone as destAddr, if any.
func uplinkForDestination(uplinks []config.BinkpUplink, destAddr string) (config.BinkpUplink, bool) {
	dest, err := mail.ParseAddress(destAddr)
	if err != nil {
		return config.BinkpUplink{}, false
	}
	for _, u := range uplinks {
		addr, err := mail.ParseAddress(u.Address)
		if err == nil && addr.Zone == dest.Zone {
			return u, true
		}
	}
	return config.BinkpUplink{}, false
}

// buildPacket bundles pending into a single FTS-0001 packet addressed
// (at the packet-header level) between ourAddr and uplinkAddr -- the
// BinkP link's own two endpoints -- stamped with packetPassword (the
// uplink's configured PacketPassword; empty means none). Each
// message's own OrigAddr/DestAddr carries its real origin/destination,
// which internal/mail encodes as INTL/FMPT/TOPT kludge lines when they
// differ from the packet header (e.g. a point address, or routing
// through this uplink to a third system).
func buildPacket(ourAddr, uplinkAddr mail.Address, packetPassword string, pending []netmail.Message) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: ourAddr,
		DestAddr: uplinkAddr,
		Created:  time.Now(),
		Password: packetPassword,
	})
	if err != nil {
		return nil, fmt.Errorf("tosser: building outbound packet: %w", err)
	}

	for _, m := range pending {
		destAddr, err := mail.ParseAddress(m.ToAddress)
		if err != nil {
			return nil, fmt.Errorf("tosser: message %d has an unparseable destination %q: %w", m.ID, m.ToAddress, err)
		}
		origAddr := ourAddr
		if m.FromAddress != "" {
			if a, err := mail.ParseAddress(m.FromAddress); err == nil {
				origAddr = a
			}
		}
		attr := mail.AttrPrivate
		if m.Crash {
			attr |= mail.AttrCrash
		}
		if err := w.WriteMessage(mail.Message{
			OrigAddr: origAddr,
			DestAddr: destAddr,
			Attr:     attr,
			Written:  m.PostedAt,
			ToName:   m.ToName,
			FromName: m.FromName,
			Subject:  m.Subject,
			Body:     m.Body,
		}); err != nil {
			return nil, fmt.Errorf("tosser: writing message %d: %w", m.ID, err)
		}
	}

	if err := w.Close(); err != nil {
		return nil, fmt.Errorf("tosser: closing outbound packet: %w", err)
	}
	return &buf, nil
}

// inboundStats counts what tossInbound filed away, split by whether
// each message was netmail or echomail (see Result.Received/
// ReceivedEcho).
type inboundStats struct {
	netmail, echo int
}

// tossInbound reads a complete FTS-0001 packet from r and files each
// message either into the local netmail store or a message area,
// depending on whether it carries an AREA kludge (see echoAreaTag):
//
//   - Echomail (AREA kludge present) is tossed into the area matching
//     that tag, auto-creating it as Pending if this is the first
//     message ever seen for it (see message.Store.EnsureArea) --
//     invisible to callers until the sysop reviews and approves it.
//   - Netmail (no AREA kludge) is delivered straight to a user's
//     inbox if ToName resolves to a local account (case-insensitive,
//     matching internal/bbs's own recipient lookup), or kept queued
//     (ToUserID unset) with the sender's address preserved otherwise,
//     so nothing is silently dropped even though there's nowhere
//     local to put it yet.
//
// If expectedPassword is non-empty, the packet's own header password
// (internal/mail.PacketHeader.Password) must match it case-
// insensitively -- FTN packet passwords, like most FTN passwords, are
// conventionally uppercased by tossers regardless of how a sysop
// typed them, confirmed against a real uplink that stamped its
// packets in all caps while our configured value used mixed case. A
// mismatch discards the rest of the packet (see below) before any
// message in it is stored, since a wrong packet password indicates
// either a misconfiguration or a forged packet, not partial content
// to salvage.
//
// Whatever the outcome, r is always drained to completion before
// returning: binkp.Config.ReceiveFile's contract requires reading
// every byte of the file, since the network-reading side on the other
// end of the pipe blocks writing further data frames until this side
// keeps consuming them -- returning early, as an earlier version of
// this password check did, deadlocks that writer forever instead of
// cleanly failing the session.
func tossInbound(r io.Reader, expectedPassword string, netmailStore *netmail.Store, messages *message.Store, users *user.Store) (stats inboundStats, err error) {
	defer func() {
		if _, drainErr := io.Copy(io.Discard, r); drainErr != nil && err == nil {
			err = fmt.Errorf("tosser: draining inbound packet: %w", drainErr)
		}
	}()

	pr, err := mail.NewReader(r)
	if err != nil {
		return stats, fmt.Errorf("tosser: reading inbound packet: %w", err)
	}
	if expectedPassword != "" && !strings.EqualFold(pr.Header.Password, expectedPassword) {
		return stats, fmt.Errorf("tosser: inbound packet password does not match this uplink's configured packet password")
	}

	for {
		msg, err := pr.ReadMessage()
		if err == io.EOF {
			return stats, nil
		}
		if err != nil {
			return stats, fmt.Errorf("tosser: reading inbound message %d: %w", stats.netmail+stats.echo+1, err)
		}

		if tag, ok := echoAreaTag(msg.Body); ok {
			if err := tossEcho(tag, msg, messages); err != nil {
				return stats, fmt.Errorf("tosser: tossing echomail message %d: %w", stats.netmail+stats.echo+1, err)
			}
			stats.echo++
			continue
		}

		var toUserID int64
		if recipient, err := users.ByUsername(msg.ToName); err == nil {
			toUserID = recipient.ID
		} else if !errors.Is(err, user.ErrNotFound) {
			return stats, fmt.Errorf("tosser: resolving recipient %q: %w", msg.ToName, err)
		}

		fromName := msg.FromName
		if fromName == "" {
			fromName = msg.OrigAddr.String()
		}
		crash := msg.Attr&mail.AttrCrash != 0
		if _, err := netmailStore.Receive(fromName, msg.OrigAddr.String(), toUserID, msg.ToName, "", msg.Subject, msg.Body, msg.Written, crash); err != nil {
			return stats, fmt.Errorf("tosser: storing inbound message %d: %w", stats.netmail+stats.echo+1, err)
		}
		stats.netmail++
	}
}

// echoAreaTag returns the echomail area tag from body's leading AREA
// line, if present. Per FTS-0009 (the echomail convention layered on
// top of FTS-0001), AREA is deliberately the one exception to every
// other kludge's \x01 control-byte prefix -- it's written as a bare
// "AREA:tag" first line precisely so a system with no echomail
// support still sees ordinary (if odd) text rather than a control
// character, unlike the \x01-prefixed kludges that follow it (MSGID,
// PID, TID, ...; see internal/mail's scanAddressingKludges, which
// parses INTL/FMPT/TOPT among those the same way, leaving AREA and
// the rest for this tosser layer to interpret). Confirmed against
// real inbound fsxNet traffic (Synchronet- and binkd-tossed messages
// alike), which all write a bare "AREA:tag" line with no \x01.
func echoAreaTag(body string) (string, bool) {
	const prefix = "AREA:"
	firstLine, _, _ := strings.Cut(body, "\n")
	if len(firstLine) > len(prefix) && strings.EqualFold(firstLine[:len(prefix)], prefix) {
		return strings.TrimSpace(firstLine[len(prefix):]), true
	}
	return "", false
}

// stripLeadingKludges drops the leading AREA line (see echoAreaTag)
// and every kludge line after it (anything still marked with FTN's
// \x01 control byte) from body, so a tossed echomail message displays
// as clean text in the BBS reader instead of literal control lines
// meant for tossers, not readers.
func stripLeadingKludges(body string) string {
	lines := strings.Split(body, "\n")
	i := 0
	if i < len(lines) && len(lines[i]) > 5 && strings.EqualFold(lines[i][:5], "AREA:") {
		i++
	}
	for i < len(lines) && strings.HasPrefix(lines[i], "\x01") {
		i++
	}
	return strings.Join(lines[i:], "\n")
}

// tossEcho files msg into the message area tagged tag, creating it as
// Pending (see message.Store.EnsureArea) if this is the first message
// ever seen for it -- so an unrecognized incoming echo area doesn't
// silently drop mail, but also doesn't appear in the BBS until the
// sysop has reviewed and approved it.
func tossEcho(tag string, msg *mail.Message, messages *message.Store) error {
	area, _, err := messages.EnsureArea(tag, tag, "")
	if err != nil {
		return fmt.Errorf("ensuring area %q: %w", tag, err)
	}

	fromName := msg.FromName
	if fromName == "" {
		fromName = msg.OrigAddr.String()
	}
	if _, err := messages.ReceiveEcho(area.ID, fromName, msg.Subject, stripLeadingKludges(msg.Body), msg.Written); err != nil {
		return fmt.Errorf("storing message in area %q: %w", tag, err)
	}
	return nil
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
