package tosser

import (
	"bytes"
	"fmt"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/mail"
	"git.maik.ch/nullmodem/bbs/internal/tic"
)

// InspectedMessage summarizes one packed FTS-0001 message for the web
// admin's Packet Analyzer -- enough to see what a packet actually
// carries without dumping raw bytes.
type InspectedMessage struct {
	OrigAddr, DestAddr string
	FromName, ToName   string
	Subject            string
	Written            time.Time
	Private            bool
	// AreaTag is the echomail area this message belongs to (its own
	// "AREA:tag" body line, see echoAreaTag), or "" for netmail.
	AreaTag string
	// BodySize is the message body's length in bytes, kludges
	// included -- shown instead of the body itself, which can be large
	// and is already visible via the raw preview/download.
	BodySize int
}

// InspectedPacket summarizes one FTS-0001 .pkt file: its header plus a
// per-message summary of everything it carries.
type InspectedPacket struct {
	Name               string
	OrigAddr, DestAddr string
	Created            time.Time
	Messages           []InspectedMessage
}

// InspectedTIC summarizes one FTS-0006 TIC descriptor.
type InspectedTIC struct {
	Area        string
	File        string
	Description string
	SizeBytes   int64
	HasCRC32    bool
	CRC32       uint32
	Origin      string
}

// Inspection is the result of inspecting one archived inbound file
// (see internal/archive): what kind of FTN artifact it is, and a
// structured summary of its contents, for the web admin's Packet
// Analyzer to show beyond a raw byte dump.
type Inspection struct {
	// Kind is "packet", "bundle", "tic", or "unknown".
	Kind string
	// Packets holds one entry for a plain packet (Kind == "packet") or
	// one per packet found inside a bundle (Kind == "bundle").
	Packets []InspectedPacket
	TIC     *InspectedTIC
}

// Inspect examines one archived inbound file's raw bytes by name and
// content -- the same dispatch handleInboundFile itself uses -- and
// returns a structured summary. Purely read-only (no tossing, no DB
// writes), safe to call any number of times on already-archived data.
func Inspect(name string, data []byte) (Inspection, error) {
	switch {
	case isPacketFile(name):
		p, err := inspectPacket(name, data)
		if err != nil {
			return Inspection{}, err
		}
		return Inspection{Kind: "packet", Packets: []InspectedPacket{p}}, nil

	case isPacketBundleFile(name):
		named, err := extractPacketBundle(name, data)
		if err != nil {
			return Inspection{}, err
		}
		packets := make([]InspectedPacket, 0, len(named))
		for _, np := range named {
			p, err := inspectPacket(np.name, np.data)
			if err != nil {
				return Inspection{}, fmt.Errorf("inspecting %s from bundle %s: %w", np.name, name, err)
			}
			packets = append(packets, p)
		}
		return Inspection{Kind: "bundle", Packets: packets}, nil

	case isTICFile(name):
		f, err := tic.Parse(data)
		if err != nil {
			return Inspection{}, err
		}
		return Inspection{Kind: "tic", TIC: &InspectedTIC{
			Area:        f.Area,
			File:        f.Name,
			Description: f.Description,
			SizeBytes:   f.SizeBytes,
			HasCRC32:    f.HasCRC32,
			CRC32:       f.CRC32,
			Origin:      f.Origin,
		}}, nil

	default:
		return Inspection{Kind: "unknown"}, nil
	}
}

func inspectPacket(name string, data []byte) (InspectedPacket, error) {
	pkt, err := mail.ReadPacket(bytes.NewReader(data))
	if err != nil {
		return InspectedPacket{}, err
	}
	p := InspectedPacket{
		Name:     name,
		OrigAddr: pkt.Header.OrigAddr.String(),
		DestAddr: pkt.Header.DestAddr.String(),
		Created:  pkt.Header.Created,
	}
	for _, m := range pkt.Messages {
		areaTag, _ := echoAreaTag(m.Body)
		p.Messages = append(p.Messages, InspectedMessage{
			OrigAddr: m.OrigAddr.String(),
			DestAddr: m.DestAddr.String(),
			FromName: m.FromName,
			ToName:   m.ToName,
			Subject:  m.Subject,
			Written:  m.Written,
			Private:  m.Attr&mail.AttrPrivate != 0,
			AreaTag:  areaTag,
			BodySize: len(m.Body),
		})
	}
	return p, nil
}
