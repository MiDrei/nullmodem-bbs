package tosser

import (
	"bytes"
	"fmt"
	"hash/crc32"
	"io"
	"strings"

	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/file"
	"git.maik.ch/swissmaik/nullmodem/internal/tic"
)

// isTICFile reports whether name looks like a TIC (FTS-0006 "type 2"
// file distribution) descriptor -- the small text sidecar
// accompanying each file transferred through a file-echo.
func isTICFile(name string) bool {
	return strings.HasSuffix(strings.ToLower(name), ".tic")
}

// TICConfig bundles what inbound TIC/file-echo tossing needs across
// every BinkP session: only where to file received files (see
// file.Store.EnsureArea/Receive). Which TIC "Pw" values a given
// session should accept, and the in-session correlator pairing each
// inbound .tic descriptor with its file transfer, are both
// necessarily specific to one session (see ticSession) and
// constructed fresh by Poll/Answer for each one, never shared the way
// this static config is -- multiple inbound sessions can run
// concurrently (see cmd/mailer's startInboundListener). A nil
// *TICConfig disables TIC handling entirely: a .tic file (or anything
// that happens to match a still-pending one) then falls through to
// the ordinary "unsupported inbound file" bucket, exactly as before
// this existed.
type TICConfig struct {
	Files *file.Store
}

// acceptedTICPasswords mirrors acceptedPacketPasswords exactly (see
// its doc comment for why every password configured for primary's
// host is accepted, not just primary's own) -- for
// config.BinkpUplink.TICPassword instead of PacketPassword.
func acceptedTICPasswords(primary config.BinkpUplink, allUplinks []config.BinkpUplink) []string {
	seen := map[string]bool{}
	var out []string
	add := func(pw string) {
		if pw == "" {
			return
		}
		key := strings.ToLower(pw)
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, pw)
	}

	add(primary.TICPassword)
	for _, u := range allUplinks {
		if u.Host == primary.Host {
			add(u.TICPassword)
		}
	}
	return out
}

// ticSession holds one BinkP session's TIC/file-echo state: the file
// store to toss into, which "Pw" values this session accepts, and a
// correlator pairing each inbound .tic descriptor with its file
// transfer -- BinkP doesn't guarantee which of the two arrives first,
// so each is buffered until its other half shows up, keyed
// case-insensitively by filename (FTN file-echo filenames are
// conventionally case-insensitive). Not safe for concurrent use --
// exactly one per session, matching how Poll/Answer already handle
// everything else about one session's state.
type ticSession struct {
	files             *file.Store
	acceptedPasswords []string
	pendingTICs       map[string]tic.File
	pendingFiles      map[string][]byte
}

func newTICSession(files *file.Store, acceptedPasswords []string) *ticSession {
	return &ticSession{
		files:             files,
		acceptedPasswords: acceptedPasswords,
		pendingTICs:       map[string]tic.File{},
		pendingFiles:      map[string][]byte{},
	}
}

// receive handles one inbound file that isn't an FTS-0001 packet or
// packet bundle: either a .tic descriptor or a file-echo payload.
// Both halves are buffered (fully read into memory -- file-echo
// attachments are typically KB-MB scale, the same order of magnitude
// this codebase already buffers whole packets/bundles at) until their
// pair shows up; once a pair is complete, toss files it immediately.
// Always drains r fully itself (buffering IS draining here),
// satisfying binkp.Config.ReceiveFile's contract regardless of which
// branch is taken.
func (ts *ticSession) receive(name string, r io.Reader, res *Result) error {
	data, err := io.ReadAll(r)
	if err != nil {
		return fmt.Errorf("tosser: reading inbound file %s: %w", name, err)
	}

	if isTICFile(name) {
		// TEMPORARY -- see Result.TICDebug's own doc comment.
		res.TICDebug = append(res.TICDebug, fmt.Sprintf("%s:\n%s", name, data))
		desc, err := tic.Parse(data)
		if err != nil {
			res.SkippedFiles = append(res.SkippedFiles, name)
			return nil
		}
		key := strings.ToUpper(desc.Name)
		if payload, ok := ts.pendingFiles[key]; ok {
			delete(ts.pendingFiles, key)
			return ts.toss(desc, payload, res)
		}
		ts.pendingTICs[key] = desc
		return nil
	}

	key := strings.ToUpper(name)
	if desc, ok := ts.pendingTICs[key]; ok {
		delete(ts.pendingTICs, key)
		return ts.toss(desc, data, res)
	}
	ts.pendingFiles[key] = data
	return nil
}

// toss validates desc (accepted password, matching size/CRC-32
// against the actually-received payload -- neither trusted blindly)
// and, if it checks out, files payload into the local file-echo area
// named by desc.Area, auto-created as Pending if never seen before
// (see file.Store.EnsureArea, mirroring tossEcho exactly for
// echomail). A validation failure reports name in res.SkippedFiles
// rather than erroring the whole session over one bad file.
func (ts *ticSession) toss(desc tic.File, payload []byte, res *Result) error {
	if len(ts.acceptedPasswords) > 0 {
		matched := false
		for _, pw := range ts.acceptedPasswords {
			if strings.EqualFold(desc.Password, pw) {
				matched = true
				break
			}
		}
		if !matched {
			res.SkippedFiles = append(res.SkippedFiles, desc.Name)
			return nil
		}
	}
	if desc.SizeBytes > 0 && int64(len(payload)) != desc.SizeBytes {
		res.SkippedFiles = append(res.SkippedFiles, desc.Name)
		return nil
	}
	if desc.HasCRC32 && crc32.ChecksumIEEE(payload) != desc.CRC32 {
		res.SkippedFiles = append(res.SkippedFiles, desc.Name)
		return nil
	}

	area, _, err := ts.files.EnsureArea(desc.Area, desc.Area, "")
	if err != nil {
		return fmt.Errorf("tosser: ensuring file area %q: %w", desc.Area, err)
	}
	origin := desc.Origin
	if origin == "" {
		origin = "unknown"
	}
	_, created, err := ts.files.Receive(area.ID, origin, desc.Name, desc.Description, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("tosser: storing tossed file %q in area %q: %w", desc.Name, desc.Area, err)
	}
	if created {
		res.ReceivedFiles++
	}
	return nil
}

// flushUnmatched reports every .tic descriptor or file payload still
// waiting for its other half once the BinkP session has ended -- the
// two are expected in the same session (real hub software sends them
// together), so anything left here didn't arrive at all, not just
// "hasn't arrived yet".
func (ts *ticSession) flushUnmatched(res *Result) {
	for _, desc := range ts.pendingTICs {
		res.SkippedFiles = append(res.SkippedFiles, desc.Name+" (TIC with no matching file)")
	}
	for name := range ts.pendingFiles {
		res.SkippedFiles = append(res.SkippedFiles, name+" (file with no matching TIC)")
	}
}
