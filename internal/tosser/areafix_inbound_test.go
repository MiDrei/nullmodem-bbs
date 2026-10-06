package tosser

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/areafix"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/db"
	"git.maik.ch/nullmodem/bbs/internal/file"
	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/message"
	"git.maik.ch/nullmodem/bbs/internal/netmail"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// newTestStoresWithRobot mirrors newTestStoresWithAreafix, plus a
// file.Store, for the inbound-robot tests below (Filefix needs a real
// file-area catalog to validate tags and serve %LIST against).
func newTestStoresWithRobot(t *testing.T) (*netmail.Store, *message.Store, *file.Store, *user.Store, *areafix.EchoStore, *areafix.FileStore) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return netmail.NewStore(sqlDB), message.NewStore(sqlDB), file.NewStore(sqlDB, t.TempDir()), user.NewStore(sqlDB),
		areafix.NewEchoStore(sqlDB), areafix.NewFileStore(sqlDB)
}

// downlinkUplink is the "uplink" entry authenticating an inbound
// robot request in these tests -- named for what it actually is from
// the requester's point of view (our downlink), even though it lives
// in the same config.BinkpUplink list/type as our own uplinks (see
// config.Binkp.Uplinks' doc comment: the same list authenticates
// inbound callers too).
var downlinkUplink = config.BinkpUplink{
	Address:         "21:3/100",
	Host:            "downlink.example.com:24554",
	AreafixPassword: "areasecret",
	FilefixPassword: "filesecret",
}

func downlinkOrigAddr() mail.Address {
	addr, _ := mail.ParseAddress(downlinkUplink.Address)
	return addr
}

func pendingReplyTo(t *testing.T, netmailStore *netmail.Store, toAddress string) *netmail.Message {
	t.Helper()
	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	for i := range pending {
		if pending[i].ToAddress == toAddress {
			return &pending[i]
		}
	}
	t.Fatalf("no queued reply to %s found in PendingOutbound: %+v", toAddress, pending)
	return nil
}

func TestHandleAreafixRequestSubscribesToExistingArea(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := echoSubs.Grant(downlinkUplink.Host, "TESTAREA"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		BBSName:      "Test BBS",
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "+TESTAREA\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true for a correctly authenticated Areafix request")
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 || subs[0].AreaTag != "TESTAREA" {
		t.Fatalf("inbound subscriptions = %+v, want exactly one for TESTAREA", subs)
	}

	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if !reply.Crash {
		t.Fatal("reply.Crash = false, want true (dialed immediately)")
	}
	if !strings.Contains(reply.Body, "+TESTAREA: added") {
		t.Fatalf("reply body = %q, want it to confirm +TESTAREA was added", reply.Body)
	}
	// FromName must be robot.BBSName, never "Areafix"/"Filefix" --
	// see TestHandleAreafixRequestReplyLoopSelfTerminates for why.
	if reply.FromName != "Test BBS" {
		t.Fatalf("reply.FromName = %q, want robot.BBSName (Test BBS), not the robot's own name", reply.FromName)
	}
}

// TestHandleAreafixRequestReplyLoopSelfTerminates is an end-to-end
// regression test for a real, hours-long netmail loop observed live
// between two NullModem test systems: handleAreafixRequest's own
// reply (see replyTo) used to carry the robot's own name ("Areafix"/
// "Filefix") as its FromName, same as the request it was replying to
// did before requestAreaCommand's own fix. Since a reply's ToName
// always mirrors the triggering message's FromName right back, a
// robot-named reply looked exactly like a fresh incoming request to
// whichever side received it next -- which then replied the same way,
// forever. This feeds handleAreafixRequest's own reply back into
// itself, mimicking the peer mirroring ToName back at us exactly as a
// real one does, and requires it NOT be treated as a new request the
// second time around.
func TestHandleAreafixRequestReplyLoopSelfTerminates(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		BBSName:      "Test BBS",
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	// A wrong-password request, so the reply is the short rejection
	// path -- irrelevant to what's under test here (only FromName/
	// ToName matter), and simpler to trigger than a real subscribe.
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "wrong-password",
		Body:     "+TESTAREA\n",
	}
	if handled, err := handleAreafixRequest(msg, robot, messages, netmailStore); err != nil || !handled {
		t.Fatalf("handleAreafixRequest: handled=%v err=%v", handled, err)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)

	// The peer would mirror this reply's FromName back as ToName --
	// simulate that arriving back at us.
	mirrored := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   reply.FromName,
		FromName: "Downlink Sysop",
		Subject:  "Re: " + reply.Subject,
		Body:     reply.Body,
	}
	handled, err := handleAreafixRequest(mirrored, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest (mirrored): %v", err)
	}
	if handled {
		t.Fatalf("handled = true for a reply mirrored back at us (ToName %q) -- want it left alone as ordinary netmail, not treated as a new request", mirrored.ToName)
	}
}

