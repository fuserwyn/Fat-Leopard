package database

import (
	"database/sql"
	"fmt"
	"net/url"
	"strings"
)

const trackerBoardSchema = `
CREATE TABLE IF NOT EXISTS pack_tracker_tasks (
	id            BIGSERIAL PRIMARY KEY,
	num           INT NOT NULL,
	prompt        TEXT NOT NULL,
	when_at       TIMESTAMP WITH TIME ZONE NOT NULL,
	when_label    TEXT NOT NULL DEFAULT '',
	repeat        TEXT NOT NULL DEFAULT 'разово',
	kind          TEXT NOT NULL DEFAULT 'task',
	status        TEXT NOT NULL DEFAULT 'pending',
	dev_column    TEXT NOT NULL DEFAULT 'todo',
	qa_column     TEXT,
	qa_status     TEXT,
	handed_to_qa  BOOLEAN NOT NULL DEFAULT FALSE,
	auto_review   BOOLEAN NOT NULL DEFAULT FALSE,
	manual_qa     BOOLEAN NOT NULL DEFAULT FALSE,
	fast_track    BOOLEAN NOT NULL DEFAULT FALSE,
	auto_push     BOOLEAN NOT NULL DEFAULT FALSE,
	needs_approval BOOLEAN NOT NULL DEFAULT FALSE,
	approvals     JSONB NOT NULL DEFAULT '[]'::jsonb,
	error         TEXT NOT NULL DEFAULT '',
	result        TEXT NOT NULL DEFAULT '',
	steps         JSONB NOT NULL DEFAULT '[]'::jsonb,
	author_id     BIGINT,
	created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
	last_run_at   TIMESTAMP WITH TIME ZONE,
	updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE UNIQUE INDEX IF NOT EXISTS pack_tracker_tasks_num_uidx
	ON pack_tracker_tasks (num);
CREATE INDEX IF NOT EXISTS pack_tracker_tasks_board_idx
	ON pack_tracker_tasks (dev_column, created_at DESC);

CREATE TABLE IF NOT EXISTS pack_tracker_attachments (
	id         TEXT PRIMARY KEY,
	task_id    BIGINT NOT NULL REFERENCES pack_tracker_tasks(id) ON DELETE CASCADE,
	name       TEXT NOT NULL,
	mime       TEXT NOT NULL,
	size       INT NOT NULL,
	data       BYTEA NOT NULL,
	created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS pack_tracker_attachments_task_idx
	ON pack_tracker_attachments (task_id, created_at);

CREATE TABLE IF NOT EXISTS leo_autonomy (
	id            BOOLEAN PRIMARY KEY DEFAULT TRUE,
	active_until  TIMESTAMP WITH TIME ZONE,
	every_hours   INT NOT NULL DEFAULT 4,
	tasks_per_run INT NOT NULL DEFAULT 3,
	last_run_at   TIMESTAMP WITH TIME ZONE,
	last_note     TEXT,
	updated_by    BIGINT,
	updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS pack_tracker_settings (
	key        TEXT PRIMARY KEY,
	value      TEXT NOT NULL DEFAULT '',
	updated_by BIGINT,
	updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS ms_tracker_jobs (
	id              BIGSERIAL PRIMARY KEY,
	source_task_id  BIGINT NOT NULL DEFAULT 0,
	source_num      INT NOT NULL DEFAULT 0,
	author_id       BIGINT NOT NULL DEFAULT 0,
	prompt          TEXT NOT NULL,
	phase           TEXT NOT NULL DEFAULT 'doing',
	when_at         TIMESTAMPTZ NOT NULL,
	when_label      TEXT NOT NULL DEFAULT '',
	status          TEXT NOT NULL DEFAULT 'pending',
	error           TEXT NOT NULL DEFAULT '',
	result          TEXT NOT NULL DEFAULT '',
	steps           JSONB NOT NULL DEFAULT '[]'::jsonb,
	model           TEXT NOT NULL DEFAULT '',
	auto_push       BOOLEAN NOT NULL DEFAULT FALSE,
	branch          TEXT NOT NULL DEFAULT '',
	created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
	updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS ms_tracker_jobs_due_idx
	ON ms_tracker_jobs (status, when_at);
`

// trackerDB — пул доски. Если второй базы нет, тесты и локалка пишут в основную.
func (d *Database) trackerDB() *sql.DB {
	if d == nil {
		return nil
	}
	if d.tracker != nil {
		return d.tracker
	}
	return d.db
}

