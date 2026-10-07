// Package discord bridges the BBS's chat rooms to Discord channels:
// what's said in a room goes to its channel (through a webhook, under
// the speaker's name), what's said in the channel shows up in the room
// as "name@discord". The bot connects outward only (Discord's gateway
// and API), so the BBS needs no open port for it. Which room goes to
// which channel is set per room (chat.RoomInfo.DiscordChannel); the
// bot itself in config.DiscordConfig.
package discord

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/bwmarrin/discordgo"

	"github.com/midrei/nullmodem-bbs/internal/chat"
	"github.com/midrei/nullmodem-bbs/internal/config"
)

// Permissions the bot needs in the bridged channels: view, send, read
// the history, manage webhooks (to speak under the callers' names).
const Permissions = discordgo.PermissionViewChannel | discordgo.PermissionSendMessages |
	discordgo.PermissionReadMessageHistory | discordgo.PermissionManageWebhooks

// webhookName is the webhook the bridge creates in each channel.
const webhookName = "NullModem BBS"

// MaxAge: lines older than this when the bridge gets to them (it was
// offline) stay on the BBS.
const MaxAge = 2 * time.Minute

// MaxLines: a longer Discord message comes over as its first lines.
const MaxLines = 6

// Logger is what the bridge reports through.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Settings is what the bridge runs with.
type Settings struct {
	config.DiscordConfig
	// BBSName speaks for the board (someone entered or left a room).
	BBSName string
}

// Status is how the bridge is doing.
type Status struct {
	Enabled   bool      `json:"enabled"`
	HasToken  bool      `json:"has_token"`
	Connected bool      `json:"connected"`
	Bot       string    `json:"bot,omitempty"`
	BotID     string    `json:"bot_id,omitempty"`
	Error     string    `json:"error,omitempty"`
	Since     time.Time `json:"since,omitempty"` // connected, or failing, since
	Guilds    []string  `json:"guilds,omitempty"`
}

// Channel is a text channel the bot can see.
type Channel struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Guild string `json:"guild"`
}

// Bridge connects the chat rooms to Discord. Run it once; Settings is
// read again every SettingsEvery and on Reload.
type Bridge struct {
	Chat     *chat.Store
	Settings func() Settings
	Logger   Logger
	// Every is how often new lines are forwarded (default 1s).
	Every time.Duration

	mu        sync.Mutex
	sess      *discordgo.Session
	token     string
	status    Status
	channels  map[string]string // Discord channel -> room
	hooks     map[string]*discordgo.Webhook
	noHook    map[string]bool // channel: no webhook permission, the bot speaks
	reload    chan struct{}
	lastID    int64
	lastRetry time.Time
	refused   string // the token Discord refused
}

// SettingsEvery is how often Run looks at the settings again.
var SettingsEvery = 15 * time.Second

// retryEvery: after a failed connect, wait this long.
var retryEvery = 30 * time.Second

// Reload makes Run apply changed settings now -- and try a refused
// token again (the sysop may have fixed the intents meanwhile).
func (b *Bridge) Reload() {
	b.mu.Lock()
	ch := b.reload
	b.refused, b.lastRetry = "", time.Time{}
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
	st := b.status
	if b.sess != nil && b.sess.State != nil {
		st.Guilds = nil
		for _, g := range b.sess.State.Guilds {
			st.Guilds = append(st.Guilds, g.Name)
		}
		sort.Strings(st.Guilds)
	}
	return st
}

