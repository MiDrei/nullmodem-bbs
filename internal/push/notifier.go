package push

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/i18n"
)

// Logger is what the notifier reports problems to.
type Logger interface {
	Warn(format string, args ...any)
}

// Notifier watches for new mail and notifies the recipients' devices:
// netmail to a user (not their own), and echomail in an area they may
// read addressed to their username or real name. Data areas (hidden)
// are left out. Where it got to is kept in push_state; on its first
// run it starts at the newest mail, so nothing old is announced.
type Notifier struct {
	DB     *sql.DB
	Sender *Sender
	Logger Logger
	// Every defaults to 20 seconds.
	Every time.Duration
	// Lang is the language a user reads their notifications in (an
	// internal/i18n code); nil: English.
	Lang func(userID int64) string
}

// t is key's text in user's language.
func (n *Notifier) t(user int64, key string, args ...any) string {
	lang := i18n.Fallback
	if n.Lang != nil {
		lang = n.Lang(user)
	}
	return i18n.T(lang, key, args...)
}

// Run checks for new mail until ctx ends.
func (n *Notifier) Run(ctx context.Context) {
	every := n.Every
	if every == 0 {
		every = 20 * time.Second
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		if err := n.Check(ctx); err != nil && n.Logger != nil {
			n.Logger.Warn("%v", err)
		}
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
	}
}

type pending struct {
	userID int64
	n      Notification
	kind   string // "netmail" or "echomail"
	// key and args make n.Title in the user's language -- only once the
	// rows are read: looking the language up needs the database's one
	// connection, which an open result set holds.
	key  string
	args []any
}

