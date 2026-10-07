# NullModem BBS

**English** · [Deutsch](README.de.md)

[![CI](https://github.com/midrei/nullmodem-bbs/actions/workflows/ci.yml/badge.svg)](https://github.com/midrei/nullmodem-bbs/actions/workflows/ci.yml)

A bulletin board system in the style of Synchronet, Mystic and ENiGMA½ — over
Telnet and SSH with ANSI/CP437 like in the nineties, plus a web portal for
callers, a web administration for the sysop and an FTN mailer for FidoNet,
fsxNet and friends. Written in Go, data in SQLite, interface in SvelteKit;
runs as a Docker image.

Part of the NullModem family:

| Repo | Contents |
|---|---|
| **bbs** (this one) | the BBS itself |
| [kit](https://github.com/midrei/nullmodem-kit) | shared foundation: ANSI/CP437, QWK/QWKE, Zmodem |
| [reader](https://github.com/midrei/nullmodem-reader) | NullModem Reader (`nmr`), offline reader for this BBS's QWK mail |

## What it does

**Telnet/SSH**
- ANSI screens with placeholders (`{BBSNAME}`, `{USERNAME}`, `{FILL:x}` …),
  menus from YAML, lightbar lists, security levels 0–255
- Message areas (local and echomail), netmail, file areas — with message
  search (Telnet, portal, reader), "new files" across all areas and file
  search
- Among callers: one-liner wall after login, teleconference (chat), node
  messages to other callers online (under "Who's online"), paging the sysop:
  the sysop gets a push notification and answers in the web admin (Chat &
  One-liners) or on a node
- Polls (V): the sysop asks in the web admin, callers vote over Telnet or in
  the portal (Community) and see the result as bars; BBS list (B), kept by
  the callers
- Nodelists (I): taken from the networks' file echos (FSXNET.Z75 …), for
  looking things up and for checking netmail addresses ("→ Agency BBS,
  Dunedin")
- Full-screen editor for writing (arrow keys, Home/End, Page Up/Down, word
  wrap, reply with quote; ^Z save, ^X abort, ^Y delete line); whoever prefers
  writing line by line picks the line editor in their profile
- New messages after login: an overview (netmail, to you, new per area),
  "Read new messages" reads all areas in turn, "Messages to you" only those
  to you; which areas is decided by the same selection as for QWK
- File download and upload by Zmodem (Synchronet's `sexyz`)
- Fetching QWK packets and uploading replies, choice of areas
- Remote doors over RLogin (door networks like DoorParty, other BBSes): host
  and both user names with placeholders in the web admin
- Doors: native Linux doors via `DOOR32.SYS` and DOS doors under DOSBox-X
  (DOOR.SYS, DORINFO1.DEF, DOORFILE.SR), managed in the web admin with
  templates (installable with a click: MRC Chat (uMRC), Immortal Barons,
  Usurper, Usurper Reborn, Judge Dredd; prepared: LORD, TradeWars, OO2,
  DoorMUD); daily maintenance per door (at the time set, never while someone
  is playing)
- Multilingual: English, German formal (Sie) and German informal (Du) —
  Telnet/SSH, portal, reader app, front page and admin; chosen at
  registration and in the profile; every text can be changed in the web admin
  (language editor), screens per language (`main.de.ans`) or with catalog
  texts (`{T:key}`), menu items with translations; English steps in wherever
  something is missing
- Email gateway: every caller can write email from netmail and gets mail at
  handle@your-domain — Telnet, portal, reader app, QWK and a point's reader;
  answering a mail answers by email. The BBS fetches a (catch-all) mailbox
  over IMAP and sends over SMTP; a level and a daily limit per caller
- Profile: real name, time zone, language, password, QWK settings
- Whoever registers first becomes sysop

**Web** (`:8090`)
- Public front page (`/`): welcome screen, every way in (web terminal,
  Telnet/SSH, portal, reader app, QWK), who's online, last callers,
  one-liners, doors, FTN addresses, statistics of the last 30 days; link
  preview (OpenGraph) with the welcome screen as its image; an RSS feed per
  public area (can be turned off)
- Threads: replies linked by the REPLY kludge, direct replies or the subject;
  thread view in the portal and the reader app, thread list (T) and `[`/`]`
  in Telnet, REPLY in outgoing echomail, reference number in the QWK packet
- Chat rooms (`/rooms`, `/join` in the teleconference) with bridges to Discord
  channels and Matrix rooms: BBS callers appear there under their names, the
  others on the BBS as `name@discord` / `name@matrix`; outgoing connections
  only
- My areas: every caller can take areas out of new scan, QWK and the reader
  app (Telnet K, portal ✓, reader); new areas are in automatically
- Chat in the portal and the reader app too; public download links for files
  of shared areas (a page with link preview); door bulletins (scoreboards,
  news) in the doors menu, the portal and on the front page
- BBS list with an hourly online check; a monthly recap by netmail to the
  sysops
- Menu editor (Admin → Content → Menus): items, actions, SL, new menus, a
  preview as in Telnet that is checked against the screen, a version's new
  stock items with a click; changes apply without a restart
- Statistics (Admin → System → Statistics): calls, writers, areas, echomail
  per network, doors, downloads, BinkP sessions
- Portal for callers (`/login`, `/message-areas` …): messages, netmail, files,
  QWK, profile — the same features as over Telnet
- Web terminal (`/terminal`): the BBS in the browser, without a Telnet client —
  with the pixel font of the ANSI screens and a key bar for phones; lockouts
  and the connection limit apply to the visitor's real address
- Reader for phones (`/reader`): a lean web app for reading and answering
  echomail and netmail, installable on the home screen (iOS, Android); read
  status as everywhere on the BBS. Offline reading (unread mail is fetched
  ahead, what's written offline is sent later) and notifications (Web Push)
  for new netmail and echomail to you
- Administration for the sysop (`/admin`, SL 255): users, areas, the security
  level matrix, logs, BinkP uplinks, Areafix, archive, ANSI designer for the
  screens
- Protection (Admin → Users → Security): addresses with too many failed logins
  (Telnet, SSH, portal, admin) are locked out, longer on repeat; a limit of
  simultaneous connections per address; allow and block lists (IP or range).
  New users wait for approval (they may already read and send netmail to the
  sysop), a push to the sysop on sign-up; blocked handles
- Two-factor login (TOTP) for the admin and the Telnet sysop menu, password
  reset for users, warnings by push (service down, uplink unreachable,
  backup missing, disk full, netmail stuck) — see
  [docs/security.md](docs/security.md)
- Nightly backup (Admin → Backups): database, configuration, menus, screens
  and keys as one `.tar.gz`, optionally with files and doors; an encrypted
  off-site copy (age) to S3, OpenStack Swift, SFTP or WebDAV; download in the
  admin, restoring see [docs/backup.md](docs/backup.md)
- REST API; the QWK endpoints are also used by NullModem Reader and the
  scripts in `scripts/multimail/`

**FTN**
- BinkP mailer (binkp/1.1), inbound and outbound, crash and hold, several
  AKAs
- Tosser for echomail, netmail and TIC file echos
- Areafix/Filefix, for your own downlinks too
- TIC "Replaces": a file replaces older ones in the area (wildcards too),
  and the line is passed on
- Maintenance (Admin → Maintenance): clears out old echomail, files, read
  netmail, the log, BinkP transcripts and the inbound archive at night and
  compacts the database; limits per area, a preview before deleting
- InterBBS Last Callers: reads the list from FSX_DAT (both common formats),
  shows it after login and in the portal and reports your own callers; data
  areas like FSX_DAT can be hidden from callers
- Points, e.g. a reader like FidoMail

## Layout

Three independent services, all from the same image, connected only through
the shared SQLite database and `configs/bbs.yaml`:

| Service | Program | Ports |
|---|---|---|
| `bbs` | `cmd/bbs` | 2323 Telnet, 2222 SSH |
| `mailer` | `cmd/mailer` | 24554 BinkP |
| `web` | `cmd/web` | 8090 portal, administration, API |

If you're not on FTN, just leave `mailer` out.

## Quick start with Docker

```sh
cp configs/bbs.yaml.example configs/bbs.yaml
docker compose up -d --build
telnet localhost 2323        # register first = sysop
```

Or without building: put `image: ghcr.io/midrei/nullmodem-bbs:latest` (or a
version tag) in `docker-compose.yml`'s three `image:` lines and drop `--build`.

Then log in at `http://localhost:8090/admin` with the same account.
Configuration, the data directory, your own screens, multi-arch builds and
versioned images: [docs/docker.md](docs/docker.md).

## Development

```sh
go build -o bin/ ./cmd/...           # bbs, mailer, web
(cd web && npm ci && npm run build)  # interface into web/build
./bin/bbs & ./bin/web &              # reads configs/bbs.yaml and configs/web.yaml
go test ./...
```

- Go 1.26+, Node 24 for the interface; everything without cgo, SQLite too
  (`modernc.org/sqlite`).
- Zmodem needs `sexyz` in the `PATH` — so do the Zmodem tests, which are
  skipped otherwise: [docs/building-sexyz.md](docs/building-sexyz.md).
- The kit is pulled in at a fixed version in `go.mod`. To change BBS and kit
  together, put BBS, kit and reader side by side and use the `go.work` in the
  parent directory; kit releases are described in the kit's README.
- The version number is in `internal/version/version.go` and shows on the
  welcome screen.
- Texts are in `internal/i18n/lang/` (`en.yaml`, `de.yaml`, `de-du.yaml`); a
  new text goes into all three. Docs for sysops come in English and German
  (`x.md`, `x.de.md`).

## More documentation

- [docs/handbook.md](docs/handbook.md) — **Sysop handbook**: setting up,
  networks, areas, doors, users, day to day, updates, troubleshooting
- [docs/docker.md](docs/docker.md) — running with Docker
- [docs/adding-a-door.md](docs/adding-a-door.md) — setting up doors, native and
  under DOSBox-X
- [docs/points.md](docs/points.md) — points and your own reader apps (FidoMail)
- [docs/building-sexyz.md](docs/building-sexyz.md) — building `sexyz`, why not
  lrzsz
- [docs/third-party.md](docs/third-party.md) — third-party software in the image
  and its licenses
- [scripts/multimail/README.md](scripts/multimail/README.md) — QWK exchange
  by script, e.g. for MultiMail

## Contributing

Bugs and ideas: [issues](https://github.com/midrei/nullmodem-bbs/issues).
Pull requests are welcome -- see [CONTRIBUTING.md](CONTRIBUTING.md) for how to
build, test and the project's conventions. Security problems please report
privately: [SECURITY.md](SECURITY.md).

## Third-party software

The Docker image contains Synchronet's `sexyz` (GNU GPL, version 2 or later)
as a separate program for Zmodem transfers; license texts, notices and the
complete source code are in the image under `/usr/local/share/doc/sexyz/`.
Details: [docs/third-party.md](docs/third-party.md).

## License

MIT — see [LICENSE](LICENSE). Third-party software in the Docker image keeps
its own license (see above).
