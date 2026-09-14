package bot

import (
	"testing"
	"time"

	"leo-bot/internal/database"
)

func TestTrackerTaskShippedToProd(t *testing.T) {
	shipped := database.TrackerTask{
		Status:    "done",
		DevColumn: trackerColDone,
		Steps:     []string{"пуш в main", "ждём сборку на стенде"},
	}
	if !trackerTaskShippedToProd(shipped) {
		t.Fatal("expected shipped")
	}
	pending := database.TrackerTask{Status: "done", DevColumn: trackerColDone, Steps: []string{"тест пройден"}}
	if trackerTaskShippedToProd(pending) {
		t.Fatal("test only is not prod ship")
	}
}

func TestTrackerTaskAffectsUserExperience(t *testing.T) {
	user := database.TrackerTask{
		Num:    91,
		Prompt: "Нужна фича @-упоминаний участников стаи в ленте и чате",
		Result: "Сделана фича @-упоминаний с уведомлениями в мини-аппе.",
		Steps:  []string{"сделано: упоминания в ленте и чате"},
	}
	if !trackerTaskAffectsUserExperience(user) {
		t.Fatal("mentions are user-facing")
	}
	internal := database.TrackerTask{
		Num:    107,
		Prompt: "fix(tracker): перезапускать ревью если агент фазы не стартовал",
		Result: "Планировщик трекера больше не держит конвейер.",
	}
	if trackerTaskAffectsUserExperience(internal) {
		t.Fatal("tracker pipeline is internal")
	}
	adminTG := database.TrackerTask{
		Num:    99,
		Prompt: "В Telegram-уведомлении админам показывать автора задачи",
		Result: "Добавлена строка Поставил в уведомление админам.",
	}
	if trackerTaskAffectsUserExperience(adminTG) {
		t.Fatal("admin telegram notify is not pack UX")
	}
	board := database.TrackerTask{
		Num:    102,
		Prompt: "Нужна опция ставить аппрув прям на доске в трекере",
		Result: "На карточках в колонке Аппрув появились кнопки.",
		Steps:  []string{"сделано: кнопки аппрува на доске"},
	}
	if !trackerTaskAffectsUserExperience(board) {
		t.Fatal("board approve buttons are miniapp UX")
	}
}

func TestCollectReleaseNoteFeaturesDedup(t *testing.T) {
	tasks := []database.TrackerTask{
		{
			Num: 105, Status: "done", DevColumn: trackerColDone,
			Prompt: "На тренировку ставить реакцию 👍 от Лео",
			Steps:  []string{"пуш в main", "сделано: дефолтная реакция в ленте"},
		},
		{
			Num: 105, Status: "done", DevColumn: trackerColDone,
			Prompt: "duplicate",
			Steps:  []string{"пуш в main"},
		},
	}
	features := collectReleaseNoteFeatures(tasks)
	if len(features) != 1 {
		t.Fatalf("dedup: got %d", len(features))
	}
	if features[0].Num != 105 {
		t.Fatalf("num: %d", features[0].Num)
	}
}

func TestReleaseNotesShouldRunToday(t *testing.T) {
	monday := time.Date(2026, 9, 14, 10, 0, 0, 0, time.UTC)
	if !releaseNotesShouldRunToday(monday, "") {
		t.Fatal("first run on monday")
	}
	tuesday := time.Date(2026, 9, 15, 10, 0, 0, 0, time.UTC)
	if releaseNotesShouldRunToday(tuesday, "") {
		t.Fatal("not on tuesday")
	}
	if releaseNotesShouldRunToday(monday, "2026-09-07") {
		t.Fatal("too soon after last")
	}
	if !releaseNotesShouldRunToday(monday, "2026-08-25") {
		t.Fatal("14+ days since last")
	}
}