// EnsureTrackerSchema — таблицы доски на том пуле, куда сейчас пишет трекер.
func (d *Database) EnsureTrackerSchema() error {
	if d == nil || d.trackerDB() == nil {
		return fmt.Errorf("база недоступна")
	}
	if _, err := d.trackerDB().Exec(trackerBoardSchema); err != nil {
		return fmt.Errorf("tracker schema: %w", err)
	}
	if err := d.migrateTrackerBoardSchema(); err != nil {
		return err
	}
	return nil
}

func (d *Database) migrateTrackerBoardSchema() error {
	if d == nil || d.trackerDB() == nil {
		return fmt.Errorf("база недоступна")
	}
	return migrateTrackerBoardOnDB(d.trackerDB())
}

func migrateTrackerBoardOnDB(db *sql.DB) error {
	if db == nil {
		return fmt.Errorf("база недоступна")
	}
	_, err := db.Exec(`
		ALTER TABLE pack_tracker_tasks
			ADD COLUMN IF NOT EXISTS needs_approval BOOLEAN NOT NULL DEFAULT FALSE;
		ALTER TABLE pack_tracker_tasks
			ADD COLUMN IF NOT EXISTS approvals JSONB NOT NULL DEFAULT '[]'::jsonb;
		ALTER TABLE pack_tracker_tasks
			ADD COLUMN IF NOT EXISTS approval_notified_at TIMESTAMPTZ;
		ALTER TABLE pack_tracker_tasks
			ADD COLUMN IF NOT EXISTS approval_reminder_sent_at TIMESTAMPTZ;
	`)
	if err != nil {
		return fmt.Errorf("tracker schema migrate: %w", err)
	}
	return nil
}

// AttachTrackerDatabase — подключить БД только трекера, перенести туда карточки
// и автономию с базы стаи, потом снести таблицы с Лео.
func (d *Database) AttachTrackerDatabase(databaseURL string) error {
	if d == nil || d.db == nil {
		return fmt.Errorf("база недоступна")
	}
	databaseURL = strings.TrimSpace(databaseURL)
	if databaseURL == "" || samePostgresDSN(d.dsn, databaseURL) {
		if databaseURL != "" && d.logger != nil {
			d.logger.Warn("TRACKER_DATABASE_URL совпадает с DATABASE_URL — доска на базе Лео")
		}
		return d.EnsureTrackerSchema()
	}
	u := ensureSessionTimeZone(databaseURL)
	tdb, err := sql.Open("postgres", u)
	if err != nil {
		return fmt.Errorf("tracker db: %w", err)
	}
	tdb.SetMaxOpenConns(8)
	tdb.SetMaxIdleConns(2)
	if err := tdb.Ping(); err != nil {
		_ = tdb.Close()
		return fmt.Errorf("tracker db ping: %w", err)
	}
	if _, err := tdb.Exec(trackerBoardSchema); err != nil {
		_ = tdb.Close()
		return fmt.Errorf("tracker schema: %w", err)
	}
	if err := migrateTrackerBoardOnDB(tdb); err != nil {
		_ = tdb.Close()
		return err
	}
	if err := copyTrackerTables(d.db, tdb); err != nil {
		_ = tdb.Close()
		return err
	}
	if err := dropTrackerTablesFromLeo(d.db, tdb); err != nil {
		_ = tdb.Close()
		return err
	}
	d.tracker = tdb
	if d.logger != nil {
		d.logger.Info("трекер пишет в отдельную БД, не в базу стаи")
	}
	return nil
}

func samePostgresDSN(a, b string) bool {
	ha, na := postgresHostDB(a)
	hb, nb := postgresHostDB(b)
	return ha != "" && ha == hb && na != "" && na == nb
}

func postgresHostDB(raw string) (host, name string) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", ""
	}
	name = strings.TrimPrefix(u.Path, "/")
	if i := strings.IndexByte(name, '?'); i >= 0 {
		name = name[:i]
	}
	return strings.ToLower(u.Host), strings.ToLower(name)
}

