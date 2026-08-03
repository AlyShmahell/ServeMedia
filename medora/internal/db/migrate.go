package db

import (
	"context"
	"fmt"
)

const migrationV1 = `
CREATE TABLE IF NOT EXISTS schema_migrations (
  version INTEGER PRIMARY KEY,
  applied_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY,
  username TEXT NOT NULL UNIQUE COLLATE NOCASE,
  password_hash TEXT NOT NULL,
  role TEXT NOT NULL CHECK (role IN ('admin','user')),
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  token_hash TEXT NOT NULL UNIQUE,
  expires_at TEXT NOT NULL,
  created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS libraries (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('movies','tv')),
  path TEXT NOT NULL UNIQUE,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_items (
  id INTEGER PRIMARY KEY,
  library_id INTEGER NOT NULL REFERENCES libraries(id) ON DELETE CASCADE,
  kind TEXT NOT NULL CHECK (kind IN ('movie','show')),
  title TEXT NOT NULL,
  sort_title TEXT,
  year INTEGER,
  path TEXT NOT NULL,
  runtime_seconds INTEGER,
  plot TEXT,
  poster_path TEXT,
  backdrop_path TEXT,
  nfo_path TEXT,
  mtime INTEGER NOT NULL,
  date_added TEXT NOT NULL,
  UNIQUE(library_id, path)
);

CREATE TABLE IF NOT EXISTS seasons (
  id INTEGER PRIMARY KEY,
  show_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
  season_number INTEGER NOT NULL,
  title TEXT,
  poster_path TEXT,
  UNIQUE(show_id, season_number)
);

CREATE TABLE IF NOT EXISTS episodes (
  id INTEGER PRIMARY KEY,
  season_id INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
  show_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
  episode_number INTEGER NOT NULL,
  title TEXT,
  path TEXT NOT NULL UNIQUE,
  runtime_seconds INTEGER,
  plot TEXT,
  still_path TEXT,
  nfo_path TEXT,
  mtime INTEGER NOT NULL,
  date_added TEXT NOT NULL,
  UNIQUE(season_id, episode_number)
);

CREATE TABLE IF NOT EXISTS watch_progress (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  media_item_id INTEGER REFERENCES media_items(id) ON DELETE CASCADE,
  episode_id INTEGER REFERENCES episodes(id) ON DELETE CASCADE,
  position_seconds REAL NOT NULL DEFAULT 0,
  duration_seconds REAL,
  completed INTEGER NOT NULL DEFAULT 0,
  updated_at TEXT NOT NULL,
  CHECK (
    (media_item_id IS NOT NULL AND episode_id IS NULL) OR
    (media_item_id IS NULL AND episode_id IS NOT NULL)
  )
);
CREATE UNIQUE INDEX IF NOT EXISTS watch_progress_movie ON watch_progress(user_id, media_item_id) WHERE media_item_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS watch_progress_ep ON watch_progress(user_id, episode_id) WHERE episode_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS scan_jobs (
  id INTEGER PRIMARY KEY,
  library_id INTEGER REFERENCES libraries(id) ON DELETE SET NULL,
  status TEXT NOT NULL,
  progress_pct INTEGER NOT NULL DEFAULT 0,
  message TEXT,
  started_at TEXT,
  finished_at TEXT
);

CREATE TABLE IF NOT EXISTS transcode_jobs (
  id INTEGER PRIMARY KEY,
  source_path TEXT NOT NULL,
  output_dir TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at TEXT NOT NULL,
  last_access TEXT NOT NULL
);
`

const migrationV2 = `
PRAGMA foreign_keys=OFF;

CREATE TABLE libraries_v2 (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('movies','tv')),
  path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, path)
);

INSERT INTO libraries_v2(id, user_id, name, type, path, created_at, updated_at)
SELECT l.id,
  COALESCE(
    (SELECT id FROM users WHERE role = 'admin' ORDER BY id LIMIT 1),
    (SELECT id FROM users ORDER BY id LIMIT 1),
    0
  ),
  l.name, l.type, l.path, l.created_at, l.updated_at
FROM libraries l;

DROP TABLE libraries;
ALTER TABLE libraries_v2 RENAME TO libraries;

CREATE TABLE episodes_v2 (
  id INTEGER PRIMARY KEY,
  season_id INTEGER NOT NULL REFERENCES seasons(id) ON DELETE CASCADE,
  show_id INTEGER NOT NULL REFERENCES media_items(id) ON DELETE CASCADE,
  episode_number INTEGER NOT NULL,
  title TEXT,
  path TEXT NOT NULL,
  runtime_seconds INTEGER,
  plot TEXT,
  still_path TEXT,
  nfo_path TEXT,
  mtime INTEGER NOT NULL,
  date_added TEXT NOT NULL,
  UNIQUE(season_id, episode_number),
  UNIQUE(show_id, path)
);

INSERT INTO episodes_v2 SELECT * FROM episodes;
DROP TABLE episodes;
ALTER TABLE episodes_v2 RENAME TO episodes;

PRAGMA foreign_keys=ON;
`

