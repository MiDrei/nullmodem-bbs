package qwkdoor

import (
	"archive/zip"
	"fmt"

	"git.maik.ch/nullmodem/kit/qwk"
)

// MaxReplyBytes bounds a reply packet's unpacked .MSG files. A caller's
// replies are a few KB; qwk.ParseReplyPacket holds them all in memory,
// so a small zip that unpacks to gigabytes would take the daemon down.
const MaxReplyBytes = 16 << 20

// ParseReply is qwk.ParseReplyPacket after checking the packet's
// declared sizes (archive/zip refuses an entry that unpacks to more
// than it declares, so the check holds).
func ParseReply(path, bbsID string) ([]qwk.PackedMessage, error) {
	zr, err := zip.OpenReader(path)
	if err == nil {
		var total uint64
		for _, f := range zr.File {
			total += f.UncompressedSize64
		}
		zr.Close()
		if total > MaxReplyBytes || len(zr.File) > 1000 {
			return nil, fmt.Errorf("qwk: the packet unpacks to more than %d MB", MaxReplyBytes>>20)
		}
	}
	// Not a zip at all: ParseReplyPacket says so in its own words.
	return qwk.ParseReplyPacket(path, bbsID)
}