// TestHandleAreafixRequestDoesNotMisidentifyAReplyToOurOwnRequest
// locks in requestAreaCommand's FromName fix: a reply to a request we
// sent (ToName mirrors that request's own FromName, per
// handleAreafixRequest's own replyTo) must never itself be recognized
// as a fresh incoming request just because it happens to come from a
// configured uplink -- only an inbound message actually addressed TO
// "Areafix"/"Filefix" is a request. handled=false here means it falls
// through to ordinary netmail storage instead (see
// netmail.Store.UnresolvedInbox, which exists specifically to surface
// a reply like this).
func TestHandleAreafixRequestDoesNotMisidentifyAReplyToOurOwnRequest(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	// Shaped exactly like handleAreafixRequest's own replyTo builds a
	// reply: ToName is our own bbsName (whatever FromName our original
	// request carried, see requestAreaCommand), FromName is the remote
	// robot's own name.
	reply := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Test BBS",
		FromName: "Areafix",
		Subject:  "Re: areasecret",
		Body:     "+TESTAREA: added\r",
	}

	handled, err := handleAreafixRequest(reply, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false -- this is a reply to our own request, not an incoming one")
	}
}

func TestHandleAreafixRequestWrongPasswordSendsRejection(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "wrongpassword",
		Body:     "+TESTAREA\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true -- a known link with a wrong password is still a recognized robot request")
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("inbound subscriptions = %+v, want none recorded after a wrong password", subs)
	}

	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if reply.Body != "Password incorrect." {
		t.Fatalf("reply body = %q, want %q", reply.Body, "Password incorrect.")
	}
}

func TestHandleAreafixRequestUnknownSenderFallsThrough(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: mail.Address{Zone: 21, Net: 9, Node: 999}, // not a configured uplink
		ToName:   "Areafix",
		FromName: "Stranger",
		Subject:  "areasecret",
		Body:     "+TESTAREA\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false -- an unrecognized sender must fall through to ordinary netmail storage, not an automated reply")
	}
	pending, err := netmailStore.PendingOutbound()
	if err != nil {
		t.Fatalf("PendingOutbound: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("PendingOutbound = %+v, want no reply queued for an unauthenticated sender", pending)
	}
}

func TestHandleAreafixRequestBlankPasswordDisablesRobotForThatLink(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	uplinkNoPassword := config.BinkpUplink{Address: "21:3/100", Host: "downlink.example.com:24554"}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{uplinkNoPassword},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "",
		Body:     "+TESTAREA\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if handled {
		t.Fatal("handled = true, want false -- a link with no Areafix password configured must leave the robot disabled, not open to an empty-subject request")
	}
}

