package tosser

import (
	"errors"
	"fmt"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
)

// RobotConfig bundles what the inbound Areafix/Filefix robot (see
// handleAreafixRequest) needs beyond the plain netmail/message stores
// every inbound toss already has: this system's own addresses (to
// address the reply and pick the right AKA, mirroring outbound
// requests -- see ourAddressForUplink), this system's own display name
// (BBSName -- see requestAreaCommand's doc comment for why a reply
// must never claim to be from "Areafix"/"Filefix" itself), every
// configured uplink (both to authenticate the requester by its
// claimed FTN address and to look up which Areafix/Filefix password
// applies), the echo/file-area subscription stores an accepted
// "+TAG"/"-TAG" command is recorded into (Direction Inbound: a
// downlink asking us, the mirror image of the Outbound direction our
// own RequestEchoAreaChanges/RequestFileAreaChanges record), and the
// file-area catalog a Filefix "%LIST" reply draws from (Areafix's own
// %LIST uses the *message.Store tossInbound already has). A nil
// *RobotConfig disables the robot entirely -- an inbound netmail
// addressed to "Areafix"/"Filefix" is then just filed as ordinary
// netmail, exactly as before this existed.
type RobotConfig struct {
	OurAddresses []string
	BBSName      string
	Uplinks      []config.BinkpUplink
	EchoStore    *areafix.EchoStore
	FileStore    *areafix.FileStore
	Files        *file.Store
}

// handleAreafixRequest processes msg if it's an inbound netmail
// addressed to "Areafix" or "Filefix" (defaultAreafixRobotName/
// defaultFilefixRobotName) from a sender matching one of robot's
// configured Uplinks, mirroring the outbound requests this system
// itself sends (see RequestEchoAreaChanges/RequestFileAreaChanges):
// the password is expected in Subject (not the body -- see
// requestAreaCommand's doc comment for why), "+TAG"/"-TAG" body lines
// subscribe/unsubscribe, and "%LIST" asks for the catalog of areas
// available to request.
//
// Returns handled=false (msg left untouched, for the caller's normal
// netmail storage) whenever this system can't authenticate the
// request as coming from a real, configured link: robot is nil,
// ToName isn't a robot name, the sender's address doesn't match any
// configured uplink, or that uplink has no password configured for
// this specific robot. Areafix/Filefix passwords are opt-in per
// uplink this way -- leaving one blank disables the inbound robot for
// that link entirely rather than silently accepting an
// unauthenticated request, so an unrecognized/not-yet-configured
// sender's netmail still reaches a sysop (e.g. via
// netmail.Store.UnresolvedInbox) instead of vanishing into a bounce.
// A wrong password, by contrast, IS authenticated enough to recognize
// as a genuine (if mistaken) robot request: handled=true and a
// rejection reply is queued, mirroring how a real Areafix robot
// answers a bad password rather than staying silent or storing it as
// ordinary mail.
func handleAreafixRequest(msg *mail.Message, robot *RobotConfig, messages *message.Store, netmailStore *netmail.Store) (handled bool, err error) {
	if robot == nil {
		return false, nil
	}
	var isAreafix bool
	switch {
	case strings.EqualFold(msg.ToName, defaultAreafixRobotName):
		isAreafix = true
	case strings.EqualFold(msg.ToName, defaultFilefixRobotName):
		isAreafix = false
	default:
		return false, nil
	}
	if isAreafix && robot.EchoStore == nil {
		return false, nil
	}
	if !isAreafix && (robot.FileStore == nil || robot.Files == nil) {
		return false, nil
	}

	uplink, ok := uplinkForAddress(msg.OrigAddr, robot.Uplinks)
	if !ok {
		return false, nil
	}
	robotName := defaultFilefixRobotName
	password := uplink.FilefixPassword
	if isAreafix {
		robotName = defaultAreafixRobotName
		password = uplink.AreafixPassword
	}
	if password == "" {
		return false, nil
	}
	if len(robot.OurAddresses) == 0 {
		return true, fmt.Errorf("tosser: inbound %s robot: no FTN addresses configured for this system", robotName)
	}

	ourAddr := ourAddressForUplink(robot.OurAddresses, msg.OrigAddr)
	// replyFromName must never be robotName ("Areafix"/"Filefix"):
	// this reply's ToName mirrors msg.FromName right back at the
	// sender, so if this side's own FromName here were the robot's
	// name too, a peer's inbound handler (this same function, on
	// either end) would misidentify the reply as a fresh incoming
	// request and answer it, which answers back the same way, forever
	// -- confirmed live as a real, hours-long netmail loop between two
	// NullModem test systems before this fix (see
	// requestAreaCommand's own doc comment for the outbound-request
	// half of this same rule).
	replyFromName := robot.BBSName
	if replyFromName == "" {
		replyFromName = "BBS"
	}
	replyTo := func(body string) error {
		_, err := netmailStore.SendSystem(replyFromName, ourAddr.String(), msg.FromName, msg.OrigAddr.String(), "Re: "+msg.Subject, body, true)
		return err
	}

	if !strings.EqualFold(strings.TrimSpace(msg.Subject), password) {
		return true, replyTo("Password incorrect.")
	}

	list, changes := parseAreafixCommands(msg.Body)
	var reply strings.Builder
	for _, c := range changes {
		status, err := applyAreaChange(c, uplink.Host, isAreafix, robot, messages)
		if err != nil {
			return true, err
		}
		switch status {
		case areaChangeNotFound:
			fmt.Fprintf(&reply, "%s: no such area\r", c.Tag)
			continue
		case areaChangeNotPermitted:
			fmt.Fprintf(&reply, "%s: not permitted\r", c.Tag)
			continue
		}
		sign, verb := "-", "removed"
		if c.Subscribe {
			sign, verb = "+", "added"
		}
		fmt.Fprintf(&reply, "%s%s: %s\r", sign, c.Tag, verb)
	}

	if list {
		lines, err := areaCatalog(uplink.Host, isAreafix, robot, messages)
		if err != nil {
			return true, err
		}
		if reply.Len() > 0 {
			reply.WriteString("\r")
		}
		reply.WriteString(strings.Join(lines, "\r"))
	}

	if reply.Len() == 0 {
		reply.WriteString("Command processed.")
	}
	return true, replyTo(reply.String())
}

