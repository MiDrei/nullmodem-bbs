// Package file implements the BBS's file base: named areas (file
// libraries), each SL-gated for download and upload, holding metadata
// for files a sysop has imported into managed on-disk storage.
//
// This is catalog/metadata only for now -- no in-session transfer
// protocol (Zmodem etc.) is implemented, so files are imported from a
// path the sysop has already placed on the server (e.g. via SCP)
// rather than uploaded through a telnet/SSH session. See ImportFile.
package file

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ErrTagTaken is returned by CreateArea when the tag is already in
// use (case-insensitively).
var ErrTagTaken = errors.New("file: area tag already taken")

// ErrAreaNotFound is returned when an area tag or ID doesn't exist.
var ErrAreaNotFound = errors.New("file: area not found")

// ErrDuplicateFilename is returned by ImportFile when the area
// already has a file with that name.
var ErrDuplicateFilename = errors.New("file: a file with that name already exists in this area")

// ErrFileNotFound is returned when a file ID doesn't exist.
var ErrFileNotFound = errors.New("file: file not found")

// Area is one named file library.
type Area struct {
	ID          int64
	Tag         string
	Name        string
	Description string
	// Network groups related file areas by FTN network (e.g.
	// "fsxNet", "FidoNet") once a BinkP mailer exists to feed them;
	// empty means a local-only area with no network affiliation.
	Network       string
	MinSLDownload int
	MinSLUpload   int
	SortOrder     int
	CreatedAt     time.Time
	// Pending mirrors message.Area.Pending: set when internal/tosser's
	// TIC/file-echo toss auto-creates this area for a tag it hadn't
	// seen before (see EnsureArea), the same way its echomail toss
	// does for message areas.
	Pending bool
}

// CanDownload reports whether an account at securityLevel may browse
// and (once implemented) download from this area.
func (a Area) CanDownload(securityLevel int) bool { return securityLevel >= a.MinSLDownload }

// CanUpload reports whether an account at securityLevel may upload
// (once implemented) to this area.
func (a Area) CanUpload(securityLevel int) bool { return securityLevel >= a.MinSLUpload }

// File is one file's metadata within an Area.
type File struct {
	ID          int64
	AreaID      int64
	Filename    string
	Description string
	SizeBytes   int64
	StoragePath string
	// UploadedBy is unset (Valid false) for a file internal/tosser
	// tossed in from a remote FTN system via TIC/file-echo, which has
	// no local uploader account -- mirrors message.Message.FromUserID
	// exactly (see IsFromRemote and file.Store.Receive).
	UploadedBy     sql.NullInt64
	UploadedByName string // joined from users.username for a local uploader, or the stored origin name for a remote one (see Receive)
	UploadedAt     time.Time
	DownloadCount  int
	// SeenBy is the raw seen_by column: every downlink (net/node pair,
	// space-separated) internal/tosser has already forwarded this file
	// to -- see SeenByNetNodes/MarkSeenBy, and message.Message's
	// identically motivated SEEN-BY tracking for echomail (that one
	// embedded in Body instead, since this system has nowhere else to
	// put it for a file).
	SeenBy string
}

// IsFromRemote reports whether f arrived from a remote FTN system via
// internal/tosser's TIC/file-echo toss rather than being uploaded
// locally -- mirrors message.Message.IsFromRemote exactly.
func (f *File) IsFromRemote() bool { return !f.UploadedBy.Valid }

// Store persists file Areas and their Files in the shared SQLite
// database, and manages the on-disk directory files are copied into.
type Store struct {
	db       *sql.DB
	filesDir string
}

// NewStore wraps an already-opened database handle (see internal/db)
// and the root directory managed files are stored under, organized as
// <filesDir>/<area tag>/<filename>.
func NewStore(db *sql.DB, filesDir string) *Store {
	return &Store{db: db, filesDir: filesDir}
}

// CreateArea adds a new file area. network is the FTN network it
// belongs to (e.g. "fsxNet"), or "" for a local-only area.
func (s *Store) CreateArea(tag, name, description, network string, minSLDownload, minSLUpload int) (*Area, error) {
	res, err := s.db.Exec(
		`INSERT INTO file_areas (tag, name, description, network, min_sl_download, min_sl_upload) VALUES (?, ?, ?, ?, ?, ?)`,
		tag, name, description, network, minSLDownload, minSLUpload,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return nil, ErrTagTaken
		}
		return nil, fmt.Errorf("file: create area %s: %w", tag, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("file: last insert id: %w", err)
	}
	return s.AreaByID(id)
}

