package tosser

import (
	"fmt"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
)

// defaultAreafixRobotName and defaultFilefixRobotName are the
// near-universal FTN convention for these robots' netmail recipient
// name -- not currently configurable, since no real hub encountered
// so far needs anything else; add a config override if one ever does.
const (
	defaultAreafixRobotName = "Areafix"
	defaultFilefixRobotName = "Filefix"
)

// areaListCommand is the near-universal convention for asking an
// Areafix/Filefix robot to reply with every area it carries -- real
// hub software varies in exactly how it formats that reply (see
// areafix.ParseAreaListReply), but "%LIST" itself is understood
// almost everywhere.
const areaListCommand = "%LIST"

// AreaChange is one area a RequestEchoAreaChanges/
// RequestFileAreaChanges caller wants added (Subscribe true) or
// dropped (false) -- batched into a single netmail so requesting
// several areas at once doesn't mean several separate messages (and
// several separate replies to sift through).
type AreaChange struct {
	Tag       string
	Subscribe bool
}

// RequestEchoAreaSubscription is RequestEchoAreaChanges for a single
// area -- see that function's doc comment.
func RequestEchoAreaSubscription(netmailStore *netmail.Store, subs *areafix.EchoStore, ourAddresses []string, bbsName string, uplink config.BinkpUplink, areaTag string, subscribe bool) (*netmail.Message, error) {
	return RequestEchoAreaChanges(netmailStore, subs, ourAddresses, bbsName, uplink, []AreaChange{{Tag: areaTag, Subscribe: subscribe}})
}

// RequestEchoAreaChanges queues a single Crash-priority netmail asking
// uplink's own Areafix robot to add/drop every area in changes (one
// "+TAG"/"-TAG" line each), and records each request in subs (see
// areafix.EchoStore) so the web admin UI can show what's pending.
// ourAddresses picks whichever AKA shares uplink's own FTN zone (see
// ourAddressForUplink), matching how Poll stamps outbound mail's
// origin. bbsName identifies us as the request's sender (see
// requestAreaCommand's doc comment for why this must not be
// defaultAreafixRobotName itself). The request is Crash-priority and
// addressed to uplink's own configured Address specifically, so
// routeOutbound sends it to exactly that uplink (never a different,
// same-zone one) and cmd/mailer's universal crash-trigger dials it
// immediately rather than waiting for a scheduled poll.
func RequestEchoAreaChanges(netmailStore *netmail.Store, subs *areafix.EchoStore, ourAddresses []string, bbsName string, uplink config.BinkpUplink, changes []AreaChange) (*netmail.Message, error) {
	msg, err := requestAreaCommand(netmailStore, ourAddresses, bbsName, uplink, defaultAreafixRobotName, uplink.AreafixPassword, changeLines(changes))
	if err != nil {
		return nil, err
	}
	if err := recordChanges(subs, uplink.Host, changes, areafix.Outbound); err != nil {
		return msg, fmt.Errorf("tosser: queued the request but failed to record it: %w", err)
	}
	return msg, nil
}

// RequestEchoAreaList queues a Crash-priority netmail asking uplink's
// Areafix robot to reply with every echo area it carries (see
// areaListCommand) -- the reply arrives later as ordinary inbound
// netmail; see netmail.Store.InboxFromAddress and
// areafix.ParseAreaListReply for pulling it back out and turning it
// into a pickable list.
func RequestEchoAreaList(netmailStore *netmail.Store, ourAddresses []string, bbsName string, uplink config.BinkpUplink) (*netmail.Message, error) {
	return requestAreaCommand(netmailStore, ourAddresses, bbsName, uplink, defaultAreafixRobotName, uplink.AreafixPassword, []string{areaListCommand})
}

// RequestFileAreaSubscription is RequestEchoAreaSubscription's exact
// counterpart for a file-echo (TIC) area, addressed to uplink's
// Filefix robot instead of Areafix.
func RequestFileAreaSubscription(netmailStore *netmail.Store, subs *areafix.FileStore, ourAddresses []string, bbsName string, uplink config.BinkpUplink, areaTag string, subscribe bool) (*netmail.Message, error) {
	return RequestFileAreaChanges(netmailStore, subs, ourAddresses, bbsName, uplink, []AreaChange{{Tag: areaTag, Subscribe: subscribe}})
}

