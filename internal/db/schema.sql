CREATE TABLE IF NOT EXISTS users (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    username       TEXT NOT NULL COLLATE NOCASE UNIQUE,
    password_hash  TEXT NOT NULL,
    security_level INTEGER NOT NULL DEFAULT 10,
    created_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    last_login_at  TIMESTAMP,
    total_calls    INTEGER NOT NULL DEFAULT 0
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
    created_at    TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Seeded once (idempotent via the UNIQUE tag) so a fresh install has a
-- working message base without requiring sysop setup first.
INSERT OR IGNORE INTO message_areas (tag, name, description, min_sl_read, min_sl_write, sort_order)
VALUES ('general', 'General Discussion', 'General chat for all callers', 0, 0, 0);

CREATE TABLE IF NOT EXISTS messages (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id       INTEGER NOT NULL REFERENCES message_areas(id) ON DELETE CASCADE,
    from_user_id  INTEGER NOT NULL REFERENCES users(id),
    to_name       TEXT NOT NULL DEFAULT 'All',
    subject       TEXT NOT NULL,
    body          TEXT NOT NULL,
    posted_at     TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_messages_area_posted ON messages(area_id, posted_at);

-- Tracks exactly which individual messages a caller has actually
-- opened in the reader, so a message stays flagged "New" in the
-- message list -- and counted in the area lightbar's "New" column --
-- until it's really read, not merely until the area was visited.
CREATE TABLE IF NOT EXISTS message_reads (
    user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    message_id  INTEGER NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    PRIMARY KEY (user_id, message_id)
);

CREATE TABLE IF NOT EXISTS file_areas (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    tag              TEXT NOT NULL COLLATE NOCASE UNIQUE,
    name             TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    min_sl_download  INTEGER NOT NULL DEFAULT 0,
    min_sl_upload    INTEGER NOT NULL DEFAULT 0,
    sort_order       INTEGER NOT NULL DEFAULT 0,
    created_at       TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT OR IGNORE INTO file_areas (tag, name, description, min_sl_download, min_sl_upload, sort_order)
VALUES ('general', 'General Files', 'General file library for all callers', 0, 0, 0);

CREATE TABLE IF NOT EXISTS files (
    id               INTEGER PRIMARY KEY AUTOINCREMENT,
    area_id          INTEGER NOT NULL REFERENCES file_areas(id) ON DELETE CASCADE,
    filename         TEXT NOT NULL,
    description      TEXT NOT NULL DEFAULT '',
    size_bytes       INTEGER NOT NULL,
    storage_path     TEXT NOT NULL,
    uploaded_by      INTEGER NOT NULL REFERENCES users(id),
    uploaded_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    download_count   INTEGER NOT NULL DEFAULT 0,
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
-- NULL when to_address is a remote system this BBS can't reach yet,
-- leaving the message queued until the mailer is built. Since a
-- netmail message always has exactly one recipient (unlike an echo
-- area's many readers), read state lives directly on the row rather
-- than needing a separate per-user reads table.
CREATE TABLE IF NOT EXISTS netmail_messages (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    from_user_id   INTEGER NOT NULL REFERENCES users(id),
    from_address   TEXT NOT NULL DEFAULT '',
    to_user_id     INTEGER REFERENCES users(id) ON DELETE CASCADE,
    to_name        TEXT NOT NULL,
    to_address     TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,
    body           TEXT NOT NULL,
    posted_at      TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
    read_at        TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_netmail_to_user_posted ON netmail_messages(to_user_id, posted_at);

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
