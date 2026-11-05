package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/database"
)

func TestStandWaitDecision(t *testing.T) {
	started := time.Date(2026, 8, 20, 21, 0, 0, 0, time.UTC)
	since := started.Add(-45 * time.Second)

	out := standWaitDecision([]standDeploy{
		{Status: "SUCCESS", CreatedAt: started.Add(20 * time.Second)},
	}, since, started, started.Add(20*time.Second))
	if out.Err != nil || !out.Done {
		t.Fatalf("fresh success: %+v", out)
	}

	out = standWaitDecision([]standDeploy{
		{ID: "fail-1", Status: "FAILED", CreatedAt: started.Add(10 * time.Second)},
	}, since, started, started.Add(10*time.Second))
	if out.Err == nil || out.Done || out.FailedID != "fail-1" {
		t.Fatalf("fresh fail must error: %+v", out)
	}

	out = standWaitDecision([]standDeploy{
		{Status: "DEPLOYING", CreatedAt: started.Add(5 * time.Second)},
		{Status: "SUCCESS", CreatedAt: started.Add(-2 * time.Minute)},
	}, since, started, started.Add(2*time.Minute))
	if out.Err != nil || out.Done {
		t.Fatalf("in-flight must wait: %+v", out)
	}

	out = standWaitDecision([]standDeploy{
		{Status: "SUCCESS", CreatedAt: started.Add(5 * time.Second)},
		{Status: "DEPLOYING", CreatedAt: started.Add(12 * time.Second)},
	}, since, started, started.Add(20*time.Second))
	if out.Err != nil || out.Done {
		t.Fatalf("первый SUCCESS при живой более новой сборке: %+v", out)
	}

	old := started.Add(-3 * time.Minute)
	out = standWaitDecision([]standDeploy{
		{Status: "SUCCESS", CreatedAt: old},
		{Status: "SKIPPED", CreatedAt: started.Add(10 * time.Second)},
	}, since, started, started.Add(10*time.Second))
	if out.Err != nil || !out.Done {
		t.Fatalf("свежий SKIPPED — эту службу не пересобирали: %+v", out)
	}
}

func TestStandWaitLeoFailBeatsMiniAppSkip(t *testing.T) {
	started := time.Date(2026, 8, 23, 8, 40, 0, 0, time.UTC)
	since := started.Add(-45 * time.Second)
	old := started.Add(-3 * time.Minute)
	out := standWaitServices(map[string][]standDeploy{
		"MiniApp": {
			{Status: "SUCCESS", CreatedAt: old},
			{Status: "SKIPPED", CreatedAt: started.Add(10 * time.Second)},
		},
		"ms_leo": {
			{ID: "leo-fail", Status: "FAILED", CreatedAt: started.Add(12 * time.Second)},
		},
	}, map[string]string{"ms_leo": "leo-fail"}, since, started, started.Add(time.Minute))
	if out.Err == nil || out.Done || out.FailedID != "leo-fail" {
		t.Fatalf("leo fail must win: %+v", out)
	}
	if out.FailedSvc != "ms_leo" {
		t.Fatalf("svc %q", out.FailedSvc)
	}
}

func TestStandWaitLeoSuccessAndMiniAppSkip(t *testing.T) {
	started := time.Date(2026, 8, 23, 8, 50, 0, 0, time.UTC)
	since := started.Add(-45 * time.Second)
	old := started.Add(-3 * time.Minute)
	out := standWaitServices(map[string][]standDeploy{
		"MiniApp": {
			{Status: "SUCCESS", CreatedAt: old},
			{Status: "SKIPPED", CreatedAt: started.Add(10 * time.Second)},
		},
		"ms_leo": {
			{ID: "leo-new", Status: "SUCCESS", CreatedAt: started.Add(20 * time.Second)},
		},
	}, map[string]string{"ms_leo": "leo-new"}, since, started, started.Add(trackerStandSkipGrace+time.Second))
	if out.Err != nil || !out.Done {
		t.Fatalf("ждём свой leo, MiniApp без заказа не мешает: %+v", out)
	}
}