// Channels lists the text channels the bot can see, by server.
func (b *Bridge) Channels() []Channel {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := []Channel{}
	if b.sess == nil || b.sess.State == nil {
		return out
	}
	b.sess.State.RLock()
	defer b.sess.State.RUnlock()
	for _, g := range b.sess.State.Guilds {
		for _, c := range g.Channels {
			if c.Type == discordgo.ChannelTypeGuildText || c.Type == discordgo.ChannelTypeGuildNews {
				out = append(out, Channel{ID: c.ID, Name: c.Name, Guild: g.Name})
			}
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Guild != out[j].Guild {
			return out[i].Guild < out[j].Guild
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// Run keeps the bridge going until ctx ends.
func (b *Bridge) Run(ctx context.Context) {
	every := b.Every
	if every == 0 {
		every = time.Second
	}
	b.mu.Lock()
	b.reload = make(chan struct{}, 1)
	b.mu.Unlock()
	if id, err := b.Chat.LastLineID(); err == nil {
		b.lastID = id // nothing from before the start
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	settingsAt := time.Time{}
	var set Settings
	for {
		if time.Since(settingsAt) >= SettingsEvery {
			set, settingsAt = b.Settings(), time.Now()
			b.apply(set)
		}
		b.forward(set)
		select {
		case <-ctx.Done():
			b.disconnect()
			return
		case <-b.reload:
			settingsAt = time.Time{}
		case <-tick.C:
		}
	}
}

// apply connects, reconnects with a new token, or disconnects.
func (b *Bridge) apply(set Settings) {
	b.mu.Lock()
	b.status.Enabled, b.status.HasToken = set.Enabled, set.Token != ""
	same := b.sess != nil && b.token == set.Token
	b.mu.Unlock()
	if !set.Enabled || set.Token == "" {
		b.disconnect()
		return
	}
	if same {
		return
	}
	b.disconnect()
	b.mu.Lock()
	// A refused token isn't tried again until it's replaced; anything
	// else (no network) every retryEvery.
	if b.refused != "" && b.refused == set.Token {
		b.mu.Unlock()
		return
	}
	if time.Since(b.lastRetry) < retryEvery && b.status.Error != "" {
		b.mu.Unlock()
		return
	}
	b.lastRetry = time.Now()
	b.mu.Unlock()
	if err := b.connect(set.Token); err != nil {
		b.mu.Lock()
		if b.status.Error == "" {
			b.status.Since = time.Now()
		}
		b.status.Error = err.Error()
		if errors.Is(err, errRefused) {
			b.refused = set.Token
		}
		b.mu.Unlock()
		b.Logger.Warn("discord: %v", err)
	}
}

func (b *Bridge) connect(token string) error {
	sess, err := discordgo.New("Bot " + token)
	if err != nil {
		return err
	}
	sess.Identify.Intents = discordgo.IntentsGuilds | discordgo.IntentsGuildMessages | discordgo.IntentsMessageContent
	sess.AddHandler(func(s *discordgo.Session, r *discordgo.Ready) {
		b.mu.Lock()
		b.status.Connected, b.status.Error, b.status.Since = true, "", time.Now()
		b.status.Bot, b.status.BotID = r.User.Username, r.User.ID
		b.mu.Unlock()
		b.Logger.Info("discord: connected as %s (%d servers)", r.User.Username, len(r.Guilds))
	})
	sess.AddHandler(func(s *discordgo.Session, _ *discordgo.Disconnect) {
		b.mu.Lock()
		if b.status.Connected {
			b.status.Connected, b.status.Since = false, time.Now()
		}
		b.mu.Unlock()
	})
	sess.AddHandler(func(s *discordgo.Session, _ *discordgo.Resumed) {
		b.mu.Lock()
		b.status.Connected = true
		b.mu.Unlock()
	})
	sess.AddHandler(b.onMessage)
	if err := sess.Open(); err != nil {
		return explain(err)
	}
	b.mu.Lock()
	b.sess, b.token = sess, token
	b.hooks, b.noHook = map[string]*discordgo.Webhook{}, map[string]bool{}
	b.mu.Unlock()
	return nil
}

// errRefused: Discord said no to the token or the intents; trying
// again won't help until the sysop changes something.
var errRefused = errors.New("Discord refused")

// explain turns Discord's answers into something a sysop can act on.
func explain(err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "4004"), strings.Contains(msg, "401"):
		return fmt.Errorf("%w the token -- copy it again from the Developer Portal (Bot -> Reset Token)", errRefused)
	case strings.Contains(msg, "4014"):
		return fmt.Errorf(`%w the bot's intents -- turn on "Message Content Intent" under Bot in the Developer Portal, then save again`, errRefused)
	}
	return fmt.Errorf("connecting to Discord: %w", err)
}

func (b *Bridge) disconnect() {
	b.mu.Lock()
	sess := b.sess
	b.sess, b.token = nil, ""
	b.status.Connected, b.status.Bot, b.status.BotID = false, "", ""
	b.mu.Unlock()
	if sess != nil {
		sess.Close()
	}
}

// forward sends the rooms' new lines to their channels.
func (b *Bridge) forward(set Settings) {
	b.mu.Lock()
	connected := b.sess != nil && b.status.Connected
	b.mu.Unlock()
	lines, err := b.Chat.LinesSince(b.lastID, 100)
	if err != nil || len(lines) == 0 {
		return
	}
	rooms, err := b.Chat.ListRooms()
	if err != nil {
		return
	}
	toChannel := map[string]string{}
	fromChannel := map[string]string{}
	for _, r := range rooms {
		if r.DiscordChannel != "" {
			toChannel[r.Name] = r.DiscordChannel
			fromChannel[r.DiscordChannel] = r.Name
		}
	}
	b.mu.Lock()
	b.channels = fromChannel
	b.mu.Unlock()
	if !connected {
		b.lastID = lines[len(lines)-1].ID // offline: they stay on the BBS
		return
	}
	for _, l := range lines {
		b.lastID = l.ID
		ch := toChannel[l.Room]
		if ch == "" || time.Since(l.At) > MaxAge {
			continue
		}
		name, text, ok := outgoing(l, set)
		if !ok {
			continue
		}
		if err := b.send(ch, name, text); err != nil {
			b.Logger.Warn("discord: sending to #%s: %v", l.Room, err)
		}
	}
}

// outgoing is what a room's line says on Discord, and under which name.
func outgoing(l chat.Line, set Settings) (name, text string, ok bool) {
	return chat.BridgeLine(l, chat.SourceDiscord, set.Quiet, set.BBSName)
}

// send says text in channel under name: through the bridge's webhook,
// or -- without the permission for one -- as the bot, name first.
func (b *Bridge) send(channel, name, text string) error {
	b.mu.Lock()
	sess := b.sess
	hook := b.hooks[channel]
	noHook := b.noHook[channel]
	botID := b.status.BotID
	b.mu.Unlock()
	if sess == nil {
		return errors.New("not connected")
	}
	none := &discordgo.MessageAllowedMentions{Parse: []discordgo.AllowedMentionType{}}
	if hook == nil && !noHook {
		var err error
		if hook, err = findOrCreateHook(sess, channel, botID); err != nil {
			b.Logger.Warn("discord: no webhook in channel %s (%v) -- the bot speaks itself; give it \"Manage Webhooks\" to show the callers' names", channel, err)
			noHook = true
		}
		b.mu.Lock()
		b.hooks[channel], b.noHook[channel] = hook, noHook
		b.mu.Unlock()
	}
	if hook != nil {
		_, err := sess.WebhookExecute(hook.ID, hook.Token, false, &discordgo.WebhookParams{
			Content: text, Username: webhookUsername(name), AllowedMentions: none,
		})
		if err == nil {
			return nil
		}
		// Deleted meanwhile: find or make it again next time.
		b.mu.Lock()
		delete(b.hooks, channel)
		b.mu.Unlock()
		return err
	}
	_, err := sess.ChannelMessageSendComplex(channel, &discordgo.MessageSend{
		Content: "**" + name + "**: " + text, AllowedMentions: none,
	})
	return err
}

func findOrCreateHook(sess *discordgo.Session, channel, botID string) (*discordgo.Webhook, error) {
	hooks, err := sess.ChannelWebhooks(channel)
	if err != nil {
		return nil, err
	}
	for _, h := range hooks {
		if h.Name == webhookName && h.Token != "" && (h.User == nil || h.User.ID == botID) {
			return h, nil
		}
	}
	return sess.WebhookCreate(channel, webhookName, "")
}

// webhookUsername: Discord refuses some names ("discord", "clyde",
// "@", "#", ":", "```") and wants 1..80 characters.
func webhookUsername(name string) string {
	r := strings.NewReplacer("@", "", "#", "", ":", "", "```", "")
	n := strings.TrimSpace(r.Replace(name))
	for _, bad := range []string{"discord", "clyde"} {
		if i := strings.Index(strings.ToLower(n), bad); i >= 0 {
			n = n[:i] + strings.Repeat("_", len(bad)) + n[i+len(bad):]
		}
	}
	if n == "" {
		n = "caller"
	}
	if len([]rune(n)) > 80 {
		n = string([]rune(n)[:80])
	}
	return n
}

// onMessage brings a message from a bridged channel into its room.
func (b *Bridge) onMessage(s *discordgo.Session, m *discordgo.MessageCreate) {
	if m.Author == nil || m.Author.Bot || m.WebhookID != "" {
		return // bots, and our own webhook's messages coming back
	}
	b.mu.Lock()
	room := b.channels[m.ChannelID]
	b.mu.Unlock()
	if room == "" {
		return
	}
	content, err := m.ContentWithMoreMentionsReplaced(s)
	if err != nil {
		content = m.ContentWithMentionsReplaced()
	}
	var files []string
	for _, a := range m.Attachments {
		files = append(files, a.URL)
	}
	name := displayName(m.Author.Username, m.Author.GlobalName, "")
	if m.Member != nil {
		name = displayName(m.Author.Username, m.Author.GlobalName, m.Member.Nick)
	}
	for _, line := range incoming(content, files) {
		if _, err := b.Chat.Post(room, name, chat.SourceDiscord, chat.Say, line); err != nil {
			b.Logger.Warn("discord: %v", err)
			return
		}
	}
}

var customEmoji = regexp.MustCompile(`<a?(:[A-Za-z0-9_~]+:)\d+>`)

// incoming turns a Discord message into room lines: custom emoji as
// :name:, attachments as their links, at most MaxLines lines.
func incoming(content string, attachments []string) []string {
	content = customEmoji.ReplaceAllString(content, "$1")
	var out []string
	for _, l := range strings.Split(content, "\n") {
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
	out = append(out, attachments...)
	if len(out) > MaxLines {
		more := len(out) - (MaxLines - 1)
		out = append(out[:MaxLines-1], fmt.Sprintf("(... %d more lines on Discord)", more))
	}
	return out
}

// displayName: the server nickname, else the display name, else the
// account name -- without spaces at the ends, at most 30 characters.
func displayName(username, global, nick string) string {
	n := nick
	if n == "" {
		n = global
	}
	if n == "" {
		n = username
	}
	n = strings.TrimSpace(n)
	if r := []rune(n); len(r) > 30 {
		n = string(r[:30])
	}
	return n
}

// Down reports a bridge that should be up but hasn't been for a while.
func (b *Bridge) Down(after time.Duration) (bool, string) {
	st := b.Status()
	if !st.Enabled || !st.HasToken || st.Connected || st.Since.IsZero() || time.Since(st.Since) < after {
		return false, ""
	}
	return true, st.Error
}
