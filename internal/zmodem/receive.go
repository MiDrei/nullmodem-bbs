package zmodem

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// Receive receives one or more files the caller's terminal client
// sends via Zmodem into destDir (which must already exist and be
// used for nothing else concurrently; Receive doesn't create it) by
// shelling out to sexyz, the same way Send does -- see Send's/this
// package's own doc comments for the rationale.
//
// Every filename arriving here comes from the caller's own terminal
// client and can't be trusted, unlike Send's path (a filename this
// BBS already chose) -- confirmed directly in sexyz's own source
// (sexyz.c, the RECVDIR branch) rather than assumed: receiving into a
// directory always runs the incoming name through getfname() first,
// which returns only the text after the last '/' or '\\', before
// joining it to destDir, so a sender offering "../../etc/passwd" (or
// any embedded path at all) lands as a plain "passwd" inside destDir,
// never above it. No CVE history to point to the way lrzsz's
// --restricted had, but read from the actual code path this runs
// rather than taken on trust either.
//
// Like Send, switches conn into raw mode for the duration if it
// supports that (see rawSwitcher), and passes sexyz -telnet when it
// does -- sexyz then owns IAC escaping/unescaping on the raw stream
// in both directions instead of this package's own telnet handling.
//
// Returns the base names of every new file that appeared in destDir
// during the transfer (Receive only manages the transfer; importing
// their contents into this BBS's own file store is the caller's job
// -- see internal/bbs's uploadFile), plus any leftover bytes the same
// way Send does.
func Receive(conn io.ReadWriter, destDir string) ([]string, []byte, error) {
	isTelnet, restore := setRaw(conn)
	defer restore()

	before, err := listFiles(destDir)
	if err != nil {
		return nil, nil, fmt.Errorf("zmodem: listing %s: %w", destDir, err)
	}

	var args []string
	if isTelnet {
		args = append(args, "-telnet")
	}
	// Trailing slash matters: without it, sexyz treats an existing
	// path argument as ambiguous between "receive into this directory"
	// and "receive as this exact filename" for some inputs -- confirmed
	// by hand during development, a bare destDir landed the file as
	// literally "<destDir>readme.txt" (no separator) instead of inside
	// it. destDir already exists (Receive's own precondition), so this
	// never creates anything on its own.
	args = append(args, "-8", "rz", strings.TrimRight(destDir, "/")+"/")
	cmd := exec.Command("sexyz", args...)
	var stderr bytes.Buffer

	leftover, runErr := runSexyz(cmd, conn, &stderr, "sexyz rz")

	after, listErr := listFiles(destDir)
	if listErr != nil {
		return nil, leftover, fmt.Errorf("zmodem: listing %s: %w", destDir, listErr)
	}
	received := newNames(before, after)

	return received, leftover, runErr
}

func listFiles(dir string) (map[string]bool, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(entries))
	for _, e := range entries {
		if !e.IsDir() {
			names[e.Name()] = true
		}
	}
	return names, nil
}

// newNames returns, sorted, every name in after that wasn't already
// in before -- Receive's way of discovering what sexyz actually wrote
// without having to parse its output.
func newNames(before, after map[string]bool) []string {
	var names []string
	for name := range after {
		if !before[name] {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
