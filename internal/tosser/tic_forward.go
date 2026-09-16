package tosser

import (
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/areafix"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/mail"
)

// PendingFileForward is one file ready to be forwarded to a downlink
// via TIC/file-echo, paired with its area's tag (needed for the TIC's
// own "Area" line) -- returned only by RoutedOutboundFileForward, the
// file-echo mirror of message.PendingEcho/RoutedOutboundEchoForward.
type PendingFileForward struct {
	file.File
	AreaTag string
}

// RoutedOutboundFileForward is RoutedOutboundEchoForward's exact
// counterpart for file-echo: every file in an area target has an
// active inbound Filefix subscription to (see areafix.FileStore,
// Direction Inbound) that target's own address doesn't already carry
// in that file's SeenBy (file.SeenByNetNodes -- see
// file.Store.MarkSeenBy, called once Poll confirms a forwarded file
// was actually delivered). A subscription naming a tag with no
// matching local file area is silently skipped rather than erroring
// the whole poll over it.
func RoutedOutboundFileForward(files *file.Store, fileGrants *areafix.FileStore, target config.BinkpUplink) ([]PendingFileForward, error) {
	targetAddr, err := mail.ParseAddress(target.Address)
	if err != nil {
		return nil, fmt.Errorf("tosser: downlink address %q: %w", target.Address, err)
	}
	targetNetNode := file.NetNode(targetAddr.Net, targetAddr.Node)

	subs, err := fileGrants.ListForUplink(target.Host, areafix.Inbound)
	if err != nil {
		return nil, fmt.Errorf("tosser: loading inbound file-echo subscriptions for %s: %w", target.Host, err)
	}

	var out []PendingFileForward
	for _, sub := range subs {
		area, err := files.AreaByTag(sub.AreaTag)
		if err != nil {
			if errors.Is(err, file.ErrAreaNotFound) {
				continue
			}
			return nil, fmt.Errorf("tosser: resolving subscribed file area %q: %w", sub.AreaTag, err)
		}
		areaFiles, err := files.ListFiles(area.ID)
		if err != nil {
			return nil, fmt.Errorf("tosser: loading files for area %q: %w", sub.AreaTag, err)
		}
		for _, f := range areaFiles {
			if file.SeenByNetNodes(f.SeenBy)[targetNetNode] {
				continue
			}
			out = append(out, PendingFileForward{File: f, AreaTag: sub.AreaTag})
		}
	}
	return out, nil
}

// encodeTIC renders pf's outbound TIC (FTS-0006) descriptor bytes,
// addressed from ourAddr, describing a payload of crc/size bytes --
// the outbound mirror of internal/tic.Parse. password is written as
// the "Pw" line only when non-empty (see config.BinkpUplink.
// TICPassword, checked the same way on the inbound side -- see
// acceptedTICPasswords).
func encodeTIC(pf PendingFileForward, ourAddr mail.Address, password string, size int64, crc uint32) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "Area %s\r\n", pf.AreaTag)
	fmt.Fprintf(&b, "Origin %s\r\n", ourAddr.String())
	fmt.Fprintf(&b, "From %s\r\n", ourAddr.String())
	fmt.Fprintf(&b, "File %s\r\n", pf.Filename)
	fmt.Fprintf(&b, "Size %d\r\n", size)
	fmt.Fprintf(&b, "Crc %08X\r\n", crc)
	if pf.Description != "" {
		lines := strings.Split(pf.Description, "\n")
		fmt.Fprintf(&b, "Desc %s\r\n", lines[0])
		for _, extra := range lines[1:] {
			fmt.Fprintf(&b, "Ldesc %s\r\n", extra)
		}
	}
	if password != "" {
		fmt.Fprintf(&b, "Pw %s\r\n", password)
	}
	return []byte(b.String())
}

// ticOutboundName is the .tic descriptor's own outbound filename --
// keyed by the local file ID rather than a timestamp (unlike
// buildPacket's own %08x.pkt naming) so it's guaranteed unique even
// when forwarding several files to the same downlink within one
// session, and deterministic (useful for matching this exact file
// against binkp.Result.FilesSent after the session -- see Poll).
func ticOutboundName(fileID int64) string {
	return fmt.Sprintf("%08x.tic", fileID)
}

// readFileForForwarding reads pf's stored content fully into memory
// (file-echo attachments are typically KB-MB scale, the same order of
// magnitude this codebase already buffers whole packets/TIC payloads
// at on the inbound side -- see ticSession.receive) and returns it
// alongside its CRC-32, needed for both the outbound TIC's own "Crc"
// line and the binkp.OutboundFile's Size.
func readFileForForwarding(pf PendingFileForward) (data []byte, crc uint32, err error) {
	data, err = os.ReadFile(pf.StoragePath)
	if err != nil {
		return nil, 0, fmt.Errorf("tosser: reading %q for forwarding: %w", pf.Filename, err)
	}
	return data, crc32.ChecksumIEEE(data), nil
}
