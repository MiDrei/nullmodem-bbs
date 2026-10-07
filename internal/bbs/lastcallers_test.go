package bbs

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/config"
	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/lastcallers"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestLastCallerIsPostedAndListed(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	users, messages := user.NewStore(sqlDB), message.NewStore(sqlDB)
	sysop, _ := users.Register("sysop", "password123", user.SLSysop)
	caller, _ := users.Register("caller", "password123", user.SLNewUser)
	users.SetTimezone(caller.ID, "Europe/Zurich")
	caller, _ = users.ByID(caller.ID)
	area, _ := messages.CreateArea("FSX_DAT", "Data", "", "fsxNet", 0, 0)
	// Another board's record, already there.
	messages.ReceiveEcho(area.ID, "ibbslastcall", "ibbslastcall-data",
		lastcallers.Encode(lastcallers.Record{Alias: "Osiron", BBS: "The X-Bit BBS", Date: "09/30/26", Time: "07:04a"}), "21:4/107 1", time.Now().Add(-time.Hour))

	s := NewServer(Options{BBSName: "Maiks Place BBS", Users: users, Messages: messages,
		LastCallers: config.LastCallersConfig{Enabled: true, Address: "bbs.maik.ch:2323"}})
	s.postLastCaller(caller)

	pending, _ := messages.PendingOutboundEcho("fsxNet")
	if len(pending) != 1 || pending[0].FromName != "ibbslastcall" || pending[0].Subject != "ibbslastcall-data" || pending[0].FromUserID.Int64 != sysop.ID {
		t.Fatalf("pending = %+v, want one record going out as ibbslastcall", pending)
	}
	recs := lastcallers.Parse(pending[0].Body)
	if len(recs) != 1 || recs[0].Alias != "caller" || recs[0].BBS != "Maiks Place BBS" || recs[0].Location != "Zurich" ||
		recs[0].System != "Linux" || recs[0].Address != "bbs.maik.ch:2323" {
		t.Fatalf("record = %+v", recs)
	}

	conn := newFakeConn("\r")
	if err := s.showLastCallers(NewTerminal(conn), caller); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "InterBBS Last Callers") || strings.Index(out, "caller") > strings.Index(out, "Osiron") || !strings.Contains(out, "The X-Bit BBS") {
		t.Fatalf("list should show both, newest first:\n%s", out)
	}

	// A location in the profile wins over the time zone's city.
	users.SetPlace(caller.ID, "Neunkirch, Switzerland")
	caller, _ = users.ByID(caller.ID)
	if got := callerPlace(caller); got != "Neunkirch, Switzerland" {
		t.Fatalf("callerPlace = %q", got)
	}

	off := NewServer(Options{Users: users, Messages: messages})
	off.postLastCaller(caller)
	if again, _ := messages.PendingOutboundEcho("fsxNet"); len(again) != 1 {
		t.Fatalf("posted although not taking part: %d pending", len(again))
	}
}
