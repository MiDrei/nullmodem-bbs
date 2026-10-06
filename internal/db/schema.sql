CREATE TABLE IF NOT EXISTS users (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    username       TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash  TEXT NOT NULL,
    real_name      TEXT NOT NULL DEFAULT '',
    security_level INTEGER NOT NULL DEFAULT 10,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at  TIMESTAMP,
    total_calls    INTEGER NOT NULL DEFAULT 0,
    -- IANA zone name (e.g. "Europe/Zurich") times are shown in, on
    -- Telnet/SSH and in the web portal alike; '' means not set (see
    -- user.User.Location).
    timezone       TEXT NOT NULL DEFAULT '',
    -- 1: QWK packets carry echomail's SEEN-BY/PATH lines (see
    -- qwkdoor.BuildPacketForUser); off by default, since most QWK
    -- readers show them as text.
    qwk_routing    INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_users_security_level ON users(security_level);

CREATE TABLE IF NOT EXISTS message_areas (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    tag           TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name          TEXT NOT NULL,
    description   TEXT NOT NULL DEFAULT '',
    -- Groups related echo areas by FTN network (e.g. "fsxNet",
    -- "FidoNet") once a BinkP mailer exists to feed them -- empty
    -- means a local-only area with no network affiliation. A database
    -- created before this column existed gets it via ensureColumn
    -- (see db.go), since CREATE TABLE IF NOT EXISTS doesn't retrofit
    -- a column onto an already-created table.
    network       TEXT NOT NULL DEFAULT '',
    min_sl_read   INTEGER NOT NULL DEFAULT 0,
    min_sl_write  INTEGER NOT NULL DEFAULT 0,
    sort_order    INTEGER NOT NULL DEFAULT 0,
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Set on an area internal/tosser auto-created for an inbound
    -- echomail AREA kludge it hadn't seen before. Pending areas are
    -- hidden from every BBS-facing listing (ListAreas/ListAreaStats)
    -- and the normal web admin area list until the sysop reviews and
    -- approves them (see ApproveArea) -- an area a sysop creates by
    -- hand is never pending.
    pending       INTEGER NOT NULL DEFAULT 0
);

-- Seeded once (idempotent via the UNIQUE tag) so a fresh install has a
-- working message base without requiring sysop setup first.
INSERT OR IGNORE INTO message_areas (tag, name, description, min_sl_read, min_sl_write, sort_order)
VALUES ('general', 'General Discussion', 'General chat for all callers', 0, 0, 0);