// uplinkForAddress returns the configured uplink whose own Address
// matches addr, if any -- the per-message counterpart of matchUplink,
// which matches a whole BinkP session's claimed M_ADR addresses
// instead of one already-parsed mail.Address.
func uplinkForAddress(addr mail.Address, uplinks []config.BinkpUplink) (config.BinkpUplink, bool) {
	for _, u := range uplinks {
		uAddr, err := mail.ParseAddress(u.Address)
		if err == nil && uAddr == addr {
			return u, true
		}
	}
	return config.BinkpUplink{}, false
}

// parseAreafixCommands parses body's command lines -- changeLines'
// exact inverse: "+TAG"/"-TAG" to subscribe/unsubscribe, "%LIST"
// (case-insensitive -- see areaListCommand) to request the area
// catalog. Any other line is silently ignored, mirroring how a real
// Areafix robot skips content it doesn't recognize (kludges, a
// trailing signature, ...) rather than rejecting the whole request
// over it.
func parseAreafixCommands(body string) (list bool, changes []AreaChange) {
	for _, raw := range strings.Split(body, "\n") {
		line := strings.TrimSpace(strings.TrimRight(raw, "\r"))
		if line == "" {
			continue
		}
		if strings.EqualFold(line, areaListCommand) {
			list = true
			continue
		}
		if line[0] != '+' && line[0] != '-' {
			continue
		}
		tag := strings.TrimSpace(line[1:])
		if tag == "" {
			continue
		}
		changes = append(changes, AreaChange{Tag: tag, Subscribe: line[0] == '+'})
	}
	return list, changes
}

// areaChangeStatus is applyAreaChange's outcome, distinguishing "no
// such area" from "that area exists but this downlink isn't granted
// access to it" -- each gets its own distinct reply line (see
// handleAreafixRequest) rather than one generic rejection, so a sysop
// reading the exchange (or a confused downlink sysop asking about it)
// can tell a typo apart from a permission that needs granting.
type areaChangeStatus int

const (
	areaChangeOK areaChangeStatus = iota
	areaChangeNotFound
	areaChangeNotPermitted
)

