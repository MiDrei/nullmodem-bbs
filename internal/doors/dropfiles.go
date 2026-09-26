package doors

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Drop file formats a door can ask for (Door.DropFile).
const (
	DropFileDoorSys    = "door.sys"
	DropFileDorInfo    = "dorinfo"
	DropFileDoorFileSR = "doorfile.sr"
	DropFileDoor32Sys  = "door32.sys"
)

// DropFileFormats lists every supported Door.DropFile value.
var DropFileFormats = []string{DropFileDoorSys, DropFileDorInfo, DropFileDoorFileSR, DropFileDoor32Sys}

// validateDropFile rejects a Door.DropFile value Run can't write.
func validateDropFile(door Door) error {
	if door.DropFile == "" {
		return nil
	}
	for _, f := range DropFileFormats {
		if door.DropFile == f {
			return nil
		}
	}
	return fmt.Errorf("doors: %s: unknown drop file format %q", door.Name, door.DropFile)
}

// writeDropFile writes sess in format into dir and returns the file
// name it used (the one a launch command's "{dropfile}" refers to).
func writeDropFile(dir, format string, sess Session) (string, error) {
	switch format {
	case DropFileDoorSys:
		return "DOOR.SYS", writeDoorSys(filepath.Join(dir, "DOOR.SYS"), sess)
	case DropFileDorInfo:
		// DORINFO1.DEF is what single-node doors look for; multi-node
		// ones want DORINFO<node>.DEF. Both carry the same content.
		if err := writeDorInfo(filepath.Join(dir, "DORINFO1.DEF"), sess); err != nil {
			return "", err
		}
		if sess.Node > 1 {
			if err := writeDorInfo(filepath.Join(dir, dorInfoName(sess.Node)), sess); err != nil {
				return "", err
			}
		}
		return "DORINFO1.DEF", nil
	case DropFileDoorFileSR:
		return "DOORFILE.SR", writeDoorFileSR(filepath.Join(dir, "DOORFILE.SR"), sess)
	case DropFileDoor32Sys:
		return "DOOR32.SYS", writeDoor32Sys(filepath.Join(dir, "DOOR32.SYS"), sess)
	}
	return "", fmt.Errorf("doors: unknown drop file format %q", format)
}

// dorInfoName is DORINFO<node>.DEF. The node is a single character in
// DOS 8.3 names: 1-9, then A-Z for nodes 10-35, as RBBS and QuickBBS
// number them.
func dorInfoName(node int) string {
	switch {
	case node >= 1 && node <= 9:
		return "DORINFO" + strconv.Itoa(node) + ".DEF"
	case node >= 10 && node <= 35:
		return "DORINFO" + string(rune('A'+node-10)) + ".DEF"
	}
	return "DORINFO1.DEF"
}

// writeLines writes lines CRLF-terminated, the way DOS doors expect
// their drop files.
func writeLines(path, what string, lines []string) error {
	data := strings.Join(lines, "\r\n") + "\r\n"
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		return fmt.Errorf("doors: writing %s: %w", what, err)
	}
	return nil
}

// splitName splits a name into first and last for the formats that
// store them separately; a one-word handle has no last name.
func splitName(name string) (first, last string) {
	name = strings.TrimSpace(name)
	if i := strings.IndexByte(name, ' '); i >= 0 {
		return name[:i], strings.TrimSpace(name[i+1:])
	}
	return name, ""
}

// dosDate is MM/DD/YY, the date format every DOS drop file uses. A
// zero t (no previous call) becomes today.
func dosDate(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.Format("01/02/06")
}

// recordNumber is the caller's user record number, at least 1.
func recordNumber(sess Session) string {
	if sess.UserID < 1 {
		return "1"
	}
	return strconv.FormatInt(sess.UserID, 10)
}

// writeDoor32Sys writes the 11-line DOOR32.SYS dropfile format at
// path -- see https://raw.githubusercontent.com/NuSkooler/ansi-bbs/master/docs/dropfile_formats/door32_sys.txt
// for the full field-by-field spec. Comm type is always 2 (Telnet)
// and the comm/socket handle is always 3, matching the fixed fd Run
// hands the door process via ExtraFiles (Go always numbers a spawned
// child's ExtraFiles starting at 3, right after its inherited stdin/
// stdout/stderr).
func writeDoor32Sys(path string, sess Session) error {
	bbsID := sess.BBSName
	if bbsID == "" {
		bbsID = "NullModem BBS"
	}
	var b strings.Builder
	b.WriteString("2\n")        // Comm Type: 2=Telnet
	b.WriteString("3\n")        // Comm/Socket Handle
	b.WriteString("57600\n")    // Baud Rate (informational only over a socket)
	b.WriteString(bbsID + "\n") // BBSID
	// Record position stays 1, as it always was: doors that key save
	// games on it (rather than the name) would otherwise lose them.
	b.WriteString("1\n")                                     // User's Record Position
	b.WriteString(sess.RealName + "\n")                      // User's Real Name
	b.WriteString(sess.Handle + "\n")                        // User's Handle/Alias
	b.WriteString(strconv.Itoa(sess.AccessLevel) + "\n")     // User's Access Level
	b.WriteString(strconv.Itoa(sess.TimeLeftMinutes) + "\n") // User's Time Left (minutes)
	b.WriteString("1\n")                                     // Emulation: 1=Ansi
	b.WriteString(strconv.Itoa(sess.Node) + "\n")            // Current Node Number
	if err := os.WriteFile(path, []byte(b.String()), 0o644); err != nil {
		return fmt.Errorf("doors: writing DOOR32.SYS: %w", err)
	}
	return nil
}

