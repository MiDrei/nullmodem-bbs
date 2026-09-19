# Adding a door game

`internal/doors` runs external "door" programs and bridges them to a
caller's connection the same way `internal/zmodem` bridges a file
transfer: the door process gets handed the raw connection, not this
project's own line-oriented/ANSI-cooked `Terminal`.

## How it works

This package writes a [DOOR32.SYS](https://raw.githubusercontent.com/NuSkooler/ansi-bbs/master/docs/dropfile_formats/door32_sys.txt)
dropfile (the modern, Linux/Windows-native successor to the classic
DOS `DOOR.SYS`/`DORINFOx.DEF` formats) naming comm type 2 (Telnet) and
socket handle 3, then hands the door process an already-connected
`AF_UNIX` socket at exactly that file descriptor via Go's
`exec.Cmd.ExtraFiles` -- the door reads/writes that socket directly
(`recv()`/`send()`), the same way it would an inherited descriptor
under Mystic or ENiGMA½ on Linux. This works for **any door with a
native Linux/Windows port that reads DOOR32.SYS's socket-handle field
as a raw inherited file descriptor** -- that's what the format was
invented for. A classic DOS-only door would need a DOS emulator
(DOSBox-X, DOSEMU2) and a FOSSIL driver bridging its own serial port
to this same socket instead -- not implemented here yet.

## Configuring a door

Add an entry to `configs/bbs.yaml`:

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