// AreaByID loads a single area by primary key.
func (s *Store) AreaByID(id int64) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, network, min_sl_download, min_sl_upload, sort_order, created_at, pending
		 FROM file_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, network, min_sl_download, min_sl_upload, sort_order, created_at, pending
		 FROM file_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLDownload, &a.MinSLUpload, &a.SortOrder, &a.CreatedAt, &a.Pending); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAreaNotFound
		}
		return nil, fmt.Errorf("file: load area: %w", err)
	}
	return &a, nil
}

// CountAreas returns the total number of file areas, for the web
// admin dashboard.
func (s *Store) CountAreas() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM file_areas`).Scan(&n); err != nil {
		return 0, fmt.Errorf("file: count areas: %w", err)
	}
	return n, nil
}

// Networks mirrors message.Store's Networks: every distinct non-empty
// Network value already in use across all file areas, sorted.
func (s *Store) Networks() ([]string, error) {
	rows, err := s.db.Query(`SELECT DISTINCT network FROM file_areas WHERE network != '' ORDER BY network`)
	if err != nil {
		return nil, fmt.Errorf("file: list networks: %w", err)
	}
	defer rows.Close()

	var networks []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			return nil, fmt.Errorf("file: scan network: %w", err)
		}
		networks = append(networks, n)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: list networks: %w", err)
	}
	return networks, nil
}

// ListAreas returns every non-pending area downloadable at
// securityLevel, grouped by network (local/ungrouped areas -- empty
// Network -- sort first) then ordered for menu display within each
// group. A pending area (see EnsureArea) never appears here.
func (s *Store) ListAreas(securityLevel int) ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_download, min_sl_upload, sort_order, created_at, pending
		 FROM file_areas WHERE min_sl_download <= ? AND pending = 0 ORDER BY network, sort_order, name`, securityLevel)
}

// AllAreas returns every non-pending area regardless of SL gating,
// for sysop administration (e.g. picking a destination area to import
// a file into), in the same network-grouped order as ListAreas. See
// PendingAreas for the areas this excludes.
func (s *Store) AllAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_download, min_sl_upload, sort_order, created_at, pending
		 FROM file_areas WHERE pending = 0 ORDER BY network, sort_order, name`)
}

// PendingAreas returns every area awaiting sysop review (see
// EnsureArea), oldest first.
func (s *Store) PendingAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, network, min_sl_download, min_sl_upload, sort_order, created_at, pending
		 FROM file_areas WHERE pending = 1 ORDER BY created_at`)
}

