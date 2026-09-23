package tic

import (
	"errors"
	"testing"
)

func TestParseBasicFields(t *testing.T) {
	data := "Area FSX_FILES\r\n" +
		"File readme.zip\r\n" +
		"Desc A short description\r\n" +
		"Size 12345\r\n" +
		"Crc A1B2C3D4\r\n" +
		"Origin 21:3/100\r\n" +
		"Pw secret1\r\n"

	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Area != "FSX_FILES" {
		t.Fatalf("Area = %q, want %q", f.Area, "FSX_FILES")
	}
	if f.Name != "readme.zip" {
		t.Fatalf("Name = %q, want %q", f.Name, "readme.zip")
	}
	if f.Description != "A short description" {
		t.Fatalf("Description = %q, want %q", f.Description, "A short description")
	}
	if f.SizeBytes != 12345 {
		t.Fatalf("SizeBytes = %d, want 12345", f.SizeBytes)
	}
	if !f.HasCRC32 || f.CRC32 != 0xA1B2C3D4 {
		t.Fatalf("CRC32/HasCRC32 = %08X/%v, want A1B2C3D4/true", f.CRC32, f.HasCRC32)
	}
	if f.Origin != "21:3/100" {
		t.Fatalf("Origin = %q, want %q", f.Origin, "21:3/100")
	}
	if f.Password != "secret1" {
		t.Fatalf("Password = %q, want %q", f.Password, "secret1")
	}
}

func TestParseCombinesDescAndLdescInOrder(t *testing.T) {
	data := "Area FSX_FILES\r\n" +
		"File readme.zip\r\n" +
		"Desc First line\r\n" +
		"Ldesc Second line\r\n" +
		"Ldesc Third line\r\n"

	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "First line\nSecond line\nThird line"
	if f.Description != want {
		t.Fatalf("Description = %q, want %q", f.Description, want)
	}
}

func TestParseAcceptsAreaDescAsDescSynonym(t *testing.T) {
	// Confirmed live against a real dege.au/htick hub, which sends
	// "AreaDesc" instead of FTS-0006's standard "Desc" -- see
	// internal/tosser's Archive-backed Packet Analyzer, which is how
	// this was actually caught.
	data := "Area FSX_IMGE\r\n" +
		"File apod0923.zip\r\n" +
		"AreaDesc Astronomy picture of the day\r\n" +
		"Ldesc more detail\r\n"

	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	want := "Astronomy picture of the day\nmore detail"
	if f.Description != want {
		t.Fatalf("Description = %q, want %q", f.Description, want)
	}
}

func TestParseKeywordsAreCaseInsensitive(t *testing.T) {
	data := "AREA FSX_FILES\r\nfile readme.zip\r\nDESC hello\r\n"
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Area != "FSX_FILES" || f.Name != "readme.zip" || f.Description != "hello" {
		t.Fatalf("Parse = %+v, want Area/Name/Description populated regardless of keyword case", f)
	}
}

func TestParseIgnoresUnrecognizedKeywords(t *testing.T) {
	data := "Area FSX_FILES\r\nFile readme.zip\r\nFrom 21:3/1\r\nPath 21:3/1 1234567890\r\nSeenby 21:3\r\n"
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Area != "FSX_FILES" || f.Name != "readme.zip" {
		t.Fatalf("Parse = %+v, unrecognized keywords must not prevent parsing what's understood", f)
	}
}

func TestParseMissingAreaReturnsError(t *testing.T) {
	_, err := Parse([]byte("File readme.zip\r\n"))
	if !errors.Is(err, ErrMissingArea) {
		t.Fatalf("Parse error = %v, want ErrMissingArea", err)
	}
}

func TestParseMissingFileReturnsError(t *testing.T) {
	_, err := Parse([]byte("Area FSX_FILES\r\n"))
	if !errors.Is(err, ErrMissingFile) {
		t.Fatalf("Parse error = %v, want ErrMissingFile", err)
	}
}

func TestParseUnparseableSizeAndCrcAreLeftZero(t *testing.T) {
	data := "Area FSX_FILES\r\nFile readme.zip\r\nSize notanumber\r\nCrc notanumber\r\n"
	f, err := Parse([]byte(data))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.SizeBytes != 0 {
		t.Fatalf("SizeBytes = %d, want 0 for an unparseable value", f.SizeBytes)
	}
	if f.HasCRC32 {
		t.Fatal("HasCRC32 = true for an unparseable value, want false")
	}
}

func TestParseHandlesBareLfLineEndings(t *testing.T) {
	// Not every real tosser writes CRLF -- confirm plain LF works too.
	f, err := Parse([]byte("Area FSX_FILES\nFile readme.zip\nDesc hi\n"))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if f.Area != "FSX_FILES" || f.Name != "readme.zip" || f.Description != "hi" {
		t.Fatalf("Parse = %+v, want it to work with bare LF line endings too", f)
	}
}
