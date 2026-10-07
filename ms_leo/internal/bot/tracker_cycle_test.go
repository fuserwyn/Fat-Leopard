package bot

import (
	"encoding/json"
	"testing"
	"time"

	"leo-bot/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
)

// Админ нажимает «Обновить»: созревшая карточка уходит в «В работе»,
// в ответе started=1 — так мини-апп понимает, что кнопка сработала.
func TestAdminRefreshClaimsDueTask(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	now := time.Date(2026, 8, 20, 9, 20, 0, 0, time.UTC)
	mock.ExpectQuery(`UPDATE pack_tracker_tasks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "num", "prompt", "author_id"}).
			AddRow(int64(11), 1, "починить кнопку обновить", int64(42)))
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "починить кнопку обновить",
		status: "running", col: "doing", author: 42, at: now,
	})
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "починить кнопку обновить",
		status: "running", col: "doing", author: 42, at: now,
	})
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "починить кнопку обновить",
		status: "running", col: "doing", author: 42, at: now,
	})

	listCols := []string{
		"id", "num", "prompt", "when_at", "when_label", "repeat", "kind",
		"status", "dev_column", "qa_column", "qa_status", "handed_to_qa",
		"auto_review", "manual_qa", "fast_track", "auto_push",
		"needs_approval", "approvals",
		"approval_notified_at", "approval_reminder_sent_at",
		"error", "result", "steps", "author_id",
		"created_at", "last_run_at", "updated_at", "attachments_count",
	}
	mock.ExpectQuery(`FROM pack_tracker_tasks t`).
		WillReturnRows(sqlmock.NewRows(listCols).AddRow(
			int64(11), 1, "починить кнопку обновить", now, "20.08 09:19", "разово", "task",
			"running", "doing", nil, nil, false,
			false, false, false, true,
			false, []byte("[]"),
			nil, nil,
			"", "", []byte(`["Взяли в работу по расписанию"]`), int64(42),
			now, now, now, 0,
		))

	b := &Bot{db: database.NewForTest(sqlDB)}
	raw, err := b.trackerRequest("refresh", 0, nil, 42, "Admin")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		Tasks   []map[string]any `json:"tasks"`
		Started int              `json:"started"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if out.Started != 1 {
		t.Fatalf("started: %d body=%s", out.Started, raw)
	}
	if len(out.Tasks) != 1 || out.Tasks[0]["dev_column"] != "doing" || out.Tasks[0]["status"] != "running" {
		t.Fatalf("board after refresh: %#v", out.Tasks)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

// Весь цикл админа теми же op, что шлёт TrackerScreen: create → refresh →
// move (review, test) → qa start/pass → ship.
func TestAdminTrackerRequestFullCycle(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	b := &Bot{db: database.NewForTest(sqlDB)}
	adminID := int64(42)
	now := time.Now()

	mock.ExpectQuery(`INSERT INTO pack_tracker_tasks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "num", "created_at", "updated_at"}).
			AddRow(int64(11), 1, now, now))

	raw, err := b.trackerRequest("create", 0, map[string]any{
		"prompt": "прогнать доску под админом",
		"when":   "через 1 мин",
	}, adminID, "Admin")
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	var created struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &created); err != nil || created.ID != 11 {
		t.Fatalf("create body: %s err=%v", raw, err)
	}

	// Срок ещё не наступил — «Обновить» ничего не снимает, доска та же.
	mock.ExpectQuery(`UPDATE pack_tracker_tasks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "num", "prompt", "author_id"}))
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "pending", col: "todo", author: adminID, at: now,
	})
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "pending", col: "todo", author: adminID, at: now,
	})
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "pending", col: "todo", author: adminID, at: now,
	})
	expectTrackerList(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "pending", col: "todo", author: adminID, at: now,
	})
	raw, err = b.trackerRequest("refresh", 0, nil, adminID, "Admin")
	if err != nil {
		t.Fatalf("refresh empty: %v", err)
	}
	var board struct {
		Started int `json:"started"`
	}
	if err := json.Unmarshal(raw, &board); err != nil || board.Started != 0 {
		t.Fatalf("refresh empty: %s err=%v", raw, err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "pending", col: "todo", author: adminID, at: now,
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := b.trackerRequest("move", 11, map[string]any{"column": "doing"}, adminID, "Admin"); err != nil {
		t.Fatalf("move doing: %v", err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "running", col: "doing", author: adminID, at: now,
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := b.trackerRequest("move", 11, map[string]any{"column": "review"}, adminID, "Admin"); err != nil {
		t.Fatalf("move review: %v", err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "reviewing", col: "review", author: adminID, at: now,
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := b.trackerRequest("move", 11, map[string]any{"column": "test"}, adminID, "Admin"); err != nil {
		t.Fatalf("move test: %v", err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "holding", col: "test", author: adminID, at: now, handed: true, qaCol: "todo",
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := b.trackerRequest("qa", 11, map[string]any{"action": "start"}, adminID, "Admin"); err != nil {
		t.Fatalf("qa start: %v", err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "holding", col: "test", author: adminID, at: now, handed: true, qaCol: "doing", qaStatus: "start",
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	if _, err := b.trackerRequest("qa", 11, map[string]any{"action": "pass"}, adminID, "Admin"); err != nil {
		t.Fatalf("qa pass: %v", err)
	}

	expectTrackerGet(mock, trackerListRow{
		id: 11, num: 1, prompt: "прогнать доску под админом",
		status: "holding", col: "deploy", author: adminID, at: now, handed: true, qaCol: "done", qaStatus: "pass",
	})
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(int64(11)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	raw, err = b.trackerRequest("ship", 11, map[string]any{"id": float64(11)}, adminID, "Admin")
	if err != nil {
		t.Fatalf("ship: %v", err)
	}
	var ship struct {
		OK       bool `json:"ok"`
		Deployed bool `json:"deployed"`
		Skipped  bool `json:"skipped"`
	}
	if err := json.Unmarshal(raw, &ship); err != nil || !ship.OK || ship.Deployed || ship.Skipped {
		t.Fatalf("ship body: %s err=%v", raw, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

type trackerListRow struct {
	id       int64
	num      int
	prompt   string
	status   string
	col      string
	author   int64
	at       time.Time
	handed   bool
	qaCol    string
	qaStatus string
}

func trackerListColumns() []string {
	return []string{
		"id", "num", "prompt", "when_at", "when_label", "repeat", "kind",
		"status", "dev_column", "qa_column", "qa_status", "handed_to_qa",
		"auto_review", "manual_qa", "fast_track", "auto_push",
		"needs_approval", "approvals",
		"approval_notified_at", "approval_reminder_sent_at",
		"error", "result", "steps", "author_id",
		"created_at", "last_run_at", "updated_at", "attachments_count",
	}
}

func expectTrackerList(mock sqlmock.Sqlmock, row trackerListRow) {
	var qaCol, qaStatus any
	if row.qaCol != "" {
		qaCol = row.qaCol
	}
	if row.qaStatus != "" {
		qaStatus = row.qaStatus
	}
	mock.ExpectQuery(`FROM pack_tracker_tasks t`).
		WillReturnRows(sqlmock.NewRows(trackerListColumns()).AddRow(
			row.id, row.num, row.prompt, row.at, "20.08 09:20", "разово", "task",
			row.status, row.col, qaCol, qaStatus, row.handed,
			false, false, false, true,
			false, []byte("[]"),
			nil, nil,
			"", "", []byte(`[]`), row.author,
			row.at, nil, row.at, 0,
		))
}

func expectTrackerGet(mock sqlmock.Sqlmock, row trackerListRow) {
	var qaCol, qaStatus any
	if row.qaCol != "" {
		qaCol = row.qaCol
	}
	if row.qaStatus != "" {
		qaStatus = row.qaStatus
	}
	mock.ExpectQuery(`WHERE t.id = \$1`).
		WithArgs(row.id).
		WillReturnRows(sqlmock.NewRows(trackerListColumns()).AddRow(
			row.id, row.num, row.prompt, row.at, "20.08 09:20", "разово", "task",
			row.status, row.col, qaCol, qaStatus, row.handed,
			false, false, false, true,
			false, []byte("[]"),
			nil, nil,
			"", "", []byte(`[]`), row.author,
			row.at, nil, row.at, 0,
		))
}