// ApproveArea clears an area's Pending flag. Idempotent.
func (s *Store) ApproveArea(id int64) error {
	if _, err := s.db.Exec(`UPDATE file_areas SET pending = 0 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("file: approve area %d: %w", id, err)
	}
	return nil
}

// EnsureArea returns the area tagged tag, creating it as Pending if it
// doesn't exist yet -- mirrors message.Store's EnsureArea, for when
// TIC/file-echo tossing needs it; nothing calls this yet.
func (s *Store) EnsureArea(tag, name, network string) (area *Area, created bool, err error) {
	if existing, err := s.AreaByTag(tag); err == nil {
		return existing, false, nil
	} else if !errors.Is(err, ErrAreaNotFound) {
		return nil, false, err
	}

	res, err := s.db.Exec(
		`INSERT INTO file_areas (tag, name, description, network, min_sl_download, min_sl_upload, pending) VALUES (?, ?, '', ?, 0, 0, 1)`,
		tag, name, network,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			existing, err := s.AreaByTag(tag)
			return existing, false, err
		}
		return nil, false, fmt.Errorf("file: ensure area %s: %w", tag, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, false, fmt.Errorf("file: last insert id: %w", err)
	}
	area, err = s.AreaByID(id)
	return area, true, err
}

func (s *Store) queryAreas(query string, args ...any) ([]Area, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("file: list areas: %w", err)
	}
	defer rows.Close()

	var areas []Area
	for rows.Next() {
		var a Area
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.Network, &a.MinSLDownload, &a.MinSLUpload, &a.SortOrder, &a.CreatedAt, &a.Pending); err != nil {
			return nil, fmt.Errorf("file: scan area: %w", err)
		}
		areas = append(areas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: list areas: %w", err)
	}
	return areas, nil
}

// AreaWithStats bundles an Area with one caller's per-area file
// counts, for the lightbar area listing: Total files, New (uploaded
// after that caller's last visit, or all of them if they've never
// visited), and Yours (files they uploaded themselves).
type AreaWithStats struct {
	Area  Area
	Total int
	New   int
	Yours int
}

// ListAreaStats is ListAreas plus, for userID, each area's Total/New/
// Yours counts (see AreaWithStats) in a single query. Like ListAreas,
// a pending area never appears here.
func (s *Store) ListAreaStats(securityLevel int, userID int64) ([]AreaWithStats, error) {
	rows, err := s.db.Query(
		`SELECT a.id, a.tag, a.name, a.description, a.network, a.min_sl_download, a.min_sl_upload, a.sort_order, a.created_at, a.pending,
		        (SELECT COUNT(1) FROM files f WHERE f.area_id = a.id) AS total,
		        (SELECT COUNT(1) FROM files f WHERE f.area_id = a.id AND f.uploaded_by = ?) AS yours,
		        (SELECT COUNT(1) FROM files f WHERE f.area_id = a.id
		           AND NOT EXISTS (SELECT 1 FROM file_reads r
		                           WHERE r.user_id = ? AND r.file_id = f.id)) AS new
		 FROM file_areas a
		 WHERE a.min_sl_download <= ? AND a.pending = 0
		 ORDER BY a.network, a.sort_order, a.name`,
		userID, userID, securityLevel,
	)
	if err != nil {
		return nil, fmt.Errorf("file: list area stats: %w", err)
	}
	defer rows.Close()

	var stats []AreaWithStats
	for rows.Next() {
		var st AreaWithStats
		if err := rows.Scan(&st.Area.ID, &st.Area.Tag, &st.Area.Name, &st.Area.Description, &st.Area.Network,
			&st.Area.MinSLDownload, &st.Area.MinSLUpload, &st.Area.SortOrder, &st.Area.CreatedAt, &st.Area.Pending,
			&st.Total, &st.Yours, &st.New); err != nil {
			return nil, fmt.Errorf("file: scan area stats: %w", err)
		}
		stats = append(stats, st)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: list area stats: %w", err)
	}
	return stats, nil
}

// ReadFileIDs returns the set of file IDs within areaID that userID
// has actually opened in the file reader -- see message.Store's
// ReadMessageIDs, which this mirrors.
func (s *Store) ReadFileIDs(userID, areaID int64) (map[int64]bool, error) {
	rows, err := s.db.Query(
		`SELECT r.file_id FROM file_reads r
		 JOIN files f ON f.id = r.file_id
		 WHERE r.user_id = ? AND f.area_id = ?`,
		userID, areaID,
	)
	if err != nil {
		return nil, fmt.Errorf("file: read file ids for area %d, user %d: %w", areaID, userID, err)
	}
	defer rows.Close()

	read := make(map[int64]bool)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("file: scan read file id: %w", err)
		}
		read[id] = true
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: read file ids for area %d, user %d: %w", areaID, userID, err)
	}
	return read, nil
}

// MarkFileRead records that userID has actually opened fileID in the
// file reader, so ListAreaStats/ReadFileIDs stop counting it as new.
// Idempotent: viewing the same file again is a no-op.
func (s *Store) MarkFileRead(userID, fileID int64) error {
	if _, err := s.db.Exec(
		`INSERT OR IGNORE INTO file_reads (user_id, file_id) VALUES (?, ?)`,
		userID, fileID,
	); err != nil {
		return fmt.Errorf("file: mark file %d read for user %d: %w", fileID, userID, err)
	}
	return nil
}

// UpdateArea changes an existing area's editable fields (not its tag,
// which is treated as a stable identifier once created).
func (s *Store) UpdateArea(id int64, name, description, network string, minSLDownload, minSLUpload, sortOrder int) (*Area, error) {
	if _, err := s.db.Exec(
		`UPDATE file_areas SET name = ?, description = ?, network = ?, min_sl_download = ?, min_sl_upload = ?, sort_order = ? WHERE id = ?`,
		name, description, network, minSLDownload, minSLUpload, sortOrder, id,
	); err != nil {
		return nil, fmt.Errorf("file: update area %d: %w", id, err)
	}
	return s.AreaByID(id)
}

// DeleteArea removes an area, its file metadata (via ON DELETE
// CASCADE), and the on-disk files themselves. Disk removal is
// best-effort: a file already missing on disk doesn't abort the
// operation.
func (s *Store) DeleteArea(id int64) error {
	files, err := s.ListFiles(id)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM file_areas WHERE id = ?`, id); err != nil {
		return fmt.Errorf("file: delete area %d: %w", id, err)
	}
	for _, f := range files {
		os.Remove(f.StoragePath)
	}
	return nil
}

