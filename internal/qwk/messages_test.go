package qwk

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteReadMessagesDATRoundTrip(t *testing.T) {
	written := time.Date(2026, time.September, 23, 14, 5, 0, 0, time.UTC)
	messages := []PackedMessage{
		{
			Header: MessageHeader{
				Status:     ' ',
				Number:     1,
				Written:    written,
				To:         "ALL",
				From:       "ALICE",
				Subject:    "Hello there",
				Conference: 3,
			},
			Text: "line one\nline two\nline three",
		},
		{
			Header: MessageHeader{
				Status:        '*',
				Number:        2,
				Written:       written,
				To:            "BOB",
				From:          "ALICE",
				Subject:       "Re: Hello there",
				RefNumber:     1,
				Conference:    0,
				LogicalNumber: 2,
				HasTagline:    true,
			},
			Text: "a private reply",
		},
	}

	var buf bytes.Buffer
	if err := WriteMessagesDAT(&buf, messages); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}

	if buf.Len()%blockSize != 0 {
		t.Fatalf("output length %d is not a multiple of %d", buf.Len(), blockSize)
	}

	got, err := ReadMessagesDAT(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadMessagesDAT: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d messages, want 2", len(got))
	}

	m0 := got[0]
	if m0.Header.Status != ' ' || m0.Header.Number != 1 || m0.Header.To != "ALL" ||
		m0.Header.From != "ALICE" || m0.Header.Subject != "Hello there" || m0.Header.Conference != 3 {
		t.Fatalf("message 0 header = %+v", m0.Header)
	}
	if !m0.Header.Written.Equal(written) {
		t.Fatalf("message 0 Written = %v, want %v", m0.Header.Written, written)
	}
	if m0.Text != "line one\nline two\nline three" {
		t.Fatalf("message 0 Text = %q", m0.Text)
	}
	// header block + 1 text block (78 bytes fits in one 128-byte block)
	if m0.Header.Blocks != 2 {
		t.Fatalf("message 0 Blocks = %d, want 2", m0.Header.Blocks)
	}

	m1 := got[1]
	if m1.Header.Status != '*' || m1.Header.RefNumber != 1 || m1.Header.LogicalNumber != 2 || !m1.Header.HasTagline {
		t.Fatalf("message 1 header = %+v", m1.Header)
	}
	if m1.Text != "a private reply" {
		t.Fatalf("message 1 Text = %q", m1.Text)
	}
}

func TestWriteMessagesDATSpansMultipleTextBlocks(t *testing.T) {
	longText := strings.Repeat("x", 300) // spans 3 blocks of 128 bytes
	messages := []PackedMessage{{
		Header: MessageHeader{To: "ALL", From: "ALICE", Subject: "long"},
		Text:   longText,
	}}
	var buf bytes.Buffer
	if err := WriteMessagesDAT(&buf, messages); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}
	got, err := ReadMessagesDAT(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadMessagesDAT: %v", err)
	}
	if len(got) != 1 || got[0].Text != longText {
		t.Fatalf("got = %+v, want the exact %d-byte text back", got, len(longText))
	}
	// header (1) + ceil(300/128)=3 text blocks = 4
	if got[0].Header.Blocks != 4 {
		t.Fatalf("Blocks = %d, want 4", got[0].Header.Blocks)
	}
}

func TestWriteMessagesDATEmptyTextProducesHeaderOnlyMessage(t *testing.T) {
	messages := []PackedMessage{{Header: MessageHeader{To: "ALL", From: "ALICE", Subject: "empty"}, Text: ""}}
	var buf bytes.Buffer
	if err := WriteMessagesDAT(&buf, messages); err != nil {
		t.Fatalf("WriteMessagesDAT: %v", err)
	}
	// record 0 + 1 header block, no text blocks
	if buf.Len() != 2*blockSize {
		t.Fatalf("output length = %d, want %d", buf.Len(), 2*blockSize)
	}
	got, err := ReadMessagesDAT(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("ReadMessagesDAT: %v", err)
	}
	if len(got) != 1 || got[0].Text != "" || got[0].Header.Blocks != 1 {
		t.Fatalf("got = %+v", got)
	}
}

func TestReadMessagesDATOnEmptyInputReturnsNoMessages(t *testing.T) {
	got, err := ReadMessagesDAT(bytes.NewReader(nil))
	if err != nil {
		t.Fatalf("ReadMessagesDAT: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got = %+v, want none", got)
	}
}

func TestEncodeHeaderTruncatesOverlongFields(t *testing.T) {
	h := MessageHeader{
		To:      strings.Repeat("A", 40),
		From:    strings.Repeat("B", 40),
		Subject: strings.Repeat("C", 40),
	}
	b := encodeHeader(h)
	got := decodeHeader(b)
	if len(got.To) != 25 || len(got.From) != 25 || len(got.Subject) != 25 {
		t.Fatalf("decoded field lengths = %d/%d/%d, want 25/25/25", len(got.To), len(got.From), len(got.Subject))
	}
}
