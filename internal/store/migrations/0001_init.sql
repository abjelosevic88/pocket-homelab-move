CREATE TABLE users (
  id            INTEGER PRIMARY KEY AUTOINCREMENT,
  email         TEXT NOT NULL UNIQUE,
  name          TEXT NOT NULL DEFAULT '',
  password_hash TEXT NOT NULL DEFAULT '',
  role          TEXT NOT NULL DEFAULT 'member',
  created_at    TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  updated_at    TEXT
);

CREATE TABLE sessions (
  id         TEXT PRIMARY KEY,
  user_id    INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

CREATE TABLE api_tokens (
  id           INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id      INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name         TEXT NOT NULL DEFAULT '',
  token_hash   TEXT NOT NULL UNIQUE,
  prefix       TEXT NOT NULL DEFAULT '',
  last_used_at TEXT,
  created_at   TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now'))
);

-- Box-wide settings (title, setup flag); everything else is personal.
CREATE TABLE settings (
  key   TEXT PRIMARY KEY,
  value TEXT NOT NULL DEFAULT ''
);

CREATE TABLE user_settings (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  key     TEXT NOT NULL,
  value   TEXT NOT NULL DEFAULT '',
  PRIMARY KEY (user_id, key)
);

CREATE TABLE cache (
  key        TEXT PRIMARY KEY,
  value      TEXT NOT NULL DEFAULT '',
  fetched_at TEXT NOT NULL
);

-- Moving out: one archive of a person's data from the apps they picked.
CREATE TABLE moves_out (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'queued',   -- queued | running | done | failed
  apps        TEXT NOT NULL DEFAULT '[]',       -- JSON list of app slugs
  results     TEXT NOT NULL DEFAULT '[]',       -- JSON per app: state, files
  file        TEXT NOT NULL DEFAULT '',         -- the zip on disk
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  finished_at TEXT NOT NULL DEFAULT '',
  expires_at  TEXT NOT NULL DEFAULT ''
);
CREATE INDEX moves_out_user ON moves_out(user_id, id);

-- Moving in: one uploaded file or archive and what was found in it.
CREATE TABLE moves_in (
  id          INTEGER PRIMARY KEY AUTOINCREMENT,
  user_id     INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  status      TEXT NOT NULL DEFAULT 'uploading', -- uploading | scanning | ready | running | done | failed
  name        TEXT NOT NULL DEFAULT '',
  source      TEXT NOT NULL DEFAULT '',
  source_name TEXT NOT NULL DEFAULT '',
  file        TEXT NOT NULL DEFAULT '',
  size_bytes  INTEGER NOT NULL DEFAULT 0,
  error       TEXT NOT NULL DEFAULT '',
  notes       TEXT NOT NULL DEFAULT '[]',
  created_at  TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%fZ','now')),
  finished_at TEXT NOT NULL DEFAULT ''
);
CREATE INDEX moves_in_user ON moves_in(user_id, id);

-- One thing found in a move-in, converted to an open format, and where it goes.
CREATE TABLE move_items (
  id        INTEGER PRIMARY KEY AUTOINCREMENT,
  move_id   INTEGER NOT NULL REFERENCES moves_in(id) ON DELETE CASCADE,
  user_id   INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  label     TEXT NOT NULL DEFAULT '',
  detail    TEXT NOT NULL DEFAULT '',
  kind      TEXT NOT NULL DEFAULT '',
  count     INTEGER NOT NULL DEFAULT 0,
  file      TEXT NOT NULL DEFAULT '',
  file_name TEXT NOT NULL DEFAULT '',
  bytes     INTEGER NOT NULL DEFAULT 0,
  sha256    TEXT NOT NULL DEFAULT '',
  target    TEXT NOT NULL DEFAULT '',
  path      TEXT NOT NULL DEFAULT '',
  extra     TEXT NOT NULL DEFAULT '{}',
  include   INTEGER NOT NULL DEFAULT 1,
  manual    TEXT NOT NULL DEFAULT '',
  status    TEXT NOT NULL DEFAULT 'waiting',  -- waiting | sent | failed | skipped | done-before
  result    TEXT NOT NULL DEFAULT 'null',
  error     TEXT NOT NULL DEFAULT '',
  sent_at   TEXT NOT NULL DEFAULT '',
  sort      INTEGER NOT NULL DEFAULT 0
);
CREATE INDEX move_items_user ON move_items(user_id, move_id, sort);
CREATE INDEX move_items_sent ON move_items(user_id, target, sha256, status);
