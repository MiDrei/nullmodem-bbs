package binkp

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"git.maik.ch/nullmodem/bbs/internal/version"
)

// OutboundFile is a file this side offers to send during a session --
// typically an FTS-0001 .pkt bundle, built by internal/mail, handed
// to this package as an opaque byte stream so the protocol layer
// doesn't need to know anything about packet contents.
type OutboundFile struct {
	Name    string
	Size    int64
	ModTime time.Time
	Data    io.Reader
}

// InboundFile describes a file the peer is sending us, announced via
// M_FILE before its data frames arrive.
type InboundFile struct {
	Name    string
	Size    int64
	ModTime time.Time
}

// Config holds one side's session parameters. Both Dial (originator)
// and Answer (answerer) take the same Config shape since most of it
// -- addresses, password, the files offered, how to handle received
// ones -- applies symmetrically.
type Config struct {
	// OurAddresses are the FTN addresses (e.g. "1:234/56.0") this
	// side identifies as. At least one is required.
	OurAddresses []string
	// Password authenticates the session. An originator with a
	// non-empty Password always sends it (as a CRAM-MD5 response if
	// the answerer advertised support, otherwise in the clear); an
	// answerer with a non-empty Password requires and checks it. An
	// empty Password on the answerer side means "open node, no auth"
	// -- unless PasswordForAddresses is set, which takes over instead.
	Password string
	// NoCRAM makes an originator send Password in the clear even when
	// the answerer offers CRAM-MD5 -- only for diagnosing a peer that
	// misbehaves after a CRAM login. Ignored on the answerer side.
	NoCRAM bool
	// PasswordForAddresses, set only on the answerer side, overrides
	// Password once the caller's M_ADR has been read (received before
	// their M_PWD, so this is called in time): it's handed the
	// caller's claimed addresses and returns the password to check
	// their M_PWD against, or ok=false to reject an unrecognized
	// caller outright. Lets one listener serve several known callers
	// (e.g. one per configured uplink, each with its own password)
	// instead of one fixed Password for the whole node. Whether CRAM-
	// MD5 gets advertised is still decided before this can run (no
	// M_ADR yet), based on whether either Password or
	// PasswordForAddresses is set at all.
	PasswordForAddresses func(peerAddrs []string) (password string, ok bool)
	// OutboundFilesForAddresses, set only on the answerer side, is the
	// answerer's counterpart to the originator's static OutboundFiles:
	// an answerer doesn't know which peer is calling (and so what it
	// owes that peer) until PasswordForAddresses has matched it mid-
	// handshake, so its outbound files can't be prepared up front the
	// way an originator's can. Called with the same peerAddrs
	// PasswordForAddresses received, once authentication actually
	// succeeds (immediately before this side's M_OK) -- never for a
	// caller that fails to authenticate, so nothing is offered to
	// someone who didn't get in. Its result becomes OutboundFiles for
	// the rest of this session. Meaningless (never called) unless
	// PasswordForAddresses is also set.
	OutboundFilesForAddresses func(peerAddrs []string) []OutboundFile
	// SysName, Sysop, and Location are sent as informational M_NUL
	// lines (SYS/ZYZ/LOC); all optional.
	SysName, Sysop, Location string

	// OutboundFiles are sent, in order, before this side's M_EOB. On
	// the answerer side, set this directly only for a fixed/static
	// offer known before the session starts (rare -- almost always
	// OutboundFilesForAddresses instead, since an answerer usually
	// serves several possible callers with different outbound mail
	// each); OutboundFilesForAddresses, if also set, overwrites
	// whatever's here once the caller authenticates.
	OutboundFiles []OutboundFile
	// RescanOutboundFiles, if set, is called after every batch this
	// side sends (see FSP-1024 section 4.2, "Re-initialise session
	// after EOB") to check whether anything new should go out in
	// another batch before the session ends -- e.g. mail that got
	// tossed into the outbound queue while this session was already in
	// progress. Only ever called when the peer also advertised
	// binkp/1.1 (see the session-level doc comment on this behavior
	// near runTransfer); a nil result or nil func means "nothing new",
	// which is also exactly OutboundFiles' own zero-batch case, so a
	// caller that doesn't wire this up behaves identically to one that
	// only ever offers a single batch.
	RescanOutboundFiles func() []OutboundFile
	// ReceiveFile is called once per file the peer sends us; it must
	// read r to completion (exactly Size bytes) before returning, or
	// the session aborts. A nil ReceiveFile discards received files.
	ReceiveFile func(f InboundFile, r io.Reader) error
	// Recorder, if set, receives one human-readable line per frame
	// exchanged in either direction (see SessionRecorder's own doc
	// comment) -- nil means no recording, the default for every
	// existing caller/test.
	Recorder SessionRecorder
}

