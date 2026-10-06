package bbs

import (
	"strings"
	"testing"

	"git.maik.ch/nullmodem/bbs/internal/user"
)

func TestNewFilesSearchAndAllSeen(t *testing.T) {
	s := testServer(t)
	bob, _ := s.Users.Register("bob", "password123", user.SLNewUser)
	alice, _ := s.Users.Register("alice", "password123", user.SLNewUser)
	general, _ := s.Files.AreaByTag("general")
	for _, n := range []string{"FSXNET.Z75", "apod1001.zip"} {
		if _, err := s.Files.UploadFile(general.ID, bob.ID, n, "fsxNet nodelist and pictures", strings.NewReader("data")); err != nil {
			t.Fatal(err)
		}
	}

	conn := newFakeConn("Q\r\n")
	if err := s.newFiles(NewTerminal(conn), alice); err != nil {
		t.Fatal(err)
	}
	out := conn.out.String()
	if !strings.Contains(out, "FSXNET.Z75") || !strings.Contains(out, "apod1001.zip") || !strings.Contains(out, "A = all seen") {
		t.Fatalf("new files list: %q", out)
	}

	conn = newFakeConn("apod\r\nQ\r\n")
	s.searchFiles(NewTerminal(conn), alice)
	if out := conn.out.String(); !strings.Contains(out, "apod1001.zip") || strings.Contains(out, "FSXNET.Z75 ") {
		t.Fatalf("search: %q", out)
	}

	// The summary after login counts them and offers F.
	conn = newFakeConn("\r")
	s.loginSummary(NewTerminal(conn), alice)
	if out := plainText(conn.out.String()); !strings.Contains(out, "2 new files") || !strings.Contains(out, "[F] Files") {
		t.Fatalf("summary: %q", out)
	}

	conn = newFakeConn("A\r\n\r\n")
	s.newFiles(NewTerminal(conn), alice)
	if _, n, _ := s.Files.UnreadFiles(alice.ID, alice.SecurityLevel, 10); n != 0 {
		t.Fatalf("%d still new after A", n)
	}
	conn = newFakeConn("\r\n")
	s.newFiles(NewTerminal(conn), alice)
	if !strings.Contains(conn.out.String(), "No new files") {
		t.Fatal("no note when nothing is new")
	}
}
