-- 0001_init: initial Dietsis schema. See docs/DATABASE.md for semantics.
--
-- Conventions
--   * Every table is STRICT: SQLite rejects values of the wrong storage class.
--   * Timestamps are INTEGER Unix epoch milliseconds (UTC), set by the server.
--   * Dates are TEXT 'YYYY-MM-DD' calendar days in the user's local timezone (validated in Go).
--   * Every user-owned table carries user_id as the leading primary-key column, so every
--     lookup is scoped by owner and cross-user IDOR is structurally impossible.
--   * Range limits (age, kcal, ...) live in internal/validate; CHECKs here guard only invariants
--     (enums, non-negative numbers) so policy changes do not need a table rebuild.
--   * Mutable rows carry `version`, incremented on each write; used for conflict detection.

CREATE TABLE users (
    id          TEXT    NOT NULL PRIMARY KEY,           -- server-generated UUID
    google_sub  TEXT    NOT NULL UNIQUE,                -- stable Google account identifier
    email       TEXT    NOT NULL,
    name        TEXT    NOT NULL DEFAULT '',
    picture     TEXT    NOT NULL DEFAULT '',
    data_epoch  INTEGER NOT NULL DEFAULT 1 CHECK (data_epoch >= 1), -- bumped by "delete all data"
    created_at  INTEGER NOT NULL,
    updated_at  INTEGER NOT NULL
) STRICT;

CREATE TABLE sessions (
    token_hash   BLOB    NOT NULL PRIMARY KEY,          -- SHA-256 of the cookie token; raw token never stored
    user_id      TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    created_at   INTEGER NOT NULL,
    last_seen_at INTEGER NOT NULL,
    expires_at   INTEGER NOT NULL,
    user_agent   TEXT    NOT NULL DEFAULT ''
) STRICT, WITHOUT ROWID;

CREATE INDEX sessions_user_idx    ON sessions(user_id);
CREATE INDEX sessions_expires_idx ON sessions(expires_at);

CREATE TABLE profiles (
    user_id               TEXT    NOT NULL PRIMARY KEY REFERENCES users(id) ON DELETE CASCADE,
    display_name          TEXT    NOT NULL,
    gender                TEXT    NOT NULL CHECK (gender IN ('male', 'female')),
    age                   INTEGER NOT NULL CHECK (age > 0),
    height_cm             REAL    NOT NULL CHECK (height_cm > 0),
    weight_kg             REAL    NOT NULL CHECK (weight_kg > 0),
    goal_weight_kg        REAL    NOT NULL CHECK (goal_weight_kg > 0),
    activity_level        TEXT    NOT NULL CHECK (activity_level IN ('sedentary', 'light', 'moderate', 'active', 'very_active')),
    calorie_target        INTEGER NOT NULL CHECK (calorie_target > 0),
    protein_target_g      INTEGER NOT NULL CHECK (protein_target_g > 0),
    water_target_glasses  INTEGER NOT NULL CHECK (water_target_glasses > 0),
    calorie_target_manual INTEGER NOT NULL DEFAULT 0 CHECK (calorie_target_manual IN (0, 1)),
    protein_target_manual INTEGER NOT NULL DEFAULT 0 CHECK (protein_target_manual IN (0, 1)),
    water_target_manual   INTEGER NOT NULL DEFAULT 0 CHECK (water_target_manual IN (0, 1)),
    version               INTEGER NOT NULL DEFAULT 1,
    created_at            INTEGER NOT NULL,
    updated_at            INTEGER NOT NULL
) STRICT;

CREATE TABLE favorites (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    food_id    TEXT    NOT NULL,                        -- 'b:<slug>' built-in or 'c:<uuid>' custom
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, food_id)
) STRICT, WITHOUT ROWID;

CREATE TABLE days (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date          TEXT    NOT NULL,
    water_glasses INTEGER NOT NULL DEFAULT 0 CHECK (water_glasses >= 0),
    steps         INTEGER CHECK (steps IS NULL OR steps >= 0),
    note          TEXT    NOT NULL DEFAULT '',
    version       INTEGER NOT NULL DEFAULT 1,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, date)
) STRICT, WITHOUT ROWID;

