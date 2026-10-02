// Package push sends the mobile reader's notifications (web push, as
// an installed web app on iOS 16.4+ and any desktop browser gets
// them): new netmail for a user, and echomail addressed to them by
// name. Each device subscribes itself (Store); the web daemon's
// Notifier looks for new mail every few seconds and sends to the
// recipients' devices through their browser vendor's push service,
// signed with this BBS's VAPID key pair (LoadOrCreateKeys).
package push

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	webpush "github.com/SherClockHolmes/webpush-go"
)

// Keys is the VAPID key pair push services know this BBS by. A device
// subscribes with the public key; a new pair invalidates every
// subscription, so it is created once and kept.
type Keys struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// LoadOrCreateKeys reads the key pair at path, creating it first if
// there is none.
func LoadOrCreateKeys(path string) (Keys, error) {
	var k Keys
	data, err := os.ReadFile(path)
	if err == nil {
		if err := json.Unmarshal(data, &k); err != nil || k.Public == "" || k.Private == "" {
			return Keys{}, fmt.Errorf("push: %s is not a VAPID key pair", path)
		}
		return k, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return Keys{}, fmt.Errorf("push: reading %s: %w", path, err)
	}
	k.Private, k.Public, err = webpush.GenerateVAPIDKeys()
	if err != nil {
		return Keys{}, fmt.Errorf("push: generating VAPID keys: %w", err)
	}
	data, _ = json.Marshal(k)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return Keys{}, fmt.Errorf("push: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return Keys{}, fmt.Errorf("push: writing %s: %w", path, err)
	}
	return k, nil
}

// Subscription is one device's push subscription and what it wants.
type Subscription struct {
	ID       int64  `json:"-"`
	UserID   int64  `json:"-"`
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
	Origin   string `json:"-"`
	Netmail  bool   `json:"netmail"`
	Echomail bool   `json:"echomail"`
}

// Store keeps the subscriptions.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ErrNotFound is a subscription that isn't stored (for this user).
var ErrNotFound = errors.New("push: no such subscription")

// Save adds s, or updates the device's existing subscription -- which
// then belongs to s.UserID (a device that logged in as someone else).
func (st *Store) Save(s Subscription) error {
	_, err := st.db.Exec(`INSERT INTO push_subscriptions (user_id, endpoint, p256dh, auth, origin, netmail, echomail)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(endpoint) DO UPDATE SET user_id = excluded.user_id, p256dh = excluded.p256dh,
			auth = excluded.auth, origin = excluded.origin, netmail = excluded.netmail, echomail = excluded.echomail`,
		s.UserID, s.Endpoint, s.P256dh, s.Auth, s.Origin, s.Netmail, s.Echomail)
	if err != nil {
		return fmt.Errorf("push: saving subscription: %w", err)
	}
	return nil
}

// Get returns the user's subscription with endpoint.
func (st *Store) Get(userID int64, endpoint string) (Subscription, error) {
	rows, err := st.query(`WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	if err != nil {
		return Subscription{}, err
	}
	if len(rows) == 0 {
		return Subscription{}, ErrNotFound
	}
	return rows[0], nil
}

// Delete removes the user's subscription with endpoint.
func (st *Store) Delete(userID int64, endpoint string) error {
	_, err := st.db.Exec(`DELETE FROM push_subscriptions WHERE user_id = ? AND endpoint = ?`, userID, endpoint)
	if err != nil {
		return fmt.Errorf("push: deleting subscription: %w", err)
	}
	return nil
}

// ForUser returns the user's subscriptions.
func (st *Store) ForUser(userID int64) ([]Subscription, error) {
	return st.query(`WHERE user_id = ?`, userID)
}

func (st *Store) deleteID(id int64) error {
	_, err := st.db.Exec(`DELETE FROM push_subscriptions WHERE id = ?`, id)
	return err
}

func (st *Store) query(where string, args ...any) ([]Subscription, error) {
	rows, err := st.db.Query(`SELECT id, user_id, endpoint, p256dh, auth, origin, netmail, echomail
		FROM push_subscriptions `+where+` ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("push: loading subscriptions: %w", err)
	}
	defer rows.Close()
	var out []Subscription
	for rows.Next() {
		var s Subscription
		if err := rows.Scan(&s.ID, &s.UserID, &s.Endpoint, &s.P256dh, &s.Auth, &s.Origin, &s.Netmail, &s.Echomail); err != nil {
			return nil, fmt.Errorf("push: loading subscriptions: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// Notification is what a device shows; URL is the reader page a tap
// opens.
type Notification struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
	// Tag replaces an earlier notification with the same tag.
	Tag string `json:"tag,omitempty"`
}

// Sender delivers notifications to subscriptions.
type Sender struct {
	Keys  Keys
	Store *Store
	// send is webpush.SendNotificationWithContext; tests replace it.
	send func(ctx context.Context, msg []byte, s *webpush.Subscription, o *webpush.Options) (int, error)
}

func NewSender(keys Keys, store *Store) *Sender {
	return &Sender{Keys: keys, Store: store, send: func(ctx context.Context, msg []byte, s *webpush.Subscription, o *webpush.Options) (int, error) {
		res, err := webpush.SendNotificationWithContext(ctx, msg, s, o)
		if err != nil {
			return 0, err
		}
		res.Body.Close()
		return res.StatusCode, nil
	}}
}

// Send delivers n to s. A subscription the push service no longer
// knows (the app was removed, notifications turned off) is deleted.
func (sn *Sender) Send(ctx context.Context, s Subscription, n Notification) error {
	msg, _ := json.Marshal(n)
	subscriber := s.Origin
	if subscriber == "" {
		subscriber = "https://localhost"
	}
	status, err := sn.send(ctx, msg, &webpush.Subscription{
		Endpoint: s.Endpoint,
		Keys:     webpush.Keys{P256dh: s.P256dh, Auth: s.Auth},
	}, &webpush.Options{
		Subscriber:      subscriber,
		VAPIDPublicKey:  sn.Keys.Public,
		VAPIDPrivateKey: sn.Keys.Private,
		TTL:             24 * 60 * 60,
		Urgency:         webpush.UrgencyNormal,
	})
	if err != nil {
		return fmt.Errorf("push: sending: %w", err)
	}
	switch {
	case status == 404 || status == 410:
		return sn.Store.deleteID(s.ID)
	case status >= 300:
		return fmt.Errorf("push: the push service answered %d", status)
	}
	return nil
}

// ToSysops sends n to every device of every sysop.
func (sn *Sender) ToSysops(ctx context.Context, db *sql.DB, n Notification) error {
	ids, err := (&Notifier{DB: db}).sysops(ctx)
	if err != nil {
		return err
	}
	var errs []string
	for _, id := range ids {
		subs, err := sn.Store.ForUser(id)
		if err != nil {
			return err
		}
		for _, s := range subs {
			if err := sn.Send(ctx, s, n); err != nil {
				errs = append(errs, err.Error())
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("push: %s", errs[0])
	}
	return nil
}
