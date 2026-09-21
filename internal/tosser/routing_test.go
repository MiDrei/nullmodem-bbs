package tosser

import (
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// mainUplink and crashUplink model the real-world shape this routing
// exists for: one "main" uplink carrying everything by default, and a
// second, poll-disabled uplink dedicated to Crash mail for its own
// FTN network (see routeOutbound's doc comment).
var (
	mainUplink  = config.BinkpUplink{Address: "21:3/100", Host: "n3.z21.example.org:24554"}
	crashUplink = config.BinkpUplink{Address: "954:700/1", Host: "n700.z954.example.org:24554", PollDisabled: true}
	allUplinks  = []config.BinkpUplink{mainUplink, crashUplink}
)

func newRoutingTestStore(t *testing.T) (*netmail.Store, int64) {
	t.Helper()
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	users := user.NewStore(sqlDB)
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	return netmail.NewStore(sqlDB), alice.ID
}

func TestRouteOutboundOrdinaryMailDefaultsToMainUplink(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Bob", "21:3/100", "Hi", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}
	// Even ordinary mail addressed to the crash-only network's zone
	// defaults to whichever uplink is asked, since only Crash mail
	// gets the specific-uplink treatment.
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Carol", "954:700/2", "Hi", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 2 {
		t.Fatalf("routed to main = %+v, want both ordinary messages", routedMain)
	}

	routedCrash, err := RoutedOutbound(store, crashUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(crash): %v", err)
	}
	if len(routedCrash) != 0 {
		t.Fatalf("routed to crash-only uplink = %+v, want none (no Crash mail)", routedCrash)
	}
}

func TestRouteOutboundCrashMailReservedForMatchingUplink(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Dave", "954:700/2", "Urgent", "body", true); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 0 {
		t.Fatalf("routed to main = %+v, want none -- Crash mail for zone 954 must wait for the crash-only uplink", routedMain)
	}

	routedCrash, err := RoutedOutbound(store, crashUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(crash): %v", err)
	}
	if len(routedCrash) != 1 {
		t.Fatalf("routed to crash-only uplink = %+v, want the one Crash message", routedCrash)
	}
}

func TestRouteOutboundCrashMailForMainsOwnZoneStillUsesMain(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Eve", "21:3/200", "Urgent", "body", true); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 1 {
		t.Fatalf("routed to main = %+v, want the Crash message (same zone as main)", routedMain)
	}

	routedCrash, err := RoutedOutbound(store, crashUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(crash): %v", err)
	}
	if len(routedCrash) != 0 {
		t.Fatalf("routed to crash-only uplink = %+v, want none", routedCrash)
	}
}

func TestRouteOutboundCrashMailWithNoMatchingUplinkFallsBackToMain(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Frank", "1:234/56", "Urgent", "body", true); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 1 {
		t.Fatalf("routed to main = %+v, want the Crash message (no uplink claims zone 1)", routedMain)
	}
}

func TestRouteOutboundMixedCrashAndOrdinaryMail(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Ordinary", "954:700/2", "Hi", "body", false); err != nil {
		t.Fatalf("Send: %v", err)
	}
	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Crash", "954:700/2", "Urgent", "body", true); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 1 || routedMain[0].ToName != "Ordinary" {
		t.Fatalf("routed to main = %+v, want just the ordinary message", routedMain)
	}

	routedCrash, err := RoutedOutbound(store, crashUplink, allUplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(crash): %v", err)
	}
	if len(routedCrash) != 1 || routedCrash[0].ToName != "Crash" {
		t.Fatalf("routed to crash-only uplink = %+v, want just the Crash message", routedCrash)
	}
}

// TestRouteOutboundCrashMailMatchesUplinkByAKAAddressesWhenSet locks
// in config.BinkpUplink.AKAAddresses overriding the default "matches
// Address's own zone" rule entirely once set: restricted's own
// Address is zone 954, but its AKAAddresses claims a zone-5 AKA
// instead (a zone neither mainUplink nor crashUplink's own Address
// naturally owns), so zone-5 Crash mail must go to restricted, not
// main -- the exact "which hub owns which of my AKAs" matching the
// web admin's per-uplink checkboxes configure.
func TestRouteOutboundCrashMailMatchesUplinkByAKAAddressesWhenSet(t *testing.T) {
	store, aliceID := newRoutingTestStore(t)
	restricted := crashUplink
	restricted.AKAAddresses = []string{"5:1/1"}
	uplinks := []config.BinkpUplink{mainUplink, restricted}

	if _, err := store.Send(aliceID, "21:3/100.1", 0, "Grace", "5:1/300", "Urgent", "body", true); err != nil {
		t.Fatalf("Send: %v", err)
	}

	routedMain, err := RoutedOutbound(store, mainUplink, uplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(main): %v", err)
	}
	if len(routedMain) != 0 {
		t.Fatalf("routed to main = %+v, want none -- zone 5 now belongs to restricted per its AKAAddresses", routedMain)
	}

	routedRestricted, err := RoutedOutbound(store, restricted, uplinks)
	if err != nil {
		t.Fatalf("RoutedOutbound(restricted): %v", err)
	}
	if len(routedRestricted) != 1 {
		t.Fatalf("routed to restricted = %+v, want the one Crash message", routedRestricted)
	}
}
