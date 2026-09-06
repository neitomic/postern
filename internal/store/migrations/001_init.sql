CREATE TABLE schema_migrations (
    version    INTEGER PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

CREATE TABLE hosts (
    id               INTEGER PRIMARY KEY,
    name             TEXT    NOT NULL COLLATE NOCASE,
    login_user       TEXT    NOT NULL,
    port             INTEGER NOT NULL,
    key_fingerprint  TEXT    NOT NULL,
    pubkey           TEXT    NOT NULL,
    tags_json        TEXT    NOT NULL DEFAULT '[]',
    last_seen        INTEGER NULL,
    created_at       INTEGER NOT NULL,
    updated_at       INTEGER NOT NULL,
    disabled         INTEGER NOT NULL DEFAULT 0,
    CHECK (port >= 1 AND port <= 65535),
    CHECK (disabled IN (0, 1)),
    CHECK (json_valid(tags_json))
);

CREATE UNIQUE INDEX idx_hosts_name ON hosts(name);
CREATE UNIQUE INDEX idx_hosts_port ON hosts(port);
CREATE UNIQUE INDEX idx_hosts_fingerprint ON hosts(key_fingerprint);
CREATE INDEX idx_hosts_last_seen ON hosts(last_seen);

CREATE TABLE tokens (
    id           TEXT PRIMARY KEY,
    kind         TEXT NOT NULL,          -- MVP: 'join' only
    secret_hash  TEXT NOT NULL,          -- hex(sha256 digest), 64 chars
    expires_at   INTEGER NOT NULL,
    used_at      INTEGER NULL,
    created_at   INTEGER NOT NULL,
    note         TEXT NULL,
    bound_name   TEXT NULL COLLATE NOCASE,
    CHECK (kind IN ('join'))
);

CREATE INDEX idx_tokens_expires ON tokens(expires_at);
CREATE INDEX idx_tokens_kind ON tokens(kind);