func TestHandleAreafixRequestListRepliesWithCatalog(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if _, err := messages.CreateArea("CHAT", "Random Chat", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	// SECRET exists locally but is never granted to this downlink --
	// %LIST must leave it out of the catalog entirely, not merely
	// unmarked, so it can't be discovered this way at all.
	if _, err := messages.CreateArea("SECRET", "Sysop Only", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := echoSubs.Grant(downlinkUplink.Host, "TESTAREA"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := echoSubs.Grant(downlinkUplink.Host, "CHAT"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	if err := echoSubs.Request(downlinkUplink.Host, "TESTAREA", areafix.Inbound); err != nil {
		t.Fatalf("pre-subscribe: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "%LIST\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true")
	}

	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	// The reply's own body is stored using bare-CR line endings (see
	// requestAreaCommand's doc comment -- the same convention this
	// robot's reply mirrors, since it's queued the same way and read
	// back unconverted from the local DB here, unlike a genuinely
	// received message whose toss already ran fromFTNLineEndings on
	// it). ParseAreaListReply expects "\n"-delimited text, matching
	// what a real recipient's own toss would hand it, so convert
	// before parsing -- exactly what happens for real on the wire.
	normalized := strings.ReplaceAll(reply.Body, "\r", "\n")
	parsed := areafix.ParseAreaListReply(normalized)
	// TESTAREA and CHAT (created above) -- the schema's always-seeded
	// "general" area is also in the catalog, but ParseAreaListReply
	// deliberately doesn't recognize its all-lowercase tag as a
	// plausible area tag at all (see looksLikeAreaTag: indistinguishable
	// from an ordinary English word without this system's own
	// lowercase-by-convention default, which real hub tags never are),
	// so it's correctly absent from parsed rather than a bug here.
	if len(parsed) != 2 {
		t.Fatalf("parsed %d areas from reply body %q, want 2", len(parsed), reply.Body)
	}
	byTag := map[string]areafix.ParsedArea{}
	for _, p := range parsed {
		byTag[p.Tag] = p
	}
	if !byTag["TESTAREA"].Subscribed {
		t.Fatalf("TESTAREA entry = %+v, want Subscribed true (already recorded as an inbound subscription)", byTag["TESTAREA"])
	}
	if byTag["CHAT"].Subscribed {
		t.Fatalf("CHAT entry = %+v, want Subscribed false", byTag["CHAT"])
	}
	if _, ok := byTag["SECRET"]; ok {
		t.Fatalf("parsed areas = %+v, want SECRET absent (never granted to this downlink)", parsed)
	}
	if strings.Contains(reply.Body, "SECRET") {
		t.Fatalf("reply body = %q, want it to not even mention the ungranted SECRET area", reply.Body)
	}
}

func TestHandleAreafixRequestUnsubscribeRemovesRecord(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := echoSubs.Request(downlinkUplink.Host, "TESTAREA", areafix.Inbound); err != nil {
		t.Fatalf("pre-subscribe: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "-TESTAREA\n",
	}

	if _, err := handleAreafixRequest(msg, robot, messages, netmailStore); err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("inbound subscriptions = %+v, want none after unsubscribing", subs)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if !strings.Contains(reply.Body, "-TESTAREA: removed") {
		t.Fatalf("reply body = %q, want it to confirm -TESTAREA was removed", reply.Body)
	}
}

func TestHandleAreafixRequestUnknownAreaTagRepliesNoSuchArea(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "+NOPE\n",
	}

	if _, err := handleAreafixRequest(msg, robot, messages, netmailStore); err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("inbound subscriptions = %+v, want none recorded for a nonexistent tag", subs)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if !strings.Contains(reply.Body, "NOPE: no such area") {
		t.Fatalf("reply body = %q, want it to say NOPE has no such area", reply.Body)
	}
}

// TestHandleAreafixRequestUngrantedAreaRepliesNotPermitted is a
// regression test for the default-deny grant model (see
// echo_area_grants' schema comment): a downlink authenticated with
// the correct password still can't subscribe to a real, existing area
// the sysop hasn't explicitly granted it -- distinct from "no such
// area" (TestHandleAreafixRequestUnknownAreaTagRepliesNoSuchArea),
// since the area does exist, just not for this downlink.
func TestHandleAreafixRequestUngrantedAreaRepliesNotPermitted(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "+TESTAREA\n",
	}

	if _, err := handleAreafixRequest(msg, robot, messages, netmailStore); err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("inbound subscriptions = %+v, want none recorded for an ungranted area", subs)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if !strings.Contains(reply.Body, "TESTAREA: not permitted") {
		t.Fatalf("reply body = %q, want it to say TESTAREA is not permitted", reply.Body)
	}
}

// TestHandleAreafixRequestUnsubscribeAllowedEvenWithoutAGrant checks
// applyAreaChange's other half: a downlink must always be able to
// unsubscribe from an area, even one it was never (or is no longer)
// granted access to -- see applyAreaChange's doc comment for why an
// unsubscribe is never blocked by the grant check.
func TestHandleAreafixRequestUnsubscribeAllowedEvenWithoutAGrant(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	// Simulate a subscription that predates a since-revoked grant --
	// recorded directly, bypassing the (grant-gated) +TAG path.
	if err := echoSubs.Request(downlinkUplink.Host, "TESTAREA", areafix.Inbound); err != nil {
		t.Fatalf("pre-subscribe: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "-TESTAREA\n",
	}

	if _, err := handleAreafixRequest(msg, robot, messages, netmailStore); err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 0 {
		t.Fatalf("inbound subscriptions = %+v, want the unsubscribe to succeed regardless of grant state", subs)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if !strings.Contains(reply.Body, "-TESTAREA: removed") {
		t.Fatalf("reply body = %q, want it to confirm -TESTAREA was removed", reply.Body)
	}
}

func TestHandleAreafixRequestFilefixSubscribesToExistingFileArea(t *testing.T) {
	netmailStore, messages, files, _, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := files.CreateArea("UTILS", "Utilities", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := fileSubs.Grant(downlinkUplink.Host, "UTILS"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}
	msg := &mail.Message{
		OrigAddr: downlinkOrigAddr(),
		ToName:   "Filefix",
		FromName: "Downlink Sysop",
		Subject:  "filesecret",
		Body:     "+UTILS\n",
	}

	handled, err := handleAreafixRequest(msg, robot, messages, netmailStore)
	if err != nil {
		t.Fatalf("handleAreafixRequest: %v", err)
	}
	if !handled {
		t.Fatal("handled = false, want true for a correctly authenticated Filefix request")
	}

	subs, err := fileSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 || subs[0].AreaTag != "UTILS" {
		t.Fatalf("inbound file-area subscriptions = %+v, want exactly one for UTILS", subs)
	}
	reply := pendingReplyTo(t, netmailStore, downlinkUplink.Address)
	if reply.ToName != "Downlink Sysop" {
		t.Fatalf("reply.ToName = %q, want the requester's own name %q", reply.ToName, "Downlink Sysop")
	}
	if !strings.Contains(reply.Body, "+UTILS: added") {
		t.Fatalf("reply body = %q, want it to confirm +UTILS was added", reply.Body)
	}
}

// TestTossInboundRoutesAreafixRequestsToTheRobotInsteadOfStoringThem
// is an end-to-end regression test through tossInbound itself (not
// just handleAreafixRequest directly): a real inbound packet carrying
// a netmail addressed to "Areafix" from a configured downlink must be
// consumed by the robot, not land in anyone's netmail inbox as an
// ordinary (unresolved) message.
func TestTossInboundRoutesAreafixRequestsToTheRobotInsteadOfStoringThem(t *testing.T) {
	netmailStore, messages, files, users, echoSubs, fileSubs := newTestStoresWithRobot(t)
	if _, err := messages.CreateArea("TESTAREA", "Test Area", "", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if err := echoSubs.Grant(downlinkUplink.Host, "TESTAREA"); err != nil {
		t.Fatalf("Grant: %v", err)
	}
	robot := &RobotConfig{
		OurAddresses: []string{"21:3/1"},
		Uplinks:      []config.BinkpUplink{downlinkUplink},
		EchoStore:    echoSubs,
		FileStore:    fileSubs,
		Files:        files,
	}

	ourAddr, err := mail.ParseAddress("21:3/1")
	if err != nil {
		t.Fatalf("ParseAddress: %v", err)
	}
	var buf bytes.Buffer
	pw, err := mail.NewWriter(&buf, mail.PacketHeader{OrigAddr: downlinkOrigAddr(), DestAddr: ourAddr})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	if err := pw.WriteMessage(mail.Message{
		OrigAddr: downlinkOrigAddr(),
		DestAddr: ourAddr,
		Written:  time.Now(),
		ToName:   "Areafix",
		FromName: "Downlink Sysop",
		Subject:  "areasecret",
		Body:     "+TESTAREA\n",
	}); err != nil {
		t.Fatalf("WriteMessage: %v", err)
	}
	if err := pw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	stats, err := tossInbound(&buf, nil, netmailStore, messages, users, robot)
	if err != nil {
		t.Fatalf("tossInbound: %v", err)
	}
	if stats.netmail != 0 {
		t.Fatalf("stats.netmail = %d, want 0 -- the Areafix request must be consumed by the robot, not stored as netmail", stats.netmail)
	}

	subs, err := echoSubs.ListForUplink(downlinkUplink.Host, areafix.Inbound)
	if err != nil {
		t.Fatalf("ListForUplink: %v", err)
	}
	if len(subs) != 1 || subs[0].AreaTag != "TESTAREA" {
		t.Fatalf("inbound subscriptions = %+v, want exactly one for TESTAREA", subs)
	}

	unresolved, err := netmailStore.UnresolvedInbox(10)
	if err != nil {
		t.Fatalf("UnresolvedInbox: %v", err)
	}
	if len(unresolved) != 0 {
		t.Fatalf("UnresolvedInbox = %+v, want empty -- the request must not also land there", unresolved)
	}
}