// Result summarizes a completed session.
type Result struct {
	// RemoteAddresses are the FTN addresses the peer claimed via
	// M_ADR. The caller decides whether to trust them for anything
	// beyond logging -- this package doesn't cross-check them against
	// any configured node list.
	RemoteAddresses []string
	// FilesSent are the names of our OutboundFiles the peer
	// acknowledged with M_GOT.
	FilesSent []string
	// FilesReceived are the names of files the peer sent us that were
	// fully received (and accepted by ReceiveFile without error).
	FilesReceived []string
}

type role int

const (
	roleOriginator role = iota
	roleAnswerer
)

// closeGracePeriod bounds how long Dial waits, once a session has
// finished successfully, to see whether the peer closes the
// connection on its own before this side does. A var rather than a
// const so tests can shrink it.
//
// Modeled on binkterm-php's BinkpSession -- a known-working real-
// world implementation this project already treats as a reference
// (see CLAUDE.md) -- which waits (3s, by default) for exactly this
// reason before self-closing. Closing the instant we consider
// ourselves done risks a race where the peer -- typically an
// answerer, like a real hub -- hasn't yet finished reading/
// processing our trailing M_GOT when our end hangs up: observed
// live, a real uplink resent the exact same mail on every single
// poll despite our M_GOT always going out without a write error,
// consistent with the peer never actually registering it. In
// practice this costs nothing against a peer that closes its own end
// promptly (including this package's own Answer side, used
// throughout this package's tests) -- the read below returns io.EOF
// almost immediately and the wait ends early; the full period is
// only ever spent against a peer that lingers.
var closeGracePeriod = 3 * time.Second

// Dial opens an originating (caller) BinkP session to addr
// ("host:port"), runs the full handshake and file transfer, gives the
// peer a brief grace period to close the connection on its own (see
// closeGracePeriod), and closes the connection before returning.
func Dial(ctx context.Context, addr string, cfg Config) (*Result, error) {
	if len(cfg.OurAddresses) == 0 {
		return nil, fmt.Errorf("binkp: dial %s: no OurAddresses configured", addr)
	}
	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		return nil, fmt.Errorf("binkp: dial %s: %w", addr, err)
	}
	defer conn.Close()
	result, err := runSession(ctx, conn, cfg, roleOriginator)
	if err != nil {
		return nil, err
	}
	waitForPeerToCloseFirst(conn)
	return result, nil
}

// waitForPeerToCloseFirst gives the peer up to closeGracePeriod to
// close conn on its own (see closeGracePeriod's doc comment) before
// Dial's own deferred Close runs. Best-effort and silent: if conn
// doesn't support read deadlines, or the peer sends more bytes
// instead of closing, this just returns and lets the caller's Close
// happen as it would have anyway -- the session itself already
// finished successfully by the time this runs, so there's nothing
// left here to interpret or fail on.
func waitForPeerToCloseFirst(conn net.Conn) {
	if err := conn.SetReadDeadline(time.Now().Add(closeGracePeriod)); err != nil {
		return
	}
	var buf [256]byte
	for {
		if _, err := conn.Read(buf[:]); err != nil {
			return
		}
	}
}

// Answer runs an answering (callee) BinkP session over an
// already-accepted connection. It does not close conn.
func Answer(ctx context.Context, conn net.Conn, cfg Config) (*Result, error) {
	if len(cfg.OurAddresses) == 0 {
		return nil, fmt.Errorf("binkp: answer: no OurAddresses configured")
	}
	return runSession(ctx, conn, cfg, roleAnswerer)
}

// runSession watches ctx alongside the handshake/transfer state
// machine, closing conn (aborting any in-flight read/write) if it's
// cancelled before the session finishes on its own.
func runSession(ctx context.Context, conn net.Conn, cfg Config, r role) (*Result, error) {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			conn.Close()
		case <-done:
		}
	}()

	s := &session{conn: conn, cfg: cfg, role: r}
	result, err := s.run()
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return result, nil
}

