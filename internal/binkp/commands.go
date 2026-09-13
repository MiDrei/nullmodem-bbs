package binkp

// Command is a BinkP command frame's leading byte (FTS-1026 section
// 3.2). Argument strings for each are documented on the constants.
type Command byte

const (
	// MNUL carries free-format informational text ("SYS name", "ZYZ
	// sysop", "LOC location", "VER version", "TIME rfc-ish-date", or
	// "OPT ..." capability flags like the CRAM-MD5 challenge). Purely
	// informational -- a session is valid without any of it.
	MNUL Command = iota
	// MADR carries a space-separated list of this side's FTN
	// addresses, e.g. "1:234/56.0@fidonet 21:1/100@fsxnet".
	MADR
	// MPWD carries either a plaintext password or, when the answerer
	// advertised CRAM-MD5 support, "CRAM-MD5-<hex hmac digest>" (see
	// cram.go). Sent by the originator only.
	MPWD
	// MFILE announces an incoming file: "name size unix-mtime offset"
	// (offset is always 0 here -- this implementation doesn't support
	// resuming a partial transfer).
	MFILE
	// MOK is the answerer's positive response to MPWD.
	MOK
	// MEOB ("end of batch") signals this side has no more files to
	// send. A session ends once both sides have sent and received it.
	MEOB
	// MGOT acknowledges a fully received file: "name size".
	MGOT
	// MERR reports a fatal protocol/auth error; the argument is a
	// human-readable reason. The session must be aborted on receipt.
	MERR
	// MBSY reports the peer is too busy to accept this session right
	// now; the argument is a human-readable reason.
	MBSY
	// MGET requests the peer resume sending a file from an offset --
	// unused by this implementation (no resume support), listed only
	// so readFrame's command byte is exhaustively named.
	MGET
	// MSKIP asks the peer to stop sending the current file -- unused
	// by this implementation, listed for the same reason as MGET.
	MSKIP
)

func (c Command) String() string {
	switch c {
	case MNUL:
		return "M_NUL"
	case MADR:
		return "M_ADR"
	case MPWD:
		return "M_PWD"
	case MFILE:
		return "M_FILE"
	case MOK:
		return "M_OK"
	case MEOB:
		return "M_EOB"
	case MGOT:
		return "M_GOT"
	case MERR:
		return "M_ERR"
	case MBSY:
		return "M_BSY"
	case MGET:
		return "M_GET"
	case MSKIP:
		return "M_SKIP"
	default:
		return "M_UNKNOWN"
	}
}
