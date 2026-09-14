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
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
	"git.maik.ch/swissmaik/nullmodem/internal/version"
)

// dialTimeout bounds one poll attempt against an uplink.
const dialTimeout = 60 * time.Second

// Result summarizes one poll of one uplink.
type Result struct {
	// Sent is how many queued netmail messages were bundled, handed
	// off, and acknowledged (M_GOT) by the uplink.
	Sent int
	// SentEcho is how many locally-posted echomail messages (see
	// message.Store.PendingOutboundEcho) were bundled, handed off, and
	// acknowledged (M_GOT) by the uplink, in the same packet as Sent.
	SentEcho int
	// Received is how many netmail messages were filed away (or
	// queued locally, if the recipient didn't resolve to a local
	// user) out of whatever the uplink sent back in the same session.
	Received int
	// ReceivedEcho is how many echomail messages (an AREA-kludged
	// message, as opposed to netmail) were newly tossed into a
	// message area out of whatever the uplink sent back, whether or
	// not the area already existed (see tossEcho). A message whose
	// MSGID the area already had -- the uplink resending mail it
	// never saw our M_GOT for -- is recognized as a duplicate and not
	// counted here.
	ReceivedEcho int
	// RemoteAddresses are the FTN addresses the uplink identified
	// itself as, straight from binkp.Result.
	RemoteAddresses []string
	// SkippedFiles are inbound files the uplink sent that weren't FTS-
	// 0001 mail packets (see isPacketFile) -- e.g. a .tic file-echo
	// announcement, observed live bundled into the very same session
	// as ordinary mail. This system doesn't toss file-echo content
	// yet, so these are drained and acknowledged (M_GOT) rather than
	// dropped or, worse, fed to the packet parser (which fails hard on
	// them and used to abort the whole session, including mail already
	// successfully tossed earlier in it).
	SkippedFiles []string
}