-- from_user_id is NULL for a message internal/tosser tossed in from a
-- remote FTN system's echomail (no local author account), in which
-- case from_name carries the remote author's name directly instead of
-- being joined from users.username -- mirrors netmail_messages'
-- from_user_id/from_name split (see internal/netmail's doc comment).
CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id       INTEGER NOT NULL REFERENCES message_areas(id) ON DELETE CASCADE,
    from_user_id  INTEGER REFERENCES users(id),
    from_name     TEXT NOT NULL DEFAULT '',
    to_name       TEXT NOT NULL DEFAULT 'All',
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL,
    -- The remote message's MSGID kludge (e.g. "21:3/100 5f3e2a1b"),
    -- empty for a locally-posted message or a remote one that carried
    -- no MSGID. Lets internal/tosser's ReceiveEcho recognize and skip
    -- a message a hub resends after a dropped BinkP session kept it
    -- from seeing our M_GOT (see idx_messages_area_msgid below and
    -- FTS-1026's PendingFiles requirement).
    msgid         TEXT NOT NULL DEFAULT '',
    posted_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- When a locally-posted message (from_user_id set) was last handed
    -- off to (and acknowledged by) an uplink -- see internal/tosser's
    -- PendingOutboundEcho/MarkSent. NULL for a message tossed in from
    -- a remote system, and for a local post not sent out yet, mirrors
    -- netmail_messages.sent_at.
    sent_at       TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_area_posted ON messages(area_id, posted_at);
-- Partial (msgid != '' only, so locally-posted/MSGID-less messages
-- never collide) unique index enforcing echomail dedup by MSGID within
-- an area. Not created here for an already-existing database -- the
-- column doesn't exist yet at the point this file runs on one; see
-- db.go's Open, which creates it right after ensureColumn adds msgid.

-- Tracks exactly which individual messages a caller has actually
-- opened in the reader, so a message stays flagged "New" in the
-- message list -- and counted in the area lightbar's "New" column --
-- until it's really read, not merely until the area was visited.
CREATE TABLE IF NOT EXISTS message_reads (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id  INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, message_id)
);

-- A user's chosen subset of message areas to include in their QWK
-- offline-mail packets. No rows for a user means "no explicit
-- selection yet" -- every readable area with new mail is included,
-- matching the original QWK behavior before this table existed.
CREATE TABLE IF NOT EXISTS qwk_area_selections (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    area_id  INTEGER NOT NULL REFERENCES message_areas(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, area_id)
);

CREATE TABLE IF NOT EXISTS file_areas (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    tag              TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    -- Groups related file areas by FTN network the same way
    -- message_areas.network does for echo areas -- empty means a
    -- local-only area with no network affiliation. A database created
    -- before this column existed gets it via ensureColumn (see
    -- db.go).
    network          TEXT NOT NULL DEFAULT '',
    min_sl_download  INTEGER NOT NULL DEFAULT 0,
    min_sl_upload    INTEGER NOT NULL DEFAULT 0,
    sort_order       INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    -- Mirrors message_areas.pending -- not set by anything yet (no
    -- TIC/file-echo tossing exists), but the same review/approve
    -- workflow will apply once it does.
    pending          INTEGER NOT NULL DEFAULT 0
);

INSERT OR IGNORE INTO file_areas (tag, name, description, min_sl_download, min_sl_upload, sort_order)
VALUES ('general', 'General Files', 'General file library for all callers', 0, 0, 0);

-- uploaded_by is nullable for the same reason netmail_messages.
-- from_user_id and messages.from_user_id are (see db.go's
-- migrateNetmailFromUserIDNullable/migrateMessagesFromUserIDNullable,
-- which this mirrors): a file internal/tosser tosses in from a remote
-- FTN system via TIC/file-echo (see file.Store.Receive) has no local
-- uploader account, only the file-echo's own reported origin name --
-- stored in uploaded_by_name instead, exactly like messages.from_name
-- for a remote-origin echomail message. A locally uploaded file (see
-- ImportFile/UploadFile) leaves uploaded_by_name empty and always has
-- uploaded_by set; FileByID/ListFiles join in the current username
-- when it's set, falling back to uploaded_by_name (COALESCE) when
-- it's NULL.
CREATE TABLE IF NOT EXISTS files (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id          INTEGER NOT NULL REFERENCES file_areas(id) ON DELETE CASCADE,
    filename         TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    size_bytes       INTEGER NOT NULL,
    storage_path     TEXT NOT NULL,
    uploaded_by      INTEGER REFERENCES users(id),
    uploaded_by_name TEXT NOT NULL DEFAULT '',
    uploaded_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    download_count   INTEGER NOT NULL DEFAULT 0,
    -- seen_by mirrors messages' own SEEN-BY tracking (see
    -- internal/message/seenby.go) but as its own column rather than
    -- embedded in text, since a file has no body to embed it in: the
    -- net/node pairs (space-separated, zone/point dropped -- see
    -- file.NetNode) of every downlink internal/tosser has already
    -- forwarded this file to (file.Store.MarkSeenBy), so a later
    -- routing decision doesn't send it to the same downlink twice.
    seen_by          TEXT NOT NULL DEFAULT '',
    UNIQUE (area_id, filename)
);

CREATE INDEX IF NOT EXISTS idx_files_area_uploaded ON files(area_id, uploaded_at);

-- Tracks exactly which individual files a caller has actually opened
-- in the file reader -- the file-area equivalent of message_reads.
CREATE TABLE IF NOT EXISTS file_reads (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    file_id  INTEGER NOT NULL REFERENCES files(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, file_id)
);