// RequestFileAreaChanges is RequestEchoAreaChanges's exact counterpart
// for file-echo (TIC) areas, addressed to uplink's Filefix robot
// instead of Areafix -- see that function's doc comment.
func RequestFileAreaChanges(netmailStore *netmail.Store, subs *areafix.FileStore, ourAddresses []string, bbsName string, uplink config.BinkpUplink, changes []AreaChange) (*netmail.Message, error) {
	msg, err := requestAreaCommand(netmailStore, ourAddresses, bbsName, uplink, defaultFilefixRobotName, uplink.FilefixPassword, changeLines(changes))
	if err != nil {
		return nil, err
	}
	if err := recordChanges(subs, uplink.Host, changes, areafix.Outbound); err != nil {
		return msg, fmt.Errorf("tosser: queued the request but failed to record it: %w", err)
	}
	return msg, nil
}

// RequestFileAreaList is RequestEchoAreaList's exact counterpart for
// file-echo areas, addressed to uplink's Filefix robot.
func RequestFileAreaList(netmailStore *netmail.Store, ourAddresses []string, bbsName string, uplink config.BinkpUplink) (*netmail.Message, error) {
	return requestAreaCommand(netmailStore, ourAddresses, bbsName, uplink, defaultFilefixRobotName, uplink.FilefixPassword, []string{areaListCommand})
}

// changeLines renders each AreaChange as its "+TAG"/"-TAG" command
// line, in the order given.
func changeLines(changes []AreaChange) []string {
	lines := make([]string, len(changes))
	for i, c := range changes {
		sign := "+"
		if !c.Subscribe {
			sign = "-"
		}
		lines[i] = sign + c.Tag
	}
	return lines
}

// echoSubscriptionRecorder and fileSubscriptionRecorder let
// recordChanges share one implementation across areafix.EchoStore/
// FileStore without a shared interface type in package areafix.
type echoSubscriptionRecorder interface {
	Request(uplinkHost, areaTag string, direction areafix.Direction) error
	Withdraw(uplinkHost, areaTag string, direction areafix.Direction) error
}

func recordChanges(subs echoSubscriptionRecorder, uplinkHost string, changes []AreaChange, direction areafix.Direction) error {
	for _, c := range changes {
		var err error
		if c.Subscribe {
			err = subs.Request(uplinkHost, c.Tag, direction)
		} else {
			err = subs.Withdraw(uplinkHost, c.Tag, direction)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// requestAreaCommand builds and queues the actual netmail -- shared by
// every Request*/RequestEchoAreaList/RequestFileAreaList function,
// which differ only in robot name, which password field to use, and
// which command lines to send. The password goes in the Subject line,
// not the body -- confirmed live against a real Areafix robot
// (Clearing Houz): it authenticates the Subject specifically, and a
// request with the (correct) password sitting on the body's first
// line instead was rejected as "password incorrect."
//
// bbsName, not robotName, is the message's own FromName: the request
// is addressed TO the remote robot (robotName, e.g. "Areafix"), but
// must identify US as its sender, not the robot it's headed to.
// handleAreafixRequest's own reply mirrors the request's FromName
// back as its reply's ToName (there being no other name to address a
// robot-to-robot reply to) -- were this robotName too, our own
// inbound handler would see that reply land with ToName == "Areafix"
// and misidentify it as a fresh incoming request instead of a reply
// to one we sent, worth a defensive "password incorrect" bounce back
// (the reply's "Re: <password>" subject doesn't match either
// robot's own configured password) and, against a real hub doing the
// same mirroring, an indefinite once-per-poll ping-pong. Confirmed
// live against the NullModem test network's own boss/point pair.
func requestAreaCommand(netmailStore *netmail.Store, ourAddresses []string, bbsName string, uplink config.BinkpUplink, robotName, password string, lines []string) (*netmail.Message, error) {
	if len(ourAddresses) == 0 {
		return nil, fmt.Errorf("tosser: no FTN addresses configured for this system")
	}
	if _, err := mail.ParseAddress(ourAddresses[0]); err != nil {
		return nil, fmt.Errorf("tosser: this system's FTN address %q: %w", ourAddresses[0], err)
	}
	uplinkAddr, err := mail.ParseAddress(uplink.Address)
	if err != nil {
		return nil, fmt.Errorf("tosser: uplink address %q: %w", uplink.Address, err)
	}
	ourAddr := ourAddressForUplink(ourAddresses, uplinkAddr)

	var b strings.Builder
	for _, line := range lines {
		b.WriteString(line)
		b.WriteByte('\r')
	}

	return netmailStore.SendSystem(bbsName, ourAddr.String(), robotName, uplink.Address, password, b.String(), true)
}
