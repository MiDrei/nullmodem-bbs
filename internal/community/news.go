package community

import (
	"errors"
	"fmt"
	"strings"
	"time"
)

// News is a sysop's announcement, in English and German: a caller
// reads it in theirs (In), the other language standing in for a
// missing one.
type News struct {
	ID        int64     `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	Author    string    `json:"author"`
	TitleEN   string    `json:"title_en"`
	TextEN    string    `json:"text_en"`
	TitleDE   string    `json:"title_de"`
	TextDE    string    `json:"text_de"`
	// ExpiresAt is when it stops showing; zero: never.
	ExpiresAt time.Time `json:"expires_at"`
}

// In is n's title and text in lang (an internal/i18n code): the German
// ones for "de" and "de-du", the English ones otherwise -- each
// falling back to the other language when empty.
func (n News) In(lang string) (title, text string) {
	pick := func(en, de string) string {
		if strings.HasPrefix(lang, "de") {
			en, de = de, en
		}
		if strings.TrimSpace(en) != "" {
			return en
		}
		return de
	}
	return pick(n.TitleEN, n.TitleDE), pick(n.TextEN, n.TextDE)
}

// Expired reports whether n no longer shows at t.
func (n News) Expired(t time.Time) bool {
	return !n.ExpiresAt.IsZero() && !t.Before(n.ExpiresAt)
}

const newsColumns = `id, created_at, author, title_en, text_en, title_de, text_de, expires_at`

func (s *Store) news(where string, args ...any) ([]News, error) {
	rows, err := s.db.Query(`SELECT `+newsColumns+` FROM news `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	defer rows.Close()
	var out []News
	for rows.Next() {
		var n News
		var created, expires int64
		if err := rows.Scan(&n.ID, &created, &n.Author, &n.TitleEN, &n.TextEN, &n.TitleDE, &n.TextDE, &expires); err != nil {
			return nil, fmt.Errorf("community: %w", err)
		}
		n.CreatedAt = time.UnixMilli(created)
		if expires > 0 {
			n.ExpiresAt = time.UnixMilli(expires)
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

// AllNews is every news item, the expired ones too, newest first.
func (s *Store) AllNews() ([]News, error) {
	return s.news(`ORDER BY created_at DESC, id DESC`)
}

// ActiveNews is the news not expired at now, newest first; limit 0:
// all of it.
func (s *Store) ActiveNews(now time.Time, limit int) ([]News, error) {
	q := `WHERE expires_at = 0 OR expires_at > ? ORDER BY created_at DESC, id DESC`
	if limit > 0 {
		q += fmt.Sprintf(` LIMIT %d`, limit)
	}
	return s.news(q, now.UnixMilli())
}

// UnseenNews is the active news userID hasn't seen yet, oldest first --
// the order to tell it in.
func (s *Store) UnseenNews(userID int64, now time.Time) ([]News, error) {
	return s.news(`WHERE (expires_at = 0 OR expires_at > ?)
		AND id NOT IN (SELECT news_id FROM news_seen WHERE user_id = ?)
		ORDER BY created_at, id`, now.UnixMilli(), userID)
}

// SeenNews is the ids of the news userID has seen.
func (s *Store) SeenNews(userID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(`SELECT news_id FROM news_seen WHERE user_id = ?`, userID)
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	defer rows.Close()
	seen := map[int64]bool{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("community: %w", err)
		}
		seen[id] = true
	}
	return seen, rows.Err()
}

// MarkNewsSeen records that userID has seen ids.
func (s *Store) MarkNewsSeen(userID int64, ids ...int64) error {
	for _, id := range ids {
		if _, err := s.db.Exec(`INSERT OR IGNORE INTO news_seen (user_id, news_id) VALUES (?, ?)`, userID, id); err != nil {
			return fmt.Errorf("community: %w", err)
		}
	}
	return nil
}

// ErrNewsEmpty is a news item without a title or a text in either
// language.
var ErrNewsEmpty = errors.New("community: news needs a title and a text")

// SaveNews adds n (ID 0) or changes it, returning its id. Changing
// keeps its date and who has seen it.
func (s *Store) SaveNews(n News) (int64, error) {
	for _, p := range []*string{&n.TitleEN, &n.TextEN, &n.TitleDE, &n.TextDE, &n.Author} {
		*p = strings.TrimSpace(*p)
	}
	if (n.TitleEN == "" && n.TitleDE == "") || (n.TextEN == "" && n.TextDE == "") {
		return 0, ErrNewsEmpty
	}
	var expires int64
	if !n.ExpiresAt.IsZero() {
		expires = n.ExpiresAt.UnixMilli()
	}
	if n.ID == 0 {
		res, err := s.db.Exec(`INSERT INTO news (created_at, author, title_en, text_en, title_de, text_de, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			time.Now().UnixMilli(), n.Author, n.TitleEN, n.TextEN, n.TitleDE, n.TextDE, expires)
		if err != nil {
			return 0, fmt.Errorf("community: %w", err)
		}
		return res.LastInsertId()
	}
	res, err := s.db.Exec(`UPDATE news SET title_en = ?, text_en = ?, title_de = ?, text_de = ?, expires_at = ? WHERE id = ?`,
		n.TitleEN, n.TextEN, n.TitleDE, n.TextDE, expires, n.ID)
	if err != nil {
		return 0, fmt.Errorf("community: %w", err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return 0, ErrNotFound
	}
	return n.ID, nil
}

// DeleteNews removes a news item.
func (s *Store) DeleteNews(id int64) error {
	res, err := s.db.Exec(`DELETE FROM news WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("community: %w", err)
	}
	if k, _ := res.RowsAffected(); k == 0 {
		return ErrNotFound
	}
	return nil
}
