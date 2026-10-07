// Package nodelist reads the FTN networks' nodelists -- the directory
// of every system in a network (FTS-5000, "St. Louis format") -- from
// the file echoes they arrive in (FSXNET.Z75 in FSX_NODE, ...), and
// answers who is behind an address: for checking a netmail's
// destination and for callers browsing the networks.
//
// Sync picks, per file area, the newest file named like a nodelist
// (NAME.nnn, or NAME.Znn zipped) and imports it when it's new; each
// network's list is replaced whole.
package nodelist

import (
	"archive/zip"
	"bufio"
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/midrei/nullmodem-kit/ansi"
)

// Entry is one system in a nodelist.
type Entry struct {
	Network string `json:"network"`
	Zone    int    `json:"zone"`
	Net     int    `json:"net"`
	Node    int    `json:"node"`
	// Keyword is the line's: "Zone", "Region", "Host", "Hub", "Pvt",
	// "Hold", "Down", or "" for an ordinary node.
	Keyword  string `json:"keyword"`
	Name     string `json:"name"`
	Location string `json:"location"`
	Sysop    string `json:"sysop"`
	Flags    string `json:"flags"`
}

// Address is the entry's FTN address, "21:1/101".
func (e Entry) Address() string { return fmt.Sprintf("%d:%d/%d", e.Zone, e.Net, e.Node) }

// Host is the entry's internet host (its INA flag), if any.
func (e Entry) Host() string {
	for _, f := range strings.Split(e.Flags, ",") {
		if h, ok := strings.CutPrefix(f, "INA:"); ok {
			return h
		}
	}
	return ""
}

// Parse reads a nodelist in St. Louis format: comments start with ';',
// fields are comma separated with '_' for spaces; Zone, Region and
// Host lines set the zone and net the following nodes are in.
func Parse(network string, r io.Reader) ([]Entry, error) {
	var out []Entry
	zone, net := 0, 0
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 4096), 64<<10)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\x1a ")
		if line == "" || line[0] == ';' {
			continue
		}
		f := strings.Split(line, ",")
		if len(f) < 5 {
			continue
		}
		num, err := strconv.Atoi(strings.TrimSpace(f[1]))
		if err != nil {
			continue
		}
		e := Entry{Network: network, Keyword: f[0]}
		switch strings.ToLower(f[0]) {
		case "zone":
			zone, net = num, num
			e.Zone, e.Net, e.Node = zone, num, 0
		case "region", "host":
			net = num
			e.Zone, e.Net, e.Node = zone, num, 0
		case "boss":
			continue // a point list, not here
		default:
			if zone == 0 {
				continue
			}
			e.Zone, e.Net, e.Node = zone, net, num
		}
		text := func(i int) string {
			if i >= len(f) {
				return ""
			}
			return ansi.DecodeCP437([]byte(strings.ReplaceAll(f[i], "_", " ")))
		}
		e.Name, e.Location, e.Sysop = text(2), text(3), text(4)
		if len(f) > 7 {
			e.Flags = strings.Join(f[7:], ",")
		}
		out = append(out, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("nodelist: %w", err)
	}
	return out, nil
}

// fileRE is a nodelist's file name: NAME.nnn (day of the year) or
// NAME.Znn zipped (A/J/L/R: other archivers, not read here).
var fileRE = regexp.MustCompile(`(?i)^[a-z0-9_-]+\.(z\d\d|\d\d\d)$`)

// IsNodelistFile reports whether name looks like a nodelist.
func IsNodelistFile(name string) bool { return fileRE.MatchString(name) }

// ReadFile reads a nodelist file at path, unzipping NAME.Znn.
func ReadFile(network, name, path string) ([]Entry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("nodelist: %w", err)
	}
	if bytes.HasPrefix(data, []byte("PK\x03\x04")) {
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, fmt.Errorf("nodelist: %s: %w", name, err)
		}
		for _, f := range zr.File {
			if f.FileInfo().IsDir() || f.UncompressedSize64 > 64<<20 {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("nodelist: %s: %w", name, err)
			}
			defer rc.Close()
			return Parse(network, rc)
		}
		return nil, fmt.Errorf("nodelist: %s: an empty archive", name)
	}
	return Parse(network, bytes.NewReader(data))
}

// Store keeps the imported nodelists.
type Store struct{ db *sql.DB }

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