// Poll connects to uplink, sends whatever netmail routes to it (see
// RoutedOutbound), and files away anything the uplink sends back --
// both in one BinkP session, matching how real FTN mailers exchange
// mail. ourAddresses are this system's own FTN addresses/AKAs
// (config.Config's BBS.FTNAddresses); at least one is required, since
// both the BinkP handshake and the outbound packet's header need one.
// All of them are presented to the uplink via BinkP's M_ADR (needed
// when the same uplink bridges more than one FTN network and expects
// us to identify under each); whichever one shares uplink's own FTN
// zone is used as the outbound packet's header address and every
// message's origin (see ourAddressForUplink) -- not just the first --
// so a system with an AKA per network stamps each hub's mail with the
// AKA that actually belongs to it. bbsName (config.Config's BBS.Name)
// is stamped, alongside that address, into an FTS-0004 tearline and
// origin line appended to every locally composed message (see
// appendTearline). allUplinks is every uplink configured for this
// system (config.Config's Binkp.Uplinks), used only to resolve
// routing -- it need not include uplink itself (e.g. the web admin's
// "Send Now" button tries an address the sysop hasn't saved yet), and
// uplink need not be poll-disabled just because it's being dialed
// here (that flag only governs cmd/mailer's own scheduled loop, not a
// manual or crash-triggered dial).
func Poll(ctx context.Context, ourAddresses []string, bbsName string, uplink config.BinkpUplink, allUplinks []config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store) (*Result, error) {
	if len(ourAddresses) == 0 {
		return nil, fmt.Errorf("tosser: no FTN addresses configured for this system")
	}
	if _, err := mail.ParseAddress(ourAddresses[0]); err != nil {
		return nil, fmt.Errorf("tosser: this system's FTN address %q: %w", ourAddresses[0], err)
	}

	routed, err := RoutedOutbound(netmailStore, uplink, allUplinks)
	if err != nil {
		return nil, err
	}
	routedEcho, err := RoutedOutboundEcho(messages, uplink)
	if err != nil {
		return nil, err
	}

	var outFiles []binkp.OutboundFile
	var packetName string
	if len(routed) > 0 || len(routedEcho) > 0 {
		uplinkAddr, err := mail.ParseAddress(uplink.Address)
		if err != nil {
			return nil, fmt.Errorf("tosser: uplink address %q: %w", uplink.Address, err)
		}
		ourAddr := ourAddressForUplink(ourAddresses, uplinkAddr)
		buf, err := buildPacket(ourAddr, uplinkAddr, uplink.PacketPassword, bbsName, routed, routedEcho)
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

	acceptedPasswords := acceptedPacketPasswords(uplink, allUplinks)

	res := &Result{}
	var receiveErr error
	receiveFile := func(f binkp.InboundFile, r io.Reader) error {
		err := handleInboundFile(f, r, acceptedPasswords, netmailStore, messages, users, res)
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
		for _, m := range routedEcho {
			if err := messages.MarkSent(m.ID); err != nil {
				return res, fmt.Errorf("tosser: marking echo message %d sent: %w", m.ID, err)
			}
			res.SentEcho++
		}
	}

	return res, receiveErr
}

// Answer handles one inbound BinkP connection -- a caller dialing us,
// e.g. a hub that wants to push mail to us between our own scheduled
// polls rather than waiting for us to ask. It matches the caller's
// claimed FTN address (from M_ADR) against uplinks to authenticate it
// and pick the right packet password (see binkp.Config.
// PasswordForAddresses and acceptedPacketPasswords), then files away
// whatever it sends exactly as Poll does for an outbound session --
// same handleInboundFile handling (plain packets, ArcMail bundles,
// and everything else via SkippedFiles). Unlike Poll, it doesn't also
// hand the caller this system's own queued outbound mail in the same
// session; an inbound call only ever receives, and sending stays with
// the regular scheduled/crash Poll flow.
func Answer(ctx context.Context, conn net.Conn, ourAddresses []string, uplinks []config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store) (*Result, error) {
	var matchedUplink config.BinkpUplink
	var matched bool

	res := &Result{}
	var receiveErr error
	receiveFile := func(f binkp.InboundFile, r io.Reader) error {
		if !matched {
			// PasswordForAddresses already required a match before
			// authentication could succeed, so this shouldn't happen --
			// drain defensively rather than risk deadlocking the
			// peer's writer goroutine.
			_, err := io.Copy(io.Discard, r)
			return err
		}
		err := handleInboundFile(f, r, acceptedPacketPasswords(matchedUplink, uplinks), netmailStore, messages, users, res)
		if err != nil {
			receiveErr = err
		}
		return err
	}

	sessionResult, err := binkp.Answer(ctx, conn, binkp.Config{
		OurAddresses: ourAddresses,
		PasswordForAddresses: func(peerAddrs []string) (string, bool) {
			u, ok := matchUplink(peerAddrs, uplinks)
			if !ok {
				return "", false
			}
			matchedUplink, matched = u, true
			return u.Password, true
		},
		ReceiveFile: receiveFile,
	})
	if err != nil {
		return nil, fmt.Errorf("tosser: answering inbound session: %w", err)
	}
	res.RemoteAddresses = sessionResult.RemoteAddresses
	return res, receiveErr
}

// matchUplink returns the configured uplink whose own Address matches
// one of peerAddrs (the caller's claimed FTN addresses from M_ADR), if
// any -- used by Answer to authenticate an inbound caller and pick
// which packet password(s) to accept from it.
func matchUplink(peerAddrs []string, uplinks []config.BinkpUplink) (config.BinkpUplink, bool) {
	for _, raw := range peerAddrs {
		addr, err := mail.ParseAddress(raw)
		if err != nil {
			continue
		}
		for _, u := range uplinks {
			uAddr, err := mail.ParseAddress(u.Address)
			if err == nil && uAddr == addr {
				return u, true
			}
		}
	}
	return config.BinkpUplink{}, false
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

// RoutedOutboundEcho returns the locally-posted echomail messages
// (see message.Store.PendingOutboundEcho) that Poll would bundle for
// target, without dialing anything -- so a caller can check whether
// dialing target is even worthwhile. Unlike netmail's routing (an
// ordinary message defaults to whichever uplink is asked, a Crash one
// goes to a zone-specific uplink if configured), echo routing is
// simple: target.Network says outright which areas' posts it carries,
// with no ambiguity to resolve, since an area belongs to exactly one
// network. An uplink with no Network configured never carries any
// (see config.BinkpUplink.Network's doc comment).
func RoutedOutboundEcho(messages *message.Store, target config.BinkpUplink) ([]message.PendingEcho, error) {
	pending, err := messages.PendingOutboundEcho(target.Network)
	if err != nil {
		return nil, fmt.Errorf("tosser: loading pending outbound echomail: %w", err)
	}
	return pending, nil
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

// acceptedPacketPasswords returns every distinct, non-empty
// PacketPassword configured for an uplink sharing primary's Host --
// primary's own password is always included even if primary isn't (or
// isn't yet) one of allUplinks, e.g. a manual "Send Now" dial using an
// address/host typed directly into the web admin rather than a saved
// config entry. A real hub can serve more than one of our AKAs/
// networks over what's really one physical link even though our own
// config models each as a separate uplink entry with its own packet
// password (see tossInbound's doc comment) -- accepting any of them
// avoids wrongly rejecting, and aborting the whole session over, a
// file packed under a sibling network's password.
// isPacketFile reports whether name looks like an FTS-0001 mail
// packet -- conventionally an 8-hex-digit basename with a .pkt
// extension, though this only checks the extension -- rather than
// some other kind of file a BinkP session can carry alongside mail.
// Observed live: a real hub bundled a .tic file-echo announcement
// into the very same session as an ordinary mail packet. Feeding
// anything but a real packet to mail.NewReader fails hard (a .tic
// file made it report "unsupported packet version 17930") and, before
// this check existed, aborted the entire session over it -- discarding
// the Result for mail already successfully tossed earlier in that
// same session, even though the DB writes themselves had already
// happened and stuck.
func isPacketFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".pkt")
}

// isPacketBundleFile reports whether name looks like an FTS-5005
// "ArcMail" bundle of one or more compressed FTS-0001 packets --
// conventionally an 8-hex-digit basename (like a .pkt file) with an
// extension encoding day-of-week + a one-character sequence (su/mo/
// tu/we/th/fr/sa followed by 0-9 or a-z/A-Z, e.g. ".mo0", ".th9",
// ".fra") rather than a compression format. Which archiver actually
// produced it is a matter of agreement between the two systems, not
// something the extension itself says -- see extractPacketBundle,
// which detects the real format from the file's own magic bytes.
// Observed live: a real lovlynet push included a genuine .mo0 file we
// couldn't yet process, skipped as unsupported until this existed.
func isPacketBundleFile(name string) bool {
	dot := strings.LastIndexByte(name, '.')
	if dot < 0 || len(name)-dot != 4 {
		return false
	}
	ext := name[dot+1:]
	switch strings.ToLower(ext[:2]) {
	case "su", "mo", "tu", "we", "th", "fr", "sa":
	default:
		return false
	}
	seq := ext[2]
	return seq >= '0' && seq <= '9' || seq >= 'a' && seq <= 'z' || seq >= 'A' && seq <= 'Z'
}

// namedPacket is one FTS-0001 packet file's name and contents,
// extracted from a packet bundle.
type namedPacket struct {
	name string
	data []byte
}

// extractPacketBundle decompresses a packet bundle's data (read fully
// into memory -- ArcMail bundles are small, KB-scale files, the same
// order of magnitude as the .pkt files inside them) and returns the
// name and contents of every file found inside. Only ZIP is
// recognized today -- the modern, near-universal choice among FTN
// systems still exchanging compressed bundles -- detected from its
// own magic bytes rather than name's extension (see
// isPacketBundleFile's doc comment for why the extension can't say).
// An unrecognized format is a plain error, not a panic, so the caller
// can report the file as skipped rather than losing the whole
// session over it.
func extractPacketBundle(name string, data []byte) ([]namedPacket, error) {
	if !bytes.HasPrefix(data, []byte("PK\x03\x04")) && !bytes.HasPrefix(data, []byte("PK\x05\x06")) {
		return nil, fmt.Errorf("tosser: %s: unrecognized packet bundle format (only ZIP is supported)", name)
	}
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("tosser: %s: opening zip: %w", name, err)
	}
	var packets []namedPacket
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, fmt.Errorf("tosser: %s: opening %s: %w", name, f.Name, err)
		}
		content, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			return nil, fmt.Errorf("tosser: %s: reading %s: %w", name, f.Name, err)
		}
		packets = append(packets, namedPacket{name: f.Name, data: content})
	}
	return packets, nil
}