// writeDoorSys writes the full 52-line DOOR.SYS (the GAP/Wildcat!
// standard virtually every real-mode DOS door understands; doors that
// only know the older 21-line form just stop reading early). The comm
// port is always "COM1:", matching COM1's fixed base address
// (3F8h/IRQ4) DOSBox-X assigns its own serial1 -- see
// dosboxConfigTemplate.
func writeDoorSys(path string, sess Session) error {
	sysop := sess.SysopName
	if sysop == "" {
		sysop = "Sysop"
	}
	lines := []string{
		"COM1:",                                 // 1  comm port
		"38400",                                 // 2  baud rate
		"8",                                     // 3  data bits
		strconv.Itoa(sess.Node),                 // 4  node number
		"38400",                                 // 5  locked (DTE) rate
		"Y",                                     // 6  screen display
		"N",                                     // 7  printer toggle
		"N",                                     // 8  page bell
		"N",                                     // 9  caller alarm
		sess.RealName,                           // 10 user's full name
		"",                                      // 11 calling from
		"",                                      // 12 home phone
		"",                                      // 13 work/data phone
		"",                                      // 14 password (the BBS already authenticated)
		strconv.Itoa(sess.AccessLevel),          // 15 security level
		strconv.Itoa(sess.TotalCalls),           // 16 total times on
		dosDate(sess.LastCall),                  // 17 last date called
		strconv.Itoa(sess.TimeLeftMinutes * 60), // 18 seconds remaining this call
		strconv.Itoa(sess.TimeLeftMinutes),      // 19 minutes remaining this call
		"GR",                                    // 20 graphics mode: GR=ANSI
		"24",                                    // 21 screen length
		"N",                                     // 22 expert mode
		"1",                                     // 23 conferences registered in
		"1",                                     // 24 conference exited to door from
		"12/31/99",                              // 25 expiration date
		recordNumber(sess),                      // 26 user record number
		"Z",                                     // 27 default protocol: Zmodem
		"0",                                     // 28 total uploads
		"0",                                     // 29 total downloads
		"0",                                     // 30 daily download K so far
		"999999",                                // 31 max daily download K
		"01/01/80",                              // 32 birthdate
		`C:\`,                                   // 33 main (user file) directory
		`C:\`,                                   // 34 general (GEN) directory
		sysop,                                   // 35 sysop name
		sess.Handle,                             // 36 user's handle/alias
		"00:00",                                 // 37 next event time
		"Y",                                     // 38 error-correcting connection
		"Y",                                     // 39 ANSI supported
		"Y",                                     // 40 use record locking
		"7",                                     // 41 default color
		"0",                                     // 42 time credits (minutes)
		dosDate(sess.LastCall),                  // 43 last new-files scan date
		time.Now().Format("15:04"),              // 44 time of this call
		"00:00",                                 // 45 time of last call
		"999",                                   // 46 max files per day
		"0",                                     // 47 files downloaded today
		"0",                                     // 48 total K uploaded
		"0",                                     // 49 total K downloaded
		"",                                      // 50 user comment
		"0",                                     // 51 doors opened
		"0",                                     // 52 messages left
	}
	return writeLines(path, "DOOR.SYS", lines)
}

// writeDorInfo writes DORINFOx.DEF, the 13-line QuickBBS/RBBS/Remote
// Access format (LORD, TradeWars 2002 and many others).
func writeDorInfo(path string, sess Session) error {
	bbsName := sess.BBSName
	if bbsName == "" {
		bbsName = "NullModem BBS"
	}
	sysopFirst, sysopLast := splitName(sess.SysopName)
	if sysopFirst == "" {
		sysopFirst = "Sysop"
	}
	userFirst, userLast := splitName(sess.RealName)
	lines := []string{
		bbsName,                            // 1  BBS name
		sysopFirst,                         // 2  sysop first name
		sysopLast,                          // 3  sysop last name
		"COM1",                             // 4  comm port (COM0 = local)
		"38400 BAUD,N,8,1",                 // 5  baud and line settings
		"0",                                // 6  network type (unused)
		userFirst,                          // 7  user first name
		userLast,                           // 8  user last name
		"",                                 // 9  user's location
		"1",                                // 10 graphics: 1=ANSI
		strconv.Itoa(sess.AccessLevel),     // 11 security level
		strconv.Itoa(sess.TimeLeftMinutes), // 12 minutes remaining
		"-1",                               // 13 -1 = FOSSIL (DOSBox-X provides one)
	}
	return writeLines(path, "DORINFO", lines)
}

// writeDoorFileSR writes DOORFILE.SR, the Solar Realms format
// (Barren Realms Elite, Usurper's older versions and others).
func writeDoorFileSR(path string, sess Session) error {
	lines := []string{
		sess.RealName,                      // 1 name or handle
		"1",                                // 2 ANSI
		"1",                                // 3 IBM (CP437) characters
		"24",                               // 4 page length
		"38400",                            // 5 baud rate (0 = local)
		"1",                                // 6 comm port (0 = local)
		strconv.Itoa(sess.TimeLeftMinutes), // 7 minutes remaining
		sess.RealName,                      // 8 real name
	}
	return writeLines(path, "DOORFILE.SR", lines)
}
