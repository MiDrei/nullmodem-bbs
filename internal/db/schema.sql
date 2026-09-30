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
-- netmail_point_deliveries holds copies of netmail to a point's
-- "post as" user, which stay in that user's inbox as well.
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
