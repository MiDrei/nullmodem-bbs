// Package matrix bridges the BBS's chat rooms to Matrix rooms, like
// internal/discord does to Discord channels: what's said in a room goes
// to its Matrix room (from the bot, "name: text"), what's said there
// shows up in the room as "name@matrix". It speaks the Matrix
// client-server API directly (login, /sync, send) and connects outward
// only. Encrypted rooms can't be bridged -- the bot can't read them.
package matrix

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode"

	"git.maik.ch/nullmodem/bbs/internal/chat"
	"git.maik.ch/nullmodem/bbs/internal/config"
)

// MaxAge: lines older than this when the bridge gets to them stay here.
const MaxAge = 2 * time.Minute

// MaxLines: a longer Matrix message comes over as its first lines.
const MaxLines = 6

// Logger is what the bridge reports through.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Settings is what the bridge runs with.
type Settings struct {
	config.MatrixConfig
	BBSName string
}

// Status is how the bridge is doing.
type Status struct {
	Enabled   bool      `json:"enabled"`
	HasToken  bool      `json:"has_token"`
	Connected bool      `json:"connected"`
	UserID    string    `json:"user_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	Since     time.Time `json:"since,omitempty"`
	// Warnings: bridged rooms that can't work (encrypted, not joined).
	Warnings []string `json:"warnings,omitempty"`
}

// Room is a room the bot is in.
type Room struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// Client talks to one homeserver as one account.
type Client struct {
	Homeserver string
	Token      string
	HTTP       *http.Client
	txn        atomic.Int64
}

// Error is the homeserver's answer to a failed request.
type Error struct {
	Status  int
	Code    string `json:"errcode"`
	Message string `json:"error"`
	RetryMS int    `json:"retry_after_ms"`
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("matrix: %s (%s)", e.Message, e.Code)
	}
	return fmt.Sprintf("matrix: HTTP %d", e.Status)
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(c.Homeserver, "/")+path, rd)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Token != "" {
		req.Header.Set("Authorization", "Bearer "+c.Token)
	}
	hc := c.HTTP
	if hc == nil {
		hc = &http.Client{Timeout: 60 * time.Second}
	}
	res, err := hc.Do(req)
	if err != nil {
		return fmt.Errorf("matrix: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		e := &Error{Status: res.StatusCode}
		json.Unmarshal(data, e)
		return e
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// Login exchanges a password for an access token (the password isn't
// kept); user is "@name:server" or just "name".
func Login(ctx context.Context, homeserver, user, password string) (userID, token string, err error) {
	c := &Client{Homeserver: homeserver}
	var out struct {
		UserID      string `json:"user_id"`
		AccessToken string `json:"access_token"`
	}
	err = c.do(ctx, http.MethodPost, "/_matrix/client/v3/login", map[string]any{
		"type":                        "m.login.password",
		"identifier":                  map[string]string{"type": "m.id.user", "user": user},
		"password":                    password,
		"initial_device_display_name": "NullModem BBS",
	}, &out)
	return out.UserID, out.AccessToken, err
}

// WhoAmI checks the token.
func (c *Client) WhoAmI(ctx context.Context) (string, error) {
	var out struct {
		UserID string `json:"user_id"`
	}
	err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/account/whoami", nil, &out)
	return out.UserID, err
}

// Join joins a room by ID or alias ("#room:server") and returns its ID.
func (c *Client) Join(ctx context.Context, idOrAlias string) (string, error) {
	var out struct {
		RoomID string `json:"room_id"`
	}
	err := c.do(ctx, http.MethodPost, "/_matrix/client/v3/join/"+url.PathEscape(idOrAlias), map[string]any{}, &out)
	return out.RoomID, err
}

// JoinedRooms lists the rooms the bot is in, with their names.
func (c *Client) JoinedRooms(ctx context.Context) ([]Room, error) {
	var out struct {
		Rooms []string `json:"joined_rooms"`
	}
	if err := c.do(ctx, http.MethodGet, "/_matrix/client/v3/joined_rooms", nil, &out); err != nil {
		return nil, err
	}
	rooms := []Room{}
	for _, id := range out.Rooms {
		r := Room{ID: id, Name: id}
		var name struct {
			Name string `json:"name"`
		}
		if c.do(ctx, http.MethodGet, "/_matrix/client/v3/rooms/"+url.PathEscape(id)+"/state/m.room.name", nil, &name) == nil && name.Name != "" {
			r.Name = name.Name
		} else {
			var alias struct {
				Alias string `json:"alias"`
			}
			if c.do(ctx, http.MethodGet, "/_matrix/client/v3/rooms/"+url.PathEscape(id)+"/state/m.room.canonical_alias", nil, &alias) == nil && alias.Alias != "" {
				r.Name = alias.Alias
			}
		}
		rooms = append(rooms, r)
	}
	sort.Slice(rooms, func(i, j int) bool { return strings.ToLower(rooms[i].Name) < strings.ToLower(rooms[j].Name) })
	return rooms, nil
}

// Encrypted reports whether a room has end-to-end encryption on.
func (c *Client) Encrypted(ctx context.Context, roomID string) bool {
	var out map[string]any
	return c.do(ctx, http.MethodGet, "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state/m.room.encryption", nil, &out) == nil
}

// Send posts a message (msgtype m.text or m.notice) with an HTML form.
func (c *Client) Send(ctx context.Context, roomID, msgtype, body, htmlBody string) error {
	txn := fmt.Sprintf("nm%d.%d", time.Now().UnixNano(), c.txn.Add(1))
	content := map[string]any{"msgtype": msgtype, "body": body}
	if htmlBody != "" {
		content["format"] = "org.matrix.custom.html"
		content["formatted_body"] = htmlBody
	}
	for try := 0; ; try++ {
		err := c.do(ctx, http.MethodPut, "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/send/m.room.message/"+txn, content, nil)
		var me *Error
		if errors.As(err, &me) && me.Code == "M_LIMIT_EXCEEDED" && try < 3 {
			time.Sleep(time.Duration(max(me.RetryMS, 500)) * time.Millisecond)
			continue
		}
		return err
	}
}

// DisplayName is the member's name in a room (else their localpart).
func (c *Client) DisplayName(ctx context.Context, roomID, userID string) string {
	var out struct {
		DisplayName string `json:"displayname"`
	}
	if c.do(ctx, http.MethodGet, "/_matrix/client/v3/rooms/"+url.PathEscape(roomID)+"/state/m.room.member/"+url.PathEscape(userID), nil, &out) == nil && out.DisplayName != "" {
		return out.DisplayName
	}
	return localpart(userID)
}

func localpart(userID string) string {
	n := strings.TrimPrefix(userID, "@")
	if i := strings.IndexByte(n, ':'); i > 0 {
		n = n[:i]
	}
	return n
}

// Event is a room event from /sync.
type Event struct {
	Type    string          `json:"type"`
	Sender  string          `json:"sender"`
	EventID string          `json:"event_id"`
	Content json.RawMessage `json:"content"`
}

type syncResponse struct {
	NextBatch string `json:"next_batch"`
	Rooms     struct {
		Join map[string]struct {
			Timeline struct {
				Events []Event `json:"events"`
			} `json:"timeline"`
		} `json:"join"`
		Invite map[string]json.RawMessage `json:"invite"`
	} `json:"rooms"`
}

var syncFilter = url.QueryEscape(`{"room":{"timeline":{"types":["m.room.message"],"limit":50},"state":{"types":[]},"ephemeral":{"types":[]},"account_data":{"types":[]}},"presence":{"types":[]},"account_data":{"types":[]}}`)

// Sync waits (up to timeout) for what happened since since.
func (c *Client) Sync(ctx context.Context, since string, timeout time.Duration) (*syncResponse, error) {
	path := fmt.Sprintf("/_matrix/client/v3/sync?timeout=%d&filter=%s", timeout.Milliseconds(), syncFilter)
	if since != "" {
		path += "&since=" + url.QueryEscape(since)
	}
	var out syncResponse
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return &out, err
}

// Bridge connects the chat rooms to Matrix rooms. Run it once.
type Bridge struct {
	Chat     *chat.Store
	Settings func() Settings
	Logger   Logger
	// HTTP is for tests; nil is a default client.
	HTTP *http.Client
	// Every is how often new BBS lines are forwarded (default 1s).
	Every time.Duration

	mu       sync.Mutex
	client   *Client
	key      string // homeserver + token in use
	status   Status
	toRoom   map[string]string // BBS room -> Matrix room ID
	fromRoom map[string]string // Matrix room ID -> BBS room
	names    map[string]string // room|user -> display name
	lastID   int64
	reload   chan struct{}
	cancel   context.CancelFunc
}

// SettingsEvery is how often Run looks at the settings again.
var SettingsEvery = 15 * time.Second

// Reload makes Run apply changed settings now.
func (b *Bridge) Reload() {
	b.mu.Lock()
	ch := b.reload
	b.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Status reports the connection.
func (b *Bridge) Status() Status {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.status
}

// Client is the connected client, nil when there's none.
func (b *Bridge) Client() *Client {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.client
}

// Run keeps the bridge going until ctx ends.
func (b *Bridge) Run(ctx context.Context) {
	every := b.Every
	if every == 0 {
		every = time.Second
	}
	b.mu.Lock()
	b.reload = make(chan struct{}, 1)
	b.names = map[string]string{}
	b.mu.Unlock()
	if id, err := b.Chat.LastLineID(); err == nil {
		b.lastID = id
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var settingsAt time.Time
	var set Settings
	for {
		if time.Since(settingsAt) >= SettingsEvery {
			set, settingsAt = b.Settings(), time.Now()
			b.apply(ctx, set)
		}
		b.forward(ctx, set)
		select {
		case <-ctx.Done():
			b.stop()
			return
		case <-b.reload:
			settingsAt = time.Time{}
		case <-tick.C:
		}
	}
}

func (b *Bridge) stop() {
	b.mu.Lock()
	if b.cancel != nil {
		b.cancel()
	}
	b.client, b.key, b.cancel = nil, "", nil
	b.status.Connected = false
	b.mu.Unlock()
}

// apply starts, restarts or stops the sync loop for the settings.
func (b *Bridge) apply(ctx context.Context, set Settings) {
	on := set.Enabled && set.Token != "" && set.Homeserver != ""
	key := set.Homeserver + "\x00" + set.Token
	b.mu.Lock()
	b.status.Enabled, b.status.HasToken = set.Enabled, set.Token != ""
	running := b.client != nil && b.key == key
	b.mu.Unlock()
	if !on {
		b.stop()
		return
	}
	if running {
		return
	}
	b.stop()
	c := &Client{Homeserver: set.Homeserver, Token: set.Token, HTTP: b.HTTP}
	sctx, cancel := context.WithCancel(ctx)
	b.mu.Lock()
	b.client, b.key, b.cancel = c, key, cancel
	b.status.UserID = set.UserID
	b.mu.Unlock()
	go b.syncLoop(sctx, c)
}

func (b *Bridge) fail(err error) {
	b.mu.Lock()
	if b.status.Connected || b.status.Error == "" {
		b.status.Since = time.Now()
	}
	b.status.Connected = false
	b.status.Error = explain(err)
	b.mu.Unlock()
}

func explain(err error) string {
	var me *Error
	if errors.As(err, &me) && (me.Code == "M_UNKNOWN_TOKEN" || me.Status == 401) {
		return "the homeserver refused the bot's token -- log in again"
	}
	return err.Error()
}

// syncLoop follows the rooms until ctx ends; the first sync only
// marks where "now" is (no history comes over).
func (b *Bridge) syncLoop(ctx context.Context, c *Client) {
	me, err := c.WhoAmI(ctx)
	if err != nil {
		b.fail(err)
		b.Logger.Warn("matrix: %s", explain(err))
		return
	}
	since := ""
	backoff := 5 * time.Second
	for ctx.Err() == nil {
		res, err := c.Sync(ctx, since, 30*time.Second)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			b.fail(err)
			var me *Error
			if errors.As(err, &me) && me.Status == 401 {
				b.Logger.Warn("matrix: %s", explain(err))
				return // until the token changes
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff = min(backoff*2, 5*time.Minute)
			continue
		}
		backoff = 5 * time.Second
		first := since == ""
		since = res.NextBatch
		b.mu.Lock()
		was := b.status.Connected
		b.status.Connected, b.status.Error, b.status.UserID = true, "", me
		if !was {
			b.status.Since = time.Now()
		}
		from := b.fromRoom
		b.mu.Unlock()
		if !was {
			b.Logger.Info("matrix: connected as %s", me)
			go b.checkRooms(ctx, c)
		}
		// Invited to a bridged room: go in.
		for roomID := range res.Rooms.Invite {
			if _, ok := from[roomID]; ok {
				c.Join(ctx, roomID)
			}
		}
		if first {
			continue
		}
		for roomID, room := range res.Rooms.Join {
			bbsRoom := from[roomID]
			if bbsRoom == "" {
				continue
			}
			for _, ev := range room.Timeline.Events {
				if ev.Type != "m.room.message" || ev.Sender == me {
					continue
				}
				b.bringIn(ctx, c, roomID, bbsRoom, ev)
			}
		}
	}
}

type messageContent struct {
	MsgType   string `json:"msgtype"`
	Body      string `json:"body"`
	RelatesTo *struct {
		InReplyTo *struct {
			EventID string `json:"event_id"`
		} `json:"m.in_reply_to"`
		RelType string `json:"rel_type"`
	} `json:"m.relates_to"`
}

// bringIn posts a Matrix message into its BBS room.
func (b *Bridge) bringIn(ctx context.Context, c *Client, roomID, bbsRoom string, ev Event) {
	var m messageContent
	if json.Unmarshal(ev.Content, &m) != nil {
		return
	}
	if m.RelatesTo != nil && m.RelatesTo.RelType == "m.replace" {
		return // an edit
	}
	key := roomID + "|" + ev.Sender
	b.mu.Lock()
	name, ok := b.names[key]
	b.mu.Unlock()
	if !ok {
		name = trimName(c.DisplayName(ctx, roomID, ev.Sender))
		b.mu.Lock()
		b.names[key] = name
		b.mu.Unlock()
	}
	for _, line := range incoming(m) {
		if _, err := b.Chat.Post(bbsRoom, name, chat.SourceMatrix, chat.Say, line); err != nil {
			b.Logger.Warn("matrix: %v", err)
			return
		}
	}
}

var replyFallback = regexp.MustCompile(`(?s)^(> [^\n]*\n)+\n`)

// incoming turns a Matrix message into room lines.
func incoming(m messageContent) []string {
	body := m.Body
	if m.RelatesTo != nil && m.RelatesTo.InReplyTo != nil {
		body = replyFallback.ReplaceAllString(body, "")
	}
	switch m.MsgType {
	case "m.text":
	case "m.emote":
		body = "* " + body
	case "m.image", "m.file", "m.video", "m.audio":
		body = "[" + strings.TrimPrefix(m.MsgType, "m.") + "] " + body
	default:
		return nil // notices are bots
	}
	var out []string
	for _, l := range strings.Split(body, "\n") {
		l = strings.TrimSpace(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return -1
			}
			return r
		}, l))
		if l != "" {
			out = append(out, l)
		}
	}
	if len(out) > MaxLines {
		more := len(out) - (MaxLines - 1)
		out = append(out[:MaxLines-1], fmt.Sprintf("(... %d more lines on Matrix)", more))
	}
	return out
}

func trimName(n string) string {
	n = strings.TrimSpace(n)
	if r := []rune(n); len(r) > 30 {
		n = string(r[:30])
	}
	return n
}

// checkRooms warns about bridged rooms the bot can't use.
func (b *Bridge) checkRooms(ctx context.Context, c *Client) {
	b.mu.Lock()
	rooms := map[string]string{}
	for id, name := range b.fromRoom {
		rooms[id] = name
	}
	b.mu.Unlock()
	joined := map[string]bool{}
	if list, err := c.JoinedRooms(ctx); err == nil {
		for _, r := range list {
			joined[r.ID] = true
		}
	}
	var warnings []string
	for id, name := range rooms {
		if !joined[id] {
			if _, err := c.Join(ctx, id); err != nil {
				warnings = append(warnings, fmt.Sprintf("%s: the bot isn't in %s -- invite it", name, id))
				continue
			}
		}
		if c.Encrypted(ctx, id) {
			warnings = append(warnings, fmt.Sprintf("%s: %s is encrypted -- the bot can't read it; bridge an unencrypted room", name, id))
		}
	}
	sort.Strings(warnings)
	b.mu.Lock()
	b.status.Warnings = warnings
	b.mu.Unlock()
}

// forward sends the rooms' new lines to their Matrix rooms.
func (b *Bridge) forward(ctx context.Context, set Settings) {
	lines, err := b.Chat.LinesSince(b.lastID, 100)
	if err != nil {
		return
	}
	rooms, err := b.Chat.ListRooms()
	if err != nil {
		return
	}
	to := map[string]string{}
	from := map[string]string{}
	for _, r := range rooms {
		if r.MatrixRoom != "" {
			to[r.Name] = r.MatrixRoom
			from[r.MatrixRoom] = r.Name
		}
	}
	b.mu.Lock()
	changed := len(from) != len(b.fromRoom)
	for k := range from {
		if _, ok := b.fromRoom[k]; !ok {
			changed = true
		}
	}
	b.toRoom, b.fromRoom = to, from
	c := b.client
	connected := b.status.Connected
	b.mu.Unlock()
	if changed && c != nil && connected {
		go b.checkRooms(ctx, c)
	}
	if len(lines) == 0 {
		return
	}
	b.lastID = lines[len(lines)-1].ID
	if c == nil || !connected {
		return // offline: they stay on the BBS
	}
	for _, l := range lines {
		roomID := to[l.Room]
		if roomID == "" || time.Since(l.At) > MaxAge {
			continue
		}
		name, text, ok := chat.BridgeLine(l, chat.SourceMatrix, set.Quiet, set.BBSName)
		if !ok {
			continue
		}
		var err error
		if l.Kind == chat.Say {
			err = c.Send(ctx, roomID, "m.text", name+": "+text, "<b>"+html.EscapeString(name)+"</b>: "+html.EscapeString(text))
		} else {
			err = c.Send(ctx, roomID, "m.notice", strings.Trim(text, "*"), "<em>"+html.EscapeString(strings.Trim(text, "*"))+"</em>")
		}
		if err != nil {
			b.Logger.Warn("matrix: sending to %s: %v", l.Room, err)
		}
	}
}

// Down reports a bridge that should be up but hasn't been for a while.
func (b *Bridge) Down(after time.Duration) (bool, string) {
	st := b.Status()
	if !st.Enabled || !st.HasToken || st.Connected || st.Since.IsZero() || time.Since(st.Since) < after {
		return false, ""
	}
	return true, st.Error
}
