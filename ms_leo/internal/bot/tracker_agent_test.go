package bot

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
)

func TestTrackerAgentPromptPhases(t *testing.T) {
	task := database.TrackerTask{Num: 7, Prompt: "починить стрик", Result: "подпись короче"}
	doing := trackerAgentPrompt(task, "doing")
	if !strings.Contains(doing, "#7") || !strings.Contains(doing, "починить стрик") ||
		!strings.Contains(doing, "замечания ревью") || !strings.Contains(doing, "подпись короче") {
		t.Fatalf("doing: %q", doing)
	}
	stand := trackerAgentPrompt(database.TrackerTask{
		Num: 16, Prompt: "вариации мудрости",
		Result: "Сборка на стенде не прошла: ms_leo: деплой failed\n\nЛоги сборки Railway:\nundefined: embeddedDailyWisdomVariation1",
	}, "doing")
	if !strings.Contains(stand, "Сборка Railway упала") || !strings.Contains(stand, "embeddedDailyWisdomVariation1") {
		t.Fatalf("stand logs: %q", stand)
	}
	review := trackerAgentPrompt(task, "review")
	if !strings.Contains(review, "можно на тест") || !strings.Contains(review, "ревью не принято") {
		t.Fatalf("review: %q", review)
	}
	test := trackerAgentPrompt(task, "test")
	if !strings.Contains(test, "тест пройден") || !strings.Contains(test, "подпись короче") {
		t.Fatalf("test: %q", test)
	}
}

func TestTrackerComposerPassed(t *testing.T) {
	if !trackerComposerPassed("review", "Глянул diff. Можно на тест.") {
		t.Fatal("review pass")
	}
	if trackerComposerPassed("review", "Ревью не принято: нет тестов.") {
		t.Fatal("review fail")
	}
	if !trackerComposerPassed("test", "Покрыл сценарий. Тест пройден.") {
		t.Fatal("test pass")
	}
	if trackerComposerPassed("test", "Тест не прошёл: кнопка не жмётся.") {
		t.Fatal("test fail")
	}
	if trackerComposerPassed("review", "глянул поверхностно, в целом ок") {
		t.Fatal("без «можно на тест» ревью не двигает карточку")
	}
	if !trackerComposerPassed("doing", "⏰ Задача #1 выполнена.\n\nГотово.") {
		t.Fatal("impl done is pass")
	}
	if trackerComposerPassed("review", "Посредственное ревью: на ветке есть правки. Можно на тест.") {
		t.Fatal("фальшивое ревью не закрывает карточку")
	}
	if trackerComposerPassed("test", "Минимальный тест: ветка на месте, дымовая проверка ок. Тест пройден.") {
		t.Fatal("дымовой тест не закрывает карточку")
	}
	if !trackerComposerPassed("review", "Ревью: на ветке tracker/31-358 номинал 150 скрыт в config.go. Можно на тест.") {
		t.Fatal("настоящее ревью должно двигать карточку")
	}
	if !trackerComposerPassed("test", "Тест: номинал 150 скрыт в config.go, ветка tracker/31-358. Тест пройден.") {
		t.Fatal("настоящий тест должен закрывать фазу")
	}
}

func TestTrackerStepRemoteID(t *testing.T) {
	if got := trackerStepRemoteID([]string{"Взяли", "агент:#512", "Composer: запустили"}); got != 512 {
		t.Fatalf("got %d", got)
	}
	if got := trackerStepRemoteID(nil); got != 0 {
		t.Fatalf("empty %d", got)
	}
}

func TestTrackerComposerModel(t *testing.T) {
	if got := trackerComposerModel(nil); got != trackerComposerModelKey {
		t.Fatalf("nil: %s", got)
	}
	b := &Bot{config: &config.Config{BoardReviewModel: "cursor-composer"}}
	if got := trackerDoingModelKey("cursor"); got != "cursor" {
		t.Fatalf("cursor: %s", got)
	}
	if got := trackerDoingModelKey("claude"); got != "claude" {
		t.Fatalf("claude: %s", got)
	}
	if got := trackerDoingModelKey(""); got != "cursor" {
		t.Fatalf("auto: %s", got)
	}
	if got := trackerComposerModel(b); got != "cursor-composer" {
		t.Fatalf("cfg: %s", got)
	}
}

func TestTrackerAgentBoardUserIDUsesOwnerNotAuthor(t *testing.T) {
	b := &Bot{config: &config.Config{OwnerID: 99, AdminIDs: []int64{7}}}
	if got := b.trackerAgentBoardUserID(); got != 99 {
		t.Fatalf("owner: %d", got)
	}
	if got := (&Bot{}).trackerAgentBoardUserID(); got != 0 {
		t.Fatalf("empty: %d", got)
	}
}

