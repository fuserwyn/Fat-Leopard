package bot

import (
	"encoding/json"
	"testing"
	"time"

	"leo-bot/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTrackerTaskInPipeline(t *testing.T) {
	cases := []struct {
		name string
		task database.TrackerTask
		want bool
	}{
		{
			name: "todo waits",
			task: database.TrackerTask{Status: "pending", DevColumn: trackerColTodo},
			want: false,
		},
		{
			name: "doing runs",
			task: database.TrackerTask{Status: "running", DevColumn: trackerColDoing},
			want: true,
		},
		{
			name: "review runs",
			task: database.TrackerTask{Status: "reviewing", DevColumn: trackerColReview},
			want: true,
		},
		{
			name: "test runs",
			task: database.TrackerTask{Status: "holding", DevColumn: trackerColTest},
			want: true,
		},
		{
			name: "deploy runs",
			task: database.TrackerTask{Status: "holding", DevColumn: trackerColDeploy},
			want: true,
		},
		{
			name: "done finished",
			task: database.TrackerTask{Status: "done", DevColumn: trackerColDone},
			want: false,
		},
		{
			name: "canceled in doing",
			task: database.TrackerTask{Status: "canceled", DevColumn: trackerColDoing},
			want: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := trackerTaskInPipeline(tc.task); got != tc.want {
				t.Fatalf("trackerTaskInPipeline(%+v) = %v, want %v", tc.task, got, tc.want)
			}
		})
	}
}

func TestTrackerPipelineBusy(t *testing.T) {
	b := &Bot{}
	if b.trackerPipelineBusy(1) {
		t.Fatal("nil db must not block")
	}
}

func TestTrackerKeepInWaitingQueue(t *testing.T) {
	task := database.TrackerTask{
		ID:        2,
		Status:    "running",
		DevColumn: trackerColDoing,
		Steps:     []string{"Взяли в работу по расписанию"},
	}
	trackerKeepInWaitingQueue(&task, "")
	if task.DevColumn != trackerColTodo || task.Status != "pending" {
		t.Fatalf("must revert to todo: %+v", task)
	}
	if task.Steps[len(task.Steps)-1] != trackerAgentWaitingStep {
		t.Fatalf("steps: %v", task.Steps)
	}
}

// Новая задача «сейчас» не уходит в «В работе», пока другая в конвейере.
func TestCreateTaskStaysWaitingWhenPipelineBusy(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`INSERT INTO pack_tracker_tasks`).
		WillReturnRows(sqlmock.NewRows([]string{"id", "num", "created_at", "updated_at"}).
			AddRow(int64(12), 2, now, now))
	expectTrackerListFull(mock, trackerListRow{
		id: 11, num: 1, prompt: "первая в работе",
		status: "running", col: "doing", author: 42, at: now,
	})

	b := &Bot{db: database.NewForTest(sqlDB)}
	raw, err := b.trackerRequest("create", 0, map[string]any{
		"prompt": "вторая задача",
		"when":   "сейчас",
	}, 42, "Admin")
	if err != nil {
		t.Fatal(err)
	}
	var out struct {
		ID int64 `json:"id"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || out.ID != 12 {
		t.Fatalf("create body: %s err=%v", raw, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func TestApproveTaskStaysWaitingWhenPipelineBusy(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()

	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	expectTrackerGetWithApproval(mock, trackerListRow{
		id: 20, num: 2, prompt: "ждёт аппрува",
		status: "pending", col: "approve", author: -1, at: now,
	}, true, []byte(`[200]`))
	expectTrackerListFull(mock, trackerListRow{
		id: 11, num: 1, prompt: "первая в работе",
		status: "running", col: "doing", author: 42, at: now,
	})
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	b := &Bot{db: database.NewForTest(sqlDB)}
	toast, err := b.approveTrackerTask(20, 100)
	if err != nil {
		t.Fatal(err)
	}
	if toast != "Задача в очереди — дождитесь текущей" {
		t.Fatalf("toast: %q", toast)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}

func trackerTaskListColumnsFull() []string {
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

func expectTrackerListFull(mock sqlmock.Sqlmock, row trackerListRow) {
	mock.ExpectQuery(`FROM pack_tracker_tasks t`).
		WillReturnRows(sqlmock.NewRows(trackerTaskListColumnsFull()).AddRow(
			row.id, row.num, row.prompt, row.at, "14.09 12:00", "разово", "task",
			row.status, row.col, nil, nil, row.handed,
			false, false, false, true,
			false, []byte("[]"),
			nil, nil,
			"", "", []byte(`[]`), row.author,
			row.at, nil, row.at, 0,
		))
}

func expectTrackerGetWithApproval(mock sqlmock.Sqlmock, row trackerListRow, needsApproval bool, approvals []byte) {
	mock.ExpectQuery(`WHERE t.id = \$1`).
		WithArgs(row.id).
		WillReturnRows(sqlmock.NewRows(trackerTaskListColumnsFull()).AddRow(
			row.id, row.num, row.prompt, row.at, "14.09 12:00", "разово", "task",
			row.status, row.col, nil, nil, row.handed,
			false, false, false, true,
			needsApproval, approvals,
			nil, nil,
			"", "", []byte(`[]`), row.author,
			row.at, nil, row.at, 0,
		))
	mock.ExpectQuery(`SELECT id, name, mime, size`).
		WithArgs(row.id).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "mime", "size"}))
}
