package store

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	_ "github.com/lib/pq"
)

type Job struct {
	ID           int64
	SourceTaskID int64
	SourceNum    int
	AuthorID     int64
	Prompt       string
	Phase        string
	WhenAt       time.Time
	WhenLabel    string
	Status       string
	Error        string
	Result       string
	Steps        []string
	Model        string
	AutoPush     bool
	Branch       string
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type Store struct {
	db *sql.DB
}

func Open(databaseURL string) (*Store, error) {
	if strings.TrimSpace(databaseURL) == "" {
		return nil, fmt.Errorf("DATABASE_URL пуст")
	}
	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(8)
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}
	s := &Store{db: db}
	if err := s.migrate(); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *Store) Close() {
	if s != nil && s.db != nil {
		_ = s.db.Close()
	}
}

func (s *Store) migrate() error {
	_, err := s.db.Exec(`
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
	`)
	return err
}

func (s *Store) Create(j Job) (Job, error) {
	if strings.TrimSpace(j.Prompt) == "" {
		return j, fmt.Errorf("опиши задачу")
	}
	if j.WhenAt.IsZero() {
		j.WhenAt = time.Now().Add(-time.Second)
	}
	if j.Status == "" {
		j.Status = "pending"
	}
	if j.Phase == "" {
		j.Phase = "doing"
	}
	if j.Steps == nil {
		j.Steps = []string{"Поставлена на трекер Леопарда"}
	}
	steps, _ := json.Marshal(j.Steps)
	err := s.db.QueryRow(`
		INSERT INTO ms_tracker_jobs (
			source_task_id, source_num, author_id, prompt, phase, when_at, when_label,
			status, error, result, steps, model, auto_push, branch
		) VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, created_at, updated_at
	`, j.SourceTaskID, j.SourceNum, j.AuthorID, j.Prompt, j.Phase, j.WhenAt, j.WhenLabel,
		j.Status, j.Error, j.Result, steps, j.Model, j.AutoPush, j.Branch,
	).Scan(&j.ID, &j.CreatedAt, &j.UpdatedAt)
	return j, err
}

func (s *Store) Get(id int64) (Job, error) {
	var empty Job
	row := s.db.QueryRow(`
		SELECT id, source_task_id, source_num, author_id, prompt, phase, when_at, when_label,
		       status, error, result, steps, model, auto_push, branch, created_at, updated_at
		FROM ms_tracker_jobs WHERE id = $1
	`, id)
	j, err := scanJob(row)
	if err == sql.ErrNoRows {
		return empty, fmt.Errorf("задача не найдена")
	}
	return j, err
}

func (s *Store) List(limit int) ([]Job, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, source_task_id, source_num, author_id, prompt, phase, when_at, when_label,
		       status, error, result, steps, model, auto_push, branch, created_at, updated_at
		FROM ms_tracker_jobs
		ORDER BY id DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Job, 0, 16)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) Save(j Job) error {
	if j.Steps == nil {
		j.Steps = []string{}
	}
	steps, _ := json.Marshal(j.Steps)
	_, err := s.db.Exec(`
		UPDATE ms_tracker_jobs SET
			status = $2, error = $3, result = $4, steps = $5, branch = $6, updated_at = NOW()
		WHERE id = $1
	`, j.ID, j.Status, j.Error, j.Result, steps, j.Branch)
	return err
}

func (s *Store) ClaimDue(now time.Time, limit int) ([]Job, error) {
	if limit <= 0 {
		limit = 1
	}
	if limit > 1 {
		limit = 1
	}
	rows, err := s.db.Query(`
		UPDATE ms_tracker_jobs
		SET status = 'running', updated_at = NOW(),
		    steps = (
		      CASE WHEN jsonb_typeof(steps) = 'array' THEN steps ELSE '[]'::jsonb END
		    ) || jsonb_build_array('Взяли в работу')
		WHERE id IN (
			SELECT id FROM ms_tracker_jobs
			WHERE status = 'pending' AND when_at <= $1
			  AND NOT EXISTS (
			    SELECT 1 FROM ms_tracker_jobs r WHERE r.status = 'running'
			  )
			ORDER BY when_at, id
			LIMIT $2
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, source_task_id, source_num, author_id, prompt, phase, when_at, when_label,
		          status, error, result, steps, model, auto_push, branch, created_at, updated_at
	`, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]Job, 0, limit)
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}

