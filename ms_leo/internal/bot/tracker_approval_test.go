package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
)

func TestApplyTrackerColumnApprove(t *testing.T) {
	task := database.TrackerTask{Status: "pending", DevColumn: trackerColTodo}
	if err := applyTrackerColumn(&task, trackerColApprove); err != nil {
		t.Fatal(err)
	}
	if task.DevColumn != trackerColApprove || task.Status != "pending" {
		t.Fatalf("approve column: got %s/%s", task.DevColumn, task.Status)
	}
}

func TestTrackerStatusMetaApprove(t *testing.T) {
	label, icon, phase := trackerStatusMeta("pending", trackerColApprove)
	if label != "Аппрув" || icon != "👍" || phase != "approve" {
		t.Fatalf("meta: %s %s %s", label, icon, phase)
	}
}

func TestTrackerAppendApproval(t *testing.T) {
	task := database.TrackerTask{Approvals: nil}
	if !trackerAppendApproval(&task, 100) {
		t.Fatal("expected first approval")
	}
	if trackerAppendApproval(&task, 100) {
		t.Fatal("duplicate approval should be rejected")
	}
	if !trackerAppendApproval(&task, 200) {
		t.Fatal("expected second approval")
	}
	if len(task.Approvals) != 2 {
		t.Fatalf("approvals=%v", task.Approvals)
	}
}

func TestTrackerNextColumnIncludesApprove(t *testing.T) {
	if trackerNextColumn[trackerColApprove] != trackerColDoing {
		t.Fatalf("approve should lead to doing, got %s", trackerNextColumn[trackerColApprove])
	}
}

func TestTrackerTaskDueForStartSkipsApprove(t *testing.T) {
	task := database.TrackerTask{
		Status:    "pending",
		DevColumn: trackerColApprove,
	}
	if trackerTaskDueForStart(task, task.WhenAt) {
		t.Fatal("approve column must not auto-start")
	}
}

func TestTrackerTaskDueForStartSkipsNeedsApprovalInTodo(t *testing.T) {
	task := database.TrackerTask{
		Status:        "pending",
		DevColumn:     trackerColTodo,
		NeedsApproval: true,
		Approvals:     []int64{42},
	}
	if trackerTaskDueForStart(task, task.WhenAt) {
		t.Fatal("needs_approval in todo must not auto-start")
	}
}

func TestTrackerHasApproval(t *testing.T) {
	task := database.TrackerTask{Approvals: []int64{1, 2, 3}}
	if !trackerHasApproval(task, 2) {
		t.Fatal("expected approval")
	}
	if trackerHasApproval(task, 9) {
		t.Fatal("unexpected approval")
	}
}

func TestTrackerTaskAuthorLabel(t *testing.T) {
	var b Bot
	if got := b.trackerTaskAuthorLabel(database.TrackerTask{}); got != "Из чата" {
		t.Fatalf("no author: got %q", got)
	}
	if got := b.trackerTaskAuthorLabel(database.TrackerTask{
		HasAuthor: true,
		AuthorID:  database.TrackerLeoAuthorID,
		Kind:      "leo_task",
	}); got != "Лео" {
		t.Fatalf("leo author: got %q", got)
	}
	if got := b.trackerTaskAuthorLabel(database.TrackerTask{
		HasAuthor: true,
		AuthorID:  42,
	}); got != "id 42" {
		t.Fatalf("human without db: got %q", got)
	}
}

func TestTrackerApprovalNotifyTextShowsAuthor(t *testing.T) {
	var b Bot
	text := b.trackerApprovalNotifyText(database.TrackerTask{
		Num:           99,
		Prompt:        "Задача #99.\n\nТест",
		HasAuthor:     true,
		AuthorID:      database.TrackerLeoAuthorID,
		Kind:          "leo_task",
		NeedsApproval: true,
		DevColumn:     trackerColApprove,
	}, false)
	if !strings.Contains(text, "Поставил: Лео") {
		t.Fatalf("expected Leo author in notify: %q", text)
	}
}

func TestTrackerApprovalNotifyTextReminder(t *testing.T) {
	var b Bot
	text := b.trackerApprovalNotifyText(database.TrackerTask{
		Num:           5,
		NeedsApproval: true,
		DevColumn:     trackerColApprove,
	}, true)
	if !strings.HasPrefix(text, "⏰ Напоминание:") {
		t.Fatalf("expected reminder prefix: %q", text)
	}
}

func TestTrackerApprovalPendingTargetsSkipsApproved(t *testing.T) {
	b := &Bot{config: &config.Config{OwnerID: 100, AdminIDs: []int64{200, 300}}}
	task := database.TrackerTask{
		HasAuthor: true,
		AuthorID:  999,
		Approvals: []int64{100},
	}
	pending := trackerApprovalPendingTargets(b, task)
	if len(pending) != 2 {
		t.Fatalf("pending=%v", pending)
	}
	for _, id := range pending {
		if id == 100 {
			t.Fatal("approved admin should be skipped")
		}
	}
}

func TestTrackerApprovalReminderDue(t *testing.T) {
	now := time.Date(2026, 3, 13, 12, 0, 0, 0, time.UTC)
	base := database.TrackerTask{
		NeedsApproval:       true,
		DevColumn:           trackerColApprove,
		HasApprovalNotified: true,
		ApprovalNotifiedAt:  now.Add(-61 * time.Minute),
	}
	if !trackerApprovalReminderDue(base, now) {
		t.Fatal("expected reminder due after 61 minutes")
	}
	if trackerApprovalReminderDue(base, now.Add(-2*time.Minute)) {
		t.Fatal("reminder should not be due before an hour passed")
	}
	withReminder := base
	withReminder.HasApprovalReminderSent = true
	if trackerApprovalReminderDue(withReminder, now) {
		t.Fatal("reminder should not repeat")
	}
	approved := base
	approved.Approvals = []int64{1, 2}
	if trackerApprovalReminderDue(approved, now) {
		t.Fatal("fully approved task should not remind")
	}
}