func TestTrackerTaskShippedToStand(t *testing.T) {
	if trackerTaskShippedToStand(database.TrackerTask{Steps: []string{"тест пройден"}}) {
		t.Fatal("test is not ship")
	}
	if !trackerTaskShippedToStand(database.TrackerTask{Steps: []string{"пуш в main", "ждём сборку на стенде"}}) {
		t.Fatal("shipped")
	}
	if trackerTaskShippedToStand(database.TrackerTask{Steps: []string{
		"пуш в main", "ждём сборку на стенде", "вернули в работу: сборка не прошла",
	}}) {
		t.Fatal("retry must ship again")
	}
}

func TestTrackerFailLooksLikeStub(t *testing.T) {
	if !trackerFailLooksLikeStub("ProfileScreen.tsx обрезан (25 строк, нужно ≥800)", "") {
		t.Fatal("обрезан")
	}
	if !trackerFailLooksLikeStub("сборка не прошла", "undefined: parseAmountTiers\nimported and not used: os") {
		t.Fatal("compile stub")
	}
	if trackerFailLooksLikeStub("timeout waiting for deploy", "BUILDING") {
		t.Fatal("обычный сбой стенда")
	}
}

func TestTrackerStandFailCount(t *testing.T) {
	if trackerStandFailCount(database.TrackerTask{}) != 0 {
		t.Fatal("empty")
	}
	got := trackerStandFailCount(database.TrackerTask{Steps: []string{
		"сборка на стенде не прошла", "вернули в работу: сборка не прошла",
		"сборка на стенде не прошла",
	}})
	if got != 2 {
		t.Fatalf("got %d", got)
	}
}

func TestClipStandLogsPrefersErrors(t *testing.T) {
	got := clipStandLogs("ok line\nundefined: embeddedDailyWisdomVariation1\nBuild Failed: exit 1\nnoise")
	if !strings.Contains(got, "undefined") || !strings.Contains(got, "Build Failed") || strings.Contains(got, "ok line") {
		t.Fatalf("got %q", got)
	}
}

func TestIsStandWatchService(t *testing.T) {
	if !isStandWatchService("MiniApp") || !isStandWatchService("ms_leo") ||
		!isStandWatchService("ms-leo-main") || !isStandWatchService("fat-leopard-main") {
		t.Fatal("watch app")
	}
	if isStandWatchService("ms_tracker") || isStandWatchService("ms-tracker-main") || isStandWatchService("Postgres") {
		t.Fatal("skip infra")
	}
	if isStandWatchService("ms_payments") || !isStandObserveService("ms_payments") {
		t.Fatal("payments: смотрим, но не заказываем")
	}
}

func TestTryBeginTrackerStandOnce(t *testing.T) {
	if !tryBeginTrackerStand(42) {
		t.Fatal("first")
	}
	if tryBeginTrackerStand(42) {
		t.Fatal("second must skip")
	}
	endTrackerStand(42)
	if !tryBeginTrackerStand(42) {
		t.Fatal("after end")
	}
	endTrackerStand(42)
}

// Сборка упала — задачу заново не делаем: автору сообщение с логами, а
// автодожим сборки и перезапуск агента такую карточку не трогают.
func TestFailedStandWaitsForHuman(t *testing.T) {
	note := trackerStandFailedNotice(171, "Deployment failed", "Build Failed: exit 1")
	for _, want := range []string{"#171", "Deployment failed", "Код уже в main", "Запустить снова", "Build Failed"} {
		if !strings.Contains(note, want) {
			t.Errorf("в сообщении нет %q: %s", want, note)
		}
	}
	if strings.Contains(trackerStandFailedNotice(1, "timeout", ""), "Логи") {
		t.Error("без логов блока логов нет")
	}
	failed := database.TrackerTask{DevColumn: trackerColDeploy, Status: "error",
		Error: "сборка на стенде: Deployment failed", Steps: []string{trackerStandFailedStep}}
	if trackerNeedsAgentKick(failed, time.Now(), false) || trackerCardInDoing(failed) {
		t.Error("упавшую сборку агент не переделывает")
	}
}

// Отменённая карточка с ошибкой агента не уходит Cursor (#171).
func TestCanceledCardIsNotInDoing(t *testing.T) {
	canceled := database.TrackerTask{DevColumn: trackerColCanceled, Status: "error",
		Error: "нет правок в репозитории: агент сдал только заметку"}
	if trackerCardInDoing(canceled) {
		t.Error("отменённая карточка не в работе")
	}
	if !trackerCardInDoing(database.TrackerTask{DevColumn: trackerColDoing, Status: "error"}) {
		t.Error("упавшая в работе — в работе")
	}
}
