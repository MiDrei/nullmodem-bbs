package stats

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/midrei/nullmodem-bbs/internal/db"
	"github.com/midrei/nullmodem-bbs/internal/message"
	"github.com/midrei/nullmodem-bbs/internal/user"
)

func TestReport(t *testing.T) {
	sqlDB, err := db.Open(filepath.Join(t.TempDir(), "t.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	users := user.NewStore(sqlDB)
	msgs := message.NewStore(sqlDB)
	s := NewStore(sqlDB)
	alice, _ := users.Register("alice", "password1", 10)
	bob, _ := users.Register("bob", "password1", 10)

	s.RecordCall(alice.ID, "telnet")
	s.RecordCall(alice.ID, "ssh")
	s.RecordCall(bob.ID, "web")
	s.RecordCall(bob.ID, "web") // the reader logging in again: same call
	s.RecordDoor("LORD", alice.ID, 10*time.Minute)
	s.RecordDoor("LORD", bob.ID, 5*time.Minute)

	area, _ := msgs.CreateArea("FSX_GEN", "fsxNet General", "", "fsxNet", 0, 0)
	msgs.PostMessage(area.ID, alice.ID, "All", "Hi", "x")
	msgs.ReceiveEcho(area.ID, "Remote", "Re: Hi", "y", "21:1/1 1", time.Now())
	msgs.ReceiveEcho(area.ID, "Remote", "Old", "z", "21:1/1 2", time.Now().AddDate(0, 0, -40))

	r, err := s.Report(30, true)
	if err != nil {
		t.Fatal(err)
	}
	if r.Calls != 3 || r.Callers != 2 || len(r.CallsPerDay) != 30 || r.CallsPerDay[29].Count != 3 {
		t.Errorf("calls %d callers %d last day %+v", r.Calls, r.Callers, r.CallsPerDay[len(r.CallsPerDay)-1])
	}
	if len(r.TopCallers) != 2 || r.TopCallers[0].Name != "alice" || r.TopCallers[0].Count != 2 {
		t.Errorf("top callers %+v", r.TopCallers)
	}
	if r.Posts != 1 || len(r.TopPosters) != 1 || r.TopPosters[0].Name != "alice" {
		t.Errorf("posts %d %+v", r.Posts, r.TopPosters)
	}
	if len(r.TopAreas) != 1 || r.TopAreas[0].Count != 2 {
		t.Errorf("areas %+v", r.TopAreas)
	}
	if len(r.Networks) != 1 || r.Networks[0].In != 2 || r.Networks[0].Out != 1 || len(r.Networks[0].Weeks) != weeks {
		t.Errorf("networks %+v", r.Networks)
	}
	if len(r.TopDoors) != 1 || r.TopDoors[0].Count != 2 || r.TopDoors[0].Minutes != 15 {
		t.Errorf("doors %+v", r.TopDoors)
	}
	sum := 0
	for _, n := range r.ByHour {
		sum += n
	}
	if sum != 3 || len(r.Via) != 3 {
		t.Errorf("by hour %v via %+v", r.ByHour, r.Via)
	}
	if pub, _ := s.Report(30, false); pub.Via != nil || pub.ByHour != nil {
		t.Error("the public report has the sysop's part")
	}
}

func TestNilStoreRecordsNothing(t *testing.T) {
	var s *Store
	if s.RecordCall(1, "telnet") != nil || s.RecordDoor("x", 1, time.Second) != nil {
		t.Fatal("nil store")
	}
}
