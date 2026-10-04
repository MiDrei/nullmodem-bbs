package bbs

import (
	"fmt"
	"git.maik.ch/nullmodem/bbs/internal/textfmt"
	"strconv"
	"strings"

	"git.maik.ch/nullmodem/kit/ansi"
)

// editorCommand identifies one of the classic BBS line-editor slash
// commands recognized by runLineEditor's loop.
type editorCommand int

const (
	editorNone editorCommand = iota
	editorSave
	editorAbort
	editorList
	editorDelete
)

// parseEditorCommand recognizes /S, /A, /L, and /D <n> case-
// insensitively; anything else is ordinary message text to append as
// a new line, matching what a Synchronet/Mystic-style message editor
// accepts.
func parseEditorCommand(line string) (cmd editorCommand, arg string) {
	trimmed := strings.TrimSpace(line)
	switch {
	case strings.EqualFold(trimmed, "/S"):
		return editorSave, ""
	case strings.EqualFold(trimmed, "/A"):
		return editorAbort, ""
	case strings.EqualFold(trimmed, "/L"):
		return editorList, ""
	case len(trimmed) >= 2 && strings.EqualFold(trimmed[:2], "/D"):
		return editorDelete, strings.TrimSpace(trimmed[2:])
	default:
		return editorNone, ""
	}
}

// printEditorHelp shows the line editor's command legend once, right
// after whatever header prompts (Subject, To, ...) precede the body.
func (s *Server) printEditorHelp(term *Terminal) error {
	cmd := ansi.FG(ansi.Cyan, true)
	reset := ansi.Reset
	return term.Println(reset + "\n" + term.T("editor.intro") +
		"\r\n" + cmd + "/S" + reset + " " + term.T("editor.save") + "   " +
		cmd + "/A" + reset + " " + term.T("editor.abort") + "   " +
		cmd + "/L" + reset + " " + term.T("editor.list") + "   " +
		cmd + "/D <n>" + reset + " " + term.T("editor.delete"))
}

// printEditorListing shows the message composed so far, numbered the
// same way as the line prompts, for the /L command.
func (s *Server) printEditorListing(term *Terminal, lines []string) error {
	if len(lines) == 0 {
		return term.Println(ansi.Reset + "\n" + term.T("editor.no_lines"))
	}
	var b strings.Builder
	b.WriteString(ansi.Reset + "\r\n")
	for i, line := range lines {
		fmt.Fprintf(&b, "%s%3d:%s %s\r\n", ansi.FG(ansi.Cyan, true), i+1, ansi.Reset, line)
	}
	return term.Print(b.String())
}

// replySubject prefixes subject with "Re: " for a reply, unless it's
// already a reply (case-insensitively), avoiding "Re: Re: Re: ..."
// pile-ups across a long reply chain.
func replySubject(subject string) string {
	if len(subject) >= 4 && strings.EqualFold(subject[:4], "re: ") {
		return subject
	}
	return "Re: " + subject
}

// runLineEditor is the classic BBS line editor's input loop, shared
// by every message-composing flow (echomail's postMessage, netmail's
// composeNetmail): it builds up a body line by line, with /S to save,
// /A to abort, /L to list what's been entered so far, and /D <n> to
// delete a line. saved is false on /A (lines is nil) so the caller
// can tell "the user aborted" apart from "an empty message" -- /S
// itself refuses to save an empty message and re-prompts instead of
// returning, so a true save always carries at least one line.
//
// initial is what the message starts with -- a reply's quoted original
// (see textfmt.QuoteLines) -- listed first so the caller sees it, and
// editable like typed lines (/D to drop a quoted line).
func (s *Server) runLineEditor(term *Terminal, initial []string) (lines []string, saved bool, err error) {
	if err := s.printEditorHelp(term); err != nil {
		return nil, false, err
	}
	lines = append(lines, initial...)
	if len(lines) > 0 {
		if err := s.printEditorListing(term, lines); err != nil {
			return nil, false, err
		}
	}
	for {
		if err := term.Print(ansi.Reset + fmt.Sprintf("%3d: ", len(lines)+1) + ansi.FG(ansi.Yellow, true)); err != nil {
			return nil, false, err
		}
		input, err := term.ReadLine(false)
		if err != nil {
			return nil, false, err
		}

		cmd, arg := parseEditorCommand(input)
		switch cmd {
		case editorSave:
			if len(lines) == 0 {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("editor.empty")); err != nil {
					return nil, false, err
				}
				continue
			}
			return lines, true, nil

		case editorAbort:
			return nil, false, nil

		case editorList:
			if err := s.printEditorListing(term, lines); err != nil {
				return nil, false, err
			}

		case editorDelete:
			idx, convErr := strconv.Atoi(arg)
			if convErr != nil || idx < 1 || idx > len(lines) {
				if err := term.Println(ansi.Reset + ansi.FG(ansi.Red, true) + term.T("editor.no_such_line")); err != nil {
					return nil, false, err
				}
				continue
			}
			lines = append(lines[:idx-1], lines[idx:]...)
			if err := term.Println(ansi.Reset + ansi.FG(ansi.Green, true) + term.T("editor.line_deleted", "LINE", idx)); err != nil {
				return nil, false, err
			}

		default:
			lines = append(lines, input)
		}
	}
}

// quoteForReply is a reply's starting lines: the quoted original under
// " -=> from wrote to to <=-" (textfmt.QuoteLines), then a blank line.
// Stored text is CP437; it is quoted as UTF-8 (wrapping counts and
// splits characters) and turned back.
func quoteForReply(body, from, to string) []string {
	dec := func(v string) string { return ansi.DecodeCP437([]byte(v)) }
	lines := textfmt.QuoteLines(dec(body), dec(from), dec(to))
	out := make([]string, 0, len(lines)+1)
	for _, l := range lines {
		out = append(out, string(ansi.EncodeCP437(l)))
	}
	return append(out, "")
}
