package bbs

import (
	"strings"

	"github.com/midrei/nullmodem-kit/ansi"

	"github.com/midrei/nullmodem-bbs/internal/user"
)

// New accounts may wait for the sysop's approval (config.Security
// ApproveNewUsers, user.RegisterNew): until then they read, and write
// netmail to the sysop -- nothing else that reaches other people.

// mayPost says whether u may post, use a door or upload, telling them
// why not if they may not.
func (s *Server) mayPost(term *Terminal, u *user.User) (bool, error) {
	if u.Validated {
		return true, nil
	}
	if err := term.Println(ansi.Reset + "\r\n" + ansi.FG(ansi.Yellow, true) + term.T("approval.pending") + ansi.Reset); err != nil {
		return false, err
	}
	return false, s.pauseForKey(term)
}

// isSysop reports whether userID is a local sysop -- the one a pending
// account may write netmail to.
func (s *Server) isSysop(userID int64) bool {
	u, err := s.Users.ByID(userID)
	return err == nil && u.SecurityLevel >= user.SLSysop
}

// blockedHandle reports whether handle is on the sysop's list of
// handles nobody may register.
func (s *Server) blockedHandle(handle string) bool {
	if s.Security == nil {
		return false
	}
	h := strings.ToLower(strings.TrimSpace(handle))
	for _, b := range s.Security().BlockedHandles {
		if strings.ToLower(strings.TrimSpace(b)) == h {
			return true
		}
	}
	return false
}