const migrationV3 = `
PRAGMA foreign_keys=OFF;

CREATE TABLE libraries_v3 (
  id INTEGER PRIMARY KEY,
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  name TEXT NOT NULL,
  type TEXT NOT NULL CHECK (type IN ('movies','tv','anime')),
  path TEXT NOT NULL,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  UNIQUE(user_id, path)
);

INSERT INTO libraries_v3(id, user_id, name, type, path, created_at, updated_at)
SELECT id, user_id, name, type, path, created_at, updated_at FROM libraries;

DROP TABLE libraries;
ALTER TABLE libraries_v3 RENAME TO libraries;

PRAGMA foreign_keys=ON;
`

const migrationV5 = `
ALTER TABLE seasons ADD COLUMN plot TEXT;
`

const migrationV6 = `
CREATE TABLE IF NOT EXISTS async_jobs (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL CHECK (kind IN ('subtitles','metadata')),
  scope TEXT NOT NULL CHECK (scope IN ('movie','show','season','episode')),
  target_id INTEGER NOT NULL,
  payload_json TEXT,
  status TEXT NOT NULL,
  progress_pct INTEGER NOT NULL DEFAULT 0,
  message TEXT,
  started_at TEXT,
  finished_at TEXT
);

ALTER TABLE media_items ADD COLUMN tmdb_id INTEGER;
ALTER TABLE seasons ADD COLUMN tmdb_id INTEGER;
ALTER TABLE episodes ADD COLUMN tmdb_id INTEGER;
`

const migrationV7 = `
ALTER TABLE media_items ADD COLUMN rating REAL;
`

const migrationV8 = `
ALTER TABLE media_items ADD COLUMN meta_provider TEXT;
ALTER TABLE media_items ADD COLUMN meta_id TEXT;
ALTER TABLE seasons ADD COLUMN meta_provider TEXT;
ALTER TABLE seasons ADD COLUMN meta_id TEXT;
ALTER TABLE episodes ADD COLUMN meta_provider TEXT;
ALTER TABLE episodes ADD COLUMN meta_id TEXT;
`

const migrationV4 = `
CREATE TABLE IF NOT EXISTS playback_prefs (
  user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  scope TEXT NOT NULL CHECK (scope IN ('user','library','show','movie')),
  scope_id INTEGER NOT NULL DEFAULT 0,
  volume REAL,
  muted INTEGER,
  height INTEGER,
  audio_lang TEXT,
  subtitle_lang TEXT,
  updated_at TEXT NOT NULL,
  PRIMARY KEY (user_id, scope, scope_id)
);
`

func (d *DB) Migrate(ctx context.Context) error {
	if _, err := d.SQL.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version INTEGER PRIMARY KEY,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return err
	}
	var ver int
	err := d.SQL.QueryRowContext(ctx, `SELECT COALESCE(MAX(version),0) FROM schema_migrations`).Scan(&ver)
	if err != nil {
		return err
	}
	if ver < 1 {
		if _, err := d.SQL.ExecContext(ctx, migrationV1); err != nil {
			return fmt.Errorf("migration v1: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (1, ?)`, now()); err != nil {
			return err
		}
		ver = 1
	}
	if ver < 2 {
		var hasUserID int
		_ = d.SQL.QueryRowContext(ctx, `SELECT COUNT(*) FROM pragma_table_info('libraries') WHERE name = 'user_id'`).Scan(&hasUserID)
		if hasUserID == 0 {
			if _, err := d.SQL.ExecContext(ctx, migrationV2); err != nil {
				return fmt.Errorf("migration v2: %w", err)
			}
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (2, ?)`, now()); err != nil {
			return err
		}
		ver = 2
	}
	if ver < 3 {
		if _, err := d.SQL.ExecContext(ctx, migrationV3); err != nil {
			return fmt.Errorf("migration v3: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (3, ?)`, now()); err != nil {
			return err
		}
		ver = 3
	}
	if ver < 4 {
		if _, err := d.SQL.ExecContext(ctx, migrationV4); err != nil {
			return fmt.Errorf("migration v4: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (4, ?)`, now()); err != nil {
			return err
		}
		ver = 4
	}
	if ver < 5 {
		if _, err := d.SQL.ExecContext(ctx, migrationV5); err != nil {
			return fmt.Errorf("migration v5: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (5, ?)`, now()); err != nil {
			return err
		}
		ver = 5
	}
	if ver < 6 {
		if _, err := d.SQL.ExecContext(ctx, migrationV6); err != nil {
			return fmt.Errorf("migration v6: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (6, ?)`, now()); err != nil {
			return err
		}
		ver = 6
	}
	if ver < 7 {
		if _, err := d.SQL.ExecContext(ctx, migrationV7); err != nil {
			return fmt.Errorf("migration v7: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (7, ?)`, now()); err != nil {
			return err
		}
		ver = 7
	}
	if ver < 8 {
		if _, err := d.SQL.ExecContext(ctx, migrationV8); err != nil {
			return fmt.Errorf("migration v8: %w", err)
		}
		if _, err := d.SQL.ExecContext(ctx, `INSERT INTO schema_migrations(version, applied_at) VALUES (8, ?)`, now()); err != nil {
			return err
		}
	}
	return nil
}