type session struct {
	conn    net.Conn
	cfg     Config
	role    role
	writeMu sync.Mutex

	// pending holds one frame read during the handshake but not
	// consumed there (see pushback) -- the transfer phase's
	// nextFrame calls must check it before reading the connection.
	pending *pendingFrame

	// peerBinkp11 records whether the peer's own M_NUL "VER ..." line
	// (seen anywhere from the address exchange through the end of
	// authentication -- see noteVersionLine's call sites) ended in
	// "binkp/1.1". Multi-batch behavior (see runTransfer) only ever
	// activates when this is true: a peer that never sends a VER line,
	// or advertises binkp/1.0, gets exactly this package's original
	// single-batch behavior, per FSP-1024's own fallback requirement.
	peerBinkp11 bool

	mu sync.Mutex
	// pendingSent tracks the wire (escaped, see quoteFileName) names of
	// files sent so far in this session that no M_GOT has matched yet.
	// sendOneFile adds an entry right after writing its M_FILE frame;
	// receiveBatch's M_GOT case consumes (deletes) one on a match.
	// Cross-checking against this, rather than trusting any M_GOT's
	// name unconditionally, matches binkd's own GOT()/tfile_cmp()
	// (protocol.c) -- confirmed against its source -- which requires a
	// M_GOT to name a file actually outstanding before crediting it;
	// without this, a peer echoing back a wrong or garbled M_GOT could
	// inflate gotCount/FilesSent for a file that was never actually
	// delivered. Protected by mu since sendOneFile (the sendBatch
	// goroutine) and receiveBatch (the main goroutine) touch it
	// concurrently within the same round.
	pendingSent map[string]bool
	result      Result
}

// noteVersionLine inspects one M_NUL line's argument and records
// whether it's a protocol identification string (FSP-1024 section
// 4.1: M_NUL "VER mailer version binkp/1.1") advertising binkp/1.1,
// case-insensitively per that section's own wording. Harmless no-op
// for every other M_NUL line (SYS/ZYZ/LOC/OPT).
func (s *session) noteVersionLine(arg string) {
	fields := strings.Fields(arg)
	if len(fields) < 2 || !strings.EqualFold(fields[0], "VER") {
		return
	}
	if strings.EqualFold(fields[len(fields)-1], "binkp/1.1") {
		s.peerBinkp11 = true
	}
}

type pendingFrame struct {
	isData  bool
	payload []byte
}

// nextFrame returns a pushed-back frame if one is waiting, else reads
// the next one from the connection -- the single choke point every
// read path funnels through, so this is also the only place a read
// needs recording (a pushed-back frame was already recorded the first
// time it was actually read off the wire).
func (s *session) nextFrame() (isData bool, payload []byte, err error) {
	if s.pending != nil {
		f := s.pending
		s.pending = nil
		return f.isData, f.payload, nil
	}
	isData, payload, err = readFrame(s.conn)
	if err == nil && s.cfg.Recorder != nil {
		s.cfg.Recorder.RecordFrame("recv", frameLine(isData, payload))
	}
	return isData, payload, err
}

// writeCommandFrame wraps the package-level function of the same name
// with recording -- w is usually s.conn directly, but sendInfoAndAddress
// also uses this to build a batched write into a bytes.Buffer, so the
// frame is still recorded once per logical frame regardless of how
// many end up combined into one underlying Write.
func (s *session) writeCommandFrame(w io.Writer, cmd Command, arg string) error {
	if err := writeCommandFrame(w, cmd, arg); err != nil {
		return err
	}
	if s.cfg.Recorder != nil {
		s.cfg.Recorder.RecordFrame("send", frameLine(false, append([]byte{byte(cmd)}, arg...)))
	}
	return nil
}

// writeDataFrame is writeCommandFrame's data-frame counterpart.
func (s *session) writeDataFrame(w io.Writer, data []byte) error {
	if err := writeDataFrame(w, data); err != nil {
		return err
	}
	if s.cfg.Recorder != nil {
		s.cfg.Recorder.RecordFrame("send", frameLine(true, data))
	}
	return nil
}

// pushback stashes a frame that was read to decide something (e.g.
// "is the peer sending a password?") but turned out to belong to a
// later phase, so the next nextFrame call returns it instead of
// reading past it.
func (s *session) pushback(isData bool, payload []byte) {
	s.pending = &pendingFrame{isData: isData, payload: payload}
}