-- Netmail: private, per-recipient mail (as opposed to the shared,
-- topic-based message_areas/messages tables above, which implement
-- Echomail). Added ahead of the BinkP/FidoNet mailer (see CLAUDE.md's
-- Phase 2 roadmap) so its schema doesn't need a later migration:
-- from_address/to_address hold FTN addresses (zone:net/node.point)
-- for routing once a mailer exists. to_user_id is set when the
-- recipient resolved to a local account (delivered immediately); it's
-- NULL when to_address is a remote system, either still queued for
-- internal/tosser to send (sent_at NULL) or already handed off
-- (sent_at set). from_user_id is NULL for mail internal/tosser
-- received from a remote FTN system -- there's no local sender
-- account, so from_name carries the remote sender's name directly
-- instead of being joined from users.username the way a local
-- sender's is. Since a netmail message always has exactly one
-- recipient (unlike an echo area's many readers), read state lives
-- directly on the row rather than needing a separate per-user reads
-- table.
CREATE TABLE IF NOT EXISTS netmail_messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id   INTEGER REFERENCES users(id),
    from_name      TEXT NOT NULL DEFAULT '',
    from_address   TEXT NOT NULL DEFAULT '',
    to_user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    to_name        TEXT NOT NULL,
    to_address     TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,
    body           TEXT NOT NULL,
    posted_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at        TIMESTAMP,
    sent_at        TIMESTAMP,
    -- Crash-priority mail (FTS-0001's AttrCrash) is routed and
    -- delivered specially by internal/tosser: it bypasses a
    -- crash-only uplink's disabled regular poll and gets dialed
    -- immediately instead of waiting for the next scheduled poll.
    crash          INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_netmail_to_user_posted ON netmail_messages(to_user_id, posted_at);

-- Tracks when cmd/mailer last attempted (successfully or not) to poll
-- each configured BinkP uplink, keyed by its "host:port" (see
-- internal/tosser's poll-scheduling). Needed because some hubs only
-- permit polling every hour or two: without this persisted, a daemon
-- restart would forget how recently an uplink was tried and could
-- poll it again immediately, violating that limit.
CREATE TABLE IF NOT EXISTS binkp_uplink_polls (
    host           TEXT PRIMARY KEY,
    last_polled_at TIMESTAMP NOT NULL
);

-- Currently active BBS sessions ("nodes"). Node numbers are assigned
-- by the BBS daemon's application code (not AUTOINCREMENT), so no
-- CREATE TABLE-level default applies here. This table is the shared
-- source of truth for both the BBS daemon's own [W]ho's online
-- command and the separate web admin daemon's node-monitoring
-- dashboard, since they're different processes with no other shared
-- state. The BBS daemon clears it on startup (see internal/session).
CREATE TABLE IF NOT EXISTS sessions (
    node         INTEGER PRIMARY KEY,
    remote_ip    TEXT NOT NULL,
    term_type    TEXT NOT NULL,
    username     TEXT NOT NULL,
    connected_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Activity/system log shared by both daemons (bbs and web are
-- separate processes), so the web admin log viewer can show BBS
-- daemon events too. See internal/applog.
CREATE TABLE IF NOT EXISTS logs (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    logged_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    source    TEXT NOT NULL,
    level     TEXT NOT NULL,
    message   TEXT NOT NULL
);

-- Tracks Areafix echomail subscription requests, both directions (see
-- internal/tosser's areafix.go): 'outbound' is an area WE asked
-- uplink_host's own Areafix robot for (we're its downlink for that
-- area); 'inbound' is an area a downlink asked OUR Areafix robot for
-- (they're our downlink -- see internal/tosser's Answer/hub
-- forwarding). area_tag is a bare string, not a message_areas FK: the
-- whole point of an outbound request is often a tag we don't have a
-- local Area for yet (see message.Area.Pending -- one gets
-- auto-created once the first echomail actually arrives under that
-- tag), and an inbound request names whatever tag the downlink typed,
-- valid or not, which must be recorded either way to reply/act on it.
-- One row per (uplink, tag, direction); re-requesting the same one
-- just updates requested_at instead of erroring.
CREATE TABLE IF NOT EXISTS echo_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uplink_host   TEXT NOT NULL,
    area_tag      TEXT NOT NULL COLLATE NOCASE,
    direction     TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    requested_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (uplink_host, area_tag, direction)
);

-- Filefix's exact counterpart to echo_subscriptions above, for
-- file-echo (TIC) areas instead of message areas.
CREATE TABLE IF NOT EXISTS file_echo_subscriptions (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uplink_host   TEXT NOT NULL,
    area_tag      TEXT NOT NULL COLLATE NOCASE,
    direction     TEXT NOT NULL CHECK (direction IN ('outbound', 'inbound')),
    requested_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (uplink_host, area_tag, direction)
);

-- Which local areas a downlink (uplink_host -- the same
-- config.Binkp.Uplinks entry that authenticates its inbound Areafix/
-- Filefix requests, see internal/tosser's handleAreafixRequest) is
-- actually permitted to request. Deliberately separate from
-- echo_subscriptions' 'inbound' rows above, which only record what a
-- downlink HAS requested -- this instead gates what it's ALLOWED to
-- request in the first place: a downlink with no rows here can
-- request nothing at all, even with the correct password, until the
-- sysop explicitly grants specific areas via the web admin UI. Unlike
-- echo_subscriptions' area_tag, this one IS meant to reference a real
-- local area (there's nothing to grant access to otherwise), but
-- still isn't a message_areas FK: an area can be deleted out from
-- under a stale grant without a foreign-key error, left as a harmless
-- orphan row.
CREATE TABLE IF NOT EXISTS echo_area_grants (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uplink_host   TEXT NOT NULL,
    area_tag      TEXT NOT NULL COLLATE NOCASE,
    granted_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (uplink_host, area_tag)
);

-- file_area_grants is echo_area_grants' exact counterpart for
-- file-echo (TIC) areas, gating Filefix requests instead of Areafix.
CREATE TABLE IF NOT EXISTS file_area_grants (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    uplink_host   TEXT NOT NULL,
    area_tag      TEXT NOT NULL COLLATE NOCASE,
    granted_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    UNIQUE (uplink_host, area_tag)
);

-- inbound_archive retains a short-lived raw copy of every inbound
-- BinkP file (FTS-0001 packet/bundle, TIC descriptor, file-echo
-- payload, or anything unsupported) exactly as received, mirroring
-- how binkd/ifcico-style tossers keep a "bad packets" directory --
-- except this keeps everything, not just failures, since something
-- that tossed cleanly today can still turn out to matter later. See
-- internal/archive, which owns this table; storage_path points at
-- the actual bytes on disk (kept out of the database itself), pruned
-- automatically after archive.RetentionPeriod.
CREATE TABLE IF NOT EXISTS inbound_archive (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    filename       TEXT NOT NULL,
    uplink_address TEXT NOT NULL DEFAULT '',
    uplink_host    TEXT NOT NULL DEFAULT '',
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    received_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    storage_path   TEXT NOT NULL,
    -- outcome is "ok", "skipped", or "error" -- see
    -- internal/tosser's handleInboundFile, the only writer. detail
    -- carries the error message (outcome "error") or is empty
    -- otherwise -- "skipped" doesn't get its own reason recorded here
    -- since Result.SkippedFiles already carries none either.
    outcome        TEXT NOT NULL DEFAULT '',
    detail         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_inbound_archive_received_at ON inbound_archive(received_at);

-- binkp_sessions is internal/binkplog's own record of every BinkP
-- session's complete frame-level transcript -- storage_path points at
-- the actual (redacted, human-readable) transcript text on disk, kept
-- out of the database itself, pruned automatically after
-- binkplog.RetentionPeriod, same pattern as inbound_archive above.
CREATE TABLE IF NOT EXISTS binkp_sessions (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    -- direction is "outbound" (we dialed) or "inbound" (they dialed us).
    direction      TEXT NOT NULL,
    peer_address   TEXT NOT NULL DEFAULT '',
    peer_host      TEXT NOT NULL DEFAULT '',
    started_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    storage_path   TEXT NOT NULL,
    size_bytes     INTEGER NOT NULL DEFAULT 0,
    -- outcome is "ok" or "error"; detail carries the error message.
    outcome        TEXT NOT NULL DEFAULT '',
    detail         TEXT NOT NULL DEFAULT ''
);

CREATE INDEX IF NOT EXISTS idx_binkp_sessions_started_at ON binkp_sessions(started_at);

-- services is internal/services' registry of the daemons (bbs, mailer,
-- web): each keeps its row's heartbeat fresh while it runs, the web
-- admin asks one to restart by setting restart_requested_at, and marks
-- restart_needed when a change it saved only takes effect after one.
-- Times are Unix milliseconds, compared as plain integers.
CREATE TABLE IF NOT EXISTS services (
    name                 TEXT PRIMARY KEY,
    version              TEXT NOT NULL DEFAULT '',
    pid                  INTEGER NOT NULL DEFAULT 0,
    started_at           INTEGER NOT NULL DEFAULT 0,
    heartbeat_at         INTEGER NOT NULL DEFAULT 0,
    restart_requested_at INTEGER NOT NULL DEFAULT 0,
    -- restart_mode is "now" or "idle" (bbs: once no caller is online).
    restart_mode         TEXT NOT NULL DEFAULT '',
    -- restart_needed lists why a restart is due, "; "-separated.
    restart_needed       TEXT NOT NULL DEFAULT ''
);

-- echo_point_deliveries and netmail_point_deliveries record what a
-- point of this system (a downlink with a point address, e.g. a
-- reader app like FidoMail -- see internal/tosser's points.go) has
-- already been sent, keyed by its uplink entry's host: SEEN-BY can't
-- name points, and a point shares its net/node with this system, so
-- the SEEN-BY tracking used for nodes doesn't work for them.
-- netmail_point_deliveries is no longer written (it held the copies of
-- a "post as" point's netmail, a feature removed in v0.85.0).
CREATE TABLE IF NOT EXISTS echo_point_deliveries (
    message_id  INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    uplink_host TEXT NOT NULL,
    PRIMARY KEY (message_id, uplink_host)
);

CREATE TABLE IF NOT EXISTS netmail_point_deliveries (
    message_id  INTEGER NOT NULL REFERENCES netmail_messages(id) ON DELETE CASCADE,
    uplink_host TEXT NOT NULL,
    PRIMARY KEY (message_id, uplink_host)
);

-- maintenance_runs records each cleanup run (internal/maintenance):
-- when (Unix milliseconds) and its report as JSON.
CREATE TABLE IF NOT EXISTS maintenance_runs (
    id     INTEGER PRIMARY KEY AUTOINCREMENT,
    ran_at INTEGER NOT NULL,
    report TEXT NOT NULL
);

-- Web push subscriptions of the mobile reader (internal/push): one
-- per device a user turned notifications on for. origin is the site
-- the reader was opened on, sent as the VAPID contact.
CREATE TABLE IF NOT EXISTS push_subscriptions (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    endpoint    TEXT NOT NULL UNIQUE,
    p256dh      TEXT NOT NULL,
    auth        TEXT NOT NULL,
    origin      TEXT NOT NULL DEFAULT '',
    netmail     INTEGER NOT NULL DEFAULT 1,
    echomail    INTEGER NOT NULL DEFAULT 1,
    created_at  TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- How far internal/push's notifier got: the last netmail and message
-- id it looked at, so a restart neither repeats nor floods.
CREATE TABLE IF NOT EXISTS push_state (
    key    TEXT PRIMARY KEY,
    value  INTEGER NOT NULL
);

-- internal/guard: failed logins per IP (any of Telnet, SSH, portal,
-- admin), the IPs locked out because of them, and the sysop's allow
-- and block lists (an IP or a CIDR range). Times are Unix ms.
CREATE TABLE IF NOT EXISTS login_failures (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    ip      TEXT NOT NULL,
    handle  TEXT NOT NULL DEFAULT '',
    source  TEXT NOT NULL DEFAULT '',
    at      INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_login_failures_ip_at ON login_failures(ip, at);

CREATE TABLE IF NOT EXISTS ip_lockouts (
    ip         TEXT PRIMARY KEY,
    until      INTEGER NOT NULL,
    locked_at  INTEGER NOT NULL,
    -- how many times in a row (within a day of the last one): each
    -- lockout lasts four times as long as the one before
    strikes    INTEGER NOT NULL DEFAULT 1,
    reason     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS ip_rules (
    pattern     TEXT PRIMARY KEY,
    kind        TEXT NOT NULL CHECK (kind IN ('allow', 'block')),
    note        TEXT NOT NULL DEFAULT '',
    created_at  INTEGER NOT NULL
);

-- internal/chat: the chat rooms (the teleconference, and a room per
-- caller paging the sysop), written by the bbs daemon's Telnet/SSH
-- callers and the web admin alike. kind: say, join, leave, page.
-- Times are Unix ms.
CREATE TABLE IF NOT EXISTS chat_lines (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    room      TEXT NOT NULL,
    username  TEXT NOT NULL,
    source    TEXT NOT NULL DEFAULT '',
    kind      TEXT NOT NULL DEFAULT 'say',
    text      TEXT NOT NULL DEFAULT '',
    at        INTEGER NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_chat_lines_room_id ON chat_lines(room, id);

-- Who is in a room: refreshed every few seconds while there.
CREATE TABLE IF NOT EXISTS chat_presence (
    room       TEXT NOT NULL,
    username   TEXT NOT NULL,
    source     TEXT NOT NULL,
    last_seen  INTEGER NOT NULL,
    PRIMARY KEY (room, username, source)
);

-- The one-liners wall shown after login.
CREATE TABLE IF NOT EXISTS oneliners (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id   INTEGER REFERENCES users(id) ON DELETE SET NULL,
    username  TEXT NOT NULL,
    text      TEXT NOT NULL,
    at        INTEGER NOT NULL
);

-- internal/nodelist: each network's nodelist as last imported from its
-- file echo (FSXNET.Z75 ...), one row per system; replaced whole on
-- each import.
CREATE TABLE IF NOT EXISTS nodelist_entries (
    network   TEXT NOT NULL,
    zone      INTEGER NOT NULL,
    net       INTEGER NOT NULL,
    node      INTEGER NOT NULL,
    keyword   TEXT NOT NULL DEFAULT '',
    name      TEXT NOT NULL DEFAULT '',
    location  TEXT NOT NULL DEFAULT '',
    sysop     TEXT NOT NULL DEFAULT '',
    flags     TEXT NOT NULL DEFAULT '',
    PRIMARY KEY (network, zone, net, node)
);
CREATE INDEX IF NOT EXISTS idx_nodelist_address ON nodelist_entries(zone, net, node);

CREATE TABLE IF NOT EXISTS nodelist_imports (
    network      TEXT PRIMARY KEY,
    filename     TEXT NOT NULL,
    imported_at  INTEGER NOT NULL,
    entries      INTEGER NOT NULL
);

-- internal/community: polls (the sysop asks, callers vote once and may
-- change their vote) and the BBS list callers keep.
CREATE TABLE IF NOT EXISTS polls (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    question    TEXT NOT NULL,
    created_at  INTEGER NOT NULL,
    closed      INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS poll_options (
    id        INTEGER PRIMARY KEY AUTOINCREMENT,
    poll_id   INTEGER NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    position  INTEGER NOT NULL,
    text      TEXT NOT NULL
);
CREATE TABLE IF NOT EXISTS poll_votes (
    poll_id    INTEGER NOT NULL REFERENCES polls(id) ON DELETE CASCADE,
    user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    option_id  INTEGER NOT NULL REFERENCES poll_options(id) ON DELETE CASCADE,
    at         INTEGER NOT NULL,
    PRIMARY KEY (poll_id, user_id)
);

CREATE TABLE IF NOT EXISTS bbs_list (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    name         TEXT NOT NULL,
    address      TEXT NOT NULL,
    sysop        TEXT NOT NULL DEFAULT '',
    software     TEXT NOT NULL DEFAULT '',
    description  TEXT NOT NULL DEFAULT '',
    added_by_id  INTEGER REFERENCES users(id) ON DELETE SET NULL,
    added_by     TEXT NOT NULL DEFAULT '',
    updated_at   INTEGER NOT NULL
);

-- internal/community: the sysop's news, in English and German (a
-- caller reads theirs, else the other), and which a caller has seen --
-- the unseen ones show at login. expires_at 0: never.
CREATE TABLE IF NOT EXISTS news (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    created_at  INTEGER NOT NULL,
    author      TEXT NOT NULL DEFAULT '',
    title_en    TEXT NOT NULL DEFAULT '',
    text_en     TEXT NOT NULL DEFAULT '',
    title_de    TEXT NOT NULL DEFAULT '',
    text_de     TEXT NOT NULL DEFAULT '',
    expires_at  INTEGER NOT NULL DEFAULT 0
);
CREATE TABLE IF NOT EXISTS news_seen (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    news_id  INTEGER NOT NULL REFERENCES news(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, news_id)
);

-- Two-factor login (user/totp.go): one-time recovery codes, bcrypt-
-- hashed, for an account whose authenticator is lost.
CREATE TABLE IF NOT EXISTS totp_recovery (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    hash     TEXT NOT NULL,
    used     INTEGER NOT NULL DEFAULT 0
);

-- internal/health: the problems the monitor sees now, so each is
-- announced once when it starts and once when it's over.
CREATE TABLE IF NOT EXISTS health_problems (
    key     TEXT PRIMARY KEY,
    title   TEXT NOT NULL,
    detail  TEXT NOT NULL DEFAULT '',
    since   INTEGER NOT NULL
);

-- internal/doors daily maintenance: per door, when its nightly command
-- last ran and how it went; requested_at asks for a run now (the web
-- admin's "Run now", carried out by the bbs daemon). Times Unix ms.
CREATE TABLE IF NOT EXISTS door_daily (
    door          TEXT PRIMARY KEY,
    last_day      TEXT NOT NULL DEFAULT '',
    last_at       INTEGER NOT NULL DEFAULT 0,
    ok            INTEGER NOT NULL DEFAULT 0,
    detail        TEXT NOT NULL DEFAULT '',
    requested_at  INTEGER NOT NULL DEFAULT 0
);

-- One-time data migrations already done (see db.go's backfillThreads).
CREATE TABLE IF NOT EXISTS meta (
    key   TEXT PRIMARY KEY,
    value TEXT NOT NULL DEFAULT ''
);

-- Statistics (internal/stats): every call (Telnet/SSH login, web
-- portal or reader login) and every door played.
CREATE TABLE IF NOT EXISTS calls (
    id      INTEGER PRIMARY KEY AUTOINCREMENT,
    user_id INTEGER NOT NULL,
    via     TEXT NOT NULL,
    at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE INDEX IF NOT EXISTS idx_calls_at ON calls(at);

CREATE TABLE IF NOT EXISTS door_sessions (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    door       TEXT NOT NULL,
    user_id    INTEGER NOT NULL,
    started_at TIMESTAMP NOT NULL,
    seconds    INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_door_sessions_started ON door_sessions(started_at);

-- Chat rooms callers may enter (internal/chat): the teleconference
-- "main" and whatever the sysop adds; each may be bridged to a Discord
-- channel. Page rooms ("page-<handle>") are never listed here.
CREATE TABLE IF NOT EXISTS chat_rooms (
    name            TEXT PRIMARY KEY,
    title           TEXT NOT NULL,
    topic           TEXT NOT NULL DEFAULT '',
    min_sl          INTEGER NOT NULL DEFAULT 0,
    sort_order      INTEGER NOT NULL DEFAULT 0,
    discord_channel TEXT NOT NULL DEFAULT ''
);
INSERT OR IGNORE INTO chat_rooms (name, title, topic) VALUES ('main', 'Teleconference', 'Everyone''s room');

-- Areas a caller took out of "their areas" (internal/message): the new
-- scan, QWK packets and the reader app leave them out. Everything else
-- is in -- a new area too.
CREATE TABLE IF NOT EXISTS area_unsubscribed (
    user_id  INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    area_id  INTEGER NOT NULL REFERENCES message_areas(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, area_id)
);

-- The email gateway (internal/emailgw): a netmail with
-- netmail_messages.email set is a mail to (from_user_id set) or from
-- (from_user_id NULL) that address. Here its Message-ID, the one it
-- answers, and how sending it goes.
CREATE TABLE IF NOT EXISTS email_meta (
    netmail_id   INTEGER PRIMARY KEY REFERENCES netmail_messages(id) ON DELETE CASCADE,
    message_id   TEXT NOT NULL DEFAULT '',
    in_reply_to  TEXT NOT NULL DEFAULT '',
    attempts     INTEGER NOT NULL DEFAULT 0,
    next_try     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT NOT NULL DEFAULT '',
    failed       INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX IF NOT EXISTS idx_email_meta_message_id ON email_meta(message_id) WHERE message_id != '';
