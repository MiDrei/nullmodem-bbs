# Adding a door game

`internal/doors` runs external "door" programs and bridges them to a
caller's connection the same way `internal/zmodem` bridges a file
transfer: the door process gets handed the raw connection, not this
project's own line-oriented/ANSI-cooked `Terminal`.

## How it works

Two kinds of door are supported (`Door.Kind` / `configs/bbs.yaml`'s
`kind:`), both ultimately handing the door process an already-
connected `AF_UNIX` socket at fd 3 via Go's `exec.Cmd.ExtraFiles` --
the door reads/writes that socket directly, the same way it would an
inherited descriptor under Mystic or ENiGMA½ on Linux (their own docs
call this exact trick something Node.js *can't* do without an
external bridge process; Go does it natively):

- **`native`** (the default): a door with its own native Linux/
  Windows port that reads a [DOOR32.SYS](https://raw.githubusercontent.com/NuSkooler/ansi-bbs/master/docs/dropfile_formats/door32_sys.txt)
  dropfile's socket-handle field as a raw inherited file descriptor --
  that's what the format was invented for. Usurper's Linux port (see
  below) is the reference example.
- **`dosbox`**: a classic real-mode DOS door, run under DOSBox-X.
  DOSBox-X's own nullmodem serial backend takes the *same* inherited
  fd via its `-socket N` command-line flag plus `inhsocket:1` in its
  `[serial]` config, and bridges it straight to the DOS guest's COM1 --
  the DOS door itself never knows its "modem" is actually a Unix
  socket this process created. See "Classic DOS doors via DOSBox-X"
  below for why this specific mechanism, not DOSBox-X's plain TCP-based
  nullmodem modes, is what actually works.

## Configuring a door

### `native` kind

```yaml
doors:
    - name: Usurper
      exe: /absolute/or/relative/path/to/USURPER.EXE
      dir: /absolute/or/relative/path/to/usurper/install/dir
      min_sl: 0
```

`exe` may be relative to the BBS daemon's own working directory (it's
resolved to an absolute path internally before the child process's
own working directory, `dir`, is applied -- a plain relative `exe`
combined with `dir` set to the same directory would otherwise have
the child looking for its own executable doubled up under itself,
e.g. `.../usurper/usurper/USURPER.EXE`).

### `dosbox` kind

```yaml
doors:
    - name: DOS Shell (Doorway)
      kind: dosbox
      dosbox_dir: /absolute/or/relative/path/to/the/doors/own/install/dir
      dosbox_launch_cmd: DOORWAY\DOORWAYU SYS /g:on /a:on /m:100 /v:d^U /s:{dropfile_dir} /c:dos
      min_sl: 200  # this one drops callers into a real DOS shell -- see below
```

`dosbox_dir` is mounted as `C:` in the DOSBox-X guest and
`dosbox_launch_cmd` is run from it, exactly as a sysop would type it
at the DOS prompt (`CALL START.BAT` for a door with its own batch
file, or a direct EXE invocation with whatever switches it needs).
Internal/doors writes the drop file (the full 52-line `DOOR.SYS`
unless `dropfile:` says otherwise -- see below) into a fresh
per-session scratch directory and mounts *that* as `D:` -- the literal
placeholder `{dropfile_dir}` in `dosbox_launch_cmd` is replaced with
that mount's DOS path (`D:\`) before launch, since most classic doors
take their drop file's directory via a command-line switch (DOORWAY's
`/s:` above) rather than a fixed convention. `{dropfile}` is the drop
file's full DOS path (e.g. `D:\DOOR.SYS`) and `{node}` the caller's
node number. A launch command may span several lines, run one after
another like a batch file (OO2 needs its `OOINFO` converter first).

### Drop files, door directory, lock files

These apply to both kinds:

```yaml
    - name: Legend of the Red Dragon
      kind: dosbox
      dosbox_dir: data/doors/lord
      dosbox_launch_cmd: CALL START.BAT {node}
      dropfile: dorinfo           # door.sys | dorinfo | doorfile.sr | door32.sys
      dropfile_in_door_dir: true  # LORD looks for DORINFO1.DEF in its own directory
      lock_files: []              # e.g. [OONODE.DAT]: cleared before start when nobody else plays
```

