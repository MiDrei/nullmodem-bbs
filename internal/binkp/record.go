package binkp

import "fmt"

// SessionRecorder receives one human-readable line per frame exchanged
// during a session (see Config.Recorder), tagged by direction ("send"
// or "recv") -- intended for a permanent, complete session transcript
// a sysop can inspect after the fact instead of needing to add
// temporary debug logging and reproduce a problem again. Frame content
// only, never raw wire bytes: a command frame renders as its command
// name plus argument (e.g. "M_ADR 21:3/194"), a data frame as its byte
// count only ("DATA 4096 bytes") -- file content itself is already
// captured separately (see internal/archive) and would otherwise bloat
// a transcript for no diagnostic benefit. M_PWD's argument is always
// redacted before this is ever called, in both directions -- a
// transcript is meant to be shared when asking someone else for help,
// and a password must never end up sitting in one.
type SessionRecorder interface {
	RecordFrame(direction, line string)
}

// frameLine renders one frame (as readFrame/writeFrame already decode
// it) into the line handed to SessionRecorder.RecordFrame.
func frameLine(isData bool, payload []byte) string {
	if isData {
		return fmt.Sprintf("DATA %d bytes", len(payload))
	}
	if len(payload) == 0 {
		return "(empty command frame)"
	}
	cmd := Command(payload[0])
	arg := string(payload[1:])
	if cmd == MPWD {
		arg = "***"
	}
	if arg == "" {
		return cmd.String()
	}
	return cmd.String() + " " + arg
}