func TestTrackerTaskCommitAndHasCode(t *testing.T) {
	task := database.TrackerTask{
		Steps:  []string{"Агент сдал", "коммит abc1234 выполнение", "ветка tracker/5-314"},
		Result: "Задача #5: коммит выполнения abc1234 на ветке tracker/5-314.",
	}
	if got := trackerTaskCommit(task); got != "abc1234" {
		t.Fatalf("commit: %q", got)
	}
	if !trackerTaskHasCode(task) {
		t.Fatal("commit step is code")
	}
	if trackerTaskHasCode(database.TrackerTask{Result: "план без правок"}) {
		t.Fatal("plan is not code")
	}
	if !trackerTaskHasCode(database.TrackerTask{
		Steps:  []string{"коммит abc1234 выполнение", "ветка tracker/12-17"},
		Result: "Задача #12: агент сдал план\nTRACKER_NO_CODE",
	}) {
		t.Fatal("commit and branch must still ship")
	}
}

func TestTrackerTaskBranch(t *testing.T) {
	if got := trackerTaskBranch(database.TrackerTask{Result: "код в ветке tracker/4-43.\nещё текст"}); got != "tracker/4-43" {
		t.Fatalf("result: %q", got)
	}
	if got := trackerTaskBranch(database.TrackerTask{Steps: []string{"Агент сдал", "ветка tracker/8-12 на GitHub"}}); got != "tracker/8-12" {
		t.Fatalf("steps: %q", got)
	}
	if got := trackerTaskBranch(database.TrackerTask{Result: "можно на тест"}); got != "" {
		t.Fatalf("empty: %q", got)
	}
}