- `dropfile` picks the format; empty means the kind's default
  (`DOOR32.SYS` for `native`, which is always written since it carries
  the socket handle, `DOOR.SYS` for `dosbox`). All DOS formats are
  written CRLF-terminated with the caller's handle, security level,
  node, call count and this board's own name and sysop:
  - `door.sys` -- the full 52-line GAP/Wildcat! `DOOR.SYS`.
  - `dorinfo` -- `DORINFO1.DEF` (QuickBBS/RBBS/Remote Access), plus
    `DORINFO<node>.DEF` for nodes above 1 (`A`-`Z` for 10-35).
  - `doorfile.sr` -- Solar Realms' `DOORFILE.SR`.
  - `door32.sys` -- `DOOR32.SYS`.
- `dropfile_in_door_dir` also writes it into the door's own directory,
  for doors that look for it there. Several nodes playing at once
  overwrite each other's copy there, except `DORINFO<node>.DEF` -- so
  for multi-node play prefer a door that takes a path, or the
  per-node DORINFO name.
- `lock_files` (relative to the door's directory, matched without
  regard to case) are deleted before the door starts, but only when
  no other caller is in that door at the moment -- a crashed session's
  stale node lock no longer locks everyone out, while a live one stays.

The bbs daemon re-reads the door list from `bbs.yaml` every time a
caller opens the doors menu, so edits take effect without a restart.

## Web admin: Doors page and templates

**Admin → Doors** lists, adds, edits and removes doors (removing only
takes a door off the menu; its files stay). Below the list are
ready-made templates (`internal/doors/templates.go`) for well-known
doors, with each one's launch command, drop file and quirks worked
out:

| Template | Install | Notes |
| --- | --- | --- |
| Immortal Barons (MIT, native) | downloaded from GitHub | Barren Realms Elite remake; `door.json` set to DOOR32.SYS and the world created with default settings (`-reset-from-config`); `ansi16` on |
| Usurper Reborn (GPL-2.0, native) | downloaded from GitHub (~55 MB) | runs with `stdio` (see below) |
| Usurper (GPL-2.0, native) | downloaded from GitHub | Rick Parrish's Linux build; `USURPER.CFG`/`USURP.CTL` from its samples (`BBSTYPE DOOR32`), then EDITOR's "Reset Game" driven over a pseudo-terminal; `NODE/ONLINERS.DAT` as lock file |
| Judge Dredd (MIT) | downloaded from GitHub | `JUDGE.CTL` and `DATA/REG.DAT` are set to this board's name and sysop |
| Legend of the Red Dragon | by hand | `DORINFO1.DEF` in the door dir; run `LORDCFG` once |
| TradeWars 2002 | by hand | `DORINFO1.DEF` in the door dir; run its setup once |
| Operation: Overkill II | by hand | `OOINFO` + `OOII`; `OONODE.DAT`/`BBSINFO.OO` cleared as lock files |
| DoorMUD | by hand | `DMUD.EXE -n {node} -d {dropfile_dir}` |

Native templates pick the build for the machine's architecture
(amd64, and arm64 where the door publishes one); a template without a
build for it can't be installed there. All four downloadable doors
were verified end to end here: installed from the page, played over
Telnet.

### Native doors: arguments and standard I/O

A native door's `args` may use `{dropfile}` (the absolute path of
DOOR32.SYS), `{dropfile_dir}` and `{node}`. Without any placeholder the
old behaviour stays: `/P<dropfile dir>/` is appended, Usurper's switch.

`stdio: true` also connects the door's stdin/stdout to the caller's
connection, the way Synchronet runs doors, and leaves the BBS's telnet
layer handling the protocol (as for `dosbox` doors). Usurper Reborn
needs it: it switches to standard I/O by itself as soon as its output
is redirected, which it always is here. A door on standard I/O writes bare LFs,
trusting a terminal driver to add the CR; the BBS adds it instead
(otherwise every line starts where the previous one ended).

### 16 colours

`ansi16: true` rewrites a door's 256-colour and true-colour SGR codes
(and the aixterm bright colours 90-97/100-107) into the 16 classic ANSI
colours on the way out. Classic BBS terminals (SyncTERM, MuffinTerm)
don't know `ESC[38;5;nm` and read it as separate attributes -- `5` is
blink -- which turns Immortal Barons' 256-colour title art into
stripes. Colours are matched by hue and brightness rather than raw
distance (the VGA palette's bright colours are washed out, so a vivid
red would otherwise come out dark); backgrounds get the eight
non-bright colours only. The Immortal Barons template turns it on.

