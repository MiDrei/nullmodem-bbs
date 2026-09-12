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

// Area is one named file library.
type Area struct {
	ID            int64
	Tag           string
	Name          string
	Description   string
	MinSLDownload int
	MinSLUpload   int
	SortOrder     int
	CreatedAt     time.Time
}

// CanDownload reports whether an account at securityLevel may browse
// and (once implemented) download from this area.
func (a Area) CanDownload(securityLevel int) bool { return securityLevel >= a.MinSLDownload }

// CanUpload reports whether an account at securityLevel may upload
// (once implemented) to this area.
func (a Area) CanUpload(securityLevel int) bool { return securityLevel >= a.MinSLUpload }

// File is one file's metadata within an Area.
type File struct {
	ID             int64
	AreaID         int64
	Filename       string
	Description    string
	SizeBytes      int64
	StoragePath    string
	UploadedBy     int64
	UploadedByName string // joined from users.username for display
	UploadedAt     time.Time
	DownloadCount  int
}

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

// CreateArea adds a new file area.
func (s *Store) CreateArea(tag, name, description string, minSLDownload, minSLUpload int) (*Area, error) {
	res, err := s.db.Exec(
		`INSERT INTO file_areas (tag, name, description, min_sl_download, min_sl_upload) VALUES (?, ?, ?, ?, ?)`,
		tag, name, description, minSLDownload, minSLUpload,
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
		`SELECT id, tag, name, description, min_sl_download, min_sl_upload, sort_order, created_at
		 FROM file_areas WHERE id = ?`, id,
	))
}

// AreaByTag loads a single area by its short tag (case-insensitive).
func (s *Store) AreaByTag(tag string) (*Area, error) {
	return s.scanArea(s.db.QueryRow(
		`SELECT id, tag, name, description, min_sl_download, min_sl_upload, sort_order, created_at
		 FROM file_areas WHERE tag = ?`, tag,
	))
}

func (s *Store) scanArea(row *sql.Row) (*Area, error) {
	var a Area
	if err := row.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.MinSLDownload, &a.MinSLUpload, &a.SortOrder, &a.CreatedAt); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrAreaNotFound
		}
		return nil, fmt.Errorf("file: load area: %w", err)
	}
	return &a, nil
}

// ListAreas returns every area downloadable at securityLevel, ordered
// for menu display.
// CountAreas returns the total number of file areas, for the web
// admin dashboard.
func (s *Store) CountAreas() (int, error) {
	var n int
	if err := s.db.QueryRow(`SELECT COUNT(1) FROM file_areas`).Scan(&n); err != nil {
		return 0, fmt.Errorf("file: count areas: %w", err)
	}
	return n, nil
}

func (s *Store) ListAreas(securityLevel int) ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, min_sl_download, min_sl_upload, sort_order, created_at
		 FROM file_areas WHERE min_sl_download <= ? ORDER BY sort_order, name`, securityLevel)
}

// AllAreas returns every area regardless of SL gating, for sysop
// administration (e.g. picking a destination area to import a file
// into).
func (s *Store) AllAreas() ([]Area, error) {
	return s.queryAreas(`SELECT id, tag, name, description, min_sl_download, min_sl_upload, sort_order, created_at
		 FROM file_areas ORDER BY sort_order, name`)
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
		if err := rows.Scan(&a.ID, &a.Tag, &a.Name, &a.Description, &a.MinSLDownload, &a.MinSLUpload, &a.SortOrder, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("file: scan area: %w", err)
		}
		areas = append(areas, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("file: list areas: %w", err)
	}
	return areas, nil
}

// ImportFile copies the file at sourcePath (which must already exist
// on the server, e.g. placed there by the sysop via SCP) into this
// area's managed storage directory and records its metadata.
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

	filename := filepath.Base(sourcePath)
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
	size, err := copyFile(sourcePath, destPath)
	if err != nil {
		return nil, err
	}

	res, err := s.db.Exec(
		`INSERT INTO files (area_id, filename, description, size_bytes, storage_path, uploaded_by) VALUES (?, ?, ?, ?, ?, ?)`,
		areaID, filename, description, size, destPath, uploadedBy,
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

// copyFile copies src to dst and returns the number of bytes copied.
func copyFile(src, dst string) (int64, error) {
	in, err := os.Open(src)
	if err != nil {
		return 0, fmt.Errorf("file: open %s: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return 0, fmt.Errorf("file: create %s: %w", dst, err)
	}
	defer out.Close()

	n, err := io.Copy(out, in)
	if err != nil {
		return 0, fmt.Errorf("file: copy %s to %s: %w", src, dst, err)
	}
	return n, nil
}

// FileByID loads a single file's metadata, with its uploader's
// current username joined in as UploadedByName.
func (s *Store) FileByID(id int64) (*File, error) {
	row := s.db.QueryRow(
		`SELECT f.id, f.area_id, f.filename, f.description, f.size_bytes, f.storage_path,
		        f.uploaded_by, u.username, f.uploaded_at, f.download_count
		 FROM files f JOIN users u ON u.id = f.uploaded_by WHERE f.id = ?`, id,
	)
	var f File
	if err := row.Scan(&f.ID, &f.AreaID, &f.Filename, &f.Description, &f.SizeBytes, &f.StoragePath,
		&f.UploadedBy, &f.UploadedByName, &f.UploadedAt, &f.DownloadCount); err != nil {
		return nil, fmt.Errorf("file: load %d: %w", id, err)
	}
	return &f, nil
}

// ListFiles returns every file in an area, oldest first, with each
// uploader's current username joined in as UploadedByName.
func (s *Store) ListFiles(areaID int64) ([]File, error) {
	rows, err := s.db.Query(
		`SELECT f.id, f.area_id, f.filename, f.description, f.size_bytes, f.storage_path,
		        f.uploaded_by, u.username, f.uploaded_at, f.download_count
		 FROM files f JOIN users u ON u.id = f.uploaded_by
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
			&f.UploadedBy, &f.UploadedByName, &f.UploadedAt, &f.DownloadCount); err != nil {
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