-- Nutrient columns are a per-serving snapshot taken when the food was logged, so later edits to a
-- custom food never rewrite history. Totals = per-serving x quantity (see internal/nutrition).
CREATE TABLE food_logs (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    id            TEXT    NOT NULL,                     -- client-generated UUID (offline create)
    date          TEXT    NOT NULL,
    meal          TEXT    NOT NULL CHECK (meal IN ('breakfast', 'lunch', 'dinner', 'snack')),
    food_id       TEXT,                                 -- NULL for one-off entries
    name          TEXT    NOT NULL,
    serving_label TEXT    NOT NULL,
    serving_grams REAL    CHECK (serving_grams IS NULL OR serving_grams > 0),
    quantity      REAL    NOT NULL CHECK (quantity > 0),
    kcal          REAL    NOT NULL CHECK (kcal >= 0),
    protein_g     REAL    NOT NULL CHECK (protein_g >= 0),
    carbs_g       REAL    NOT NULL CHECK (carbs_g >= 0),
    fat_g         REAL    NOT NULL CHECK (fat_g >= 0),
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    deleted_at    INTEGER,                              -- soft delete; enables restore (undo)
    PRIMARY KEY (user_id, id)
) STRICT, WITHOUT ROWID;

CREATE INDEX food_logs_user_date_idx   ON food_logs(user_id, date) WHERE deleted_at IS NULL;
CREATE INDEX food_logs_user_recent_idx ON food_logs(user_id, created_at DESC) WHERE deleted_at IS NULL;
CREATE INDEX food_logs_deleted_idx     ON food_logs(deleted_at) WHERE deleted_at IS NOT NULL;

CREATE TABLE custom_foods (
    user_id       TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    id            TEXT    NOT NULL,                     -- client-generated UUID
    name          TEXT    NOT NULL,
    serving_label TEXT    NOT NULL,
    serving_grams REAL    CHECK (serving_grams IS NULL OR serving_grams > 0),
    kcal          REAL    NOT NULL CHECK (kcal >= 0),
    protein_g     REAL    NOT NULL CHECK (protein_g >= 0),
    carbs_g       REAL    NOT NULL CHECK (carbs_g >= 0),
    fat_g         REAL    NOT NULL CHECK (fat_g >= 0),
    version       INTEGER NOT NULL DEFAULT 1,
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    PRIMARY KEY (user_id, id)
) STRICT, WITHOUT ROWID;

CREATE TABLE weights (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    date       TEXT    NOT NULL,
    weight_kg  REAL    NOT NULL CHECK (weight_kg > 0),
    waist_cm   REAL    CHECK (waist_cm IS NULL OR waist_cm > 0),
    version    INTEGER NOT NULL DEFAULT 1,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, date)                         -- one weight entry per user per day
) STRICT, WITHOUT ROWID;

-- Idempotency ledger: one row per client operation. A retried op_id returns the stored result
-- instead of being applied again (water increments are never double-counted).
CREATE TABLE sync_operations (
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    op_id      TEXT    NOT NULL,
    op_type    TEXT    NOT NULL,
    status     TEXT    NOT NULL CHECK (status IN ('applied', 'conflict', 'rejected')),
    result     TEXT    NOT NULL,                        -- JSON result replayed on retry
    created_at INTEGER NOT NULL,
    PRIMARY KEY (user_id, op_id)
) STRICT, WITHOUT ROWID;

CREATE INDEX sync_operations_created_idx ON sync_operations(created_at);

-- Incremental-sync feed. Exactly one row per (user, entity, key): every write deletes the previous
-- row for that key and appends a new one, so the table stays compact and any cursor stays valid.
-- AUTOINCREMENT guarantees seq is never reused, even after rows are deleted.
CREATE TABLE change_log (
    seq        INTEGER NOT NULL PRIMARY KEY AUTOINCREMENT,
    user_id    TEXT    NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    entity     TEXT    NOT NULL CHECK (entity IN ('profile', 'favorite', 'day', 'food_log', 'custom_food', 'weight')),
    entity_key TEXT    NOT NULL,
    changed_at INTEGER NOT NULL
) STRICT;

CREATE UNIQUE INDEX change_log_entity_idx  ON change_log(user_id, entity, entity_key);
CREATE INDEX        change_log_user_seq_idx ON change_log(user_id, seq);