// Check notifies about the mail that arrived since the last check.
func (n *Notifier) Check(ctx context.Context) error {
	lastNet, err := n.position(ctx, "netmail", `SELECT COALESCE(MAX(id), 0) FROM netmail_messages`)
	if err != nil {
		return err
	}
	lastEcho, err := n.position(ctx, "echomail", `SELECT COALESCE(MAX(id), 0) FROM messages`)
	if err != nil {
		return err
	}

	// Up to the newest rows now: whatever arrives meanwhile is next
	// time's.
	var maxNet, maxEcho int64
	if err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM netmail_messages`).Scan(&maxNet); err != nil {
		return fmt.Errorf("push: looking for new netmail: %w", err)
	}
	if err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM messages`).Scan(&maxEcho); err != nil {
		return fmt.Errorf("push: looking for new echomail: %w", err)
	}

	var out []pending
	rows, err := n.DB.QueryContext(ctx, `SELECT id, to_user_id, from_name, subject FROM netmail_messages
		WHERE id > ? AND id <= ? AND to_user_id IS NOT NULL AND (from_user_id IS NULL OR from_user_id <> to_user_id)
		ORDER BY id`, lastNet, maxNet)
	if err != nil {
		return fmt.Errorf("push: looking for new netmail: %w", err)
	}
	for rows.Next() {
		var id, to int64
		var from, subject string
		if err := rows.Scan(&id, &to, &from, &subject); err != nil {
			rows.Close()
			return fmt.Errorf("push: looking for new netmail: %w", err)
		}
		out = append(out, pending{to, Notification{
			Body: subject, URL: fmt.Sprintf("/reader/netmail/%d", id), Tag: fmt.Sprintf("netmail-%d", id),
		}, "netmail", "push.netmail", []any{"FROM", from}})
	}
	rows.Close()

	rows, err = n.DB.QueryContext(ctx, `SELECT m.id, u.id, m.from_name, m.subject, a.tag
		FROM messages m
		JOIN message_areas a ON a.id = m.area_id
		JOIN users u ON (m.to_name = u.username COLLATE NOCASE
			OR (u.real_name <> '' AND m.to_name = u.real_name COLLATE NOCASE))
		WHERE m.id > ? AND m.id <= ? AND a.hidden = 0 AND a.pending = 0
			AND a.min_sl_read <= u.security_level
			AND (m.from_user_id IS NULL OR m.from_user_id <> u.id)
			AND EXISTS (SELECT 1 FROM push_subscriptions p WHERE p.user_id = u.id AND p.echomail = 1)
		ORDER BY m.id`, lastEcho, maxEcho)
	if err != nil {
		return fmt.Errorf("push: looking for new echomail: %w", err)
	}
	for rows.Next() {
		var id, to int64
		var from, subject, tag string
		if err := rows.Scan(&id, &to, &from, &subject, &tag); err != nil {
			rows.Close()
			return fmt.Errorf("push: looking for new echomail: %w", err)
		}
		out = append(out, pending{to, Notification{
			Body: subject, URL: fmt.Sprintf("/reader/m/%d", id), Tag: fmt.Sprintf("echo-%d", id),
		}, "echomail", "push.echomail", []any{"FROM", from, "AREA", tag}})
	}
	rows.Close()

	// New accounts waiting for approval: to the sysops' devices.
	lastUser, err := n.position(ctx, "users", `SELECT COALESCE(MAX(id), 0) FROM users`)
	if err != nil {
		return err
	}
	var maxUser int64
	if err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM users`).Scan(&maxUser); err != nil {
		return fmt.Errorf("push: looking for new users: %w", err)
	}
	if maxUser > lastUser {
		sysops, err := n.sysops(ctx)
		if err != nil {
			return err
		}
		rows, err := n.DB.QueryContext(ctx, `SELECT username, real_name FROM users WHERE id > ? AND id <= ? AND validated = 0 ORDER BY id`, lastUser, maxUser)
		if err != nil {
			return fmt.Errorf("push: looking for new users: %w", err)
		}
		for rows.Next() {
			var name, real string
			rows.Scan(&name, &real)
			body := name
			if real != "" {
				body += " (" + real + ")"
			}
			for _, id := range sysops {
				out = append(out, pending{id, Notification{
					Body: body, URL: "/admin/users", Tag: "user-" + name,
				}, "users", "push.new_user", nil})
			}
		}
		rows.Close()
	}
	if err := n.setPosition(ctx, "users", maxUser); err != nil {
		return err
	}

	// Callers paging the sysop: to the sysops' devices.
	lastPage, err := n.position(ctx, "pages", `SELECT COALESCE(MAX(id), 0) FROM chat_lines`)
	if err != nil {
		return err
	}
	var maxPage int64
	if err := n.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(id), 0) FROM chat_lines`).Scan(&maxPage); err != nil {
		return fmt.Errorf("push: looking for pages: %w", err)
	}
	if maxPage > lastPage {
		rows, err := n.DB.QueryContext(ctx, `SELECT room, username, text FROM chat_lines WHERE id > ? AND id <= ? AND kind = 'page' ORDER BY id`, lastPage, maxPage)
		if err != nil {
			return fmt.Errorf("push: looking for pages: %w", err)
		}
		type page struct{ room, who, why string }
		var pages []page
		for rows.Next() {
			var p page
			rows.Scan(&p.room, &p.who, &p.why)
			pages = append(pages, p)
		}
		rows.Close()
		if len(pages) > 0 {
			sysops, err := n.sysops(ctx)
			if err != nil {
				return err
			}
			for _, p := range pages {
				for _, id := range sysops {
					out = append(out, pending{id, Notification{
						Title: n.t(id, "push.paging", "USERNAME", p.who), Body: p.why,
						URL: "/admin/chat?room=" + p.room, Tag: "page-" + p.who,
					}, "page", "", nil})
				}
			}
		}
	}
	if err := n.setPosition(ctx, "pages", maxPage); err != nil {
		return err
	}

	// Moved on first: a push service that's down must not have the
	// same mail announced again and again.
	if err := n.setPosition(ctx, "netmail", maxNet); err != nil {
		return err
	}
	if err := n.setPosition(ctx, "echomail", maxEcho); err != nil {
		return err
	}

	var errs []string
	subs := map[int64][]Subscription{}
	for _, p := range out {
		list, ok := subs[p.userID]
		if !ok {
			if list, err = n.Sender.Store.ForUser(p.userID); err != nil {
				return err
			}
			subs[p.userID] = list
		}
		if p.key != "" && len(list) > 0 {
			p.n.Title = n.t(p.userID, p.key, p.args...)
		}
		for _, s := range list {
			if (p.kind == "netmail" && !s.Netmail) || (p.kind == "echomail" && !s.Echomail) {
				continue
			}
			sctx, cancel := context.WithTimeout(ctx, 15*time.Second)
			if err := n.Sender.Send(sctx, s, p.n); err != nil {
				errs = append(errs, err.Error())
			}
			cancel()
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("push: %d notification(s) not delivered: %s", len(errs), strings.Join(errs, "; "))
	}
	return nil
}

// position is where the notifier got to for key, starting at the
// newest row (by initial) the first time.
func (n *Notifier) position(ctx context.Context, key, initial string) (int64, error) {
	var v int64
	err := n.DB.QueryRowContext(ctx, `SELECT value FROM push_state WHERE key = ?`, key).Scan(&v)
	if err == sql.ErrNoRows {
		if err := n.DB.QueryRowContext(ctx, initial).Scan(&v); err != nil {
			return 0, fmt.Errorf("push: %w", err)
		}
		return v, n.setPosition(ctx, key, v)
	}
	if err != nil {
		return 0, fmt.Errorf("push: %w", err)
	}
	return v, nil
}

func (n *Notifier) setPosition(ctx context.Context, key string, v int64) error {
	_, err := n.DB.ExecContext(ctx, `INSERT INTO push_state (key, value) VALUES (?, ?)
		ON CONFLICT(key) DO UPDATE SET value = excluded.value`, key, v)
	if err != nil {
		return fmt.Errorf("push: %w", err)
	}
	return nil
}

// sysops are the users at sysop level.
func (n *Notifier) sysops(ctx context.Context) ([]int64, error) {
	rows, err := n.DB.QueryContext(ctx, `SELECT id FROM users WHERE security_level >= 255`)
	if err != nil {
		return nil, fmt.Errorf("push: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		rows.Scan(&id)
		out = append(out, id)
	}
	return out, rows.Err()
}
