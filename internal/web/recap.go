package web

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/backup"
	"git.maik.ch/nullmodem/bbs/internal/health"
	"git.maik.ch/nullmodem/bbs/internal/stats"
	"git.maik.ch/nullmodem/bbs/internal/user"
	"git.maik.ch/nullmodem/bbs/internal/version"
)

// The monthly recap: on the 1st (from RecapHour on), a netmail to every
// sysop with the last month's numbers and what needs attention -- or
// one right away from the statistics page.

// RecapHour is when on the 1st the recap goes out (server time).
var RecapHour = 7

// recapFrom signs the recap.
const recapFrom = "NullModem BBS"

// sendRecap writes the recap of the last days days to every sysop.
func (s *Server) sendRecap(title string, days int) (int, error) {
	if s.Stats == nil || s.Netmail == nil {
		return 0, fmt.Errorf("no statistics or netmail here")
	}
	c, err := s.loadBBSConfig()
	if err != nil {
		return 0, err
	}
	x := stats.RecapExtra{BBSName: c.BBS.Name, Version: version.Short()}
	if problems, err := health.Current(s.DB); err == nil {
		for _, p := range problems {
			text := p.Title
			if p.Detail != "" {
				text += " -- " + p.Detail
			}
			x.Problems = append(x.Problems, text)
		}
	}
	if c.Backup.On() {
		x.Backup = "none yet"
		if list, err := backup.List(c.Backup.Directory()); err == nil && len(list) > 0 {
			x.Backup = fmt.Sprintf("%s (%.1f MB)", list[0].Time.Format("2006-01-02 15:04"), float64(list[0].Size)/(1<<20))
		}
	}
	s.DB.QueryRow(`SELECT COUNT(*) FROM users WHERE validated = 0`).Scan(&x.Waiting)
	body, err := s.Stats.Recap(title, days, x)
	if err != nil {
		return 0, err
	}
	sysops, err := s.Users.ListAll()
	if err != nil {
		return 0, err
	}
	sent := 0
	for _, u := range sysops {
		if u.SecurityLevel < user.SLSysop {
			continue
		}
		if _, err := s.Netmail.Receive(recapFrom, s.FTNAddress, u.ID, u.Username, "", title, body, time.Now(), false); err != nil {
			return sent, err
		}
		sent++
	}
	return sent, nil
}

// RunRecaps sends the monthly recap once a month, on the 1st.
func (s *Server) RunRecaps(ctx context.Context) {
	t := time.NewTicker(10 * time.Minute)
	defer t.Stop()
	for {
		s.recapIfDue(time.Now())
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) recapIfDue(now time.Time) {
	if now.Day() != 1 || now.Hour() < RecapHour {
		return
	}
	c, err := s.loadBBSConfig()
	if err != nil || c.BBS.MonthlyRecapOff {
		return
	}
	month := now.AddDate(0, 0, -1) // the month that just ended
	key := month.Format("2006-01")
	var last sql.NullString
	s.DB.QueryRow(`SELECT value FROM meta WHERE key = 'recap_month'`).Scan(&last)
	if last.String == key {
		return
	}
	// Claimed first: one recap, even if this runs twice at once.
	if _, err := s.DB.Exec(`INSERT INTO meta (key, value) VALUES ('recap_month', ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key); err != nil {
		return
	}
	days := time.Date(now.Year(), now.Month(), 0, 0, 0, 0, 0, now.Location()).Day()
	n, err := s.sendRecap("Monthly recap for "+month.Format("January 2006"), days)
	if err != nil {
		s.logWarn("monthly recap: %v", err)
		return
	}
	s.logInfo("monthly recap for %s sent to %d sysop(s)", month.Format("January 2006"), n)
}

// handleSendRecap: POST /api/stats/recap -- a recap of the last 30
// days to every sysop, now.
func (s *Server) handleSendRecap(w http.ResponseWriter, r *http.Request) {
	n, err := s.sendRecap("Recap of the last 30 days", 30)
	if err != nil {
		s.logWarn("recap: %v", err)
		writeError(w, http.StatusInternalServerError, "could not send the recap")
		return
	}
	writeJSON(w, http.StatusOK, map[string]int{"sent": n})
}