func (s *session) run() (*Result, error) {
	var err error
	if s.role == roleOriginator {
		err = s.originatorHandshake()
	} else {
		err = s.answererHandshake()
	}
	if err != nil {
		return nil, fmt.Errorf("binkp: handshake: %w", err)
	}
	return s.runTransfer()
}

func (s *session) send(cmd Command, arg string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeCommandFrame(s.conn, cmd, arg)
}

func (s *session) sendData(data []byte) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeDataFrame(s.conn, data)
}

// sendInfoAndAddress emits this side's informational M_NUL lines
// (VER/SYS/ZYZ/LOC -- none required by the protocol; a real peer logs
// them but doesn't act on them, so a missing SysName/Sysop/Location is
// harmless) immediately followed by M_ADR, as a single underlying
// Write call rather than one Write per frame.
//
// This matters: a real peer's own log (Mystic BBS, SysopNet) showed
// it sometimes receiving only our very first frame (VER) and nothing
// else at all -- "Client did not send address" -- immediately before
// dropping the connection, while an otherwise-identical dial moments
// later succeeded and logged every line (VER, SYS, ZYZ, then a
// correctly matched M_ADR). Four small separate Writes for one
// logical "here's who I am" burst apparently sometimes arrive as more
// TCP segments than that peer's own read loop keeps reading across
// before giving up -- not something we can fix on their end, but
// collapsing our side of it into one Write removes our own
// contribution to the problem regardless of whose behavior is
// technically "correct".
func (s *session) sendInfoAndAddress(ourAddresses []string) error {
	lines := []string{"VER NullModem-BinkP/" + version.Short() + " binkp/1.1"}
	if s.cfg.SysName != "" {
		lines = append(lines, "SYS "+s.cfg.SysName)
	}
	if s.cfg.Sysop != "" {
		lines = append(lines, "ZYZ "+s.cfg.Sysop)
	}
	if s.cfg.Location != "" {
		lines = append(lines, "LOC "+s.cfg.Location)
	}

	var buf bytes.Buffer
	for _, l := range lines {
		if err := s.writeCommandFrame(&buf, MNUL, l); err != nil {
			return err
		}
	}
	if err := s.writeCommandFrame(&buf, MADR, strings.Join(ourAddresses, " ")); err != nil {
		return err
	}

	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := s.conn.Write(buf.Bytes()); err != nil {
		return fmt.Errorf("binkp: write frame: %w", err)
	}
	return nil
}

// readHandshakeFrames reads command frames until it finds the peer's
// M_ADR, collecting a CRAM-MD5 challenge from any M_NUL "OPT ..." line
// seen along the way and tolerating (ignoring) anything else --
// M_ERR/M_BSY abort immediately since neither side can proceed
// without the other's address.
func (s *session) readHandshakeFrames() (peerAddrs, cramChallenge string, err error) {
	for {
		isData, payload, err := s.nextFrame()
		if err != nil {
			return "", "", err
		}
		if isData {
			return "", "", fmt.Errorf("unexpected data frame during handshake")
		}
		if len(payload) == 0 {
			return "", "", fmt.Errorf("empty command frame during handshake")
		}
		cmd := Command(payload[0])
		arg := string(payload[1:])
		switch cmd {
		case MNUL:
			s.noteVersionLine(arg)
			if ch, ok := parseCRAMChallenge(arg); ok {
				cramChallenge = ch
			}
		case MADR:
			return arg, cramChallenge, nil
		case MERR:
			return "", "", fmt.Errorf("peer reported error: %s", arg)
		case MBSY:
			return "", "", fmt.Errorf("peer busy: %s", arg)
		}
	}
}

// originatorHandshake is the caller's side: send our info/address,
// learn the answerer's address (and CRAM-MD5 challenge, if any), then
// authenticate if we have a password configured.
func (s *session) originatorHandshake() error {
	if err := s.sendInfoAndAddress(s.cfg.OurAddresses); err != nil {
		return err
	}

	peerAddrs, challenge, err := s.readHandshakeFrames()
	if err != nil {
		return err
	}
	s.result.RemoteAddresses = strings.Fields(peerAddrs)

	if s.cfg.Password == "" {
		return nil
	}

	pwArg := s.cfg.Password
	if challenge != "" && !s.cfg.NoCRAM {
		pwArg = cramOptPrefix + cramDigest(s.cfg.Password, challenge)
	}
	if err := s.send(MPWD, pwArg); err != nil {
		return err
	}
	return s.readPasswordResponse()
}

