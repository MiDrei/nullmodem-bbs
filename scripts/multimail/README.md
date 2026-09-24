# MultiMail QWK scripts

Two example scripts that automate reading/replying to your NullModem
BBS mail offline via [MultiMail](https://wmcbrine.com/MultiMail/)
("mm"), using the BBS portal's HTTP QWK endpoints instead of a Telnet/
Zmodem session:

- `nullmodem-qwk.sh` -- Linux/macOS (bash + curl)
- `nullmodem-qwk.ps1` -- Windows (PowerShell + curl.exe); double-click
  `nullmodem-qwk.bat` instead to run it without opening a PowerShell
  prompt yourself (PowerShell scripts don't run on double-click by
  default -- Windows opens them in an editor)

Both do the same four steps:

1. Log in via `POST /api/bbs/auth/login` to get a bearer token.
2. Download your current mail via `GET /api/bbs/qwk/download` (which
   areas are included is controlled by your QWK area selection --
   set it up first at `/qwk` in the web portal, or `[K]` in the
   Telnet/SSH main menu).
3. Launch MultiMail on the downloaded packet so you can read and
   reply offline.
4. Upload whatever `.REP` reply packet MultiMail produced via
   `POST /api/bbs/qwk/upload`.

## Setup

Install MultiMail (`mm`):
- Linux: `apt install multimail` (Debian/Ubuntu) or build from
  <https://github.com/wmcbrine/MultiMail>.
- Windows: download a build from <https://wmcbrine.com/MultiMail/>
  and make sure `mm.exe` is on your `PATH`.

Each script has a "Configuration" block right at the top -- edit
`BBS_URL`/`USERNAME`/`PASSWORD` (`$BbsUrl`/`$Username`/`$Password` in
the PowerShell version) directly there if you'd rather not be
prompted or set environment variables every time. Leaving
`USERNAME`/`PASSWORD` blank prompts for them interactively instead.
Every value can also be overridden via the matching `NULLMODEM_*`
environment variable (`NULLMODEM_URL`, `NULLMODEM_USER`,
`NULLMODEM_PASS`, ...) without editing the script -- handy for a
scheduled task, and an env var always wins over the hardcoded value.

By default, downloaded packets and reply packets go into
`incoming`/`outgoing` subfolders of `NULLMODEM_QWK_DIR` (default
`~/.nullmodem-qwk` on Linux/macOS, `%USERPROFILE%\nullmodem-qwk` on
Windows). Set `NULLMODEM_QWK_DOWN`/`NULLMODEM_QWK_UP` to point at your
own existing MultiMail "down"/"up" directories instead -- these map
directly to MultiMail's own `PacketDir`/`ReplyDir` options.

## Usage

```sh
# Linux/macOS
./nullmodem-qwk.sh
```

```powershell
# Windows, from a PowerShell prompt
.\nullmodem-qwk.ps1
```

Or just double-click `nullmodem-qwk.bat` in Explorer.