func (s *Store) SourceHasBranch(sourceTaskID int64) bool {
	if s == nil || s.db == nil || sourceTaskID <= 0 {
		return false
	}
	var n int
	if err := s.db.QueryRow(`
		SELECT COUNT(*) FROM ms_tracker_jobs
		WHERE source_task_id = $1
		  AND COALESCE(branch, '') <> ''
		  AND COALESCE(result, '') NOT ILIKE '%authentication failed%'
		  AND COALESCE(result, '') NOT ILIKE '%invalid username or token%'
		  AND COALESCE(result, '') NOT ILIKE '%git: push:%'
	`, sourceTaskID).Scan(&n); err != nil {
		return false
	}
	return n > 0
}

func (s *Store) SourceBranch(sourceTaskID int64) string {
	if s == nil || s.db == nil || sourceTaskID <= 0 {
		return ""
	}
	var branch string
	err := s.db.QueryRow(`
		SELECT branch FROM ms_tracker_jobs
		WHERE source_task_id = $1
		  AND COALESCE(branch, '') <> ''
		  AND COALESCE(result, '') NOT ILIKE '%authentication failed%'
		  AND COALESCE(result, '') NOT ILIKE '%invalid username or token%'
		  AND COALESCE(result, '') NOT ILIKE '%git: push:%'
		ORDER BY id DESC
		LIMIT 1
	`, sourceTaskID).Scan(&branch)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(branch)
}

func (s *Store) Cancel(id int64) error {
	res, err := s.db.Exec(`
		UPDATE ms_tracker_jobs
		SET status = 'canceled', updated_at = NOW(),
		    steps = steps || jsonb_build_array('Отменена')
		WHERE id = $1 AND status IN ('pending', 'running')
	`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("задачу нельзя отменить")
	}
	return nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanJob(row scanner) (Job, error) {
	var j Job
	var steps []byte
	err := row.Scan(
		&j.ID, &j.SourceTaskID, &j.SourceNum, &j.AuthorID, &j.Prompt, &j.Phase, &j.WhenAt, &j.WhenLabel,
		&j.Status, &j.Error, &j.Result, &steps, &j.Model, &j.AutoPush, &j.Branch, &j.CreatedAt, &j.UpdatedAt,
	)
	if err != nil {
		return j, err
	}
	if len(steps) > 0 {
		_ = json.Unmarshal(steps, &j.Steps)
	}
	return j, nil
}

func AppendStep(j *Job, step string) {
	step = strings.TrimSpace(step)
	if j == nil || step == "" {
		return
	}
	j.Steps = append(j.Steps, step)
	if len(j.Steps) > 80 {
		j.Steps = j.Steps[len(j.Steps)-80:]
	}
}

// FailStale снимает задачи, зависшие в running дольше cutoff. Исполнитель
// берёт новую задачу только когда running нет, поэтому одна потерянная
// при рестарте задача иначе держала бы всю очередь навсегда.
func (s *Store) FailStale(cutoff time.Time, reason string) ([]Job, error) {
	rows, err := s.db.Query(`
		UPDATE ms_tracker_jobs
		SET status = 'error', error = $2, updated_at = NOW(),
		    steps = (
		      CASE WHEN jsonb_typeof(steps) = 'array' THEN steps ELSE '[]'::jsonb END
		    ) || jsonb_build_array('Сняли как зависшую')
		WHERE status = 'running' AND updated_at < $1
		RETURNING id, source_task_id, source_num, author_id, prompt, phase, when_at, when_label,
		          status, error, result, steps, model, auto_push, branch, created_at, updated_at
	`, cutoff, reason)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Job
	for rows.Next() {
		j, err := scanJob(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, j)
	}
	return out, rows.Err()
}
