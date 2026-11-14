package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/database"
)

// Админка трекера снова пишет по-русски; карточки, начатые пока шаги
// писались по-английски, должны читаться как раньше.
func TestTrackerBriefReadsLegacyEnglishSteps(t *testing.T) {
	task := database.TrackerTask{
		Num:    5,
		Prompt: "Старая задача",
		Steps: []string{
			"done: поправил подпись",
			"commit abc1234 execution",
			"commit def5678 review",
			"review: пройдено без замечаний",
			"commit fedcba9 test",
			"test: прогнали всё",
			"push to main",
		},
	}
	brief := trackerDoneBrief(task)
	for _, want := range []string{
		"1. Выполнение: поправил подпись",
		"2. Ревью: пройдено без замечаний",
		"3. Тест: прогнали всё",
		"Выехала на прод (ветка main)",
	} {
		if !strings.Contains(brief, want) {
			t.Fatalf("brief missing %q: %q", want, brief)
		}
	}
}

func TestTrackerApplyNotifyLabelsCommitInRussian(t *testing.T) {
	task := database.TrackerTask{Status: "holding", DevColumn: trackerColReview}
	applyTrackerNotify(&task, "done", "Можно на тест.\n\ncommit abc1234 review")
	found := false
	for _, s := range task.Steps {
		if s == "коммит abc1234 ревью" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steps: %#v", task.Steps)
	}
}

func TestTrackerTaskTitleStripsSprintPrefixBothLanguages(t *testing.T) {
	if got := trackerTaskTitle("[Спринт 3] Починить ленту"); got != "Починить ленту" {
		t.Fatalf("ru sprint: %q", got)
	}
	if got := trackerTaskTitle("[Sprint 4] Fix feed"); got != "Fix feed" {
		t.Fatalf("en sprint: %q", got)
	}
}

func TestTrackerApprovalRoundCountsLegacyReset(t *testing.T) {
	task := database.TrackerTask{Steps: []string{"Approvals reset", trackerApprovalsResetStep + ": текст поменяли"}}
	if got := trackerApprovalRound(task); got != 3 {
		t.Fatalf("round: %d", got)
	}
}
