package bot

import (
	"testing"
	"time"

	"leo-bot/internal/database"
)

func approvalTask(approvals []int64, waited time.Duration, now time.Time) database.TrackerTask {
	return database.TrackerTask{
		ID: 7, Num: 7, Prompt: "Починить кнопку", NeedsApproval: true, DevColumn: trackerColApprove,
		Approvals: approvals, HasAuthor: true, AuthorID: 100,
		HasApprovalNotified: true, ApprovalNotifiedAt: now.Add(-waited),
	}
}

func TestTrackerLeoVoteDue(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if trackerLeoVoteDue(approvalTask([]int64{200}, 23*time.Hour, now), now) {
		t.Error("меньше суток — рано")
	}
	if !trackerLeoVoteDue(approvalTask([]int64{200}, 25*time.Hour, now), now) {
		t.Error("1/2 больше суток — Лео голосует")
	}
	if trackerLeoVoteDue(approvalTask(nil, 48*time.Hour, now), now) {
		t.Error("0/2 — Лео не голосует первым")
	}
	if trackerLeoVoteDue(approvalTask([]int64{200, 300}, 48*time.Hour, now), now) {
		t.Error("2/2 — голос не нужен")
	}

	own := approvalTask([]int64{200}, 48*time.Hour, now)
	own.Kind = "leo_task"
	if trackerLeoVoteDue(own, now) {
		t.Error("свою задачу Лео не одобряет")
	}

	voted := approvalTask([]int64{200}, 48*time.Hour, now)
	voted.Steps = []string{trackerLeoVoteStep(1), "Лео не поддержал: расплывчато"}
	if trackerLeoVoteDue(voted, now) {
		t.Error("в этом круге Лео уже голосовал")
	}
	voted.Steps = append(voted.Steps, "Формулировку обновили", "Аппрувы сброшены после правки")
	if !trackerLeoVoteDue(voted, now) {
		t.Error("после сброса аппрувов — новый круг, Лео голосует снова")
	}

	moved := approvalTask([]int64{200}, 48*time.Hour, now)
	moved.DevColumn = trackerColDoing
	if trackerLeoVoteDue(moved, now) {
		t.Error("карточка уже не на аппруве")
	}
}

func TestTrackerApprovalExpired(t *testing.T) {
	now := time.Date(2026, 10, 7, 12, 0, 0, 0, time.UTC)
	if trackerApprovalExpired(approvalTask(nil, 4*24*time.Hour, now), now) {
		t.Error("четыре дня — ещё ждём")
	}
	if !trackerApprovalExpired(approvalTask(nil, 5*24*time.Hour+time.Minute, now), now) {
		t.Error("0/2 больше пяти дней — отмена")
	}
	if trackerApprovalExpired(approvalTask([]int64{200}, 10*24*time.Hour, now), now) {
		t.Error("с одним голосом карточку не отменяем")
	}
	// Уведомление не уходило — считаем от создания.
	old := approvalTask(nil, 0, now)
	old.HasApprovalNotified = false
	old.CreatedAt = now.Add(-6 * 24 * time.Hour)
	if !trackerApprovalExpired(old, now) {
		t.Error("без отметки об уведомлении срок идёт от создания")
	}
}

func TestParseLeoVote(t *testing.T) {
	approve, reason, ok := parseLeoVote("Вот ответ:\n{\"approve\": true, \"reason\": \" Понятная задача \"}")
	if !ok || !approve || reason != "Понятная задача" {
		t.Errorf("за: %v %q %v", approve, reason, ok)
	}
	if approve, _, ok = parseLeoVote(`{"approve": false, "reason": "Расплывчато"}`); !ok || approve {
		t.Errorf("против: %v %v", approve, ok)
	}
	for _, bad := range []string{"", "да, одобряю", `{"reason": "нет поля approve"}`, `{"approve": "yes"}`} {
		if _, _, ok := parseLeoVote(bad); ok {
			t.Errorf("должно не разобраться: %q", bad)
		}
	}
}

func TestTrackerApproverIDsInView(t *testing.T) {
	task := approvalTask([]int64{200, database.TrackerLeoAuthorID}, time.Hour, time.Now())
	view := trackerTaskView(task, false)
	ids, ok := view["approver_ids"].([]int64)
	if !ok || len(ids) != 2 || ids[0] != 200 || ids[1] != database.TrackerLeoAuthorID {
		t.Fatalf("approver_ids: %#v", view["approver_ids"])
	}
	if got := trackerTaskView(approvalTask(nil, time.Hour, time.Now()), false)["approver_ids"].([]int64); got == nil {
		t.Fatal("без голосов — пустой массив, не null")
	}
	if !trackerAppendApproval(&task, 300) || trackerAppendApproval(&task, database.TrackerLeoAuthorID) || trackerAppendApproval(&task, 0) {
		t.Fatal("правила добавления голоса")
	}
}