// ImportFile copies the file at sourcePath (which must already exist
// on the server, e.g. placed there by the sysop via SCP) into this
// area's managed storage directory and records its metadata. See also
// UploadFile, for a file streamed directly from an HTTP request.
func (s *Store) ImportFile(areaID, uploadedBy int64, sourcePath, description string) (*File, error) {
	area, err := s.AreaByID(areaID)
	if err != nil {
		return nil, err
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("file: stat %s: %w", sourcePath, err)
	}
	if info.IsDir() {
		return nil, fmt.Errorf("file: %s is a directory, not a file", sourcePath)
	}

	in, err := os.Open(sourcePath)
	if err != nil {
		return nil, fmt.Errorf("file: open %s: %w", sourcePath, err)
	}
	defer in.Close()

	return s.storeFile(area, filepath.Base(sourcePath), sql.NullInt64{Int64: uploadedBy, Valid: true}, "", description, in)
}

// UploadFile stores a file uploaded directly through the web admin
// UI (an HTTP multipart request body), sidestepping the need for the
// sysop to place it on the server first (see ImportFile) -- unlike a
// telnet/SSH session, a browser can just send the bytes.
func (s *Store) UploadFile(areaID, uploadedBy int64, filename, description string, src io.Reader) (*File, error) {
	area, err := s.AreaByID(areaID)
	if err != nil {
		return nil, err
	}
	// filepath.Base guards against a client sending a path (e.g.
	// "../../etc/passwd") as the filename; only the base name is ever
	// trusted for the on-disk path.
	return s.storeFile(area, filepath.Base(filename), sql.NullInt64{Int64: uploadedBy, Valid: true}, "", description, src)
}

// Receive stores a file tossed in from a remote FTN system via
// TIC/file-echo (internal/tosser) -- ImportFile/UploadFile's
// counterpart for content with no local uploader account, attributing
// it to originName (the file-echo's own reported origin) instead,
// exactly like message.Store.ReceiveEcho's from_user_id NULL/from_name
// convention (see File.UploadedBy/UploadedByName). Not an error if
// areaID already has a file by this name (see ErrDuplicateFilename):
// created is false and the existing File is returned, treating a hub
// resending a file it never saw our BinkP ack for the same way
// tossEcho treats a resent MSGID rather than failing the whole toss.
func (s *Store) Receive(areaID int64, originName, filename, description string, src io.Reader) (f *File, created bool, err error) {
	area, err := s.AreaByID(areaID)
	if err != nil {
		return nil, false, err
	}
	name := filepath.Base(filename)
	f, err = s.storeFile(area, name, sql.NullInt64{}, originName, description, src)
	if err != nil {
		if errors.Is(err, ErrDuplicateFilename) {
			existing, ferr := s.fileByAreaAndName(areaID, name)
			if ferr != nil {
				return nil, false, ferr
			}
			return existing, false, nil
		}
		return nil, false, err
	}
	return f, true, nil
}

// fileByAreaAndName loads one area's file by its exact stored
// filename -- Receive's way of returning the existing File when
// storeFile reports a duplicate rather than just the bare error.
func (s *Store) fileByAreaAndName(areaID int64, filename string) (*File, error) {
	var id int64
	if err := s.db.QueryRow(`SELECT id FROM files WHERE area_id = ? AND filename = ?`, areaID, filename).Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("file: locating %s in area %d: %w", filename, areaID, err)
	}
	return s.FileByID(id)
}