// applyAreaChange records one accepted +/-TAG command against the
// appropriate subscription store (Direction Inbound), first checking
// that a local area with that tag actually exists, then -- for a
// subscribe (+TAG) only -- that uplinkHost has actually been granted
// access to it (see echo_area_grants/file_area_grants' schema
// comment): a downlink authenticated by the right password still
// can't subscribe to anything the sysop hasn't explicitly granted it.
// An unsubscribe (-TAG) is never blocked this way even for an area
// whose grant was since revoked -- a downlink must always be able to
// drop a subscription it already has (or thinks it has) rather than
// getting stuck with no way to clean up its own state.
func applyAreaChange(c AreaChange, uplinkHost string, isAreafix bool, robot *RobotConfig, messages *message.Store) (areaChangeStatus, error) {
	if isAreafix {
		if _, err := messages.AreaByTag(c.Tag); err != nil {
			if errors.Is(err, message.ErrAreaNotFound) {
				return areaChangeNotFound, nil
			}
			return areaChangeNotFound, err
		}
		if c.Subscribe {
			granted, err := robot.EchoStore.IsGranted(uplinkHost, c.Tag)
			if err != nil {
				return areaChangeNotFound, err
			}
			if !granted {
				return areaChangeNotPermitted, nil
			}
		}
		return areaChangeOK, recordChanges(robot.EchoStore, uplinkHost, []AreaChange{c}, areafix.Inbound)
	}
	if _, err := robot.Files.AreaByTag(c.Tag); err != nil {
		if errors.Is(err, file.ErrAreaNotFound) {
			return areaChangeNotFound, nil
		}
		return areaChangeNotFound, err
	}
	if !c.Subscribe {
		return areaChangeOK, recordChanges(robot.FileStore, uplinkHost, []AreaChange{c}, areafix.Inbound)
	}
	granted, err := robot.FileStore.IsGranted(uplinkHost, c.Tag)
	if err != nil {
		return areaChangeNotFound, err
	}
	if !granted {
		return areaChangeNotPermitted, nil
	}
	return areaChangeOK, recordChanges(robot.FileStore, uplinkHost, []AreaChange{c}, areafix.Inbound)
}

// areaCatalog renders the "%LIST" reply body: one line per area
// uplinkHost has been granted access to (see echo_area_grants/
// file_area_grants -- an area that exists locally but was never
// granted to this downlink is left out entirely, not merely
// unmarked, so %LIST can't be used to discover areas a downlink isn't
// permitted to see at all), "+TAG Name" if it already has an inbound
// subscription to it (Direction Inbound) or " TAG Name" otherwise --
// the plainest, near-universal shape (see areafix.ParseAreaListReply's
// parseWhitespaceLine) so any Areafix-aware client, this system's own
// outbound parser included, can read it back.
func areaCatalog(uplinkHost string, isAreafix bool, robot *RobotConfig, messages *message.Store) ([]string, error) {
	type entry struct{ tag, name string }
	var entries []entry
	var subs []areafix.Subscription
	var granted map[string]bool
	var err error
	if isAreafix {
		areas, aerr := messages.AllAreas()
		if aerr != nil {
			return nil, aerr
		}
		granted, err = robot.EchoStore.GrantedTags(uplinkHost)
		if err != nil {
			return nil, err
		}
		for _, a := range areas {
			if granted[strings.ToUpper(a.Tag)] {
				entries = append(entries, entry{a.Tag, a.Name})
			}
		}
		subs, err = robot.EchoStore.ListForUplink(uplinkHost, areafix.Inbound)
	} else {
		areas, aerr := robot.Files.AllAreas()
		if aerr != nil {
			return nil, aerr
		}
		granted, err = robot.FileStore.GrantedTags(uplinkHost)
		if err != nil {
			return nil, err
		}
		for _, a := range areas {
			if granted[strings.ToUpper(a.Tag)] {
				entries = append(entries, entry{a.Tag, a.Name})
			}
		}
		subs, err = robot.FileStore.ListForUplink(uplinkHost, areafix.Inbound)
	}
	if err != nil {
		return nil, err
	}

	subscribed := make(map[string]bool, len(subs))
	for _, s := range subs {
		subscribed[strings.ToUpper(s.AreaTag)] = true
	}
	lines := make([]string, len(entries))
	for i, e := range entries {
		marker := " "
		if subscribed[strings.ToUpper(e.tag)] {
			marker = "+"
		}
		lines[i] = fmt.Sprintf("%s%s %s", marker, e.tag, e.name)
	}
	return lines, nil
}