// Replace sets network's nodelist to entries, imported from filename.
func (s *Store) Replace(network, filename string, entries []Entry) error {
	tx, err := s.db.Begin()
	if err != nil {
		return fmt.Errorf("nodelist: %w", err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM nodelist_entries WHERE network = ?`, network); err != nil {
		return fmt.Errorf("nodelist: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT OR REPLACE INTO nodelist_entries (network, zone, net, node, keyword, name, location, sysop, flags)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("nodelist: %w", err)
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.Exec(network, e.Zone, e.Net, e.Node, e.Keyword, e.Name, e.Location, e.Sysop, e.Flags); err != nil {
			return fmt.Errorf("nodelist: %w", err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO nodelist_imports (network, filename, imported_at, entries) VALUES (?, ?, ?, ?)
		ON CONFLICT(network) DO UPDATE SET filename = excluded.filename, imported_at = excluded.imported_at, entries = excluded.entries`,
		network, filename, time.Now().UnixMilli(), len(entries)); err != nil {
		return fmt.Errorf("nodelist: %w", err)
	}
	return tx.Commit()
}

const entryColumns = `network, zone, net, node, keyword, name, location, sysop, flags`

func scanEntries(rows *sql.Rows) ([]Entry, error) {
	defer rows.Close()
	out := []Entry{}
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.Network, &e.Zone, &e.Net, &e.Node, &e.Keyword, &e.Name, &e.Location, &e.Sysop, &e.Flags); err != nil {
			return nil, fmt.Errorf("nodelist: %w", err)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// Lookup returns the system at zone:net/node, if a nodelist has it.
func (s *Store) Lookup(zone, net, node int) (Entry, bool, error) {
	rows, err := s.db.Query(`SELECT `+entryColumns+` FROM nodelist_entries WHERE zone = ? AND net = ? AND node = ? LIMIT 1`, zone, net, node)
	if err != nil {
		return Entry{}, false, fmt.Errorf("nodelist: %w", err)
	}
	list, err := scanEntries(rows)
	if err != nil || len(list) == 0 {
		return Entry{}, false, err
	}
	return list[0], true, nil
}

var addrRE = regexp.MustCompile(`^(\d+):(\d+)/(\d+)(?:\.\d+)?(?:@\S+)?$`)

// LookupAddress is Lookup for "zone:net/node[.point][@domain]" -- a
// point is looked up as its boss node.
func (s *Store) LookupAddress(addr string) (Entry, bool, error) {
	m := addrRE.FindStringSubmatch(strings.TrimSpace(addr))
	if m == nil {
		return Entry{}, false, nil
	}
	z, _ := strconv.Atoi(m[1])
	n, _ := strconv.Atoi(m[2])
	f, _ := strconv.Atoi(m[3])
	return s.Lookup(z, n, f)
}

// Search finds systems by name, sysop, location or address (a prefix
// like "21:1/" or "21:"), in address order.
func (s *Store) Search(network, q string, limit int) ([]Entry, error) {
	q = strings.TrimSpace(q)
	where := []string{"1 = 1"}
	var args []any
	if network != "" {
		where = append(where, "network = ?")
		args = append(args, network)
	}
	if q != "" {
		where = append(where, `(instr(lower(name), lower(?)) > 0 OR instr(lower(sysop), lower(?)) > 0
			OR instr(lower(location), lower(?)) > 0 OR (zone || ':' || net || '/' || node) LIKE ? || '%')`)
		args = append(args, q, q, q, q)
	}
	args = append(args, limit)
	rows, err := s.db.Query(`SELECT `+entryColumns+` FROM nodelist_entries WHERE `+strings.Join(where, " AND ")+
		` ORDER BY zone, net, node LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("nodelist: %w", err)
	}
	return scanEntries(rows)
}

// Import is a network's last import.
type Import struct {
	Network    string    `json:"network"`
	Filename   string    `json:"filename"`
	ImportedAt time.Time `json:"imported_at"`
	Entries    int       `json:"entries"`
}

// Imports lists the networks with an imported nodelist.
func (s *Store) Imports() ([]Import, error) {
	rows, err := s.db.Query(`SELECT network, filename, imported_at, entries FROM nodelist_imports ORDER BY network`)
	if err != nil {
		return nil, fmt.Errorf("nodelist: %w", err)
	}
	defer rows.Close()
	out := []Import{}
	for rows.Next() {
		var i Import
		var at int64
		if err := rows.Scan(&i.Network, &i.Filename, &at, &i.Entries); err != nil {
			return nil, fmt.Errorf("nodelist: %w", err)
		}
		i.ImportedAt = time.UnixMilli(at)
		out = append(out, i)
	}
	return out, rows.Err()
}

// Candidate is a nodelist file in a file area, as Sync considers it.
type Candidate struct {
	Network  string
	Filename string
	Path     string
}

// Candidates finds, per network, the newest nodelist file in its file
// areas.
func (s *Store) Candidates() ([]Candidate, error) {
	rows, err := s.db.Query(`SELECT a.network, f.filename, f.storage_path FROM files f
		JOIN file_areas a ON a.id = f.area_id
		WHERE a.network <> '' ORDER BY f.uploaded_at DESC, f.id DESC`)
	if err != nil {
		return nil, fmt.Errorf("nodelist: %w", err)
	}
	defer rows.Close()
	seen := map[string]bool{}
	var out []Candidate
	for rows.Next() {
		var c Candidate
		if err := rows.Scan(&c.Network, &c.Filename, &c.Path); err != nil {
			return nil, fmt.Errorf("nodelist: %w", err)
		}
		if seen[c.Network] || !IsNodelistFile(c.Filename) {
			continue
		}
		seen[c.Network] = true
		out = append(out, c)
	}
	return out, rows.Err()
}

// Logger is what Sync reports to.
type Logger interface {
	Info(format string, args ...any)
	Warn(format string, args ...any)
}

// Sync imports every network's newest nodelist file that isn't the one
// imported last (or, with force, every one); it returns how many it
// imported.
func (s *Store) Sync(force bool, log Logger) (int, error) {
	cands, err := s.Candidates()
	if err != nil {
		return 0, err
	}
	done := map[string]string{}
	if imps, err := s.Imports(); err == nil {
		for _, i := range imps {
			done[i.Network] = i.Filename
		}
	}
	n := 0
	for _, c := range cands {
		if !force && strings.EqualFold(done[c.Network], c.Filename) {
			continue
		}
		entries, err := ReadFile(c.Network, c.Filename, c.Path)
		if err != nil {
			if log != nil {
				log.Warn("nodelist %s: %v", c.Filename, err)
			}
			continue
		}
		if len(entries) == 0 {
			continue
		}
		if err := s.Replace(c.Network, c.Filename, entries); err != nil {
			return n, err
		}
		if log != nil {
			log.Info("nodelist: imported %s for %s (%d systems)", c.Filename, c.Network, len(entries))
		}
		n++
	}
	return n, nil
}
