package binkp

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteReadCommandFrameRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCommandFrame(&buf, MADR, "1:234/56.0"); err != nil {
		t.Fatalf("writeCommandFrame: %v", err)
	}

	isData, payload, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if isData {
		t.Fatal("expected a command frame, got a data frame")
	}
	if len(payload) == 0 || Command(payload[0]) != MADR {
		t.Fatalf("payload command = %v, want M_ADR", payload)
	}
	if string(payload[1:]) != "1:234/56.0" {
		t.Fatalf("payload arg = %q, want %q", payload[1:], "1:234/56.0")
	}
}

func TestWriteReadDataFrameRoundTrips(t *testing.T) {
	var buf bytes.Buffer
	data := []byte("hello binkp")
	if err := writeDataFrame(&buf, data); err != nil {
		t.Fatalf("writeDataFrame: %v", err)
	}

	isData, payload, err := readFrame(&buf)
	if err != nil {
		t.Fatalf("readFrame: %v", err)
	}
	if !isData {
		t.Fatal("expected a data frame, got a command frame")
	}
	if !bytes.Equal(payload, data) {
		t.Fatalf("payload = %q, want %q", payload, data)
	}
}

func TestCommandFrameArgTooLongRejected(t *testing.T) {
	var buf bytes.Buffer
	longArg := strings.Repeat("x", maxFrameLen)
	if err := writeCommandFrame(&buf, MNUL, longArg); err == nil {
		t.Fatal("expected an error for an oversized command argument")
	}
}

func TestDataFrameTooLargeRejected(t *testing.T) {
	var buf bytes.Buffer
	if err := writeDataFrame(&buf, make([]byte, maxFrameLen+1)); err == nil {
		t.Fatal("expected an error for an oversized data frame")
	}
}

func TestReadFrameOnEmptyReaderReturnsEOF(t *testing.T) {
	var buf bytes.Buffer
	if _, _, err := readFrame(&buf); err == nil {
		t.Fatal("expected an error reading from an empty buffer")
	}
}

func TestMultipleFramesInSequence(t *testing.T) {
	var buf bytes.Buffer
	if err := writeCommandFrame(&buf, MNUL, "SYS Test BBS"); err != nil {
		t.Fatalf("writeCommandFrame 1: %v", err)
	}
	if err := writeDataFrame(&buf, []byte("chunk1")); err != nil {
		t.Fatalf("writeDataFrame: %v", err)
	}
	if err := writeCommandFrame(&buf, MEOB, ""); err != nil {
		t.Fatalf("writeCommandFrame 2: %v", err)
	}

	isData, payload, err := readFrame(&buf)
	if err != nil || isData || Command(payload[0]) != MNUL {
		t.Fatalf("frame 1 = (isData=%v, payload=%q, err=%v), want M_NUL command frame", isData, payload, err)
	}
	isData, payload, err = readFrame(&buf)
	if err != nil || !isData || string(payload) != "chunk1" {
		t.Fatalf("frame 2 = (isData=%v, payload=%q, err=%v), want data frame \"chunk1\"", isData, payload, err)
	}
	isData, payload, err = readFrame(&buf)
	if err != nil || isData || Command(payload[0]) != MEOB {
		t.Fatalf("frame 3 = (isData=%v, payload=%q, err=%v), want M_EOB command frame", isData, payload, err)
	}
}
