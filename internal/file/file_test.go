package file

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
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

func TestCountAreas(t *testing.T) {
	s, _ := newTestStore(t)

	// The schema seeds one "general" area, so CountAreas starts at 1.
	if n, err := s.CountAreas(); err != nil || n != 1 {
		t.Fatalf("CountAreas() = %d, %v; want 1, nil", n, err)
	}
	if _, err := s.CreateArea("dev", "Dev", "", 0, 0); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	if n, err := s.CountAreas(); err != nil || n != 2 {
		t.Fatalf("CountAreas() = %d, %v; want 2, nil", n, err)
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

func TestAllAreasIgnoresSecurityLevel(t *testing.T) {
	s, _ := newTestStore(t)
	if _, err := s.CreateArea("sysop-only", "Sysop Only", "", 200, 200); err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	all, err := s.AllAreas()
	if err != nil {
		t.Fatalf("AllAreas: %v", err)
	}
	// The seeded "general" area plus the one just created.
	if len(all) != 2 {
		t.Fatalf("AllAreas() = %+v, want 2 areas", all)
	}
}

func TestUpdateArea(t *testing.T) {
	s, _ := newTestStore(t)
	area, err := s.CreateArea("dev", "Dev", "old desc", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}

	updated, err := s.UpdateArea(area.ID, "Dev Files", "new desc", 10, 20, 5)
	if err != nil {
		t.Fatalf("UpdateArea: %v", err)
	}
	if updated.Name != "Dev Files" || updated.Description != "new desc" || updated.MinSLDownload != 10 ||
		updated.MinSLUpload != 20 || updated.SortOrder != 5 {
		t.Fatalf("UpdateArea result = %+v, want updated fields", updated)
	}
	if updated.Tag != "dev" {
		t.Fatalf("UpdateArea changed tag to %q, want unchanged %q", updated.Tag, "dev")
	}
}

func TestDeleteAreaRemovesFilesFromDisk(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("temp", "Temp", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	src := writeTempFile(t, "content")
	f, err := s.ImportFile(area.ID, u.ID, src, "")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	if err := s.DeleteArea(area.ID); err != nil {
		t.Fatalf("DeleteArea: %v", err)
	}
	if _, err := s.AreaByID(area.ID); !errors.Is(err, ErrAreaNotFound) {
		t.Fatalf("AreaByID after delete = %v, want ErrAreaNotFound", err)
	}
	if _, err := os.Stat(f.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("file %s still exists on disk after area delete", f.StoragePath)
	}
}

func TestDeleteFileRemovesFromDiskAndDB(t *testing.T) {
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
	f, err := s.ImportFile(area.ID, u.ID, src, "")
	if err != nil {
		t.Fatalf("ImportFile: %v", err)
	}

	if err := s.DeleteFile(f.ID); err != nil {
		t.Fatalf("DeleteFile: %v", err)
	}
	if _, err := s.FileByID(f.ID); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("FileByID after delete = %v, want ErrFileNotFound", err)
	}
	if _, err := os.Stat(f.StoragePath); !os.IsNotExist(err) {
		t.Fatalf("file %s still exists on disk after delete", f.StoragePath)
	}
}

func TestDeleteFileUnknownID(t *testing.T) {
	s, _ := newTestStore(t)
	if err := s.DeleteFile(999999); !errors.Is(err, ErrFileNotFound) {
		t.Fatalf("DeleteFile(unknown) = %v, want ErrFileNotFound", err)
	}
}

func TestUploadFileFromReader(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := s.UploadFile(area.ID, u.ID, "notes.txt", "uploaded via web", strings.NewReader("hello upload"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if f.Filename != "notes.txt" || f.SizeBytes != int64(len("hello upload")) {
		t.Fatalf("UploadFile result = %+v, want filename notes.txt and correct size", f)
	}
	data, err := os.ReadFile(f.StoragePath)
	if err != nil {
		t.Fatalf("reading uploaded file: %v", err)
	}
	if string(data) != "hello upload" {
		t.Fatalf("stored content = %q, want %q", data, "hello upload")
	}
}

func TestUploadFileStripsPathFromFilename(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.AreaByTag("general")
	if err != nil {
		t.Fatalf("AreaByTag: %v", err)
	}
	u, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	f, err := s.UploadFile(area.ID, u.ID, "../../etc/passwd", "", strings.NewReader("x"))
	if err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if f.Filename != "passwd" {
		t.Fatalf("Filename = %q, want path stripped to %q", f.Filename, "passwd")
	}
	if !strings.HasPrefix(f.StoragePath, s.filesDir) {
		t.Fatalf("StoragePath = %q, want it under the managed files dir %q", f.StoragePath, s.filesDir)
	}
}

func fileStatsFor(t *testing.T, stats []AreaWithStats, tag string) AreaWithStats {
	t.Helper()
	for _, st := range stats {
		if st.Area.Tag == tag {
			return st
		}
	}
	t.Fatalf("no area stats for tag %q in %+v", tag, stats)
	return AreaWithStats{}
}

func TestListAreaStatsCountsTotalNewAndYours(t *testing.T) {
	s, users := newTestStore(t)
	area, err := s.CreateArea("uploads", "Uploads", "", 0, 0)
	if err != nil {
		t.Fatalf("CreateArea: %v", err)
	}
	alice, err := users.Register("alice", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register alice: %v", err)
	}
	bob, err := users.Register("bob", "password123", user.SLNewUser)
	if err != nil {
		t.Fatalf("Register bob: %v", err)
	}

	if _, err := s.UploadFile(area.ID, alice.ID, "one.txt", "", strings.NewReader("1")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	if _, err := s.UploadFile(area.ID, bob.ID, "two.txt", "", strings.NewReader("2")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}

	// Alice has never visited: everything in the area is new to her,
	// and one of the two files is hers.
	stats, err := s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats: %v", err)
	}
	got := fileStatsFor(t, stats, "uploads")
	if got.Total != 2 || got.New != 2 || got.Yours != 1 {
		t.Fatalf("alice's stats = %+v, want Total=2 New=2 Yours=1", got)
	}

	if err := s.MarkAreaRead(alice.ID, area.ID); err != nil {
		t.Fatalf("MarkAreaRead: %v", err)
	}
	stats, err = s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats after read: %v", err)
	}
	got = fileStatsFor(t, stats, "uploads")
	if got.New != 0 {
		t.Fatalf("alice's New after MarkAreaRead = %d, want 0", got.New)
	}

	if _, err := s.UploadFile(area.ID, bob.ID, "three.txt", "", strings.NewReader("3")); err != nil {
		t.Fatalf("UploadFile: %v", err)
	}
	stats, err = s.ListAreaStats(user.SLNewUser, alice.ID)
	if err != nil {
		t.Fatalf("ListAreaStats after new upload: %v", err)
	}
	got = fileStatsFor(t, stats, "uploads")
	if got.New != 1 || got.Total != 3 {
		t.Fatalf("alice's stats after new upload = %+v, want New=1 Total=3", got)
	}

	bobStats, err := s.ListAreaStats(user.SLNewUser, bob.ID)
	if err != nil {
		t.Fatalf("ListAreaStats for bob: %v", err)
	}
	gotBob := fileStatsFor(t, bobStats, "uploads")
	if gotBob.New != 3 || gotBob.Yours != 2 {
		t.Fatalf("bob's stats = %+v, want New=3 (never visited) Yours=2", gotBob)
	}
}
