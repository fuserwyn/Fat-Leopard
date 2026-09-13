package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Своя доска задач в админке. Карточки и фото лежат у нас: так трекер
// не зависит от чужой гостевой сессии и переживает деплой вместе со стаей.

const (
	TrackerLeoAuthorID = int64(-1)
	trackerMaxPrompt   = 4000
	trackerMaxAttBytes = 8 << 20
	trackerMaxAtts     = 6
)

// TrackerTask — строка доски. Вложения подгружаются отдельно.
type TrackerTask struct {
	ID               int64
	Num              int
	Prompt           string
	WhenAt           time.Time
	WhenLabel        string
	Repeat           string
	Kind             string
	Status           string
	DevColumn        string
	QaColumn         string
	QaStatus         string
	HandedToQa       bool
	AutoReview       bool
	ManualQa         bool
	FastTrack        bool
	AutoPush         bool
	NeedsApproval    bool
	Approvals              []int64
	ApprovalNotifiedAt     time.Time
	HasApprovalNotified    bool
	ApprovalReminderSentAt time.Time
	HasApprovalReminderSent bool
	Error            string
	Result           string
	Steps            []string
	AuthorID         int64
	HasAuthor        bool
	CreatedAt        time.Time
	LastRunAt        time.Time
	HasLastRun       bool
	UpdatedAt        time.Time
	AttachmentsCount int
	Attachments      []TrackerAttachment
}

// TrackerAttachment — метаданные фото. Байты только в Get.
type TrackerAttachment struct {
	ID   string
	Name string
	Mime string
	Size int
	Data []byte
}

const trackerTaskSelect = `
		SELECT t.id, t.num, t.prompt, t.when_at, t.when_label, t.repeat, t.kind,
		       t.status, t.dev_column, t.qa_column, t.qa_status, t.handed_to_qa,
		       t.auto_review, t.manual_qa, t.fast_track, t.auto_push,
		       t.needs_approval, t.approvals,
		       t.approval_notified_at, t.approval_reminder_sent_at,
		       t.error, t.result, t.steps, t.author_id,
		       t.created_at, t.last_run_at, t.updated_at,
		       (SELECT COUNT(*) FROM pack_tracker_attachments a WHERE a.task_id = t.id)
		FROM pack_tracker_tasks t
`