func copyTrackerTables(src, dst *sql.DB) error {
	if err := copyIfDestEmpty(src, dst, "pack_tracker_tasks",
		"id, num, prompt, when_at, when_label, repeat, kind, status, dev_column, qa_column, qa_status, handed_to_qa, auto_review, manual_qa, fast_track, auto_push, needs_approval, approvals, error, result, steps, author_id, created_at, last_run_at, updated_at",
	); err != nil {
		return err
	}
	if err := copyIfDestEmpty(src, dst, "pack_tracker_attachments",
		"id, task_id, name, mime, size, data, created_at",
	); err != nil {
		return err
	}
	if err := copyIfDestEmpty(src, dst, "leo_autonomy",
		"id, active_until, every_hours, tasks_per_run, last_run_at, last_note, updated_by, updated_at",
	); err != nil {
		return err
	}
	if err := copyIfDestEmpty(src, dst, "pack_tracker_settings",
		"key, value, updated_by, updated_at",
	); err != nil {
		return err
	}
	if err := copyIfDestEmpty(src, dst, "ms_tracker_jobs",
		"id, source_task_id, source_num, author_id, prompt, phase, when_at, when_label, status, error, result, steps, model, auto_push, branch, created_at, updated_at",
	); err != nil {
		return err
	}
	if _, err := dst.Exec(`
		SELECT setval(pg_get_serial_sequence('pack_tracker_tasks', 'id'),
			COALESCE((SELECT MAX(id) FROM pack_tracker_tasks), 1), true)
	`); err != nil {
		return err
	}
	if _, err := dst.Exec(`
		SELECT setval(pg_get_serial_sequence('ms_tracker_jobs', 'id'),
			COALESCE((SELECT MAX(id) FROM ms_tracker_jobs), 1), true)
	`); err != nil {
		return err
	}
	return nil
}

func copyIfDestEmpty(src, dst *sql.DB, table, insertSQL string) error {
	if !tableExists(src, table) {
		return nil
	}
	var destN, srcN int
	if tableExists(dst, table) {
		if err := dst.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&destN); err != nil {
			return err
		}
	}
	if destN > 0 {
		return nil
	}
	if err := src.QueryRow(`SELECT COUNT(*) FROM ` + table).Scan(&srcN); err != nil {
		return err
	}
	if srcN == 0 {
		return nil
	}
	return copyTableRows(src, dst, table, insertSQL)
}

func copyTableRows(src, dst *sql.DB, table, cols string) error {
	rows, err := src.Query("SELECT " + cols + " FROM " + table)
	if err != nil {
		return fmt.Errorf("читать %s: %w", table, err)
	}
	defer rows.Close()
	names, err := rows.Columns()
	if err != nil {
		return err
	}
	tx, err := dst.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	placeholders := make([]string, len(names))
	for i := range names {
		placeholders[i] = fmt.Sprintf("$%d", i+1)
	}
	q := fmt.Sprintf(
		"INSERT INTO %s (%s) VALUES (%s) ON CONFLICT DO NOTHING",
		table, cols, strings.Join(placeholders, ","),
	)
	for rows.Next() {
		vals := make([]any, len(names))
		ptrs := make([]any, len(names))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return err
		}
		if _, err := tx.Exec(q, vals...); err != nil {
			return fmt.Errorf("писать %s: %w", table, err)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.Commit()
}

func tableExists(db *sql.DB, name string) bool {
	var n int
	err := db.QueryRow(`
		SELECT COUNT(*) FROM information_schema.tables
		WHERE table_schema = 'public' AND table_name = $1
	`, name).Scan(&n)
	return err == nil && n > 0
}

func dropTrackerTablesFromLeo(src, dst *sql.DB) error {
	if tableExists(src, "pack_tracker_tasks") {
		var srcN, dstN int
		if err := src.QueryRow(`SELECT COUNT(*) FROM pack_tracker_tasks`).Scan(&srcN); err != nil {
			return err
		}
		if tableExists(dst, "pack_tracker_tasks") {
			if err := dst.QueryRow(`SELECT COUNT(*) FROM pack_tracker_tasks`).Scan(&dstN); err != nil {
				return err
			}
		}
		if srcN > 0 && dstN == 0 {
			return fmt.Errorf("не перенесли доску: в новой БД пусто")
		}
	}
	_, err := src.Exec(`
		DROP TABLE IF EXISTS pack_tracker_attachments;
		DROP TABLE IF EXISTS pack_tracker_settings;
		DROP TABLE IF EXISTS pack_tracker_tasks;
		DROP TABLE IF EXISTS leo_autonomy;
		DROP TABLE IF EXISTS ms_tracker_jobs;
	`)
	return err
}
