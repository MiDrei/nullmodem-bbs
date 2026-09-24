package qwk

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteToReaderEXTWritesAliasLine(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteToReaderEXT(&buf, "alice"); err != nil {
		t.Fatalf("WriteToReaderEXT: %v", err)
	}
	if buf.String() != "ALIAS alice\r\n" {
		t.Fatalf("TOREADER.EXT contents = %q, want %q", buf.String(), "ALIAS alice\r\n")
	}
}

func TestWriteToReaderEXTWritesNothingForBlankUsername(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteToReaderEXT(&buf, ""); err != nil {
		t.Fatalf("WriteToReaderEXT: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("TOREADER.EXT contents = %q, want empty for a blank username", buf.String())
	}
}

func TestWithQWKEKludgesAddsOnlyFieldsOverTheClassicLimit(t *testing.T) {
	shortTo := "All"
	shortFrom := "alice"
	longSubject := "This subject is deliberately longer than twenty-five characters"

	got := withQWKEKludges(shortTo, shortFrom, longSubject, "the body")

	if strings.Contains(got, "TO:") || strings.Contains(got, "FROM:") {
		t.Fatalf("kludge text = %q, should not carry TO:/FROM: kludges for fields under the 25-char limit", got)
	}
	want := "SUBJ:" + longSubject + "\n\nthe body"
	if got != want {
		t.Fatalf("kludge text = %q, want %q", got, want)
	}
}

func TestWithQWKEKludgesAddsAllThreeWhenAllExceedTheLimit(t *testing.T) {
	longTo := "A Very Long Recipient Name Indeed"
	longFrom := "A Very Long Sender Name As Well"
	longSubject := "A Very Long Subject Line That Exceeds Limits"

	got := withQWKEKludges(longTo, longFrom, longSubject, "body text")

	want := "TO:" + longTo + "\nFROM:" + longFrom + "\nSUBJ:" + longSubject + "\n\nbody text"
	if got != want {
		t.Fatalf("kludge text = %q, want %q", got, want)
	}
}

func TestWithQWKEKludgesLeavesTextUnchangedWhenNothingExceedsTheLimit(t *testing.T) {
	got := withQWKEKludges("All", "alice", "Hello", "the body")
	if got != "the body" {
		t.Fatalf("kludge text = %q, want the original body unchanged", got)
	}
}