// ListTrackerTasks — вся доска, свежие сверху. Лимит, чтобы админка не тянула архив.
func (d *Database) ListTrackerTasks() ([]TrackerTask, error) {
	if d == nil || d.trackerDB() == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	rows, err := d.trackerDB().Query(trackerTaskSelect + `
		ORDER BY t.id DESC
		LIMIT 300
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TrackerTask, 0, 32)
	for rows.Next() {
		t, err := scanTrackerTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetTrackerTask — карточка с метаданными вложений, без байтов.
func (d *Database) GetTrackerTask(id int64) (TrackerTask, error) {
	var empty TrackerTask
	if d == nil || d.trackerDB() == nil || id <= 0 {
		return empty, fmt.Errorf("задача не найдена")
	}
	row := d.trackerDB().QueryRow(trackerTaskSelect+`WHERE t.id = $1`, id)
	t, err := scanTrackerTask(row)
	if err == sql.ErrNoRows {
		return empty, fmt.Errorf("задача не найдена")
	}
	if err != nil {
		return empty, err
	}
	atts, err := d.listTrackerAttachmentMeta(id)
	if err != nil {
		return empty, err
	}
	t.Attachments = atts
	t.AttachmentsCount = len(atts)
	return t, nil
}

// GetTrackerTaskByNum — карточка по номеру на доске (#1, #236).
// Уведомление часто шлёт номер, а не внутренний id.
func (d *Database) GetTrackerTaskByNum(num int) (TrackerTask, error) {
	var empty TrackerTask
	if d == nil || d.trackerDB() == nil || num <= 0 {
		return empty, fmt.Errorf("задача не найдена")
	}
	row := d.trackerDB().QueryRow(trackerTaskSelect+`WHERE t.num = $1`, num)
	t, err := scanTrackerTask(row)
	if err == sql.ErrNoRows {
		return empty, fmt.Errorf("задача не найдена")
	}
	return t, err
}

// FindOpenTrackerTask — самая свежая карточка в работе или на review.
// Если id в уведомлении чужой (внешняя доска), двигаем то, что сейчас открыто.
func (d *Database) FindOpenTrackerTask() (TrackerTask, error) {
	var empty TrackerTask
	if d == nil || d.trackerDB() == nil {
		return empty, fmt.Errorf("база недоступна")
	}
	row := d.trackerDB().QueryRow(trackerTaskSelect + `
		WHERE t.status NOT IN ('done', 'canceled', 'cancelled')
		ORDER BY
		  CASE
		    WHEN t.status IN ('running', 'reviewing') THEN 0
		    WHEN COALESCE(NULLIF(t.dev_column, ''), 'todo') IN ('doing', 'review') THEN 0
		    ELSE 1
		  END,
		  t.last_run_at DESC NULLS LAST, t.id DESC
		LIMIT 1
	`)
	t, err := scanTrackerTask(row)
	if err == sql.ErrNoRows {
		return empty, nil
	}
	return t, err
}

// CreateTrackerTask — новая карточка. Номер — следующий на доске.
func (d *Database) CreateTrackerTask(t TrackerTask) (TrackerTask, error) {
	if d == nil || d.trackerDB() == nil {
		return t, fmt.Errorf("база недоступна")
	}
	t.Prompt = clipTrackerText(t.Prompt, trackerMaxPrompt)
	if t.Prompt == "" {
		return t, fmt.Errorf("опиши задачу")
	}
	if t.Repeat == "" {
		t.Repeat = "разово"
	}
	if t.Kind == "" {
		t.Kind = "task"
	}
	if t.Status == "" {
		t.Status = "pending"
	}
	if t.DevColumn == "" {
		t.DevColumn = "todo"
	}
	if t.WhenAt.IsZero() {
		t.WhenAt = time.Now()
	}
	if t.Steps == nil {
		t.Steps = []string{}
	}
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return t, err
	}
	var author any
	if t.HasAuthor {
		author = t.AuthorID
	}
	err = d.trackerDB().QueryRow(`
		INSERT INTO pack_tracker_tasks (
			num, prompt, when_at, when_label, repeat, kind, status, dev_column,
			qa_column, qa_status, handed_to_qa, auto_review, manual_qa, fast_track,
			auto_push, needs_approval, approvals, error, result, steps, author_id
		)
		VALUES (
			COALESCE((SELECT MAX(num) FROM pack_tracker_tasks), 0) + 1,
			$1, $2, $3, $4, $5, $6, $7, NULLIF($8, ''), NULLIF($9, ''),
			$10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20
		)
		RETURNING id, num, created_at, updated_at
	`, t.Prompt, t.WhenAt, t.WhenLabel, t.Repeat, t.Kind, t.Status, t.DevColumn,
		t.QaColumn, t.QaStatus, t.HandedToQa, t.AutoReview, t.ManualQa, t.FastTrack,
		t.AutoPush, t.NeedsApproval, marshalTrackerApprovals(t.Approvals), t.Error, t.Result, steps, author,
	).Scan(&t.ID, &t.Num, &t.CreatedAt, &t.UpdatedAt)
	return t, err
}

// SaveTrackerTask — обновить карточку целиком.
func (d *Database) SaveTrackerTask(t TrackerTask) error {
	if d == nil || d.trackerDB() == nil || t.ID <= 0 {
		return fmt.Errorf("задача не найдена")
	}
	t.Prompt = clipTrackerText(t.Prompt, trackerMaxPrompt)
	if t.Prompt == "" {
		return fmt.Errorf("опиши задачу")
	}
	if t.Steps == nil {
		t.Steps = []string{}
	}
	steps, err := json.Marshal(t.Steps)
	if err != nil {
		return err
	}
	var author any
	if t.HasAuthor {
		author = t.AuthorID
	}
	var lastRun any
	if t.HasLastRun {
		lastRun = t.LastRunAt
	}
	res, err := d.trackerDB().Exec(`
		UPDATE pack_tracker_tasks SET
			prompt = $2, when_at = $3, when_label = $4, repeat = $5, kind = $6,
			status = $7, dev_column = $8, qa_column = NULLIF($9, ''),
			qa_status = NULLIF($10, ''), handed_to_qa = $11, auto_review = $12,
			manual_qa = $13, fast_track = $14, auto_push = $15, needs_approval = $16,
			approvals = $17, error = $18,
			result = $19, steps = $20, author_id = $21, last_run_at = $22,
			updated_at = NOW()
		WHERE id = $1
	`, t.ID, t.Prompt, t.WhenAt, t.WhenLabel, t.Repeat, t.Kind, t.Status, t.DevColumn,
		t.QaColumn, t.QaStatus, t.HandedToQa, t.AutoReview, t.ManualQa, t.FastTrack,
		t.AutoPush, t.NeedsApproval, marshalTrackerApprovals(t.Approvals), t.Error,
		t.Result, steps, author, lastRun)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("задача не найдена")
	}
	return nil
}

// ClaimDueTrackerTasks атомарно забирает карточки, срок которых наступил:
// «Ожидает» + when_at <= NOW(). Ставит «В работе», пишет шаг и last_run_at.
// Одновременно в конвейере может быть только одна карточка — остальные ждут.
// Сравниваем с часами базы, не с time.Now() из Go: сессия Postgres — Москва,
// сервер в UTC, и lib/pq может отдать «наивный» UTC. Тогда карточка висит
// ещё три часа, как #1 на доске: срок 10:10, а колонка всё ещё «Ожидает».
// FOR UPDATE SKIP LOCKED — если крутятся две реплики, одну карточку возьмёт одна.
func (d *Database) ClaimDueTrackerTasks(now time.Time) ([]TrackerTask, error) {
	if d == nil || d.trackerDB() == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	_ = now
	rows, err := d.trackerDB().Query(`
		UPDATE pack_tracker_tasks
		SET status = 'running',
		    dev_column = 'doing',
		    last_run_at = NOW(),
		    updated_at = NOW(),
		    steps = (
		      CASE
		        WHEN jsonb_typeof(steps) = 'array' THEN steps
		        ELSE '[]'::jsonb
		      END
		    ) || jsonb_build_array('Взяли в работу по расписанию')
		WHERE id IN (
			SELECT id FROM pack_tracker_tasks
			WHERE status IN ('pending', 'scheduled')
			  AND COALESCE(NULLIF(dev_column, ''), 'todo') = 'todo'
			  AND NOT COALESCE(needs_approval, FALSE)
			  AND when_at <= NOW()
			  AND NOT EXISTS (
			    SELECT 1 FROM pack_tracker_tasks busy
			    WHERE busy.dev_column IN ('doing', 'review', 'test', 'deploy')
			      AND busy.status NOT IN ('done', 'canceled')
			  )
			ORDER BY when_at, id
			LIMIT 1
			FOR UPDATE SKIP LOCKED
		)
		RETURNING id, num, prompt, author_id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TrackerTask, 0, 4)
	for rows.Next() {
		var t TrackerTask
		var author sql.NullInt64
		if err := rows.Scan(&t.ID, &t.Num, &t.Prompt, &author); err != nil {
			return nil, err
		}
		t.Status = "running"
		t.DevColumn = "doing"
		t.HasLastRun = true
		if author.Valid {
			t.AuthorID = author.Int64
			t.HasAuthor = true
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// DeleteTrackerTask — снять карточку. Фото уйдут каскадом.
func (d *Database) DeleteTrackerTask(id int64) error {
	if d == nil || d.trackerDB() == nil || id <= 0 {
		return fmt.Errorf("задача не найдена")
	}
	res, err := d.trackerDB().Exec(`DELETE FROM pack_tracker_tasks WHERE id = $1`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("задача не найдена")
	}
	return nil
}

// AddTrackerAttachment — приложить фото к задаче.
func (d *Database) AddTrackerAttachment(taskID int64, name, mime string, data []byte) (TrackerAttachment, error) {
	var att TrackerAttachment
	if d == nil || d.trackerDB() == nil || taskID <= 0 {
		return att, fmt.Errorf("задача не найдена")
	}
	if len(data) == 0 || len(data) > trackerMaxAttBytes {
		return att, fmt.Errorf("картинка должна быть до 8 МБ")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "photo.jpg"
	}
	mime = strings.TrimSpace(mime)
	if mime == "" {
		mime = "image/jpeg"
	}
	var n int
	if err := d.trackerDB().QueryRow(`
		SELECT COUNT(*) FROM pack_tracker_attachments WHERE task_id = $1
	`, taskID).Scan(&n); err != nil {
		return att, err
	}
	if n >= trackerMaxAtts {
		return att, fmt.Errorf("к задаче можно приложить не больше %d фото", trackerMaxAtts)
	}
	att = TrackerAttachment{
		ID:   strings.ReplaceAll(uuid.NewString(), "-", ""),
		Name: name,
		Mime: mime,
		Size: len(data),
		Data: data,
	}
	_, err := d.trackerDB().Exec(`
		INSERT INTO pack_tracker_attachments (id, task_id, name, mime, size, data)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, att.ID, taskID, att.Name, att.Mime, att.Size, att.Data)
	if err != nil {
		return TrackerAttachment{}, err
	}
	return att, nil
}

// GetTrackerAttachment — байты одного фото.
func (d *Database) GetTrackerAttachment(taskID int64, attID string) (TrackerAttachment, error) {
	var att TrackerAttachment
	attID = strings.TrimSpace(attID)
	if d == nil || d.trackerDB() == nil || taskID <= 0 || attID == "" {
		return att, fmt.Errorf("фото не найдено")
	}
	err := d.trackerDB().QueryRow(`
		SELECT id, name, mime, size, data
		FROM pack_tracker_attachments
		WHERE task_id = $1 AND id = $2
	`, taskID, attID).Scan(&att.ID, &att.Name, &att.Mime, &att.Size, &att.Data)
	if err == sql.ErrNoRows {
		return att, fmt.Errorf("фото не найдено")
	}
	return att, err
}

// DeleteTrackerAttachment — снять фото. Так работает «заменить».
func (d *Database) DeleteTrackerAttachment(taskID int64, attID string) error {
	attID = strings.TrimSpace(attID)
	if d == nil || d.trackerDB() == nil || taskID <= 0 || attID == "" {
		return fmt.Errorf("фото не найдено")
	}
	res, err := d.trackerDB().Exec(`
		DELETE FROM pack_tracker_attachments WHERE task_id = $1 AND id = $2
	`, taskID, attID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("фото не найдено")
	}
	return nil
}

func (d *Database) listTrackerAttachmentMeta(taskID int64) ([]TrackerAttachment, error) {
	rows, err := d.trackerDB().Query(`
		SELECT id, name, mime, size
		FROM pack_tracker_attachments
		WHERE task_id = $1
		ORDER BY created_at
	`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TrackerAttachment, 0, 2)
	for rows.Next() {
		var a TrackerAttachment
		if err := rows.Scan(&a.ID, &a.Name, &a.Mime, &a.Size); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

type trackerRow interface {
	Scan(dest ...any) error
}

func scanTrackerTask(row trackerRow) (TrackerTask, error) {
	var t TrackerTask
	var qaCol, qaStatus sql.NullString
	var author sql.NullInt64
	var lastRun sql.NullTime
	var steps []byte
	var approvalsRaw []byte
	var approvalNotified, approvalReminder sql.NullTime
	err := row.Scan(
		&t.ID, &t.Num, &t.Prompt, &t.WhenAt, &t.WhenLabel, &t.Repeat, &t.Kind,
		&t.Status, &t.DevColumn, &qaCol, &qaStatus, &t.HandedToQa,
		&t.AutoReview, &t.ManualQa, &t.FastTrack, &t.AutoPush,
		&t.NeedsApproval, &approvalsRaw,
		&approvalNotified, &approvalReminder,
		&t.Error, &t.Result, &steps, &author,
		&t.CreatedAt, &lastRun, &t.UpdatedAt, &t.AttachmentsCount,
	)
	if err != nil {
		return t, err
	}
	t.Approvals = parseTrackerApprovals(approvalsRaw)
	t.QaColumn = qaCol.String
	t.QaStatus = qaStatus.String
	if author.Valid {
		t.AuthorID = author.Int64
		t.HasAuthor = true
	}
	if lastRun.Valid {
		t.LastRunAt = lastRun.Time
		t.HasLastRun = true
	}
	if approvalNotified.Valid {
		t.ApprovalNotifiedAt = approvalNotified.Time
		t.HasApprovalNotified = true
	}
	if approvalReminder.Valid {
		t.ApprovalReminderSentAt = approvalReminder.Time
		t.HasApprovalReminderSent = true
	}
	if len(steps) > 0 && string(steps) != "null" {
		_ = json.Unmarshal(steps, &t.Steps)
	}
	if t.Steps == nil {
		t.Steps = []string{}
	}
	return t, nil
}

func clipTrackerText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len([]rune(s)) <= max {
		return s
	}
	return string([]rune(s)[:max])
}

func marshalTrackerApprovals(ids []int64) []byte {
	if len(ids) == 0 {
		return []byte("[]")
	}
	raw, err := json.Marshal(ids)
	if err != nil {
		return []byte("[]")
	}
	return raw
}

func parseTrackerApprovals(raw []byte) []int64 {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil
	}
	return ids
}

// MarkTrackerApprovalNotified — зафиксировать первичную рассылку админам.
func (d *Database) MarkTrackerApprovalNotified(taskID int64) error {
	if d == nil || d.trackerDB() == nil || taskID <= 0 {
		return fmt.Errorf("задача не найдена")
	}
	res, err := d.trackerDB().Exec(`
		UPDATE pack_tracker_tasks
		SET approval_notified_at = NOW(),
		    approval_reminder_sent_at = NULL,
		    updated_at = NOW()
		WHERE id = $1
	`, taskID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("задача не найдена")
	}
	return nil
}

// MarkTrackerApprovalReminderSent — повторное уведомление уже ушло.
func (d *Database) MarkTrackerApprovalReminderSent(taskID int64) error {
	if d == nil || d.trackerDB() == nil || taskID <= 0 {
		return fmt.Errorf("задача не найдена")
	}
	res, err := d.trackerDB().Exec(`
		UPDATE pack_tracker_tasks
		SET approval_reminder_sent_at = NOW(),
		    updated_at = NOW()
		WHERE id = $1
	`, taskID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("задача не найдена")
	}
	return nil
}

// ListTrackerTasksAwaitingApprovalReminder — задачи на аппруве, где прошёл час
// после первого уведомления и повтор ещё не отправляли.
func (d *Database) ListTrackerTasksAwaitingApprovalReminder() ([]TrackerTask, error) {
	if d == nil || d.trackerDB() == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	rows, err := d.trackerDB().Query(trackerTaskSelect + `
		WHERE t.dev_column = 'approve'
		  AND t.needs_approval = TRUE
		  AND COALESCE(jsonb_array_length(t.approvals), 0) < 2
		  AND t.approval_notified_at IS NOT NULL
		  AND t.approval_notified_at <= NOW() - INTERVAL '1 hour'
		  AND t.approval_reminder_sent_at IS NULL
		ORDER BY t.approval_notified_at, t.id
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TrackerTask, 0, 8)
	for rows.Next() {
		t, err := scanTrackerTask(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