// readPasswordResponse reads command frames following our M_PWD until
// it finds M_OK or M_ERR, tolerating (ignoring) any M_NUL in between
// -- some real answerers (e.g. binkd-derived implementations) send
// their SYS/ZYZ/LOC/VER informational lines interleaved with or after
// the auth exchange rather than strictly before it, not just during
// the address exchange readHandshakeFrames already tolerates.
func (s *session) readPasswordResponse() error {
	for {
		isData, payload, err := s.nextFrame()
		if err != nil {
			return err
		}
		if isData || len(payload) == 0 {
			return fmt.Errorf("malformed response to M_PWD")
		}
		cmd, arg := Command(payload[0]), string(payload[1:])
		switch cmd {
		case MNUL:
			s.noteVersionLine(arg)
			continue
		case MOK:
			return nil
		case MERR:
			return fmt.Errorf("authentication failed: %s", arg)
		default:
			return fmt.Errorf("unexpected response to M_PWD: %s %q", cmd, arg)
		}
	}
}

// answererHandshake is the callee's side: optionally advertise
// CRAM-MD5, send our info/address, learn the caller's address, then
// -- if we require a password -- validate its M_PWD.
func (s *session) answererHandshake() error {
	requiresAuth := s.cfg.Password != "" || s.cfg.PasswordForAddresses != nil
	var challenge string
	if requiresAuth {
		c, err := generateChallenge()
		if err != nil {
			return err
		}
		challenge = c
		if err := s.send(MNUL, "OPT "+cramOptPrefix+challenge); err != nil {
			return err
		}
	}
	if err := s.sendInfoAndAddress(s.cfg.OurAddresses); err != nil {
		return err
	}

	peerAddrs, _, err := s.readHandshakeFrames()
	if err != nil {
		return err
	}
	s.result.RemoteAddresses = strings.Fields(peerAddrs)

	// PasswordForAddresses, when set, overrides the static Password
	// now that the caller's claimed addresses are known -- see its
	// doc comment. An unrecognized caller is rejected outright, before
	// even checking whether it sent an M_PWD at all.
	expectedPassword := s.cfg.Password
	passwordRequired := s.cfg.Password != ""
	if s.cfg.PasswordForAddresses != nil {
		pw, ok := s.cfg.PasswordForAddresses(s.result.RemoteAddresses)
		if !ok {
			_ = s.send(MERR, "unrecognized caller")
			return fmt.Errorf("peer address not recognized: %v", s.result.RemoteAddresses)
		}
		expectedPassword = pw
		passwordRequired = pw != ""
	}

	// Whether the caller sends M_PWD at all is its own decision (it
	// might have no password configured for us even though we'd
	// accept one, or vice versa) -- peek the next frame rather than
	// assuming, and push it back if it turns out to belong to the
	// transfer phase instead. Skip over any M_NUL first: some real
	// callers interleave their own informational lines with or after
	// M_ADR, before actually sending M_PWD (see readPasswordResponse's
	// doc comment for the symmetric case on the originator side).
	var isData bool
	var payload []byte
	for {
		isData, payload, err = s.nextFrame()
		if err != nil {
			return err
		}
		if !isData && len(payload) > 0 && Command(payload[0]) == MNUL {
			s.noteVersionLine(string(payload[1:]))
			continue
		}
		break
	}
	isPWD := !isData && len(payload) > 0 && Command(payload[0]) == MPWD
	if !isPWD {
		if passwordRequired {
			_ = s.send(MERR, "password required")
			return fmt.Errorf("peer did not authenticate")
		}
		// Open node and the caller sent no password -- whatever this
		// frame is belongs to the transfer phase.
		s.pushback(isData, payload)
		return nil
	}

	arg := string(payload[1:])
	var authOK bool
	switch {
	case !passwordRequired:
		// Open node: accept a password we don't require rather than
		// rejecting a caller that sent one unprompted.
		authOK = true
	default:
		if digest, ok := parseCRAMResponse(arg); ok {
			authOK = digest == cramDigest(expectedPassword, challenge)
		} else {
			authOK = arg == expectedPassword
		}
	}
	if !authOK {
		_ = s.send(MERR, "authentication failed")
		return fmt.Errorf("peer authentication failed")
	}
	if s.cfg.OutboundFilesForAddresses != nil {
		s.cfg.OutboundFiles = s.cfg.OutboundFilesForAddresses(s.result.RemoteAddresses)
	}
	return s.send(MOK, "")
}