// handleInboundFile processes one file a peer sent us during a BinkP
// session -- shared by Poll's and Answer's ReceiveFile callback. It
// dispatches by shape: a plain FTS-0001 packet is tossed directly; an
// FTS-5005 packet bundle (isPacketBundleFile) is decompressed and
// every packet inside tossed in turn, res.SkippedFiles noting the
// bundle's own name if it can't be decompressed (an unrecognized
// archive format, say) rather than aborting the session over it;
// anything else is drained and reported in res.SkippedFiles rather
// than dropped silently or fed to the packet parser (which fails hard
// on it -- see isPacketFile's doc comment).
func handleInboundFile(f binkp.InboundFile, r io.Reader, acceptedPasswords []string, netmailStore *netmail.Store, messages *message.Store, users *user.Store, res *Result) error {
	switch {
	case isPacketFile(f.Name):
		stats, err := tossInbound(r, acceptedPasswords, netmailStore, messages, users)
		res.Received += stats.netmail
		res.ReceivedEcho += stats.echo
		return err

	case isPacketBundleFile(f.Name):
		data, err := io.ReadAll(r)
		if err != nil {
			return fmt.Errorf("tosser: reading packet bundle %s: %w", f.Name, err)
		}
		packets, err := extractPacketBundle(f.Name, data)
		if err != nil {
			res.SkippedFiles = append(res.SkippedFiles, f.Name)
			return nil
		}
		for _, p := range packets {
			stats, err := tossInbound(bytes.NewReader(p.data), acceptedPasswords, netmailStore, messages, users)
			res.Received += stats.netmail
			res.ReceivedEcho += stats.echo
			if err != nil {
				return fmt.Errorf("tosser: tossing %s from bundle %s: %w", p.name, f.Name, err)
			}
		}
		return nil

	default:
		if _, err := io.Copy(io.Discard, r); err != nil {
			return fmt.Errorf("tosser: draining unsupported inbound file %s: %w", f.Name, err)
		}
		res.SkippedFiles = append(res.SkippedFiles, f.Name)
		return nil
	}
}

