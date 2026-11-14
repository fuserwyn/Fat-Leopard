package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/database"
)

func TestTrackerTaskDueForStart(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 20, 0, 0, time.UTC)
	due := database.TrackerTask{
		Status:    "pending",
		DevColumn: "todo",
		WhenAt:    now.Add(-time.Minute),
	}
	if !trackerTaskDueForStart(due, now) {
		t.Fatal("09:19 must start at 09:20")
	}
	future := due
	future.WhenAt = now.Add(time.Minute)
	if trackerTaskDueForStart(future, now) {
		t.Fatal("future when_at must wait")
	}
	running := due
	running.Status = "running"
	running.DevColumn = "doing"
	if trackerTaskDueForStart(running, now) {
		t.Fatal("already running must not claim")
	}
	emptyWhen := database.TrackerTask{Status: "pending", DevColumn: "todo"}
	if !trackerTaskDueForStart(emptyWhen, now) {
		t.Fatal("empty when_at is overdue")
	}
	scheduled := due
	scheduled.Status = "scheduled"
	scheduled.DevColumn = ""
	if !trackerTaskDueForStart(scheduled, now) {
		t.Fatal("scheduled + empty column is очередь")
	}
}

func TestTrackerDueStartedNote(t *testing.T) {
	note := trackerDueStartedNote(database.TrackerTask{
		Num:    1,
		Prompt: "починить подпись стрика",
	})
	if !strings.Contains(note, "#1") || !strings.Contains(note, "началась") {
		t.Fatalf("note: %q", note)
	}
	if !strings.Contains(note, "починить подпись стрика") {
		t.Fatalf("prompt: %q", note)
	}
	long := strings.Repeat("я", 200)
	clipped := trackerDueStartedNote(database.TrackerTask{ID: 9, Prompt: long})
	if strings.Contains(clipped, long) || !strings.HasSuffix(clipped, "…") {
		t.Fatalf("must clip prompt, got %q", clipped)
	}
}

func TestRunDueTrackerTasksNilSafe(t *testing.T) {
	var b *Bot
	b.runDueTrackerTasks()
	(&Bot{}).runDueTrackerTasks()
	(&Bot{}).kickTrackerDueIfReady(time.Now().Add(-time.Minute))
	if n, err := b.claimAndNotifyDueTrackerTasks(); err == nil || n != 0 {
		t.Fatalf("nil bot: n=%d err=%v", n, err)
	}
	if n, err := (&Bot{}).claimAndNotifyDueTrackerTasks(); err == nil || n != 0 {
		t.Fatalf("empty bot: n=%d err=%v", n, err)
	}
}

func TestTrackerTaskTitle(t *testing.T) {
	if got := trackerTaskTitle("Сделай донат 10 звезд"); got != "Сделай донат 10 звезд" {
		t.Fatalf("plain: %q", got)
	}
	if got := trackerTaskTitle("[Спринт 2] кнопка удалить"); got != "кнопка удалить" {
		t.Fatalf("sprint: %q", got)
	}
	if got := trackerTaskTitle("Задача #40.\n\nНазвание в уведомлении"); got != "Название в уведомлении" {
		t.Fatalf("skip num line: %q", got)
	}
}

func TestTrackerNotifyLabel(t *testing.T) {
	if got := trackerNotifyLabel(40, "Название в уведомлении"); got != "Задача #40: Название в уведомлении" {
		t.Fatalf("with title: %q", got)
	}
	if got := trackerNotifyLabel(40, ""); got != "Задача #40" {
		t.Fatalf("no title: %q", got)
	}
}

func TestTrackerFullyDoneNoteIncludesTitle(t *testing.T) {
	note := trackerFullyDoneNote(database.TrackerTask{Num: 40, Prompt: "Название в уведомлении"})
	if !strings.Contains(note, "Задача #40: Название в уведомлении") {
		t.Fatalf("note: %q", note)
	}
	if strings.Contains(note, "1. Выполнение:") {
		t.Fatalf("notify must stay short: %q", note)
	}
}

func TestTrackerFullyDoneNoteIncludesExecutionSummary(t *testing.T) {
	task := database.TrackerTask{
		Num:    100,
		Prompt: "Короткий отчёт в уведомлении",
		Steps:  []string{"сделано: поправил уведомление о выкате"},
	}
	note := trackerFullyDoneNote(task)
	if !strings.Contains(note, "поправил уведомление о выкате") {
		t.Fatalf("execution summary in notify: %q", note)
	}
	if strings.Contains(note, "2. Ревью:") {
		t.Fatalf("notify must not list pipeline phases: %q", note)
	}
}

func TestTrackerFullyDoneNoteSkipsDoneForTaskBoilerplate(t *testing.T) {
	task := database.TrackerTask{
		Num:    100,
		Prompt: "Необязательно в уведомлении писать про все 5 пунктов",
		Steps:  []string{"сделано: Сделано для задачи #100."},
	}
	note := trackerFullyDoneNote(task)
	if strings.Contains(note, "Сделано для задачи") {
		t.Fatalf("notify must not repeat agent boilerplate: %q", note)
	}
	wantParts := []string{
		"✅ Задача #100:",
		"Выехала на прод (ветка main).",
	}
	for _, part := range wantParts {
		if !strings.Contains(note, part) {
			t.Fatalf("notify missing %q: %q", part, note)
		}
	}
	if strings.Count(note, "\n\n") != 1 {
		t.Fatalf("notify must stay two-part without boilerplate summary: %q", note)
	}
}

func TestTrackerDoneBriefIncludesPipeline(t *testing.T) {
	task := database.TrackerTask{
		Num:    67,
		Prompt: "Короткий отчёт в выполненной задаче",
		Steps: []string{
			"сделано: поправил уведомление о выкате",
			"коммит abc1234 выполнение",
			"коммит def5678 ревью",
			"ревью: пройдено (коммит def5678)",
			"коммит fedcba9 тест",
			"тест: пройден (коммит fedcba9)",
			"пуш в main",
			"стенд собрался",
		},
	}
	brief := trackerDoneBrief(task)
	for _, want := range []string{
		"1. Выполнение: поправил уведомление о выкате",
		"2. Ревью: пройдено (коммит def5678)",
		"3. Тест: пройден (коммит fedcba9)",
		"4. Сборка: Выехала на прод (ветка main)",
		"5. Уведомление админам отправлено.",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q: %q", want, brief)
		}
	}
	note := trackerFullyDoneNote(task)
	if !TrackerNotifyIsFullyShipped(note) {
		t.Fatalf("done note must still look shipped: %q", note)
	}
}

func TestTrackerExtractAgentNote(t *testing.T) {
	text := "Задача #4: правка: коммит выполнения abc1234 на ветке tracker/4-1.\n\nИсправил кнопку.\n\nСледующий шаг — review."
	if got := trackerExtractAgentNote(text); got != "Исправил кнопку." {
		t.Fatalf("agent note: %q", got)
	}
}

func TestTrackerRecordPhaseSummaryDoing(t *testing.T) {
	task := database.TrackerTask{DevColumn: trackerColDoing}
	text := "Задача #67: отчёт: коммит выполнения abc1234 на ветке tracker/67-584.\n\nДобавил краткий отчёт.\n\nСледующий шаг — review."
	trackerRecordPhaseSummary(&task, trackerColDoing, text)
	if got := trackerStepPrefixedSummary(task.Steps, "сделано:"); got != "Добавил краткий отчёт." {
		t.Fatalf("stored doing summary: %q", got)
	}
}
