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
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
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
func Poll(ctx context.Context, ourAddresses []string, uplink config.BinkpUplink, allUplinks []config.BinkpUplink, netmailStore *netmail.Store, users *user.Store) (*Result, error) {
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
		buf, err := buildPacket(ourAddr, uplinkAddr, routed)
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
		n, err := tossInbound(r, netmailStore, users)
		res.Received += n
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
// BinkP link's own two endpoints. Each message's own OrigAddr/DestAddr
// carries its real origin/destination, which internal/mail encodes as
// INTL/FMPT/TOPT kludge lines when they differ from the packet header
// (e.g. a point address, or routing through this uplink to a third
// system).
func buildPacket(ourAddr, uplinkAddr mail.Address, pending []netmail.Message) (*bytes.Buffer, error) {
	var buf bytes.Buffer
	w, err := mail.NewWriter(&buf, mail.PacketHeader{
		OrigAddr: ourAddr,
		DestAddr: uplinkAddr,
		Created:  time.Now(),
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

// tossInbound reads a complete FTS-0001 packet from r and files each
// message into the local netmail store: delivered straight to a
// user's inbox if ToName resolves to a local account (case-
// insensitive, matching internal/bbs's own recipient lookup), or kept
// queued (ToUserID unset) with the sender's address preserved
// otherwise, so nothing is silently dropped even though there's
// nowhere local to put it yet.
func tossInbound(r io.Reader, netmailStore *netmail.Store, users *user.Store) (int, error) {
	pr, err := mail.NewReader(r)
	if err != nil {
		return 0, fmt.Errorf("tosser: reading inbound packet: %w", err)
	}

	n := 0
	for {
		msg, err := pr.ReadMessage()
		if err == io.EOF {
			return n, nil
		}
		if err != nil {
			return n, fmt.Errorf("tosser: reading inbound message %d: %w", n+1, err)
		}

		var toUserID int64
		if recipient, err := users.ByUsername(msg.ToName); err == nil {
			toUserID = recipient.ID
		} else if !errors.Is(err, user.ErrNotFound) {
			return n, fmt.Errorf("tosser: resolving recipient %q: %w", msg.ToName, err)
		}

		fromName := msg.FromName
		if fromName == "" {
			fromName = msg.OrigAddr.String()
		}
		crash := msg.Attr&mail.AttrCrash != 0
		if _, err := netmailStore.Receive(fromName, msg.OrigAddr.String(), toUserID, msg.ToName, "", msg.Subject, msg.Body, msg.Written, crash); err != nil {
			return n, fmt.Errorf("tosser: storing inbound message %d: %w", n+1, err)
		}
		n++
	}
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