func acceptedPacketPasswords(primary config.BinkpUplink, allUplinks []config.BinkpUplink) []string {
	seen := map[string]bool{}
	var out []string
	add := func(pw string) {
		if pw == "" {
			return
		}
		key := strings.ToLower(pw)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, pw)
	}

	add(primary.PacketPassword)
	for _, u := range allUplinks {
		if u.Host == primary.Host {
			add(u.PacketPassword)
		}
	}
	return out
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

// ourAddressForUplink returns whichever of ourAddresses shares
// uplinkAddr's own FTN zone -- the AKA this system presents as its
// origin when talking to that specific hub, matching how
// uplinkForDestination picks an uplink for a destination the other
// way around. A system with more than one AKA (typically one per FTN
// network it belongs to) needs this: stamping every hub's outbound
// mail with the AKA that actually belongs to its network, rather than
// always whichever AKA happens to be configured first, is what makes
// the origin line/packet header address correct for readers and
// downstream routing on that network. Falls back to ourAddresses[0]
// (already validated as parseable by Poll before this is called) if
// none share uplinkAddr's zone -- the common single-AKA case, or an
// uplink whose own configured Address's zone doesn't match any of
// ours.
func ourAddressForUplink(ourAddresses []string, uplinkAddr mail.Address) mail.Address {
	fallback, _ := mail.ParseAddress(ourAddresses[0])
	for _, raw := range ourAddresses {
		addr, err := mail.ParseAddress(raw)
		if err == nil && addr.Zone == uplinkAddr.Zone {
			return addr
		}
	}
	return fallback
}