// storeFile writes src's contents into area's managed storage
// directory under filename and records the resulting metadata. It is
// the shared tail end of ImportFile, UploadFile, and Receive.
// uploadedByName is only ever set (and uploadedBy left unset) for a
// remote-origin file via Receive; a local upload leaves it empty and
// always sets uploadedBy instead (see File.UploadedBy's doc comment).
func (s *Store) storeFile(area *Area, filename string, uploadedBy sql.NullInt64, uploadedByName, description string, src io.Reader) (*File, error) {
	destDir := filepath.Join(s.filesDir, area.Tag)
	destPath := filepath.Join(destDir, filename)

	if _, err := os.Stat(destPath); err == nil {
		return nil, ErrDuplicateFilename
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("file: stat %s: %w", destPath, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, fmt.Errorf("file: mkdir %s: %w", destDir, err)
	}

	out, err := os.Create(destPath)
	if err != nil {
		return nil, fmt.Errorf("file: create %s: %w", destPath, err)
	}
	size, copyErr := io.Copy(out, src)
	closeErr := out.Close()
	if copyErr != nil {
		os.Remove(destPath)
		return nil, fmt.Errorf("file: write %s: %w", destPath, copyErr)
	}
	if closeErr != nil {
		os.Remove(destPath)
		return nil, fmt.Errorf("file: close %s: %w", destPath, closeErr)
	}

	res, err := s.db.Exec(
		`INSERT INTO files (area_id, filename, description, size_bytes, storage_path, uploaded_by, uploaded_by_name) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		area.ID, filename, description, size, destPath, uploadedBy, uploadedByName,
	)
	if err != nil {
		os.Remove(destPath)
		if isUniqueConstraintErr(err) {
			return nil, ErrDuplicateFilename
		}
		return nil, fmt.Errorf("file: record %s: %w", filename, err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("file: last insert id: %w", err)
	}
	return s.FileByID(id)
}

// DeleteFile removes one file's metadata and its on-disk copy. Disk
// removal is best-effort: a file already missing on disk doesn't
// abort the operation.
func (s *Store) DeleteFile(id int64) error {
	f, err := s.FileByID(id)
	if err != nil {
		return err
	}
	if _, err := s.db.Exec(`DELETE FROM files WHERE id = ?`, id); err != nil {
		return fmt.Errorf("file: delete file %d: %w", id, err)
	}
	os.Remove(f.StoragePath)
	return nil
}

// RecordDownload increments a file's download counter -- called once
// internal/zmodem's Send has actually finished handing it to a BBS
// caller.
func (s *Store) RecordDownload(id int64) error {
	if _, err := s.db.Exec(`UPDATE files SET download_count = download_count + 1 WHERE id = ?`, id); err != nil {
		return fmt.Errorf("file: record download of %d: %w", id, err)
	}
	return nil
}

// FileByID loads a single file's metadata, with its uploader's
// current username joined in as UploadedByName for a local upload, or
// its stored origin name for one tossed in remotely (see Receive) --
// LEFT JOIN and COALESCE mirror message.Store.MessageByID exactly,
// necessary since uploaded_by is NULL for the latter.
func (s *Store) FileByID(id int64) (*File, error) {
	row := s.db.QueryRow(
		`SELECT f.id, f.area_id, f.filename, f.description, f.size_bytes, f.storage_path,
		        f.uploaded_by, COALESCE(u.username, f.uploaded_by_name) AS uploaded_by_name, f.uploaded_at, f.download_count, f.seen_by
		 FROM files f LEFT JOIN users u ON u.id = f.uploaded_by WHERE f.id = ?`, id,
	)
	var f File
	if err := row.Scan(&f.ID, &f.AreaID, &f.Filename, &f.Description, &f.SizeBytes, &f.StoragePath,
		&f.UploadedBy, &f.UploadedByName, &f.UploadedAt, &f.DownloadCount, &f.SeenBy); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrFileNotFound
		}
		return nil, fmt.Errorf("file: load %d: %w", id, err)
	}
	return &f, nil
}

// ListFiles returns every file in an area, oldest first, with each
// uploader's current username (or, for one tossed in remotely, its
// stored origin name -- see FileByID) joined in as UploadedByName.
func (s *Store) ListFiles(areaID int64) ([]File, error) {
	rows, err := s.db.Query(
		`SELECT f.id, f.area_id, f.filename, f.description, f.size_bytes, f.storage_path,
		        f.uploaded_by, COALESCE(u.username, f.uploaded_by_name) AS uploaded_by_name, f.uploaded_at, f.download_count, f.seen_by
		 FROM files f LEFT JOIN users u ON u.id = f.uploaded_by
		 WHERE f.area_id = ? ORDER BY f.uploaded_at, f.id`, areaID,
	)
	if err != nil {
		return nil, fmt.Errorf("file: list files for area %d: %w", areaID, err)
	}
	defer rows.Close()

	var files []File
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.ID, &f.AreaID, &f.Filename, &f.Description, &f.SizeBytes, &f.StoragePath,
			&f.UploadedBy, &f.UploadedByName, &f.UploadedAt, &f.DownloadCount, &f.SeenBy); err != nil {
			return nil, fmt.Errorf("file: scan file: %w", err)
		}
		files = append(files, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: list files for area %d: %w", areaID, err)
	}
	return files, nil
}

func isUniqueConstraintErr(err error) bool {
	return err != nil && strings.Contains(strings.ToLower(err.Error()), "unique constraint")
}