Doors are installed into `bbs.doors_dir` (default `data/doors`), one
directory each. A downloadable template is fetched, unpacked (only
regular files, never outside its directory, size-limited) into a
scratch directory and moved into place only once complete; an existing,
non-empty door directory is never overwritten. For the others the page
creates the empty directory -- unpack the game there yourself. Any
one-time setup a door needs (running its config program) is done from
the "DOS Shell (Doorway)" door; each template says what.

The templates were adapted from
[thewebexpert/bbs-door-server](https://github.com/thewebexpert/bbs-door-server)'s
launchers, minus the BNU FOSSIL driver (DOSBox-X brings its own, see
below).

Doors don't ship in this repo (their assets are 5+ MB DOS-era binaries
and game data, and one of them -- Usurper -- needs building from
source anyway) -- install one under `data/doors/<name>/` (gitignored
runtime state, same as everything else under `data/`) and point
`configs/bbs.yaml` at it.

## Usurper (the reference door this was built and tested against)

[Usurper](https://github.com/rickparrish/Usurper) is a 1993 BBS door
game, GPL-licensed since 2004, with an actively maintained
Linux/Windows port built with Lazarus/Free Pascal -- its `COMM.PAS`
(`{$IFDEF UNIX} {$DEFINE COMM_SOCKET} {$ENDIF}`) is exactly the
DOOR32.SYS-socket comm layer this package targets, confirmed by
reading the actual source (`fpSend`/`fpRecv` on the raw fd number from
the dropfile's comm-handle field) rather than assuming.

### Build

Needs a Free Pascal/Lazarus toolchain: `sudo apt-get install -y fpc
lazarus` (Debian/Ubuntu).

```sh
git clone --depth 1 https://github.com/rickparrish/Usurper.git
cd Usurper
mkdir -p bin/x86_64-linux obj/x86_64-linux
fpc -B -Tlinux -Px86_64 -Mtp -Scgi -CX -O3 -Xs -XX -l -vewnhibq \
  -FiSOURCE/USURPER -FiSOURCE/COMMON -Fiobj/x86_64-linux \
  -FuSOURCE/COMMON -FUobj/x86_64-linux/ -FEbin/x86_64-linux/ \
  -obin/x86_64-linux/USURPER.EXE SOURCE/USURPER/USURPER.PAS
# EDITOR.EXE (needed once, for initial setup below) builds the same way,
# swapping USURPER for EDITOR throughout:
fpc -B -Tlinux -Px86_64 -Mtp -Scgi -CX -O3 -Xs -XX -l -vewnhibq \
  -FiSOURCE/EDITOR -FiSOURCE/COMMON -Fiobj/x86_64-linux \
  -FuSOURCE/COMMON -FUobj/x86_64-linux/ -FEbin/x86_64-linux/ \
  -obin/x86_64-linux/EDITOR.EXE SOURCE/EDITOR/EDITOR.PAS
```

(This is the same set of flags `build.ps1` in the repo uses for its
own `x86_64-linux` target, translated from its Windows-hosted
`fpc.exe` invocation to a plain Linux `fpc`.)

### Install and one-time setup

```sh
mkdir -p data/doors/usurper
cp -r Usurper/RELEASE/* data/doors/usurper/
cp Usurper/bin/x86_64-linux/USURPER.EXE Usurper/bin/x86_64-linux/EDITOR.EXE data/doors/usurper/
cd data/doors/usurper
cp SAMPLES/USURPER.CFG .   # a ready-to-use sample sysop config
```

Usurper refuses to run until its `DATA/*.DAT` files exist. `EDITOR.EXE`
is a full-screen (Turbo-Vision-style) console app -- run it in a real
terminal, choose **Reset Game** from the main menu, confirm twice
("Reset Usurper?" then "Are you really sure?"), wait for "Usurper has
been RESET!", then **Quit**:

```sh
./EDITOR.EXE
```

Finally, tell it to use DOOR32.SYS as its dropfile format -- this is a
plain-text file, no need to go through EDITOR.EXE for it:

```sh
cat > USURP.CTL << 'EOF'
SYSOPFIRST Your
SYSOPLAST Name
BBSNAME Your BBS Name
BBSTYPE DOOR32
EOF
```

That's the whole install -- `internal/doors.Run` handles the dropfile
and socket per session from here.

## Classic DOS doors via DOSBox-X

### DOSBox-X's plain TCP-based nullmodem is broken on Linux -- don't use it

`serial1=nullmodem server:1 port:N` (DOSBox-X as the TCP listener) was
found to hang DOSBox-X's entire boot sequence indefinitely on this
platform (DOSBox-X 2025.02.01, Debian 13) -- confirmed via `strace`-
free black-box testing (repeated, from-clean-state hangs at exactly
the same point in its own boot log, with no listening socket ever
created) and matches a real, long-open upstream bug
([joncampbell123/dosbox-x#2368](https://github.com/joncampbell123/dosbox-x/issues/2368)).
`server:host port:N` (DOSBox-X as the TCP *client*, dialing out) does
work reliably -- confirmed both standalone and as the documented,
currently-in-production approach a real, much larger sysop-facing PHP
BBS project ([binkterm-php](https://github.com/awehttam/binkterm-php),
see its `docs/DOSDoors.md`) uses today. **This package uses neither**: `inhsocket:1` (below) sidesteps
DOSBox-X's TCP code entirely, which is both simpler (no port
allocation, no listen/connect race at all) and avoids depending on a
code path with a known open bug in the *other* mode of the same
feature.

### The mechanism this package actually uses: `-socket N` + `inhsocket:1`

DOSBox-X's nullmodem backend has a command-line flag, `-socket N`,
that stashes a raw file descriptor number in a global (`src/gui/
sdlmain.cpp`), and a `[serial]` parameter, `inhsocket:1`, that -- only
when combined with `-socket N` -- wraps that exact fd directly as its
serial backend's socket (`src/hardware/serialport/nullmodem.cpp`,
confirmed by reading the actual source, not assuming from the sparse
`serial1=` help text). Since Go's `exec.Cmd.ExtraFiles` always numbers
a spawned child's extra descriptors starting at fd 3 (right after
inherited stdin/stdout/stderr), `internal/doors.Run` passes `-socket 3`
and always passes the *same* socketpair end used for `native`-kind
doors -- from DOSBox-X's perspective this is indistinguishable from any
other inherited fd, so all the same lifecycle guarantees (SOCK_CLOEXEC,
the explicit process-exit/connection-failure race with a `Kill()`
fallback -- see this package's own doc comments) carry over unchanged.

`SDL_VIDEODRIVER=dummy` (fully headless, no X server, no Xvfb) works
fine with this specific mechanism -- confirmed live. This is *not* true
of DOSBox-X's TCP-based nullmodem modes in general (binkterm-php's own
`docs/DOSBox_Headless_Mode.md` reports needing a real, off-screen SDL
window on Windows for those to work at all); `-socket`/`inhsocket`
was not something they tried.

### DOSBox-X's own built-in FOSSIL emulation -- don't load BNU.COM/X00.SYS on top of it

`serial1_fossil=true` gives every DOS door FOSSIL support with no TSR
at all. Loading a real FOSSIL driver (BNU.COM) *in addition* was
confirmed live to hang some doors' own COM-port probing at startup
(silently -- no error, no output, indefinitely) where skipping it and
relying on DOSBox-X's native FOSSIL worked immediately. If a specific
door still needs a real FOSSIL TSR for some reason, `serial2=dummy`
plus loading the driver only on COM2 avoids the conflict; this
package's own template doesn't do that since it hasn't been needed.

### DOSBox-X's own `telnet:1` serial option is deliberately not used

`internal/doors`'s `[serial]` line omits `telnet:1` on purpose: with it
off, DOSBox-X's bridge is plain transparent passthrough with no telnet
awareness of its own, so this project's own telnet layer keeps doing
its ordinary IAC escaping/interpretation for a `dosbox` door exactly as
it would for normal `Terminal` output (`internal/doors.Run` only
switches that layer to raw mode for a `native` door, whose comm layer
handles IAC itself instead). Turning both on at once would double-
escape every `0xFF` byte in the door's own 8-bit CP437 output.

### `/c:dos` means "drop the caller into a real interactive DOS shell" -- gate this door's SL accordingly

Doorway's `/c:dos` switch is not a bug surface, it's the entire point
of this particular test door: after its splash screen it prints
"Enter EXIT to return" and hands the caller a live, fully interactive
`C:\>` prompt bridged straight through to their connection -- typed
DOS commands get real DOS responses. That's expected, and is in fact
proof the whole bridge works end-to-end in both directions. It also
means this specific door is a raw shell with no restrictions of its
own: `configs/bbs.yaml`'s `min_sl` is this project's *only* gate on
who gets it, so a "DOS Shell" door like this one belongs behind a high
`min_sl` (this repo's sample config uses `200`, the same threshold
`configs/menus/main.yaml` uses for the Sysop Menu) -- exactly how
binkterm-php itself restricts the equivalent door to admin accounts
only. A `dosbox_launch_cmd` that goes straight into an actual *game*
instead of a shell doesn't have this concern.

### Known quirk: `EXIT` in the autoexec doesn't always fire

`internal/doors`'s generated DOSBox-X config ends the autoexec with a
bare `EXIT` line specifically so the DOSBox-X *process* itself
terminates the moment the door's own process returns (this is what
actually drives `Run`'s "door exited on its own" path -- without it, a
caller who quits a door lands in an idle, fully interactive local DOS
prompt still bridged to their connection instead of back at this BBS's
menu, confirmed live). This works for any door that behaves like a
normal DOS program: it runs, it exits, `COMMAND.COM` moves on to the
next line. It does **not** reliably fire for a door built around a
loop-and-reinvoke batch pattern instead (Doorway/DWHost's own
documented `HOST.BAT`, for example, deliberately re-invokes itself
after each call rather than ever falling through to a next line) --
confirmed live with the unregistered `DOORWAYU.EXE` this package was
validated against (see below): typing `exit` inside it lands at a bare
`C:\>` prompt, not back at the EXIT line, and the session only actually
ends once the caller disconnects (triggering `Run`'s connection-failure
`Kill()` fallback, which still cleans up correctly either way). For an
admin-facing raw-DOS-shell door this is arguably fine as-is; a game
door with a normal single-process lifecycle won't hit this at all.

### Reference test binary: DOORWAY (unregistered, shareware)

[DOORWAY (DOORWAY to Unlimited Doors)](http://pcmicro.com/doorway/) by
Marshall Dudley is what this package's `dosbox`-kind support was built
and validated against -- a small (30 KB), single-EXE, no-overlay-file
DOS door that drops a caller straight into a real DOS shell, making it
an easy way to confirm the whole DOSBox-X bridge actually works with
zero game-specific setup. The unregistered copy has a 10-minute
session limit and is bundled and redistributed as-is by binkterm-php
(`dosbox-bridge/dos/DOORS/ADMIN/DOORWAY/DOORWAYU.EXE` in their repo) as
their own admin-only "DOSDoor DOS shell" maintenance door -- fetched
from there rather than an unknown source. A different classic DOS door
(Usurper's own original 1993/2009 DOS release, `v0.20e`, from the GPL
`ORIGINAL ARCHIVES/usurp020e.zip` in
[rickparrish/Usurper](https://github.com/rickparrish/Usurper)) was
tried first and hangs at its very first screen under this DOSBox-X
version with zero output, on every FOSSIL/comm configuration tried,
and without any CPU exception or trap logged -- a binary-specific
incompatibility unrelated to this package's own bridge mechanism
(DOORWAY, tested through the exact same code path, works immediately).

### Known limitation: telnet-negotiation preamble on non-telnet conns

Usurper's socket comm layer unconditionally sends a 6-byte telnet
negotiation preamble (`IAC WILL BINARY`, `IAC WILL ECHO`) the instant
it opens the connection, and IAC-doubles its own output -- regardless
of what DOOR32.SYS's own comm-type field says. A real telnet client's
own telnet layer consumes this transparently; `internal/doors.Run`
strips it explicitly for a non-telnet conn (SSH has no telnet layer to
consume it, so those six bytes would otherwise show up as visible
garbage). What isn't handled: a real telnet client's own `DO`/`WONT`
replies to that negotiation land in Usurper's own input stream
unfiltered, since its receive path does no IAC parsing at all -- a
long-accepted quirk of DOOR32.SYS-socket doors in general (the same
tradeoff Mystic/ENiGMA setups accept), not something worth working
around here.
