package web

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"html"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"git.maik.ch/nullmodem/kit/ansi"

	"git.maik.ch/nullmodem/bbs/internal/ansiimg"
	"git.maik.ch/nullmodem/bbs/internal/config"
	"git.maik.ch/nullmodem/bbs/internal/message"
)

// Sharing the board: a link to it shows a preview (OpenGraph tags in
// the page, the welcome screen as the picture), and -- if the sysop
// turns them on -- an RSS feed per area a new caller may read.

// baseURL is how the visitor reached us (behind the reverse proxy).
func baseURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return scheme + "://" + host
}

var (
	ogMu    sync.Mutex
	ogPNG   []byte
	ogStamp string
)

// handleOGImage: GET /og-image.png -- the welcome screen as a 1200x630
// picture, made again when it or the board's name changes.
func (s *Server) handleOGImage(w http.ResponseWriter, r *http.Request) {
	c, err := s.loadBBSConfig()
	if err != nil {
		http.Error(w, "no config", http.StatusInternalServerError)
		return
	}
	path := filepath.Join(c.BBS.ScreensDir, welcomeScreenFile)
	info, err := os.Stat(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	stamp := info.ModTime().String() + "|" + c.BBS.Name + "|" + c.BBS.Sysop
	ogMu.Lock()
	defer ogMu.Unlock()
	if ogPNG == nil || stamp != ogStamp {
		raw, err := ansi.LoadScreen(path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		vars := previewVars(c.BBS.Name, c.BBS.Sysop)
		vars["USERNAME"] = "guest"
		grid := ansi.ParseGrid(ansi.Layout(ansi.Render(raw, vars), previewWidth), previewWidth)
		var buf bytes.Buffer
		if err := png.Encode(&buf, ansiimg.Card(grid, 1200, 630)); err != nil {
			http.Error(w, "could not draw it", http.StatusInternalServerError)
			return
		}
		ogPNG, ogStamp = buf.Bytes(), stamp
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	w.Write(ogPNG)
}

// shareMeta are the tags a link preview reads (crawlers don't run the
// app's JavaScript), and the feeds' <link>s for feed readers.
func (s *Server) shareMeta(r *http.Request) string {
	c, err := s.loadBBSConfig()
	if err != nil {
		return ""
	}
	base := baseURL(r)
	host := r.Host
	if h := r.Header.Get("X-Forwarded-Host"); h != "" {
		host = h
	}
	if i := strings.LastIndex(host, ":"); i > 0 && !strings.Contains(host[i:], "]") {
		host = host[:i]
	}
	var parts []string
	if c.BBS.Location != "" {
		parts = append(parts, c.BBS.Location)
	}
	if c.Telnet.Enabled {
		parts = append(parts, "Telnet "+host+":"+portOf(c.Telnet.Addr))
	}
	parts = append(parts, "terminal in the browser, reader app, QWK")
	desc := "A bulletin board system -- " + strings.Join(parts, " · ")
	e := html.EscapeString
	var b strings.Builder
	fmt.Fprintf(&b, `<meta name="description" content="%s">`, e(desc))
	fmt.Fprintf(&b, `<meta property="og:type" content="website"><meta property="og:site_name" content="%s">`, e(c.BBS.Name))
	fmt.Fprintf(&b, `<meta property="og:title" content="%s"><meta property="og:description" content="%s">`, e(c.BBS.Name), e(desc))
	fmt.Fprintf(&b, `<meta property="og:url" content="%s/">`, e(base))
	if _, err := os.Stat(filepath.Join(c.BBS.ScreensDir, welcomeScreenFile)); err == nil {
		fmt.Fprintf(&b, `<meta property="og:image" content="%s/og-image.png"><meta property="og:image:width" content="1200"><meta property="og:image:height" content="630">`, e(base))
		b.WriteString(`<meta name="twitter:card" content="summary_large_image">`)
	}
	if c.BBS.PublicFeeds {
		if areas, err := s.feedAreas(c); err == nil {
			for _, a := range areas {
				fmt.Fprintf(&b, `<link rel="alternate" type="application/rss+xml" title="%s" href="%s/feeds/%s.xml">`,
					e(c.BBS.Name+" -- "+a.Name), e(base), e(strings.ToLower(a.Tag)))
			}
		}
	}
	return b.String()
}

// serveIndex is the app's index.html with shareMeta in its <head>.
func (s *Server) serveIndex(w http.ResponseWriter, r *http.Request, dir string) {
	page, err := os.ReadFile(filepath.Join(dir, "index.html"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if meta := s.shareMeta(r); meta != "" {
		page = bytes.Replace(page, []byte("</head>"), []byte(meta+"</head>"), 1)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Write(page)
}

// feedAreas are the areas with a public feed: shown ones a new caller
// may read.
func (s *Server) feedAreas(c *config.Config) ([]message.Area, error) {
	all, err := s.Messages.ListAreas(c.BBS.NewUserSL)
	if err != nil {
		return nil, err
	}
	var out []message.Area
	for _, a := range all {
		if !a.Hidden && !a.Pending {
			out = append(out, a)
		}
	}
	return out, nil
}

// handlePublicFeeds: GET /api/public/feeds -- the feeds for the front
// page; empty when they're off.
func (s *Server) handlePublicFeeds(w http.ResponseWriter, r *http.Request) {
	type feedDTO struct {
		Tag     string `json:"tag"`
		Name    string `json:"name"`
		Network string `json:"network"`
		URL     string `json:"url"`
	}
	out := []feedDTO{}
	c, err := s.loadBBSConfig()
	if err == nil && c.BBS.PublicFeeds {
		if areas, err := s.feedAreas(c); err == nil {
			for _, a := range areas {
				out = append(out, feedDTO{Tag: a.Tag, Name: a.Name, Network: a.Network, URL: "/feeds/" + strings.ToLower(a.Tag) + ".xml"})
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

var escapeSeq = regexp.MustCompile(`\x1b\[[0-9;?]*[A-Za-z]`)

// stripEscapes drops ANSI colour/cursor codes from a message body.
func stripEscapes(text string) string { return escapeSeq.ReplaceAllString(text, "") }

// FeedItems is how many messages a feed carries.
const FeedItems = 30

type rss struct {
	XMLName xml.Name   `xml:"rss"`
	Version string     `xml:"version,attr"`
	Channel rssChannel `xml:"channel"`
}

type rssChannel struct {
	Title       string    `xml:"title"`
	Link        string    `xml:"link"`
	Description string    `xml:"description"`
	Generator   string    `xml:"generator"`
	LastBuild   string    `xml:"lastBuildDate,omitempty"`
	Items       []rssItem `xml:"item"`
}

type rssItem struct {
	Title       string  `xml:"title"`
	Link        string  `xml:"link"`
	GUID        rssGUID `xml:"guid"`
	Author      string  `xml:"dc:creator"`
	PubDate     string  `xml:"pubDate"`
	Description string  `xml:"description"`
}

type rssGUID struct {
	Value       string `xml:",chardata"`
	IsPermaLink bool   `xml:"isPermaLink,attr"`
}

// handleFeed: GET /feeds/{file} ("fsx_gen.xml") -- the area's newest
// messages as RSS 2.0, when public feeds are on.
func (s *Server) handleFeed(w http.ResponseWriter, r *http.Request) {
	tag, ok := strings.CutSuffix(r.PathValue("file"), ".xml")
	c, err := s.loadBBSConfig()
	if !ok || err != nil || !c.BBS.PublicFeeds {
		http.NotFound(w, r)
		return
	}
	areas, err := s.feedAreas(c)
	if err != nil {
		http.Error(w, "could not load the areas", http.StatusInternalServerError)
		return
	}
	var area *message.Area
	for i := range areas {
		if strings.EqualFold(areas[i].Tag, tag) {
			area = &areas[i]
		}
	}
	if area == nil {
		http.NotFound(w, r)
		return
	}
	_, total, err := s.Messages.ListMessagesPage(area.ID, 1, 0)
	if err != nil {
		http.Error(w, "could not load the messages", http.StatusInternalServerError)
		return
	}
	msgs, _, err := s.Messages.ListMessagesPage(area.ID, FeedItems, max(0, total-FeedItems))
	if err != nil {
		http.Error(w, "could not load the messages", http.StatusInternalServerError)
		return
	}
	base := baseURL(r)
	feed := rss{Version: "2.0", Channel: rssChannel{
		Title:       c.BBS.Name + " -- " + area.Name,
		Link:        base + "/",
		Description: strings.TrimSpace(area.Description + " (" + strings.TrimSpace(area.Network+" echo "+area.Tag) + ")"),
		Generator:   "NullModem BBS",
	}}
	for i := len(msgs) - 1; i >= 0; i-- { // newest first
		m := msgs[i]
		body := ansi.DecodeCP437([]byte(message.StripSeenByAndPathForDisplay(m.Body)))
		guid := m.MsgID
		if guid == "" {
			guid = fmt.Sprintf("%s/messages/%d", base, m.ID)
		}
		feed.Channel.Items = append(feed.Channel.Items, rssItem{
			Title:       ansi.DecodeCP437([]byte(m.Subject)),
			Link:        fmt.Sprintf("%s/messages/%d", base, m.ID),
			GUID:        rssGUID{Value: guid},
			Author:      ansi.DecodeCP437([]byte(m.FromName)),
			PubDate:     m.PostedAt.UTC().Format(time.RFC1123Z),
			Description: "<pre>" + html.EscapeString(stripEscapes(body)) + "</pre>",
		})
	}
	if len(msgs) > 0 {
		feed.Channel.LastBuild = msgs[len(msgs)-1].PostedAt.UTC().Format(time.RFC1123Z)
	}
	out, err := xml.MarshalIndent(feed, "", "  ")
	if err != nil {
		http.Error(w, "could not write the feed", http.StatusInternalServerError)
		return
	}
	// dc:creator needs its namespace on the root.
	out = bytes.Replace(out, []byte(`<rss version="2.0">`), []byte(`<rss version="2.0" xmlns:dc="http://purl.org/dc/elements/1.1/">`), 1)
	w.Header().Set("Content-Type", "application/rss+xml; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write([]byte(xml.Header))
	w.Write(out)
}
