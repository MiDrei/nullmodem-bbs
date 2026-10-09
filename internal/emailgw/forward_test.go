package emailgw

import (
	"context"
	"errors"
	"io"
	"mime/quotedprintable"
	"regexp"
	"strings"
	"testing"
	"time"
)

// lastMail is the newest mail the fake SMTP server took, its body
// decoded.
func (f *fakeSMTP) lastMail(t *testing.T) sentMail {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.got) == 0 {
		t.Fatal("no mail sent")
	}
	m := f.got[len(f.got)-1]
	if i := strings.Index(m.data, "\r\n\r\n"); i >= 0 {
		body, _ := io.ReadAll(quotedprintable.NewReader(strings.NewReader(m.data[i+4:])))
		m.data = m.data[:i+4] + string(body)
	}
	return m
}

func (f *fakeSMTP) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.got)
}

func TestForwardNeedsTheCode(t *testing.T) {
	e := setup(t)
	fw := NewForwards(e.db)
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)

	for addr, want := range map[string]error{"not-an-address": ErrForwardAddress, "me@EXAMPLE.ch": ErrForwardOwnDomain} {
		if err := fw.Request(e.cfg, e.maik, addr, "en", "Test BBS", now); !errors.Is(err, want) {
			t.Errorf("%s: %v, want %v", addr, err, want)
		}
	}
	if err := fw.Request(e.cfg, e.maik, "maik@home.example", "en", "Test BBS", now); err != nil {
		t.Fatal(err)
	}
	m := e.smtp.lastMail(t)
	code := regexp.MustCompile(`\b\d{6}\b`).FindString(m.data)
	if len(m.to) != 1 || m.to[0] != "maik@home.example" || m.from != "swissmaik@example.ch" || code == "" {
		t.Fatalf("code mail: %+v", m)
	}
	if err := fw.Request(e.cfg, e.maik, "maik@home.example", "en", "Test BBS", now.Add(10*time.Second)); !errors.Is(err, ErrCodeTooSoon) {
		t.Errorf("second code at once: %v", err)
	}
	if got, _ := fw.Get(e.maik.ID); got == nil || got.Verified || !got.Pending {
		t.Fatalf("before the code: %+v", got)
	}
	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	if err := fw.Confirm(e.maik.ID, wrong, now); !errors.Is(err, ErrCodeWrong) {
		t.Errorf("wrong code: %v", err)
	}
	if err := fw.Confirm(e.maik.ID, code, now.Add(CodeValid+time.Minute)); !errors.Is(err, ErrCodeExpired) {
		t.Errorf("late code: %v", err)
	}
	if err := fw.Confirm(e.maik.ID, code[:3]+" "+code[3:], now.Add(time.Minute)); err != nil {
		t.Fatalf("right code: %v", err)
	}
	if got, _ := fw.Get(e.maik.ID); got == nil || !got.Verified || got.Pending {
		t.Fatalf("after the code: %+v", got)
	}
	// Guessing: five tries, then the code is spent.
	fw.Request(e.cfg, e.maik, "other@home.example", "en", "Test BBS", now.Add(2*time.Minute))
	for i := 0; i < codeTries; i++ {
		fw.Confirm(e.maik.ID, "999999x", now.Add(2*time.Minute))
	}
	if err := fw.Confirm(e.maik.ID, regexp.MustCompile(`\b\d{6}\b`).FindString(e.smtp.lastMail(t).data), now.Add(2*time.Minute)); !errors.Is(err, ErrCodeExpired) {
		t.Errorf("code after %d wrong guesses: %v", codeTries, err)
	}
}

func TestForwardSendsNewNetmail(t *testing.T) {
	e := setup(t)
	fw := NewForwards(e.db)
	now := time.Now()
	// Netmail from before the forwarding stays where it is.
	if _, err := e.nm.Receive("Old Friend", "21:3/100", e.maik.ID, "SwissMaik", "21:3/194", "Old", "old news", now, false); err != nil {
		t.Fatal(err)
	}
	if err := fw.Request(e.cfg, e.maik, "maik@home.example", "en", "Test BBS", now); err != nil {
		t.Fatal(err)
	}
	if err := fw.Confirm(e.maik.ID, regexp.MustCompile(`\b\d{6}\b`).FindString(e.smtp.lastMail(t).data), now); err != nil {
		t.Fatal(err)
	}
	fw.SetMarkRead(e.maik.ID, true)
	m, err := e.nm.Receive("J\x81rg", "21:3/100", e.maik.ID, "SwissMaik", "21:3/194", "Hallo", "Gr\x81ezi!\r\x01MSGID: 21:3/100 1234\rSecond line\r", now, false)
	if err != nil {
		t.Fatal(err)
	}
	before := e.smtp.count()
	e.gw.ForwardPending(context.Background(), e.cfg, now)
	if e.smtp.count() != before+1 {
		t.Fatalf("forwarded %d mails, want 1", e.smtp.count()-before)
	}
	got := e.smtp.lastMail(t)
	if got.to[0] != "maik@home.example" || !strings.Contains(got.data, "Subject: Hallo") ||
		!strings.Contains(got.data, "Grüezi!\r\nSecond line") || strings.Contains(got.data, "MSGID") ||
		!strings.Contains(got.data, "Jürg (21:3/100)") {
		t.Fatalf("forwarded mail:\n%s", got.data)
	}
	if read, _ := e.nm.MessageByID(m.ID); !read.ReadAt.Valid {
		t.Error("not marked read")
	}
	// Once only; the guest's netmail isn't anyone's to forward.
	e.nm.Receive("Jürg", "21:3/100", e.guest.ID, "Joe Guest", "21:3/194", "Hi Joe", "x", now, false)
	e.gw.ForwardPending(context.Background(), e.cfg, now.Add(time.Minute))
	if e.smtp.count() != before+1 {
		t.Errorf("forwarded again: %d", e.smtp.count()-before)
	}
	// Removed: nothing more goes out.
	fw.Remove(e.maik.ID)
	e.nm.Receive("Jürg", "21:3/100", e.maik.ID, "SwissMaik", "21:3/194", "Later", "x", now, false)
	e.gw.ForwardPending(context.Background(), e.cfg, now.Add(2*time.Minute))
	if e.smtp.count() != before+1 {
		t.Error("forwarded after the forwarding was removed")
	}
}

// A smarthost that's down: the netmail waits and goes later.
func TestForwardRetries(t *testing.T) {
	e := setup(t)
	fw := NewForwards(e.db)
	now := time.Now()
	fw.Request(e.cfg, e.maik, "maik@home.example", "en", "Test BBS", now)
	fw.Confirm(e.maik.ID, regexp.MustCompile(`\b\d{6}\b`).FindString(e.smtp.lastMail(t).data), now)
	e.nm.Receive("Jürg", "21:3/100", e.maik.ID, "SwissMaik", "21:3/194", "Hallo", "x", now, false)
	before := e.smtp.count()
	e.smtp.failMail = 451
	e.gw.ForwardPending(context.Background(), e.cfg, now)
	e.smtp.failMail = 0
	e.gw.ForwardPending(context.Background(), e.cfg, now.Add(time.Second))
	if e.smtp.count() != before {
		t.Fatal("tried again at once")
	}
	e.gw.ForwardPending(context.Background(), e.cfg, now.Add(5*time.Minute))
	if e.smtp.count() != before+1 {
		t.Errorf("not forwarded on the retry: %d", e.smtp.count()-before)
	}
}
