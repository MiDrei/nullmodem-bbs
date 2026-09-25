# Building sexyz

`internal/zmodem` shells out to Synchronet's `sexyz` binary for both
uploads and downloads (see that package's own doc comment for why,
and what it replaced). Unlike `lrzsz` (`sz`/`rz`), `sexyz` isn't
packaged by any distro this project targets, so it has to be built
from Synchronet's own source and placed on the `PATH` of whatever
account runs the `bbs` daemon.

## Build

```sh
git clone --depth 1 https://gitlab.synchro.net/main/sbbs.git
cd sbbs/src/sbbs3
make git_branch.h git_hash.h   # generates version headers `make sexyz` alone won't on its own
make RELEASE=1 sexyz
```

Needs a C toolchain (`build-essential` on Debian/Ubuntu: `gcc`, `make`).
The result is `gcc.linux.x64.exe.release/sexyz` (path varies by
architecture). Copy it somewhere on the daemon's `PATH` -- a
user-writable directory like `~/.local/bin` works fine for
development; a container image should install it under something like
`/usr/local/bin` in the build stage.

## License

`sexyz` is Synchronet's, under the GNU GPL (version 2 or later), with
LGPL 2.1 libraries and a BSD-licensed `zmodem.c`. Running it as a
separate program leaves NullModem BBS's own code unaffected, but
passing it on -- in the container image, say -- means passing on its
license texts and source too. The `Dockerfile` does that; see
[third-party.md](third-party.md).

## Why not lrzsz

`lrzsz` is what this project used before, and is what most other BBS
projects (ENiGMA½, for one) default to since it's trivially available
via `apt`/`yum`. It's written for a real serial line, though, and that
assumption caused two real, live-confirmed classes of bug once run
over a telnet session instead: fully-buffered stdio unless its own
stdout is a real tty, and (even once given one, via a pty) a real
terminal client's Zmodem sender periodically losing sync with `rz`'s
own byte-position tracking, on real hardware over a real LAN, not just
a slow or lossy link.

`sexyz` is Synchronet's own Zmodem engine -- the one SyncTERM itself
is developed and benchmarked against -- and, critically, explicitly
supports running over plain stdio pipes with no serial-line
assumptions baked in. It also understands Telnet framing itself
(`-telnet`), so it can be handed the raw byte stream directly instead
of this project maintaining its own IAC-escaping layer around an
external tool that has no idea what telnet is.

## Known limitation

A genuinely 0-byte file hangs `sexyz` indefinitely in either direction
(confirmed by hand, well past any normal Zmodem timeout) -- both ends
create the empty destination file correctly, then the session just
never finishes. Not worked around in this project: a real upload or
download of a truly empty file has no legitimate use case, and the
failure is confined to that one caller's own session rather than
anything shared.