// runTransfer sends and receives one or more batches, each a round of
// OutboundFiles (or a rescan's result) sent concurrently with reading
// whatever the peer sends, ending in M_EOB on both sides.
//
// A binkp/1.0 peer (or one that never sent a VER line at all -- see
// noteVersionLine) gets exactly this package's original behavior:
// one round, done as soon as both sides have reached M_EOB and every
// file this side offered has been M_GOT-acknowledged.
//
// Against a peer that also advertised binkp/1.1, FSP-1024 section 4.2
// applies instead: after a round ends, this side checks
// RescanOutboundFiles for anything new and, if there is any, sends
// another round rather than closing -- and, symmetrically, keeps
// answering the peer's own further rounds for as long as it keeps
// sending files. Only once a round comes back "empty" on both sides
// at once (this side had nothing new to send AND the peer's own round
// carried no M_FILE) does the session actually end, per the spec's
// "only an empty batch ends the session". Without this, a real
// binkp/1.1 peer that queues fresh mail mid-session and starts a
// second batch would find this side already gone -- the same
// interoperability bug documented and fixed for ENiGMA-BBS in
// https://github.com/NuSkooler/enigma-bbs/pull/730 ("sessions close
// instead of timing out").
func (s *session) runTransfer() (*Result, error) {
	gotCount := 0
	expectedGot := 0
	files := s.cfg.OutboundFiles

	for {
		expectedGot += len(files)

		sendErrCh := make(chan error, 1)
		go func(files []OutboundFile) { sendErrCh <- s.sendBatch(files) }(files)

		peerFilesThisRound, peerClosed, recvErr := s.receiveBatch(&gotCount, expectedGot)
		sendErr := <-sendErrCh

		if recvErr != nil {
			return nil, fmt.Errorf("binkp: receiving: %w", recvErr)
		}
		if sendErr != nil {
			return nil, fmt.Errorf("binkp: sending: %w", sendErr)
		}

		weSentAnything := len(files) > 0
		if peerClosed || !s.peerBinkp11 || (!weSentAnything && peerFilesThisRound == 0) {
			return &s.result, nil
		}
		files = s.rescanOutboundFiles()
	}
}

func (s *session) sendBatch(files []OutboundFile) error {
	for _, f := range files {
		if err := s.sendOneFile(f); err != nil {
			return err
		}
	}
	return s.send(MEOB, "")
}

// rescanOutboundFiles calls Config.RescanOutboundFiles if set, else
// reports no new files -- the same "nothing to add" result a caller
// that never wires up multi-batch mail would get anyway.
func (s *session) rescanOutboundFiles() []OutboundFile {
	if s.cfg.RescanOutboundFiles == nil {
		return nil
	}
	return s.cfg.RescanOutboundFiles()
}

// sendOneFile holds writeMu for the file's entire M_FILE-plus-data-
// frames sequence, not just one frame at a time: the receive loop's
// M_GOT acknowledgments (for files the peer already finished sending
// us) share the same connection and mutex, and a command frame like
// M_GOT slipping in between two data frames of an in-progress file
// would break receiveOneFile's "read only data frames until Size
// bytes arrive" assumption on the peer's end.
func (s *session) sendOneFile(f OutboundFile) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()

	wireName := quoteFileName(f.Name)
	arg := fmt.Sprintf("%s %d %d 0", wireName, f.Size, f.ModTime.Unix())
	if err := s.writeCommandFrame(s.conn, MFILE, arg); err != nil {
		return err
	}
	s.mu.Lock()
	if s.pendingSent == nil {
		s.pendingSent = make(map[string]bool)
	}
	s.pendingSent[wireName] = true
	s.mu.Unlock()

	buf := make([]byte, maxFrameLen)
	var sent int64
	for {
		n, err := f.Data.Read(buf)
		if n > 0 {
			if werr := s.writeDataFrame(s.conn, buf[:n]); werr != nil {
				return werr
			}
			sent += int64(n)
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("read outbound file %s: %w", f.Name, err)
		}
	}
	if sent != f.Size {
		return fmt.Errorf("outbound file %s: sent %d bytes, want %d", f.Name, sent, f.Size)
	}
	return nil
}

