package file

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"git.maik.ch/swissmaik/nullmodem/internal/db"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

func newTestStore(t *testing.T) (*Store, *user.Store) {
	t.Helper()
	dir := t.TempDir()
	sqlDB, err := db.Open(filepath.Join(dir, "test.sqlite"))
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	return NewStore(sqlDB, filepath.Join(dir, "files")), user.NewStore(sqlDB)
}

func writeTempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "upload.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	return path
}

func TestSchemaSeedsGeneralFileArea(t *testing.T) {
	s, _ := newTestStore(t)

	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	if area.Name != "General Files" {
		t.Fatalf("seeded area name = %q, want %q", area.Name, "General Files")
	}
}

func TestCreateAreaRejectsDuplicateTag(t *testing.T) {
	s, _ := newTestStore(t)

	if _, err := s.CreateArea("dup", "First", "", 0, 0); err != nil {
		t.Fatalf("first CreateArea: %v", err)
	}
	if _, err := s.CreateArea("DUP", "Second", "", 0, 0); !errors.Is(err, ErrTagTaken) {
		t.Fatalf("second CreateArea = %v, want ErrTagTaken", err)
	}
}

func TestAreaCanDownloadUpload(t *testing.T) {
	a := Area{MinSLDownload: 10, MinSLUpload: 50}
	if a.CanDownload(5) {
		t.Fatal("CanDownload(5) should be false when MinSLDownload is 10")
	}
	if !a.CanDownload(10) {
		t.Fatal("CanDownload(10) should be true when MinSLDownload is 10")
	}
	if a.CanUpload(10) {
		t.Fatal("CanUpload(10) should be false when MinSLUpload is 50")
	}
	if !a.CanUpload(50) {
		t.Fatal("CanUpload(50) should be true when MinSLUpload is 50")
	}
}

func TestListAreasFiltersBySecurityLevel(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	areas, err := s.ListAreas(0)
	if err != nil {
		t.Fatalf("ListAreas(0): %v", err)
	}
	for _, a := range areas {
		if a.Tag == "sysop-only" {
			t.Fatalf("ListAreas(0) should not include sysop-only area, got %+v", areas)
		}
	}

	all, err := s.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	found := false
	for _, a := range all {
		if a.Tag == "sysop-only" {
			found = true
		}
	}
	if !found {
		t.Fatalf("AllAreas should include sysop-only area regardless of SL, got %+v", all)
	}
}

func TestImportFileCopiesAndRecordsMetadata(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	src := writeTempFile(t, "hello world")
	f, err := s.ImportFile(area.ID, u.ID, src, "A test file")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}
	if f.SizeBytes != int64(len("hello world")) {
		t.Fatalf("SizeBytes = %d, want %d", f.SizeBytes, len("hello world"))
	}
	if f.UploadedByName != "alice" {
		t.Fatalf("UploadedByName = %q, want %q", f.UploadedByName, "alice")
	}

	data, err := os.ReadFile(f.StoragePath)
	if err != nil {
		t.Fatalf("reading stored copy: %v", err)
	}
	if string(data) != "hello world" {
		t.Fatalf("stored copy content = %q, want %q", data, "hello world")
	}

	files, err := s.ListFiles(area.ID)
	if err != nil {
		t.Fatalf("ListFiles: %v", err)
	}
	if len(files) != 1 || files[0].Filename != filepath.Base(src) {
		t.Fatalf("ListFiles = %+v, want one file named %q", files, filepath.Base(src))
	}
}

func TestImportFileRejectsDuplicateFilename(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	src := writeTempFile(t, "content")
	if _, err := s.ImportFile(area.ID, u.ID, src, ""); err != nil {
		t.Fatalf("first ImportFile: %v", err)
	}
	if _, err := s.ImportFile(area.ID, u.ID, src, ""); !errors.Is(err, ErrDuplicateFilename) {
		t.Fatalf("second ImportFile = %v, want ErrDuplicateFilename", err)
	}
}

func TestImportFileRejectsMissingSource(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = s.ImportFile(area.ID, u.ID, filepath.Join(t.TempDir(), "does-not-exist.txt"), "")
	if err == nil {
		t.Fatal("expected error for missing source file")
	}
}

func TestImportFileRejectsDirectory(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	_, err = s.ImportFile(area.ID, u.ID, t.TempDir(), "")
	if err == nil {
		t.Fatal("expected error when source is a directory")
	}
}
