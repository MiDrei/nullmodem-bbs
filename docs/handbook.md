# Sysop handbook

**English** · [Deutsch](handbook.de.md)

How to set up a NullModem BBS and run it day to day. The details are in the
linked documents; this one says what goes where and in which order to tackle
it.

- [1. Layout](#1-layout)
- [2. Installation](#2-installation)
- [3. First steps](#3-first-steps)
- [4. FTN networks](#4-ftn-networks)
- [5. Areas](#5-areas)
- [6. Doors](#6-doors)
- [7. Users](#7-users)
- [8. For the callers](#8-for-the-callers)
- [9. Day to day](#9-day-to-day)
- [10. Updates](#10-updates)
- [11. Troubleshooting](#11-troubleshooting)
- [12. Where things are](#12-where-things-are)

## 1. Layout

Three services from the same Docker image, connected through the shared
SQLite database and `configs/bbs.yaml`:

| Service | Job | Port |
|---|---|---|
| `bbs` | Telnet, SSH, doors, chat | 2323, 2222 |
| `mailer` | BinkP, tosser, Areafix/Filefix, nodelists, nightly maintenance | 24554 |
| `web` | portal, reader app, web terminal, administration, backups, warnings | 8090 |

Put a reverse proxy with TLS (e.g. Caddy) in front of port 8090. The BBS
reads the visitors' real address from `X-Forwarded-For` — behind Docker's
port mapping too.

## 2. Installation

See [docker.md](docker.md). In short:

```sh
cp configs/bbs.yaml.example configs/bbs.yaml
docker compose up -d
```

In `.env` next to `docker-compose.yml`:

```
PUID=1000          # the user who owns data/
PGID=1000
TZ=Europe/Zurich   # time zone: logs, backup names, nightly runs
```

In `configs/web.yaml`, for the web terminal under Docker:
`terminal_addr: "bbs:2323"`.

## 3. First steps

1. **Sysop account:** register first over Telnet (or `/terminal` in the
   browser) — the first account becomes sysop (SL 255) automatically.
2. **Admin:** `https://your-bbs/admin`, with the same account.
3. **Turn on two-factor login:** Users → Security, see
   [security.md](security.md). Keep the recovery codes somewhere safe.
4. **Put your own address on the allow list** (Security) if you have a fixed
   IP — then you never lock yourself out.
5. **Basics:** System → Settings (name, sysop, location — shown in the BinkP
   handshake and on the screens).
6. **Screens:** Content → Screens → adjust the welcome, main menu etc. in the
   ANSI designer. Build borders with `{FILL:x}` so they close at any width.
   A line with `{SYSOP_ONLY}` in it is shown to the sysop only (SL 200 and
   up) and left out for everyone else — the stock main menu's sysop entry.
   Saved means live at once, the welcome screen too.
   **Menus:** Content → Menus — change items (key, text, what it does, from
   which SL), reorder them, create new menus and link them. The preview shows
   the menu as a caller or the sysop sees it and warns when screen and items
   don't match (an item is missing on the screen, or the screen shows a key
   that does nothing). Once saved, it applies at the callers' next menu,
   without a restart.
7. **Notifications:** put the reader app (`/reader`) on your phone and turn on
   notifications under ⚙ — then warnings, new users and pages reach your
   phone.
8. **Check the backup:** System → Backups → "Back up now", and set up the
   encrypted off-site copy (S3, Swift, SFTP or WebDAV,
   [backup.md](backup.md)).

## 4. FTN networks

FTN → Networks & Addresses:

- Add a **network** (name and domain, e.g. `fsxNet` / `fsxnet`).
- Your **own address** per network (e.g. `21:3/194@fsxnet`).

FTN → Uplinks:

- **Uplink** (hub) with address, host, session and packet password, Areafix
  and Filefix password. "Crash only" if you only want to call when there's
  mail; "Hold" if the hub picks up.
- **Subscribe to areas:** FTN → Areafix / Filefix — request the list from the
  hub, tick, send. New echos from incoming mail land under "Pending areas"
  first and become visible once you approve them. "Ask the hub what's
  subscribed" sends `%QUERY`: the hub answers with what it has really linked
  to us, the ticks follow that, differences from our own record are marked,
  and "Take over the hub's state" puts its word into our record (nothing is
  sent). Areafix (echomail) and Filefix (file echos) answer from the same
  address; each reply counts only for the robot it comes from, and the
  history below the list shows every request and reply, refusals included
  (passwords hidden). More commands: `%LINKED` (what some robots call
  `%QUERY`), `%UNLINKED`, `%HELP`, `%PAUSE`, `%RESUME`. A hub that knows
  neither `%QUERY` nor `%LINKED` (Clearing Houz, fsxNet's hub) marks what's
  subscribed with `*` in its `%LIST` -- that's what the ticks show then.
- **Points / your own reader app:** [points.md](points.md).
- **Nodelists:** arrive on their own with the file echos (`FSX_NODE` …) and
  are taken over every 10 minutes; status under FTN → Nodelists.

What to do when a hub doesn't answer is under
[Troubleshooting](#11-troubleshooting).

## 5. Areas

- **Message areas:** name, description, network, security level for reading
  and writing, own retention (days / count). Mark data areas like `FSX_DAT`
  (InterBBS data) as "hidden" (Content → Message Areas) — then callers don't
  see them.
- **File areas:** the same, with download/upload SL. With "Public", anyone may
  download their files without logging in: every file has a page for sharing
  (`/share/f/<id>`, with link preview), the portal shows "Copy share link" for
  it, and the front page lists the newest. TIC files with "Replaces" replace
  older versions automatically.
- **Cleaning up:** System → Maintenance — set the limits, "Preview" shows what
  would go, and it runs on its own at night.

## 6. Doors

Content → Doors. Ready-made templates (MRC Chat, Usurper, Immortal Barons …)
install with one click. Your own doors: [adding-a-door.md](adding-a-door.md).

| Kind | For |
|---|---|
| DOS (DOSBox-X) | classic DOS doors, with FOSSIL, drop file of your choice |
| Native Linux | Linux doors via DOOR32.SYS or stdio |
| Remote (RLogin) | door networks like DoorParty, or another BBS |

**Daily maintenance:** many doors (TradeWars, BRE, Usurper …) want a
maintenance program to run once a day (new turns, daily events). In the door,
enter the command under "Daily maintenance" (for DOS doors the DOS commands,
e.g. `USURPER /MAINT`), plus the time (default 00:05). The BBS runs it with
no caller — never while someone is playing, a bit later then. Result and
output are in the door list, "Run maintenance" starts it right away; a
failure shows under "Needs attention".

**Bulletins (scoreboards, news):** many doors write their scoreboards and
news to files. Under "Bulletins" in the door, enter the title and file
(relative to the door's directory); "public" shows it on the front page too.
Callers read them in the doors menu (B) and in the portal under Community →
Door scores. For Immortal Barons and Usurper Reborn the BBS knows the files:
"Use the template's …" enters them — for Immortal Barons this also turns on
`BulletinDir` in `data/bbs.cfg` and sets the daily maintenance
(`immortal-barons -maint`), which rewrites them every day.

**Updates:** for MRC Chat, Immortal Barons and Usurper Reborn the BBS looks
for new releases twice a day ("Check for updates" asks right away). A new one
shows at the door with its release notes, under "Needs attention" and as a
push notification. "Update to …" replaces only the files the release ships —
save games, settings and logs stay; the replaced files are kept in
`.backup-<door>` next to the door's directory (the last update's only). A
player still in the door keeps the old version until they leave; a background
program restarts by itself. Read the release notes first: some releases want
something done, e.g. every board in an inter-BBS league updating together.
New installs from a template get the newest release.

Background programs (e.g. the MRC bridge) run as a service of their own and
show under System → Services.

## 7. Users

- **New accounts** wait for your approval (Users → "Awaiting approval" →
  Approve / Turn down). Until then they can read and may send you netmail.
  Accounts never approved are deleted by the maintenance after 30 days.
- **Security levels:** set per account under Users. Under Users → Security
  levels you give the levels you use a name (e.g. 10 New user, 20 Regular
  user, 30 Regular and email, 255 Sysop); every level field in the admin then
  offers them by name, and the Telnet sysop menu lists them when setting a
  caller's level. Without names of your own the board's levels show (waiting
  for approval, new user, sysop).
- **Forgotten password:** Users → "Password…" sets a new one.
- **Second factor lost:** another sysop resets it with "Reset 2FA"; otherwise
  see [security.md](security.md).
- **Locked addresses** and failed logins: Users → Security.

## 8. For the callers

**Telnet / SSH / web terminal (`/terminal`):**

The main menu is short; the rest is in three submenus (Q goes back):

| Main menu | |
|---|---|
| R | read new messages |
| N | news from the sysop |
| M » | **Messages:** R new messages · T messages to me · A message areas (there S = search) · N netmail (and email, when the gateway is on) · I nodelist · K my areas · O fetch QWK · U upload QWK replies |
| F » | **Files:** A file areas · N new files · S search files |
| T » | **Community:** C chat (teleconference; there `/rooms`, `/join name`) · P page the sysop · L one-liners · W who's online (with node message) · Z last callers · V polls · B BBS list |
| D / P | doors / profile (incl. line editor instead of full screen) |
| S / ? / Q | sysop menu / version / log off |

**My areas** (K; in the portal "My areas / All areas" with a ✓ per area, in
the reader app under "All"): whatever is in there is picked up by new scan,
QWK packets and the reader app (unread list, offline prefetch). Everything is
in until you take an area out — so new areas join automatically. Messages
*to me* (T) and their push notifications come from all areas.

In an area's message list, `T` switches to threads: one line per thread
(number of messages, who started it, last activity, NEW while something is
unread); Enter reads the thread in reply order.

In the message reader: `]` (or `T`) jumps to the next message in the thread,
`[` to the previous one — replies in the order they answer each other, across
the whole area.

After login: InterBBS Last Callers, one-liners, an overview of what's new.

**Languages:** the BBS speaks English, German formal (Sie) and German informal
(Du) — on Telnet/SSH as in the portal, the reader app, the front page and the
admin (language choice at the top right, the code next to the moon).
On Telnet/SSH every caller picks a language under the welcome screen, before
the handle prompt (a key, Enter keeps the board's) — login and registration
then come in it, and a new account keeps it. Later it's changed in the
profile (Telnet `Y`, then `A`; in the portal under Profile, in the reader app
in the settings) — it's a setting of the account, the same everywhere; an
existing account's language applies from login on. For everyone who never
chose, the board's language applies (Content → Languages, "The board's
language"); on the web a visitor without an account
gets their browser's language (German in the board's form, Sie or Du) and can
switch at the top of the page. Error messages, push notifications and the
welcome screen (`welcome.de.ans`) follow the language too.
What a language lacks comes in English; German (Du) first takes from German
(Sie) whatever reads the same.

- **Changing texts:** Content → Languages. Every text can be overridden per
  language; empty means "as shipped". `{NAME}` are placeholders the BBS fills
  in — a text may leave one out but not invent one (the editor shows which
  work). Only what you changed is saved, in `data/lang/<language>.yaml`; it
  applies at once and survives updates. The texts are in sections (Telnet/SSH,
  screens & menus, web, admin, messages) and groups; a text used in several
  places exists once ("Also used in: …" says where else it shows).
- **Screens:** every screen can have a version per language:
  `main.de-du.ans`, then `main.de.ans`, then `main.ans`. German versions of
  the stock screens are shipped; they take their texts from the catalog with
  `{T:key}` (`{T:col.subject:-40}` left-aligned to 40 characters,
  `{T:col.total:5}` right-aligned) — one file for Sie and Du, and the labels
  are changed in the language editor. `{T:…}` works in any screen, your own
  too. Your own `welcome.ans` is translated as `welcome.de.ans` in the
  designer.
- **Menus:** a stock menu item shows up translated by itself. Your own labels
  get their translation in the menu editor ("Other languages…"); the preview
  shows every language.

**Front page (`/`):** public, no login — welcome screen, every way in (web
terminal, Telnet/SSH, portal, reader app, QWK), who's online, last callers,
one-liners, doors and the FTN addresses for other sysops. The address to pass
on. A shared link (Telegram, Discord, Mastodon …) shows the welcome screen as
its preview (`/og-image.png`, drawn from `welcome.ans`).

**RSS feeds:** turn on System → Settings → "Public RSS feeds", and every area
a new caller may read gets `/feeds/<tag>.xml` with the newest 30 messages
(the front page lists them, feed readers find them by themselves). Areas with
a higher SL (sysop, local private ones) stay out.

**Threads:** every reply knows what it answers — from the echomail's REPLY
kludge, for replies written here directly (Telnet, portal, reader app, QWK).
Older messages without it are matched by subject ("Re: …"). In the portal
every area shows "All messages" or "Threads", every message its thread as a
tree; outgoing replies carry a REPLY so other systems can place them too.

**Chat on the web:** the portal (Chat) and the reader app (Chat in the list)
have the same rooms as the teleconference — whoever writes in the portal is
there for Telnet callers (`name (web)`), and bridged rooms reach Discord and
Matrix.

**Web:** portal (`/message-areas` …, login at `/login`) with everything from
Telnet (search through the field at the message areas), reader app
(`/reader`) for phones with offline reading and push, QWK readers like
NullModem Reader.

## 9. Day to day

- **Dashboard:** "Needs attention" shows problems (service down, uplink
  unreachable, backup overdue, disk full, netmail stuck), waiting users,
  locked addresses and who's paging you right now. Problems come as push
  notifications too. Below: per uplink when the last session went through,
  the last error and the sessions of the past 24 hours (red dot: the last
  session failed, yellow: nothing went through for two days, grey: not
  polled); and the system — last backup, off-site copy, database size, free
  space, services and the latest warnings and errors.
- **Chat & one-liners:** Community → Chat & One-liners — that's where you
  answer when someone pages, and tidy up the one-liner wall.
- **Chat rooms:** same place, under "Rooms". Besides the teleconference
  (`main`) as many as you like, each with a topic and minimum SL. Callers see
  them in the teleconference with `/rooms` and switch with `/join name`.
  Sysops come and go without a word (everyone else is announced); "Say in
  the rooms when a sysop enters or leaves" changes that. **Clear** in an
  open room deletes everything said in it; otherwise lines go after 30
  days.
- **Discord bridge:** a room can be linked to a Discord channel: what's said
  on the BBS appears there under the caller's name, what's written in Discord
  appears on the BBS as `name@discord`. The BBS makes outgoing connections
  only; no open port and no server of your own needed. Setting up (about 10
  minutes):
  1. Your own Discord server, if you don't have one yet: in the Discord app,
     **+** at the bottom of the server list → "Create My Own".
  2. [Developer Portal](https://discord.com/developers/applications) → **New
     Application** (its name becomes the bot's name).
  3. **Bot** → turn on **Message Content Intent** → Save.
  4. **Bot** → **Reset Token** → copy the token, paste it on the BBS under
     Community → Chat & One-liners → Discord bridge → **Turn on**.
  5. Once it says "Connected" there: **Add it to your server** — the link asks
     for the rights needed (view channels, send, read history, manage
     webhooks).
  6. For each room, choose the channel under **Edit**.

  Without the "Manage webhooks" right the bot writes itself (`**name**:
  text`). It announces joins and leaves as long as "Don't tell Discord …" is
  off. If the bridge is on but disconnected for more than 15 minutes, it
  shows under "Needs attention". The token is in `bbs.yaml` and never shown
  on the web again.
- **Matrix bridge:** the same, for Matrix rooms (e.g. on matrix.org): create
  an account for the bot (through Element, say), enter homeserver, bot name
  and password under "Matrix bridge" → "Log in and turn on" (only the access
  token is stored). With your own account, create a room **without
  encryption** and invite the bot (or make the room public), then choose the
  Matrix room under Edit at the BBS room or enter its address
  `#room:server`. In Matrix the bot writes "name: text", on the BBS it shows
  as `name@matrix`. The bot can't read encrypted rooms — the admin warns you
  then. A room can be linked to Discord and Matrix at the same time; what's
  said in Discord then arrives in Matrix too, and the other way round.
- **Email gateway:** Community → Email gateway. Every caller from the level
  you set gets the address handle@your-domain (SwissMaik is
  `swissmaik@bbs.example.com`, a space becomes a dot) and can write email
  wherever they write netmail: an email address as the recipient. Answering
  a mail answers by email — over Telnet, in the portal, the reader app and a
  QWK reader. Setting up:
  1. At your mail provider, a mailbox for the domain with a **catch-all**
     (all mail to any address of the domain lands in it), and an SMTP login
     that may send as any address of the domain — usually the same account.
  2. Enter the domain, the IMAP server (the mailbox) and the SMTP server,
     **Test**, turn it on, save.

  The BBS fetches the mailbox every minute; unread mail becomes netmail to
  the caller it's addressed to (`name+anything@` works too) and is marked
  read, or deleted if you choose so. Mail to unknown addresses, to callers
  below the level or not yet approved, and mail the provider flagged as spam
  is dropped. Text only: HTML mail is turned into text, attachments are
  listed but not delivered. Every mail sent starts with a line saying who
  wrote it on which board (`email.sent_by` in the language editor, e.g. to
  add the board's web address). A daily limit per caller keeps an account from
  becoming a spam source. Mail the receiving server refuses comes back to the
  writer as netmail with the reason; if the gateway can't fetch or send for
  half an hour, it shows under "Needs attention". The callers see their
  address in their profile.
- **News:** Content → News. An item has a title and a text in German and in
  English (callers reading German get the German one, everyone else the
  English one; a missing language shows the other) and, if you like, a date
  until which it shows. At login a caller sees the items they haven't seen
  yet, in the main menu under N all current ones; the latest three are on the
  front page and in the portal (Community).
- **Polls / BBS list:** Community → Polls & BBS List. Every hour the BBS
  checks whether the boards on the list answer (a TCP connection, nothing is
  sent) and shows "up/down" or "online/offline"; addresses in your own or a
  private network are never contacted.
- **Monthly recap:** on the 1st at 07:00 every sysop gets a netmail about the
  previous month — calls, writers, areas, echomail per network, doors,
  downloads, BinkP sessions, backup and whatever is wrong right now. Turn it
  off under Settings → Monthly recap; System → Statistics → "Send a recap
  now" sends one at once.
- **Statistics:** System → Statistics — calls per day and hour, most active
  callers, writers and areas, echomail per network, doors, downloads, BinkP
  sessions, new accounts (7 days to 1 year). The last 30 days without the
  sysop part are on the front page too.
- **Logs:** System → Logs — "All" with "Warnings & errors" as a quick
  overview; "BinkP sessions" shows every session with its transcript.
- **Automatic at night:** 00:05 door maintenance (set per door), 03:00
  backup, 04:00 maintenance (server time).

## 10. Updates

New version: change the image tag in `docker-compose.yml`, then

```sh
docker compose pull && docker compose up -d
```

Menus and screens under `configs/` stay untouched; new stock screens are
added without overwriting customized ones. Content → Menus shows new menu
items ("This version's stock main menu has, and yours doesn't") — take them
with **Add** and save; the preview then says whether the screen (`main.ans`)
already shows the item, otherwise "Edit screen". Before bigger updates:
"Back up now".

## 11. Troubleshooting

| Symptom | Where to look | Common cause |
|---|---|---|
| Hub doesn't answer | Logs → BinkP sessions → transcript | wrong password, hub down, firewall |
| `M_BSY … busy` | transcript | the hub thinks a session is already running (a stale lock file on its side) — ask the hub's sysop |
| Mail doesn't arrive | FTN → Packet Analyzer, Undeliverable Netmail | area not subscribed / not approved, wrong packet password |
| Caller locked out | Users → Security | too many failed logins; Unlock |
| Door doesn't start | Logs → System (door filter) | directory empty, wrong drop file, lock file |
| Times wrong | `.env` → `TZ` | container without a time zone (UTC) |
| Leftovers on screen when moving through lists | the caller's terminal | it doesn't handle the in-place updates (only changed lines are sent); `NULLMODEM_FULL_REDRAW=1` in the `bbs` service's environment sends every screen whole again |

With shell access: `docker compose logs -f mailer` (or `bbs`, `web`).

## 12. Where things are

| Path | Contents |
|---|---|
| `configs/bbs.yaml` | configuration (incl. passwords) |
| `configs/web.yaml` | web service |
| `configs/menus/`, `configs/screens/` | menus and ANSI screens (`name.de.ans` = German version) |
| `data/lang/` | your changed texts per language (language editor) |
| `data/nullmodem.sqlite` | the database |
| `data/files/`, `data/doors/` | files of the file areas, installed doors |
| `data/backups/` | nightly backups ([backup.md](backup.md)) |
| `data/binkp-sessions/`, `data/inbound-archive/` | BinkP transcripts, received packets (a few days) |
| `data/jwt_secret`, `data/ssh_host_key`, `data/vapid.json` | keys — don't hand them out |
