package bbs

import (
	"strings"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/community"
	"git.maik.ch/nullmodem/bbs/internal/user"
)

// newsScreen is the banner over the news (at login and in the list).
const newsScreen = "news.ans"

// newsBlock is one news item as lines to print: its date and title,
// then its text wrapped to the screen, then a blank line.
func newsBlock(term *Terminal, n community.News) []string {
	title, text := n.In(term.Lang)
	lines := []string{"  " + ansi.FG(ansi.Black, true) + term.Time(n.CreatedAt).Format("2006-01-02") + "  " +
		ansi.FG(ansi.Cyan, true) + toCP437(title) + ansi.Reset}
	for _, para := range strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n") {
		for _, l := range ansi.WrapText(toCP437(para), max(20, term.Width()-4)) {
			lines = append(lines, "  "+fgDim(ansi.White)+l+ansi.Reset)
		}
	}
	return append(lines, "")
}

// pageNews prints blocks under the banner, a screen at a time, and
// waits for a key after the last; footer goes above that wait.
func (s *Server) pageNews(term *Terminal, u *user.User, blocks [][]string, footer string) error {
	header := s.featureHeader(term, u, newsScreen, term.T("news.title"))
	room := max(5, term.Height()-strings.Count(header, "\n")-3)
	var page []string
	flush := func(last bool) error {
		var b strings.Builder
		b.WriteString(header)
		for _, l := range page {
			b.WriteString(l + "\r\n")
		}
		if last && footer != "" {
			b.WriteString(ansi.FG(ansi.Black, true) + footer + ansi.Reset)
		}
		if err := term.Print(b.String()); err != nil {
			return err
		}
		page = nil
		return s.pauseForKey(term)
	}
	for _, block := range blocks {
		// A block longer than a screen is cut where the screen ends.
		for len(block) > 0 {
			if len(page) > 0 && len(page)+len(block) > room {
				if err := flush(false); err != nil {
					return err
				}
			}
			n := min(len(block), room)
			page = append(page, block[:n]...)
			block = block[n:]
		}
	}
	return flush(true)
}

// showNewsAtLogin shows the news the caller hasn't seen yet, oldest
// first, and remembers they have; nothing when there's none.
func (s *Server) showNewsAtLogin(term *Terminal, u *user.User) error {
	if s.Community == nil {
		return nil
	}
	news, err := s.Community.UnseenNews(u.ID, time.Now())
	if err != nil {
		s.logWarn("news: %v", err)
		return nil
	}
	if len(news) == 0 {
		return nil
	}
	var blocks [][]string
	var ids []int64
	for _, n := range news {
		blocks = append(blocks, newsBlock(term, n))
		ids = append(ids, n.ID)
	}
	if err := s.pageNews(term, u, blocks, "-- "+term.N("news.new_count", len(news))+" --"); err != nil {
		return err
	}
	return s.Community.MarkNewsSeen(u.ID, ids...)
}

// showNews is the "builtin:news" command: the current news as a
// lightbar (newest first, the unseen ones white), Enter reading one.
func (s *Server) showNews(term *Terminal, u *user.User) error {
	if s.Community == nil {
		return nil
	}
	cur, top := 0, 0
	for {
		news, err := s.Community.ActiveNews(time.Now(), 0)
		if err != nil {
			return err
		}
		seen, err := s.Community.SeenNews(u.ID)
		if err != nil {
			return err
		}
		header := s.featureHeader(term, u, newsScreen, term.T("news.title"))
		rows := max(3, term.Height()-strings.Count(header, "\n")-5)
		cur = max(0, min(cur, len(news)-1))
		if cur < top {
			top = cur
		}
		if cur >= top+rows {
			top = cur - rows + 1
		}
		var b strings.Builder
		b.WriteString(header)
		if len(news) == 0 {
			b.WriteString("  " + fgDim(ansi.White) + term.T("news.none") + ansi.Reset + "\r\n")
			if err := term.Print(b.String()); err != nil {
				return err
			}
			return s.pauseForKey(term)
		}
		b.WriteString("  " + fgDim(ansi.White) + padCP(term.T("col.date"), 12) + term.T("news.col_title") + "\r\n" +
			fgDim(ansi.Blue) + "  " + strings.Repeat("\xc4", 76) + ansi.Reset + "\r\n")
		end := min(top+rows, len(news))
		for i := top; i < end; i++ {
			title, _ := news[i].In(term.Lang)
			date := term.Time(news[i].CreatedAt).Format("2006-01-02")
			if i == cur {
				b.WriteString("\x1b[1;37;44m  " + padCP(date, 12) + padCP(toCP437(title), 64) + ansi.Reset + "\r\n")
				continue
			}
			color := ansi.FG(ansi.White, true)
			if seen[news[i].ID] {
				color = fgDim(ansi.White)
			}
			b.WriteString("  " + ansi.FG(ansi.Black, true) + padCP(date, 12) + color + padCP(toCP437(title), 64) + ansi.Reset + "\r\n")
		}
		for i := end - top; i < rows; i++ {
			b.WriteString("\r\n")
		}
		scroll := ""
		if len(news) > rows {
			scroll = "-- " + term.T("list.range", "FROM", top+1, "TO", end, "TOTAL", len(news)) + " --"
		}
		b.WriteString("\r\n" + ansi.FG(ansi.Black, true) + scroll + ansi.Reset + "\r\n" + keyHints(term.T("news.keys")) + ansi.Reset)
		if err := term.Print(b.String()); err != nil {
			return err
		}
		k, err := term.ReadKey()
		if err != nil {
			return err
		}
		switch {
		case k.Type == KeyUp:
			cur--
		case k.Type == KeyDown:
			cur++
		case k.Type == KeyPgUp:
			cur -= rows
		case k.Type == KeyPgDn:
			cur += rows
		case k.Type == KeyHome:
			cur = 0
		case k.Type == KeyEnd:
			cur = len(news) - 1
		case k.Type == KeyEnter:
			if err := s.pageNews(term, u, [][]string{newsBlock(term, news[cur])}, ""); err != nil {
				return err
			}
			if err := s.Community.MarkNewsSeen(u.ID, news[cur].ID); err != nil {
				return err
			}
		case isKey(k, 'q'), k.Type == KeyEscape:
			return term.Print(ansi.Reset)
		}
	}
}