// receiveBatch reads command frames for one batch/round, dispatching
// M_FILE to receiveOneFile and recording M_GOT acknowledgments
// against the cumulative counters *gotCount/expectedGot (cumulative
// across every round so far -- see runTransfer), until this round is
// over: the peer has sent M_EOB (no more files coming from them this
// round) AND every file offered in this round or any earlier one has
// been M_GOT-acknowledged. M_GOT for this round's last file(s)
// commonly arrives *after* the peer's own M_EOB for this round --
// M_EOB only means "I have nothing more to send this batch", not "I'm
// done acknowledging what you sent me" -- so stopping on M_EOB alone
// would drop trailing acknowledgments and, worse, let Dial's deferred
// conn.Close race the peer still writing them. peerFiles reports how
// many M_FILEs the peer sent during this specific round (0 means an
// empty batch on their side, the multi-batch termination signal
// runTransfer checks for).
//
// It tolerates stray handshake-phase commands (M_NUL/M_ADR/M_PWD/
// M_OK) arriving late, since real implementations occasionally resend
// them. It also tolerates a peer that closes the connection instead
// of sending a formal M_EOB once it has nothing left to send --
// observed live against a real uplink that does exactly this right
// after its last file -- but only once every file offered so far has
// already been acknowledged; an EOF while our own M_GOT is still
// outstanding is a real failure, not an implicit M_EOB, and still
// surfaces as an error. Either way, an EOF-tolerated end reports
// peerClosed=true so runTransfer knows not to attempt another round
// over a connection that's already gone.
func (s *session) receiveBatch(gotCount *int, expectedGot int) (peerFiles int, peerClosed bool, err error) {
	peerEOB := false

	for {
		if peerEOB && *gotCount >= expectedGot {
			return peerFiles, false, nil
		}

		isData, payload, err := s.nextFrame()
		if err != nil {
			if err == io.EOF && *gotCount >= expectedGot {
				return peerFiles, true, nil
			}
			return peerFiles, false, err
		}
		if isData {
			return peerFiles, false, fmt.Errorf("unexpected data frame outside a file transfer")
		}
		if len(payload) == 0 {
			return peerFiles, false, fmt.Errorf("empty command frame")
		}
		cmd, arg := Command(payload[0]), string(payload[1:])
		switch cmd {
		case MFILE:
			aborted, err := s.receiveOneFile(arg)
			if err != nil {
				return peerFiles, false, err
			}
			if !aborted {
				peerFiles++
			}
		case MGOT:
			// A peer that names a file we never actually have
			// outstanding (garbled response, stray duplicate, or a
			// bare M_GOT with no argument at all) must not be
			// trusted blindly -- see pendingSent's own doc comment.
			fields := strings.Fields(arg)
			if len(fields) == 0 {
				return peerFiles, false, fmt.Errorf("malformed M_GOT argument %q", arg)
			}
			wireName := fields[0]
			s.mu.Lock()
			matched := s.pendingSent[wireName]
			if matched {
				delete(s.pendingSent, wireName)
				s.result.FilesSent = append(s.result.FilesSent, dequoteFileName(wireName))
			}
			s.mu.Unlock()
			if matched {
				*gotCount++
			}
		case MEOB:
			peerEOB = true
		case MERR:
			return peerFiles, false, fmt.Errorf("peer reported error: %s", arg)
		case MBSY:
			return peerFiles, false, fmt.Errorf("peer busy: %s", arg)
		case MNUL, MADR, MPWD, MOK:
			// Stray/duplicate handshake frame after the handshake
			// phase -- nothing to do.
		}
	}
}

