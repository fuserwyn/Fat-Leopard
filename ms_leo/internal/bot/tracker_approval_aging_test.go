package bot

import (
	"fmt"
	"strings"
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

// Отказ теста не останавливает карточку: она возвращается агенту с причиной,
// сколько бы раз подряд тест ни падал.
func TestFailedTestAlwaysReturnsCardToWork(t *testing.T) {
	task := database.TrackerTask{
		ID: 9, Num: 9, Prompt: "Добавить челленджи", DevColumn: trackerColTest, Status: "holding",
		Result: "Сделал челленджи: миграция, API, тесты.",
	}
	for attempt := 1; attempt <= 8; attempt++ {
		_ = applyTrackerColumn(&task, trackerColTest)
		reason := fmt.Sprintf("тест не прошёл: ms_leo: сборка не прошла:\nошибка №%d", attempt)
		applyTrackerPhaseVerdict(&task, trackerColTest, reason)

		if task.DevColumn != trackerColDoing {
			t.Fatalf("попытка %d: карточка должна вернуться в работу, а она в %q", attempt, task.DevColumn)
		}
		if !strings.Contains(task.Result, fmt.Sprintf("ошибка №%d", attempt)) {
			t.Fatalf("попытка %d: причина отказа должна дойти до агента: %q", attempt, task.Result)
		}
		if attempt > 1 && strings.Contains(task.Result, fmt.Sprintf("ошибка №%d", attempt-1)) {
			t.Fatalf("попытка %d: старая причина должна заменяться, а не копиться: %q", attempt, task.Result)
		}
		if !strings.HasPrefix(task.Result, "Сделал челленджи") {
			t.Fatalf("попытка %d: отчёт агента о сделанном теряться не должен: %q", attempt, task.Result)
		}
	}

	prompt := trackerAgentPrompt(task, "doing")
	if !strings.Contains(prompt, "проверка перед выкатом его не приняла") || !strings.Contains(prompt, "ошибка №8") || !strings.Contains(prompt, "Добавить челленджи") {
		t.Errorf("задание на доработку: %q", prompt)
	}

	// Пройденный тест карточку не трогает.
	passed := database.TrackerTask{DevColumn: trackerColTest, Result: "готово"}
	applyTrackerPhaseVerdict(&passed, trackerColTest, "Тест: config.go целый, ветка tracker/9-1. Тест пройден.")
	if passed.DevColumn != trackerColTest || passed.Error != "" || passed.Result != "готово" {
		t.Errorf("пройденный тест: %+v", passed)
	}
}

// Перезапуск сорвавшегося агента не имеет предела, но пауза растёт до получаса.
func TestAgentRestartsNeverStopButSlowDown(t *testing.T) {
	if trackerAgentKickWait(0) != 90*time.Second || trackerAgentKickWait(1) != 3*time.Minute || trackerAgentKickWait(3) != 12*time.Minute {
		t.Errorf("пауза удваивается: %s %s %s", trackerAgentKickWait(0), trackerAgentKickWait(1), trackerAgentKickWait(3))
	}
	if trackerAgentKickWait(5) != 30*time.Minute || trackerAgentKickWait(500) != 30*time.Minute {
		t.Errorf("пауза не больше получаса: %s", trackerAgentKickWait(500))
	}

	now := time.Now()
	failed := database.TrackerTask{
		DevColumn: trackerColDoing, Status: "error", Error: "Агент не стартовал: нет токена",
		HasLastRun: true, LastRunAt: now.Add(-31 * time.Minute),
	}
	for i := 0; i < 40; i++ {
		failed.Steps = append(failed.Steps, "Снова запускаем агента")
	}
	if !trackerNeedsAgentKick(failed, now, false) {
		t.Fatal("после сорока попыток агента всё равно перезапускаем")
	}
	failed.LastRunAt = now.Add(-10 * time.Minute)
	if trackerNeedsAgentKick(failed, now, false) {
		t.Error("но не чаще раза в полчаса")
	}
	if !trackerNeedsAgentKick(failed, now, true) {
		t.Error("кнопка «Обновить» перезапускает сразу")
	}
}
