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