// appendTearline appends an FTS-0004 tearline and origin line to
// body, identifying this system's software and the FTN address it's
// presenting to this specific hub -- readers on the wider network
// rely on the origin line to know where a message actually
// originated, and hub software commonly uses it (alongside MSGID) for
// duplicate detection and routing.
func appendTearline(body, bbsName string, origAddr mail.Address) string {
	body = strings.TrimRight(body, "\r\n")
	return fmt.Sprintf("%s\r\r--- %s\r * Origin: %s (%s)\r", body, version.Version, bbsName, origAddr.String())
}

// buildPacket bundles pendingNetmail and pendingEcho into a single
// FTS-0001 packet addressed (at the packet-header level) between
// ourAddr and uplinkAddr -- the BinkP link's own two endpoints --
// stamped with packetPassword (the uplink's configured
// PacketPassword; empty means none). A real .pkt file routinely mixes
// both kinds of message, distinguished per-message by whether its
// body starts with a bare "AREA:tag" line (see echoAreaTag) --
// there's no packet-level distinction.
//
// Each netmail message's own OrigAddr/DestAddr carries its real
// origin/destination, which internal/mail encodes as INTL/FMPT/TOPT
// kludge lines when they differ from the packet header (e.g. a point
// address, or routing through this uplink to a third system). An echo
// message is always addressed OrigAddr=ourAddr/DestAddr=uplinkAddr/
// ToName="All", like every other echomail message; its body is
// prefixed with the AREA: line and a generated MSGID (the message's
// own database ID, hex-formatted -- already unique within this
// system, which combined with our own address is everything FTN
// requires of one) ahead of the actual text. Every message here is a
// fresh local post (see RoutedOutbound/RoutedOutboundEcho -- neither
// ever returns mail relayed in from elsewhere), so appendTearline is
// always safe to add: there's no pre-existing tearline/origin from an
// upstream system to preserve or duplicate.
func buildPacket(ourAddr, uplinkAddr mail.Address, packetPassword string, bbsName string, pendingNetmail []netmail.Message, pendingEcho []message.PendingEcho) (*bytes.Buffer, error) {
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

	for _, m := range pendingNetmail {
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
			Body:     appendTearline(m.Body, bbsName, origAddr),
		}); err != nil {
			return nil, fmt.Errorf("tosser: writing message %d: %w", m.ID, err)
		}
	}

	// Point stripped: internal/mail prepends an FMPT kludge whenever a
	// message's own OrigAddr has one (needed for netmail, since the
	// packet header's binary net/node fields can't otherwise convey a
	// point origin at all), but FTS-0009 requires the bare "AREA:tag"
	// line to be the message body's literal first line with nothing
	// -- kludge or otherwise -- before it. A point system's echomail
	// conventionally flows under its boundary node's flat address
	// regardless, so this loses nothing real messages don't already
	// omit.
	echoOrigAddr := mail.Address{Zone: ourAddr.Zone, Net: ourAddr.Net, Node: ourAddr.Node}
	for _, m := range pendingEcho {
		body := fmt.Sprintf("AREA:%s\r\x01MSGID: %s %08x\r%s", m.AreaTag, echoOrigAddr.String(), m.ID, appendTearline(m.Body, bbsName, echoOrigAddr))
		if err := w.WriteMessage(mail.Message{
			OrigAddr: echoOrigAddr,
			DestAddr: uplinkAddr,
			Written:  m.PostedAt,
			ToName:   "All",
			FromName: m.FromName,
			Subject:  m.Subject,
			Body:     body,
		}); err != nil {
			return nil, fmt.Errorf("tosser: writing echo message %d: %w", m.ID, err)
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
// If expectedPasswords is non-empty, the packet's own header password
// (internal/mail.PacketHeader.Password) must match at least one of
// them case-insensitively -- FTN packet passwords, like most FTN
// passwords, are conventionally uppercased by tossers regardless of
// how a sysop typed them, confirmed against a real uplink that
// stamped its packets in all caps while our configured value used
// mixed case. Accepting any password configured for the host (see
// Poll's acceptedPacketPasswords), not just the one belonging to the
// uplink entry actually dialed, matters because a single hub can pack
// mail for more than one of our AKAs/networks into files sent over
// the very same BinkP session, each stamped with that network's own
// packet password -- observed live where one host (n3.z21.bbs.dege.au)
// identified itself in the handshake as both our fsxNet and HobbyNet
// uplink. A mismatch against every candidate discards the rest of the
// packet (see below) before any message in it is stored, since a
// wrong packet password indicates either a misconfiguration or a
// forged packet, not partial content to salvage.
//
// Whatever the outcome, r is always drained to completion before
// returning: binkp.Config.ReceiveFile's contract requires reading
// every byte of the file, since the network-reading side on the other
// end of the pipe blocks writing further data frames until this side
// keeps consuming them -- returning early, as an earlier version of
// this password check did, deadlocks that writer forever instead of
// cleanly failing the session.
func tossInbound(r io.Reader, expectedPasswords []string, netmailStore *netmail.Store, messages *message.Store, users *user.Store) (stats inboundStats, err error) {
	defer func() {
		if _, drainErr := io.Copy(io.Discard, r); drainErr != nil && err == nil {
			err = fmt.Errorf("tosser: draining inbound packet: %w", drainErr)
		}
	}()

	pr, err := mail.NewReader(r)
	if err != nil {
		return stats, fmt.Errorf("tosser: reading inbound packet: %w", err)
	}
	if len(expectedPasswords) > 0 {
		matched := false
		for _, pw := range expectedPasswords {
			if strings.EqualFold(pr.Header.Password, pw) {
				matched = true
				break
			}
		}
		if !matched {
			return stats, fmt.Errorf("tosser: inbound packet password does not match any configured packet password for this host")
		}
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
			created, err := tossEcho(tag, msg, messages)
			if err != nil {
				return stats, fmt.Errorf("tosser: tossing echomail message %d: %w", stats.netmail+stats.echo+1, err)
			}
			if created {
				stats.echo++
			}
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

// echoMsgID returns the value of body's MSGID kludge line (e.g.
// "21:3/100 5f3e2a1b"), or "" if it has none -- scanning the same
// leading AREA-then-\x01-kludges block as stripLeadingKludges, so it
// never mistakes ordinary message text for a kludge.
func echoMsgID(body string) string {
	const prefix = "MSGID:"
	lines := strings.Split(body, "\n")
	i := 0
	if i < len(lines) && len(lines[i]) > 5 && strings.EqualFold(lines[i][:5], "AREA:") {
		i++
	}
	for i < len(lines) && strings.HasPrefix(lines[i], "\x01") {
		line := lines[i][1:]
		if len(line) > len(prefix) && strings.EqualFold(line[:len(prefix)], prefix) {
			return strings.TrimSpace(line[len(prefix):])
		}
		i++
	}
	return ""
}

// tossEcho files msg into the message area tagged tag, creating it as
// Pending (see message.Store.EnsureArea) if this is the first message
// ever seen for it -- so an unrecognized incoming echo area doesn't
// silently drop mail, but also doesn't appear in the BBS until the
// sysop has reviewed and approved it. created reports whether msg was
// actually new (false for a duplicate MSGID already stored in this
// area -- see message.Store.ReceiveEcho), so the caller can avoid
// double-counting a message a hub resent after failing to see our
// M_GOT.
func tossEcho(tag string, msg *mail.Message, messages *message.Store) (created bool, err error) {
	area, _, err := messages.EnsureArea(tag, tag, "")
	if err != nil {
		return false, fmt.Errorf("ensuring area %q: %w", tag, err)
	}

	fromName := msg.FromName
	if fromName == "" {
		fromName = msg.OrigAddr.String()
	}
	_, created, err = messages.ReceiveEcho(area.ID, fromName, msg.Subject, stripLeadingKludges(msg.Body), echoMsgID(msg.Body), msg.Written)
	if err != nil {
		return false, fmt.Errorf("storing message in area %q: %w", tag, err)
	}
	return created, nil
}

func containsString(ss []string, s string) bool {
	for _, x := range ss {
		if x == s {
			return true
		}
	}
	return false
}
