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