func TestShipTrackerToMain(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/ship" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if r.Header.Get("X-Tracker-Secret") != "sec" {
			t.Fatal("secret")
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"merged":true,"base":"main","head":"tracker/4-43","deployed":true,"pinned":{"MiniApp":"dep-1"}}`))
	}))
	defer srv.Close()
	b := &Bot{config: &config.Config{BoardURL: srv.URL, BoardSecret: "sec"}}
	base, pinned, err := b.shipTrackerToMain(database.TrackerTask{
		ID: 11, Num: 4, Result: "код в ветке tracker/4-43",
	})
	if err != nil || base != "main" {
		t.Fatalf("ship: %s %v", base, err)
	}
	if pinned["MiniApp"] != "dep-1" {
		t.Fatalf("pinned %#v", pinned)
	}
	if int(got["source_task_id"].(float64)) != 11 || got["branch"] != "tracker/4-43" {
		t.Fatalf("payload %#v", got)
	}
}

func TestInspectTrackerBranch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/inspect" || r.URL.Query().Get("branch") != "tracker/12-17" {
			t.Fatalf("req %s %s", r.URL.Path, r.URL.RawQuery)
		}
		_, _ = w.Write([]byte(`{"ok":true,"exists":true,"has_impl":false}`))
	}))
	defer srv.Close()
	b := &Bot{config: &config.Config{BoardURL: srv.URL, BoardSecret: "sec"}}
	ok, err := b.inspectTrackerBranch(database.TrackerTask{Result: "ветка tracker/12-17"})
	if err != nil || ok {
		t.Fatalf("note-only: %v %v", ok, err)
	}
}

func TestTrackerPipelineNotify(t *testing.T) {
	note := trackerPipelineNotify(database.TrackerTask{Num: 3})
	if !strings.Contains(note, "#3") || strings.Contains(note, "тест и сборку") {
		t.Fatalf("must not fake a build: %q", note)
	}
	if !strings.Contains(note, "не выехала") {
		t.Fatalf("no-code note: %q", note)
	}
	withCode := trackerPipelineNotify(database.TrackerTask{Num: 3, Result: "код в ветке tracker/3-1"})
	if !strings.Contains(withCode, "запушь") || strings.Contains(withCode, "не выехала") {
		t.Fatalf("with code: %q", withCode)
	}
}

func TestTrackerNeedsAgentKick(t *testing.T) {
	now := time.Date(2026, 8, 20, 10, 50, 0, 0, time.UTC)
	failed := database.TrackerTask{
		Status:     "running",
		DevColumn:  "doing",
		Error:      "Агент не стартовал: unauthorized",
		Steps:      []string{"Взяли в работу по расписанию", "Агент не стартовал"},
		HasLastRun: true,
		LastRunAt:  now.Add(-time.Minute),
	}
	if !trackerNeedsAgentKick(failed, now, true) {
		t.Fatal("failed agent must retry on refresh")
	}
	freshClaim := database.TrackerTask{
		Status:     "running",
		DevColumn:  "doing",
		Steps:      []string{"Взяли в работу по расписанию"},
		HasLastRun: true,
		LastRunAt:  now.Add(-5 * time.Second),
	}
	if trackerNeedsAgentKick(freshClaim, now, true) {
		t.Fatal("fresh claim must not double-dispatch")
	}
	staleClaim := freshClaim
	staleClaim.LastRunAt = now.Add(-time.Hour)
	if !trackerNeedsAgentKick(staleClaim, now, false) {
		t.Fatal("stale claim without remote id must retry")
	}
	liveRemote := database.TrackerTask{
		Status:     "running",
		DevColumn:  "doing",
		Steps:      []string{"Агент: запустили", "агент:#88"},
		HasLastRun: true,
		LastRunAt:  now.Add(-time.Minute),
	}
	if trackerNeedsAgentKick(liveRemote, now, true) {
		t.Fatal("live remote job must not retry")
	}
	failedRemote := database.TrackerTask{
		Status:     "error",
		DevColumn:  "doing",
		Error:      "⚠️ Задача #66: агент не стартовал.\nнет правок в репозитории: агент сдал только заметку",
		Steps:      []string{"Два аппрува — в работу", "Агент: запустили", "агент:#580", "Ошибка"},
		HasLastRun: true,
		LastRunAt:  now.Add(-2 * time.Minute),
	}
	if !trackerNeedsAgentKick(failedRemote, now, true) {
		t.Fatal("failed start with stale remote id must retry")
	}
	if !trackerNeedsAgentKick(failedRemote, now, false) {
		t.Fatal("stale note-only failure must auto-retry")
	}
	approved := database.TrackerTask{
		Status:     "running",
		DevColumn:  "doing",
		Steps:      []string{"Два аппрува — в работу"},
		HasLastRun: true,
		LastRunAt:  now.Add(-time.Hour),
	}
	if !trackerNeedsAgentKick(approved, now, false) {
		t.Fatal("approved into work without agent must start")
	}
	freshApproved := approved
	freshApproved.LastRunAt = now.Add(-5 * time.Second)
	if trackerNeedsAgentKick(freshApproved, now, true) {
		t.Fatal("fresh approval must not double-dispatch")
	}
	waiting := approved
	waiting.Steps = []string{"Два аппрува — в работу", trackerAgentWaitingStep}
	if !trackerNeedsAgentKick(waiting, now, false) {
		t.Fatal("queued after approval must start when pipeline is free")
	}
	capped := failedRemote
	capped.Steps = append(capped.Steps, "Снова запускаем агента", "Снова запускаем агента",
		"Снова запускаем агента", "Снова запускаем агента", "Снова запускаем агента")
	if trackerNeedsAgentKick(capped, now, false) {
		t.Fatal("auto-retry must stop after cap")
	}
	if !trackerNeedsAgentKick(capped, now, true) {
		t.Fatal("manual retry must ignore cap")
	}
}

func TestRemoteTrackerCreateUsesOwnTracker(t *testing.T) {
	var gotPath, gotSecret, gotWhen string
	var sourceID float64
	var autoPush bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"ok":true,"service":"ms_tracker","wire":{"notify":true,"railway":true,"github":true,"cursor":true}}`))
			return
		}
		gotPath = r.URL.Path
		gotSecret = r.Header.Get("X-Tracker-Secret")
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		gotWhen, _ = body["when"].(string)
		sourceID, _ = body["source_task_id"].(float64)
		autoPush, _ = body["auto_push"].(bool)
		if _, ok := body["session"]; ok {
			t.Error("own tracker must not send MyVibeLab session")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"id":512,"when":"20.08 10:54"}`))
	}))
	defer srv.Close()

	b := &Bot{config: &config.Config{
		OwnerID:     99,
		BoardSecret: "secret",
		BoardRepo:   "fuserwyn/Fat-Leopard",
		BoardURL:    srv.URL,
	}}
	id, when, err := b.remoteTrackerCreate(database.TrackerTask{ID: 2, Num: 2, Prompt: "убрать огонёк", AutoPush: false}, "doing", "")
	if err != nil {
		t.Fatal(err)
	}
	if id != 512 || when != "20.08 10:54" {
		t.Fatalf("id=%d when=%q", id, when)
	}
	if gotPath != trackerBoardAPI {
		t.Fatalf("path: %s", gotPath)
	}
	if gotSecret != "secret" {
		t.Fatalf("secret: %q", gotSecret)
	}
	if gotWhen != trackerRemoteWhen || sourceID != 2 {
		t.Fatalf("when=%q source=%v", gotWhen, sourceID)
	}
	if !autoPush {
		t.Fatal("admin pipeline always auto-pushes to Railway")
	}
}

func TestDispatchTrackerAgentNilSafe(t *testing.T) {
	var b *Bot
	b.dispatchTrackerAgent(database.TrackerTask{ID: 1}, "doing")
	(&Bot{}).dispatchTrackerAgent(database.TrackerTask{ID: 1}, "review")
	(&Bot{}).kickTrackerPipeline(database.TrackerTask{ID: 1, DevColumn: trackerColReview})
}

func TestKickTrackerPipelineSkipsManualQa(t *testing.T) {
	b := &Bot{config: &config.Config{BoardURL: "https://example.test"}}
	// Без секрета dispatch сразу выйдет из горутины на session; главное —
	// ручное QA не должно запускать Composer-тест.
	b.kickTrackerPipeline(database.TrackerTask{
		ID: 1, DevColumn: trackerColTest, ManualQa: true,
	})
}
