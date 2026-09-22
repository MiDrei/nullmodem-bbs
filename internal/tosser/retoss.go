package tosser

import (
	"bytes"
	"time"

	"git.maik.ch/swissmaik/nullmodem/internal/binkp"
	"git.maik.ch/swissmaik/nullmodem/internal/config"
	"git.maik.ch/swissmaik/nullmodem/internal/message"
	"git.maik.ch/swissmaik/nullmodem/internal/netmail"
	"git.maik.ch/swissmaik/nullmodem/internal/user"
)

// RetossFile is one previously-received file (see internal/archive)
// to feed back through tossing via Retoss.
type RetossFile struct {
	Name          string
	Data          []byte
	UplinkAddress string
	UplinkHost    string
}

// Retoss re-tosses one or more archived files' raw bytes together, as
// if they'd just arrived in a single BinkP session -- for the web
// admin's Packet Analyzer to retry something whose original outcome
// wasn't what was expected, without needing a real BinkP session.
// Processing every file in one shared call (rather than one call per
// file) matters specifically for a TIC descriptor and its payload,
// which only toss successfully paired together (see ticSession) --
// selecting both in the admin UI lets Retoss correlate them exactly
// as their original session would have.
//
// allUplinks resolves which uplink's packet/TIC passwords apply,
// matched by the first file's own UplinkHost (every file passed in
// one call is expected to share it, since they arrived together);
// nothing configured for that host means no password is required to
// accept them, mirroring an open node -- the same permissive fallback
// acceptedPacketPasswords/acceptedTICPasswords already have for an
// unmatched primary.
func Retoss(files []RetossFile, allUplinks []config.BinkpUplink, netmailStore *netmail.Store, messages *message.Store, users *user.Store, robot *RobotConfig, tic *TICConfig) (*Result, error) {
	res := &Result{}
	if len(files) == 0 {
		return res, nil
	}

	var matched config.BinkpUplink
	for _, u := range allUplinks {
		if u.Host == files[0].UplinkHost {
			matched = u
			break
		}
	}
	acceptedPackets := acceptedPacketPasswords(matched, allUplinks)

	var ticSess *ticSession
	if tic != nil {
		ticSess = newTICSession(tic.Files, acceptedTICPasswords(matched, allUplinks))
	}

	for _, rf := range files {
		f := binkp.InboundFile{Name: rf.Name, Size: int64(len(rf.Data)), ModTime: time.Now()}
		if err := handleInboundFile(f, bytes.NewReader(rf.Data), acceptedPackets, netmailStore, messages, users, robot, ticSess, res, rf.UplinkAddress, rf.UplinkHost); err != nil {
			return res, err
		}
	}
	if ticSess != nil {
		ticSess.flushUnmatched(res)
	}
	return res, nil
}