// receiveOneFile receives one file following its M_FILE announcement.
// aborted is true only for the one case binkd itself tolerates rather
// than treating as a session-fatal error (confirmed against its
// source, protocol.c's EOB()): the peer sends M_EOB before this file
// finished, "due to remote bug" in binkd's own words. binkd discards
// the partial file and continues the session instead of dropping it
// entirely; we do the same, pushing the M_EOB back (see pushback) so
// receiveBatch's own loop processes it as this round's actual end
// marker rather than losing it. Any other command frame arriving
// mid-transfer is still a hard error, exactly as before.
func (s *session) receiveOneFile(arg string) (aborted bool, err error) {
	fields := strings.Fields(arg)
	if len(fields) < 2 {
		return false, fmt.Errorf("malformed M_FILE argument %q", arg)
	}
	// wireName is what M_FILE actually carried on the wire and what we
	// must echo back verbatim in M_GOT (binkd's own GOT()-sending code
	// echoes state->in.netname, the still-escaped name, never a
	// dequoted one); name is the real filename, escaping reversed (see
	// quoteFileName/dequoteFileName's doc comments), for everything
	// this package exposes to a caller.
	wireName := fields[0]
	name := dequoteFileName(wireName)
	size, err := strconv.ParseInt(fields[1], 10, 64)
	if err != nil {
		return false, fmt.Errorf("malformed M_FILE size in %q: %w", arg, err)
	}
	var modTime time.Time
	var modTimeUnix int64
	if len(fields) >= 3 {
		if unixTime, err := strconv.ParseInt(fields[2], 10, 64); err == nil {
			modTime = time.Unix(unixTime, 0)
			modTimeUnix = unixTime
		}
	}

	pr, pw := io.Pipe()
	doneCh := make(chan error, 1)
	go func() {
		if s.cfg.ReceiveFile != nil {
			doneCh <- s.cfg.ReceiveFile(InboundFile{Name: name, Size: size, ModTime: modTime}, pr)
		} else {
			_, err := io.Copy(io.Discard, pr)
			doneCh <- err
		}
	}()

	var received int64
	for received < size {
		isData, payload, err := s.nextFrame()
		if err != nil {
			pw.CloseWithError(err)
			<-doneCh
			return false, err
		}
		if !isData {
			if len(payload) > 0 && Command(payload[0]) == MEOB {
				pw.CloseWithError(fmt.Errorf("peer sent M_EOB before finishing %s (%d/%d bytes)", name, received, size))
				<-doneCh
				s.pushback(isData, payload)
				return true, nil
			}
			pw.CloseWithError(fmt.Errorf("unexpected command frame mid-file"))
			<-doneCh
			return false, fmt.Errorf("unexpected command frame mid-transfer of %s", name)
		}
		if _, err := pw.Write(payload); err != nil {
			<-doneCh
			return false, fmt.Errorf("deliver received data for %s: %w", name, err)
		}
		received += int64(len(payload))
	}
	pw.Close()

	// Acknowledge with M_GOT as soon as the transfer itself is verified
	// complete (exactly size bytes received), before waiting on our own
	// (potentially slow -- packet parsing, per-message DB writes)
	// ReceiveFile processing. Per FTS-1026, the sender keeps a file in
	// its PendingFiles list -- and is required to keep the connection
	// open -- until it sees our M_GOT; delaying our send behind local
	// processing only widens the window in which an impatient peer
	// (observed live: closes the TCP connection right after its last
	// data frame instead of waiting for M_GOT) can hang up before our
	// acknowledgment reaches it. M_GOT is a transport-layer "these bytes
	// arrived intact" receipt, not an application-layer "I filed this
	// away successfully" signal, so acking it before ReceiveFile
	// returns is correct even if ReceiveFile then fails.
	//
	// M_GOT carries three fields -- name, size, AND the same timestamp
	// M_FILE announced -- not just name and size: confirmed against
	// binkd's own source (protocol.c's GOT()/tfile_cmp()), which
	// requires exactly 3 arguments and matches all three against the
	// outbound file record, and against binkterm-php's own M_GOT
	// construction (this project's reference implementation, see
	// CLAUDE.md). A 2-field M_GOT fails binkd's parse_msg_args
	// entirely, which replies M_ERR and aborts the session -- a very
	// plausible explanation for a peer's mail never registering as
	// delivered and for a connection dying right after we've
	// acknowledged a file, both observed live against a real uplink
	// before this fix.
	if err := s.send(MGOT, fmt.Sprintf("%s %d %d", wireName, size, modTimeUnix)); err != nil {
		<-doneCh
		return false, err
	}

	if err := <-doneCh; err != nil {
		return false, fmt.Errorf("handling received file %s: %w", name, err)
	}

	s.mu.Lock()
	s.result.FilesReceived = append(s.result.FilesReceived, name)
	s.mu.Unlock()

	return false, nil
}
