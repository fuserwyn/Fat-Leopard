package bot

import (
	"testing"
	"time"

	"leo-bot/internal/database"
)

func TestTrackerTaskInPipelineIgnoresWaitingAndFailedDoing(t *testing.T) {
	cases := []struct {
		name string
		task database.TrackerTask
		want bool
	}{
		{
			name: "в работе ждёт очередь",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Steps: []string{"Взяли в работу", trackerAgentWaitingStep}},
			want: false,
		},
		{
			name: "в работе агент не стартовал",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Error: "Агент не стартовал: clone", Steps: []string{"Агент: запустили", "Агент не стартовал"}},
			want: false,
		},
		{
			name: "в работе с ошибкой",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "error"},
			want: false,
		},
		{
			name: "в работе живой агент",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running",
				Steps: []string{"Агент: запустили", "агент:#709"}},
			want: true,
		},
		{
			name: "ревью",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running",
				Steps: []string{"Агент сдал результат"}},
			want: true,
		},
		{
			name: "сборка выполнена",
			task: database.TrackerTask{DevColumn: trackerColDeploy, Status: "done"},
			want: false,
		},
	}
	for _, c := range cases {
		if got := trackerTaskInPipeline(c.task); got != c.want {
			t.Errorf("%s: trackerTaskInPipeline = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTrackerNeedsPhaseKick(t *testing.T) {
	now := time.Date(2026, 9, 14, 18, 30, 0, 0, time.UTC)
	old := now.Add(-40 * time.Minute)
	cases := []struct {
		name  string
		task  database.TrackerTask
		force bool
		want  bool
	}{
		{
			name: "ревью приехало, агента не было",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running", HasLastRun: true, LastRunAt: old,
				Steps: []string{"Агент: запустили", "агент:#709", "Агент сдал результат", "коммит 3ca7db9 выполнение", "ветка tracker/107-709"}},
			want: true,
		},
		{
			name: "ревью-агент уже запущен",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running", HasLastRun: true, LastRunAt: old,
				Steps: []string{"Агент сдал результат", "Composer-ревью: запустили"}},
			want: false,
		},
		{
			name: "только что приехало — ждём обычный старт",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running", HasLastRun: true, LastRunAt: now.Add(-time.Minute),
				Steps: []string{"Агент сдал результат"}},
			want: false,
		},
		{
			name: "обновить — без ожидания",
			task: database.TrackerTask{DevColumn: trackerColReview, Status: "running", HasLastRun: true, LastRunAt: now.Add(-time.Minute),
				Steps: []string{"Агент сдал результат"}},
			force: true,
			want:  true,
		},
		{
			name: "тест не принят — ждёт человека",
			task: database.TrackerTask{DevColumn: trackerColTest, Status: "running", HasLastRun: true, LastRunAt: old,
				Steps: []string{"Агент сдал результат", "Composer-тест не принято"}},
			want: false,
		},
		{
			name: "ручной QA",
			task: database.TrackerTask{DevColumn: trackerColTest, Status: "running", ManualQa: true, HasLastRun: true, LastRunAt: old,
				Steps: []string{"Агент сдал результат"}},
			want: false,
		},
		{
			name: "в работе не наше",
			task: database.TrackerTask{DevColumn: trackerColDoing, Status: "running", HasLastRun: true, LastRunAt: old,
				Steps: []string{"Агент сдал результат"}},
			want: false,
		},
	}
	for _, c := range cases {
		if got := trackerNeedsPhaseKick(c.task, now, c.force); got != c.want {
			t.Errorf("%s: trackerNeedsPhaseKick = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestTrackerNeedsAgentKickWaitingWithOldRemote(t *testing.T) {
	now := time.Date(2026, 9, 14, 19, 30, 0, 0, time.UTC)
	old := now.Add(-time.Hour)
	waiting := database.TrackerTask{DevColumn: trackerColDoing, Status: "running", HasLastRun: true, LastRunAt: old,
		Steps: []string{"Агент: запустили", "агент:#707", "Агент не стартовал", "Снова запускаем агента", trackerAgentWaitingStep}}
	if !trackerNeedsAgentKick(waiting, now, false) {
		t.Error("ждущая карточка со старым агент:#N должна перезапускаться")
	}
	live := database.TrackerTask{DevColumn: trackerColDoing, Status: "running", HasLastRun: true, LastRunAt: old,
		Steps: []string{"Агент: запустили", "агент:#709"}}
	if trackerNeedsAgentKick(live, now, false) {
		t.Error("карточку с живым агентом трогать нельзя")
	}
}

func TestTrackerLaterPhaseBlocksOlderWins(t *testing.T) {
	older := database.TrackerTask{ID: 108, DevColumn: trackerColReview, Status: "reviewing"}
	newer := database.TrackerTask{ID: 115, DevColumn: trackerColReview, Status: "reviewing"}
	doing := database.TrackerTask{ID: 90, DevColumn: trackerColDoing, Status: "running", Steps: []string{"агент:#736"}}
	list := []database.TrackerTask{older, newer, doing}
	if trackerLaterPhaseBlocks(older, list) {
		t.Error("старшая карточка в ревью не должна ждать младшую и «В работе»")
	}
	if !trackerLaterPhaseBlocks(newer, list) {
		t.Error("младшая карточка в ревью ждёт старшую")
	}
	shipped := database.TrackerTask{ID: 100, DevColumn: trackerColDeploy, Status: "done"}
	if trackerLaterPhaseBlocks(newer, []database.TrackerTask{shipped, newer}) {
		t.Error("выкатанная карточка конвейер не держит")
	}
}
