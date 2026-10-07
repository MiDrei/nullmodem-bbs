// Package community holds what callers share beyond messages: polls
// (the sysop asks, callers vote -- once, though they may change their
// mind) and the BBS list callers keep of other boards.
package community

import (
	"database/sql"
	"errors"
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/textclean"
	"strings"
	"time"
)

// Store keeps polls and the BBS list.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// ErrNotFound is a poll, option or BBS list entry that isn't there.
var ErrNotFound = errors.New("community: not found")

// ErrClosed is a vote in a closed poll.
var ErrClosed = errors.New("community: the poll is closed")

// Option is a poll's answer with its votes.
type Option struct {
	ID    int64  `json:"id"`
	Text  string `json:"text"`
	Votes int    `json:"votes"`
}

// Poll is a question with its options and results.
type Poll struct {
	ID        int64     `json:"id"`
	Question  string    `json:"question"`
	Options   []Option  `json:"options"`
	Total     int       `json:"total"`
	Closed    bool      `json:"closed"`
	CreatedAt time.Time `json:"created_at"`
	// MyVote is the option the asking user chose, 0 if none.
	MyVote int64 `json:"my_vote"`
}

// CreatePoll adds a poll with options (2 to 10, blank ones dropped).
func (s *Store) CreatePoll(question string, options []string) (int64, error) {
	question = strings.TrimSpace(question)
	var opts []string
	for _, o := range options {
		if o = strings.TrimSpace(o); o != "" {
			opts = append(opts, o)
		}
	}
	if question == "" || len(opts) < 2 || len(opts) > 10 {
		return 0, errors.New("community: a poll needs a question and 2 to 10 options")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return 0, fmt.Errorf("community: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.Exec(`INSERT INTO polls (question, created_at) VALUES (?, ?)`, question, time.Now().UnixMilli())
	if err != nil {
		return 0, fmt.Errorf("community: %w", err)
	}
	id, _ := res.LastInsertId()
	for i, o := range opts {
		if _, err := tx.Exec(`INSERT INTO poll_options (poll_id, position, text) VALUES (?, ?, ?)`, id, i, o); err != nil {
			return 0, fmt.Errorf("community: %w", err)
		}
	}
	return id, tx.Commit()
}

// Polls returns the polls, newest first, with results and userID's
// votes; closed ones only with includeClosed.
func (s *Store) Polls(userID int64, includeClosed bool) ([]Poll, error) {
	q := `SELECT id, question, closed, created_at FROM polls`
	if !includeClosed {
		q += ` WHERE closed = 0`
	}
	rows, err := s.db.Query(q + ` ORDER BY id DESC`)
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	var out []Poll
	for rows.Next() {
		var p Poll
		var at int64
		if err := rows.Scan(&p.ID, &p.Question, &p.Closed, &at); err != nil {
			rows.Close()
			return nil, fmt.Errorf("community: %w", err)
		}
		p.CreatedAt = time.UnixMilli(at)
		out = append(out, p)
	}
	rows.Close()
	for i := range out {
		if err := s.fill(&out[i], userID); err != nil {
			return nil, err
		}
	}
	if out == nil {
		out = []Poll{}
	}
	return out, nil
}

// Poll returns one poll, as Polls does.
func (s *Store) Poll(id, userID int64) (Poll, error) {
	var p Poll
	var at int64
	err := s.db.QueryRow(`SELECT id, question, closed, created_at FROM polls WHERE id = ?`, id).Scan(&p.ID, &p.Question, &p.Closed, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Poll{}, ErrNotFound
	}
	if err != nil {
		return Poll{}, fmt.Errorf("community: %w", err)
	}
	p.CreatedAt = time.UnixMilli(at)
	return p, s.fill(&p, userID)
}

func (s *Store) fill(p *Poll, userID int64) error {
	rows, err := s.db.Query(`SELECT o.id, o.text, (SELECT COUNT(*) FROM poll_votes v WHERE v.option_id = o.id)
		FROM poll_options o WHERE o.poll_id = ? ORDER BY o.position`, p.ID)
	if err != nil {
		return fmt.Errorf("community: %w", err)
	}
	defer rows.Close()
	p.Options, p.Total = []Option{}, 0
	for rows.Next() {
		var o Option
		if err := rows.Scan(&o.ID, &o.Text, &o.Votes); err != nil {
			return fmt.Errorf("community: %w", err)
		}
		p.Total += o.Votes
		p.Options = append(p.Options, o)
	}
	if userID > 0 {
		s.db.QueryRow(`SELECT option_id FROM poll_votes WHERE poll_id = ? AND user_id = ?`, p.ID, userID).Scan(&p.MyVote)
	}
	return rows.Err()
}

// Vote records userID's choice in a poll, replacing an earlier one.
func (s *Store) Vote(pollID, userID, optionID int64) error {
	var closed bool
	err := s.db.QueryRow(`SELECT p.closed FROM polls p JOIN poll_options o ON o.poll_id = p.id WHERE p.id = ? AND o.id = ?`,
		pollID, optionID).Scan(&closed)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return fmt.Errorf("community: %w", err)
	}
	if closed {
		return ErrClosed
	}
	_, err = s.db.Exec(`INSERT INTO poll_votes (poll_id, user_id, option_id, at) VALUES (?, ?, ?, ?)
		ON CONFLICT(poll_id, user_id) DO UPDATE SET option_id = excluded.option_id, at = excluded.at`,
		pollID, userID, optionID, time.Now().UnixMilli())
	if err != nil {
		return fmt.Errorf("community: %w", err)
	}
	return nil
}

// ClosePoll stops (or with closed false, reopens) voting.
func (s *Store) ClosePoll(id int64, closed bool) error {
	if _, err := s.db.Exec(`UPDATE polls SET closed = ? WHERE id = ?`, closed, id); err != nil {
		return fmt.Errorf("community: %w", err)
	}
	return nil
}

// DeletePoll removes a poll and its votes.
func (s *Store) DeletePoll(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM polls WHERE id = ?`, id); err != nil {
		return fmt.Errorf("community: %w", err)
	}
	return nil
}

// BBS is an entry in the BBS list.
type BBS struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Address     string    `json:"address"`
	Sysop       string    `json:"sysop"`
	Software    string    `json:"software"`
	Description string    `json:"description"`
	AddedByID   int64     `json:"added_by_id"`
	AddedBy     string    `json:"added_by"`
	UpdatedAt   time.Time `json:"updated_at"`
	// The online check (CheckBBSList): when it last looked, whether the
	// board answered, and when it last did; zero times: not yet.
	CheckedAt time.Time `json:"checked_at"`
	Online    bool      `json:"online"`
	LastUpAt  time.Time `json:"last_up_at"`
}

// Limits of a BBS list entry's fields.
const (
	MaxBBSName  = 40
	MaxBBSField = 60
	MaxBBSDesc  = 200
)

func clip(s string, n int) string {
	s = strings.TrimSpace(textclean.Line(s))
	if r := []rune(s); len(r) > n {
		return string(r[:n])
	}
	return s
}

// SaveBBS adds b (ID 0) or updates it; name and address are required.
func (s *Store) SaveBBS(b BBS) (int64, error) {
	b.Name, b.Address = clip(b.Name, MaxBBSName), clip(b.Address, MaxBBSField)
	b.Sysop, b.Software = clip(b.Sysop, MaxBBSField), clip(b.Software, MaxBBSField)
	b.Description = clip(b.Description, MaxBBSDesc)
	if b.Name == "" || b.Address == "" {
		return 0, errors.New("community: a BBS needs a name and an address")
	}
	now := time.Now().UnixMilli()
	if b.ID == 0 {
		var by any
		if b.AddedByID > 0 {
			by = b.AddedByID
		}
		res, err := s.db.Exec(`INSERT INTO bbs_list (name, address, sysop, software, description, added_by_id, added_by, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?)`, b.Name, b.Address, b.Sysop, b.Software, b.Description, by, b.AddedBy, now)
		if err != nil {
			return 0, fmt.Errorf("community: %w", err)
		}
		return res.LastInsertId()
	}
	// A new address hasn't been checked yet.
	res, err := s.db.Exec(`UPDATE bbs_list SET name = ?, address = ?, sysop = ?, software = ?, description = ?, updated_at = ?,
			checked_at = CASE WHEN address = ? THEN checked_at ELSE 0 END,
			online = CASE WHEN address = ? THEN online ELSE 0 END
		WHERE id = ?`,
		b.Name, b.Address, b.Sysop, b.Software, b.Description, now, b.Address, b.Address, b.ID)
	if err != nil {
		return 0, fmt.Errorf("community: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, ErrNotFound
	}
	return b.ID, nil
}

// BBSList returns the list, by name.
func (s *Store) BBSList() ([]BBS, error) {
	rows, err := s.db.Query(`SELECT id, name, address, sysop, software, description, COALESCE(added_by_id, 0), added_by, updated_at,
			checked_at, online, last_up_at
		FROM bbs_list ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	defer rows.Close()
	out := []BBS{}
	for rows.Next() {
		var b BBS
		var at, checked, up int64
		if err := rows.Scan(&b.ID, &b.Name, &b.Address, &b.Sysop, &b.Software, &b.Description, &b.AddedByID, &b.AddedBy, &at,
			&checked, &b.Online, &up); err != nil {
			return nil, fmt.Errorf("community: %w", err)
		}
		b.UpdatedAt = time.UnixMilli(at)
		if checked > 0 {
			b.CheckedAt = time.UnixMilli(checked)
		}
		if up > 0 {
			b.LastUpAt = time.UnixMilli(up)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// GetBBS returns one entry.
func (s *Store) GetBBS(id int64) (BBS, error) {
	list, err := s.BBSList()
	if err != nil {
		return BBS{}, err
	}
	for _, b := range list {
		if b.ID == id {
			return b, nil
		}
	}
	return BBS{}, ErrNotFound
}

// DeleteBBS removes an entry.
func (s *Store) DeleteBBS(id int64) error {
	if _, err := s.db.Exec(`DELETE FROM bbs_list WHERE id = ?`, id); err != nil {
		return fmt.Errorf("community: %w", err)
	}
	return nil
}
