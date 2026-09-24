package tosser

import "testing"

func TestInspectPacketReturnsMessageSummary(t *testing.T) {
	packet := buildTestPacket(t)
	insp, err := Inspect("00000001.pkt", packet)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if insp.Kind != "packet" {
		t.Fatalf("Kind = %q, want packet", insp.Kind)
	}
	if len(insp.Packets) != 1 {
		t.Fatalf("len(Packets) = %d, want 1", len(insp.Packets))
	}
	p := insp.Packets[0]
	if p.OrigAddr != "21:3/100" || p.DestAddr != "21:3/194" {
		t.Fatalf("OrigAddr/DestAddr = %q/%q, want 21:3/100 / 21:3/194", p.OrigAddr, p.DestAddr)
	}
	if len(p.Messages) != 1 {
		t.Fatalf("len(Messages) = %d, want 1", len(p.Messages))
	}
	m := p.Messages[0]
	if m.FromName != "Alice" || m.ToName != "Bob" || m.Subject != "Hi" {
		t.Fatalf("unexpected message summary: %+v", m)
	}
	// WriteMessage now always prepends a TZUTC kludge line (see
	// internal/mail's own doc comment on it) -- BodySize correctly
	// reflects the real on-wire body, kludge included.
	wantBody := "\x01TZUTC: +0000\nhello"
	if m.BodySize != len(wantBody) {
		t.Fatalf("BodySize = %d, want %d", m.BodySize, len(wantBody))
	}
}

func TestInspectTICReturnsParsedFields(t *testing.T) {
	data := []byte("Area FSX_IMGE\nFile picture.jpg\nDesc a nice picture\nSize 12345\nOrigin 21:3/100\n")
	insp, err := Inspect("00000001.tic", data)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if insp.Kind != "tic" {
		t.Fatalf("Kind = %q, want tic", insp.Kind)
	}
	if insp.TIC == nil {
		t.Fatalf("TIC is nil")
	}
	if insp.TIC.Area != "FSX_IMGE" || insp.TIC.File != "picture.jpg" || insp.TIC.Description != "a nice picture" {
		t.Fatalf("unexpected TIC summary: %+v", insp.TIC)
	}
}

func TestInspectUnknownFileReturnsUnknownKind(t *testing.T) {
	insp, err := Inspect("readme.txt", []byte("just some text"))
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if insp.Kind != "unknown" {
		t.Fatalf("Kind = %q, want unknown", insp.Kind)
	}
}
