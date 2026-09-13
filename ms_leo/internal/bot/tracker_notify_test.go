package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/database"

	"github.com/DATA-DOG/go-sqlmock"
)

func TestTrackerNotifyIsFullyShipped(t *testing.T) {
	if TrackerNotifyIsFullyShipped("🔧 Задача #1 началась.") {
		t.Fatal("start must stay silent")
	}
	if TrackerNotifyIsFullyShipped("Задача #4: код в ветке tracker/4-1.\nАвтопуш: ветка ушла в GitHub. Сборку Railway смотри по деплою этой ветки.") {
		t.Fatal("tracker branch must not look shipped to main")
	}
	if TrackerNotifyIsFullyShipped("можно на тест") || TrackerNotifyIsFullyShipped("ревью не принято") {
		t.Fatal("pipeline steps must stay silent")
	}
	if TrackerNotifyIsFullyShipped("Чтобы выкатить на сервер, напиши «запушь».") {
		t.Fatal("запушь is not done")
	}
	if TrackerNotifyIsFullyShipped("✅ Задача #6 выполнена.\n\nПрошла в работе, review, тест и сборку.\nЧтобы выкатить на сервер, напиши «запушь».") {
		t.Fatal("pipeline summary must stay silent")
	}
	if !TrackerNotifyIsFullyShipped("✅ Задача #4 выполнена.\nВыехала на Railway (ветка main).") {
		t.Fatal("old railway wording must still notify")
	}
	if !TrackerNotifyIsFullyShipped("✅ Задача #9 выполнена.\nВыехала на прод (ветка main).") {
		t.Fatal("prod main ship must notify")
	}
	if !TrackerNotifyIsFullyShipped("Задача #4 задеплоена на Railway в ветке main.") {
		t.Fatal("deployed on main must notify")
	}
	note := trackerFullyDoneNote(database.TrackerTask{Num: 4, Prompt: "кнопка удалить"})
	if !TrackerNotifyIsFullyShipped(note) || !strings.Contains(note, "main") {
		t.Fatalf("done note: %q", note)
	}
	if !strings.Contains(note, "Задача #4: кнопка удалить") {
		t.Fatalf("title in done note: %q", note)
	}
	if strings.Contains(note, "1. Выполнение:") || strings.Contains(note, "Уведомление админам отправлено.") {
		t.Fatalf("done note must not include 5-point brief: %q", note)
	}
}

func TestTrackerNotifyKind(t *testing.T) {
	if got := trackerNotifyKind("⏰ Задача #236 выполнена.\n\nГотово."); got != "done" {
		t.Fatalf("done: %q", got)
	}
	if got := trackerNotifyKind("🔧 Задача #1 началась."); got != "started" {
		t.Fatalf("started: %q", got)
	}
	if got := trackerNotifyKind("Задача отменена админом"); got != "canceled" {
		t.Fatalf("canceled: %q", got)
	}
	if got := trackerNotifyKind("не удалось собрать"); got != "error" {
		t.Fatalf("error: %q", got)
	}
	if got := trackerNotifyKind("⏰ Задача #1 выполнена.\n\nГотово.\n📤 Git push недоступен"); got != "done" {
		t.Fatalf("git push note is still done: %q", got)
	}
	if got := trackerNotifyKind("можно на тест"); got != "done" {
		t.Fatalf("composer review pass: %q", got)
	}
	if got := trackerNotifyKind("ревью не принято: нет тестов"); got != "done" {
		t.Fatalf("composer review fail is still a phase result: %q", got)
	}
	if got := trackerNotifyKind("Задача #4: агент сдал план\n\nTRACKER_NO_CODE"); got != "plan" {
		t.Fatalf("no-code plan: %q", got)
	}
}

func TestTrackerNotifyTaskNum(t *testing.T) {
	if got := trackerNotifyTaskNum("⏰ Задача #236 выполнена."); got != 236 {
		t.Fatalf("num: %d", got)
	}
	if got := trackerNotifyTaskNum("🔧 Задача #1 началась."); got != 1 {
		t.Fatalf("num: %d", got)
	}
	if got := trackerNotifyTaskNum("без номера"); got != 0 {
		t.Fatalf("empty: %d", got)
	}
}

func TestApplyTrackerNotifyRecordsCommit(t *testing.T) {
	task := database.TrackerTask{Status: "running", DevColumn: trackerColDoing}
	applyTrackerNotify(&task, "done", "Задача #5: коммит выполнения abc1234 на ветке tracker/5-314.")
	if task.DevColumn != trackerColReview {
		t.Fatalf("doing → review: %s", task.DevColumn)
	}
	found := false
	for _, s := range task.Steps {
		if s == "коммит abc1234 выполнение" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steps: %#v", task.Steps)
	}
}

func TestApplyBoardNotifyReviewFailStaysOnReview(t *testing.T) {
	task := database.TrackerTask{Status: "reviewing", DevColumn: trackerColReview}
	applyTrackerNotify(&task, "done", "ревью не принято: только план")
	applyTrackerPhaseVerdict(&task, trackerColReview, "ревью не принято: только план")
	if task.DevColumn != trackerColDoing || task.Status != "running" {
		t.Fatalf("review fail must return to work: %+v", task)
	}
	if trackerShouldAdvanceFromResult(database.TrackerTask{
		DevColumn: trackerColDoing,
		Status:    "running",
		Result:    "ревью не принято: только план",
	}) {
		t.Fatal("failed review must not heal back into the pipeline")
	}
	if got := trackerNotifyKind("⚠️ Задача #6: агент не стартовал.\nopenrouter HTTP 504"); got != "error" {
		t.Fatalf("startup fail: %q", got)
	}
}

func TestApplyTrackerNotifyTestCommitGoesToDeploy(t *testing.T) {
	task := database.TrackerTask{Status: "holding", DevColumn: trackerColTest}
	applyTrackerNotify(&task, "done", "Тест пройден.\n\nкоммит abc1234 тест")
	if task.DevColumn != trackerColDeploy {
		t.Fatalf("test → сборка: %s", task.DevColumn)
	}
	found := false
	for _, s := range task.Steps {
		if s == "коммит abc1234 тест" {
			found = true
		}
	}
	if !found {
		t.Fatalf("steps: %#v", task.Steps)
	}
}

func TestApplyTrackerNotifyReviewGoesToTest(t *testing.T) {
	task := database.TrackerTask{Status: "reviewing", DevColumn: trackerColReview}
	applyTrackerNotify(&task, "done", "можно на тест")
	if task.DevColumn != trackerColTest || task.Status != "holding" {
		t.Fatalf("review → test: %+v", task)
	}
}

func TestApplyTrackerNotifyMovesDoingToReview(t *testing.T) {
	task := database.TrackerTask{
		ID:        11,
		Num:       1,
		Status:    "running",
		DevColumn: trackerColDoing,
		Steps:     []string{"Взяли в работу по расписанию"},
	}
	text := "⏰ Задача #1 выполнена.\n\nГотово.\n\n- Подпись теперь только «сгорит через …».\n📤 Git push недоступен"
	applyTrackerNotify(&task, trackerNotifyKind(text), text)
	if task.Status != "reviewing" || task.DevColumn != trackerColReview {
		t.Fatalf("after done: status=%s col=%s", task.Status, task.DevColumn)
	}
	if !strings.Contains(task.Result, "выполнена") {
		t.Fatalf("result not stored: %q", task.Result)
	}
	if task.Error != "" {
		t.Fatalf("git push must not be an error: %q", task.Error)
	}
	if n := len(task.Steps); n == 0 || task.Steps[n-1] != "Агент сдал результат" {
		t.Fatalf("steps: %#v", task.Steps)
	}
}

func TestApplyTrackerNotifyFastTrackGoesToDeploy(t *testing.T) {
	task := database.TrackerTask{Status: "running", DevColumn: trackerColDoing, FastTrack: true}
	applyTrackerNotify(&task, "done", "Готово.")
	if task.DevColumn != trackerColDeploy || task.Status != "holding" {
		t.Fatalf("fast track: %+v", task)
	}
	if !trackerShouldShipAfterNotify(task) {
		t.Fatal("fast track should ship")
	}
}

func TestApplyTrackerNotifyDoesNotPullBackFromTest(t *testing.T) {
	task := database.TrackerTask{Status: "holding", DevColumn: trackerColTest, HandedToQa: true}
	applyTrackerNotify(&task, "done", "Задача #8 выполнена.\n\nтест пройден")
	if task.DevColumn != trackerColDeploy {
		t.Fatalf("composer test pass goes to build: %s", task.DevColumn)
	}
	if trackerShouldShipAfterNotify(task) {
		t.Fatal("ordinary pipeline ships via finishTrackerBuild, not this flag")
	}
	manual := database.TrackerTask{Status: "holding", DevColumn: trackerColTest, HandedToQa: true, ManualQa: true}
	applyTrackerNotify(&manual, "done", "Задача #8 выполнена.")
	if manual.DevColumn != trackerColTest {
		t.Fatalf("manual QA stays on test: %s", manual.DevColumn)
	}
}

func TestApplyTrackerNotifyErrorDoesNotMirrorResult(t *testing.T) {
	task := database.TrackerTask{Status: "running", DevColumn: trackerColDoing}
	text := "⚠️ Задача #66: агент не стартовал.\nнет правок в репозитории: агент сдал только заметку"
	applyTrackerNotify(&task, "error", text)
	if task.Status != "error" || task.Error != text {
		t.Fatalf("error: status=%s err=%q", task.Status, task.Error)
	}
	if task.Result == text {
		t.Fatal("error notify must not copy the same text into result")
	}
}

func TestApplyTrackerNotifyUnknownTextKeepsDoing(t *testing.T) {
	task := database.TrackerTask{Status: "running", DevColumn: trackerColDoing}
	applyTrackerNotify(&task, "", "агент ещё пишет код")
	if task.DevColumn != trackerColDoing || task.Status != "running" {
		t.Fatalf("progress must stay in work: %+v", task)
	}
	if task.Result != "агент ещё пишет код" {
		t.Fatalf("result: %q", task.Result)
	}
}

func TestApplyTrackerNotifyStartedOnlyFromTodo(t *testing.T) {
	todo := database.TrackerTask{Status: "pending", DevColumn: trackerColTodo}
	applyTrackerNotify(&todo, "started", "🔧 Задача #1 началась.")
	if todo.DevColumn != trackerColDoing || todo.Status != "running" {
		t.Fatalf("todo→doing: %+v", todo)
	}
	doing := database.TrackerTask{Status: "running", DevColumn: trackerColDoing}
	applyTrackerNotify(&doing, "started", "🔧 Задача #1 началась.")
	if doing.DevColumn != trackerColDoing {
		t.Fatalf("already doing: %s", doing.DevColumn)
	}
}

func TestApplyTrackerNotifyShippedGoesToDeploy(t *testing.T) {
	note := trackerFullyDoneNote(database.TrackerTask{Num: 6})
	task := database.TrackerTask{Status: "reviewing", DevColumn: trackerColReview, Num: 6}
	applyTrackerNotify(&task, "done", note)
	if task.DevColumn != trackerColDeploy {
		t.Fatalf("текст «на прод» не закрывает карточку, только сборка: %+v", task)
	}
	if task.Status == "done" {
		t.Fatal("notify must not mark done before Railway SUCCESS")
	}
}

func TestTrackerShouldKickAfterNotify(t *testing.T) {
	note := trackerFullyDoneNote(database.TrackerTask{Num: 6})
	if !trackerShouldKickAfterNotify("done", trackerColReview, note) {
		t.Fatal("shipped text must start stand wait, not skip it")
	}
	if !trackerShouldKickAfterNotify("done", trackerColReview, "можно на тест") {
		t.Fatal("composer pass still kicks the next phase")
	}
	if !trackerShouldKickAfterNotify("done", trackerColReview, "ревью не принято") {
		t.Fatal("failed review must kick the doing agent")
	}
	if trackerShouldKickAfterNotify("done", trackerColTest, "тест не прошёл") {
		t.Fatal("failed test stays on test")
	}
	if trackerShouldKickAfterNotify("started", trackerColDoing, "🔧 Задача #6 началась.") {
		t.Fatal("start must not kick")
	}
}

func TestTrackerAlreadyNotifiedShip(t *testing.T) {
	if trackerAlreadyNotifiedShip(database.TrackerTask{Num: 6}) {
		t.Fatal("fresh card")
	}
	if !trackerAlreadyNotifiedShip(database.TrackerTask{
		Steps: []string{trackerShipNotifiedStep},
	}) {
		t.Fatal("step marks notified")
	}
	if trackerAlreadyNotifiedShip(database.TrackerTask{
		Result: trackerFullyDoneNote(database.TrackerTask{Num: 6}),
	}) {
		t.Fatal("result alone is not a send; apply stores it before the DM")
	}
}

func TestTrackerNotifyDoneColumn(t *testing.T) {
	if got := trackerNotifyDoneColumn(database.TrackerTask{DevColumn: trackerColDoing}); got != trackerColReview {
		t.Fatalf("default: %s", got)
	}
	if got := trackerNotifyDoneColumn(database.TrackerTask{DevColumn: trackerColDoing, AutoReview: true}); got != trackerColReview {
		t.Fatalf("auto review still goes through review: %s", got)
	}
	if got := trackerNotifyDoneColumn(database.TrackerTask{DevColumn: trackerColReview}); got != trackerColTest {
		t.Fatalf("review → test: %s", got)
	}
	if got := trackerNotifyDoneColumn(database.TrackerTask{DevColumn: trackerColTest}); got != trackerColDeploy {
		t.Fatalf("test → build: %s", got)
	}
	shipped := database.TrackerTask{DevColumn: trackerColReview, Result: trackerFullyDoneNote(database.TrackerTask{Num: 1})}
	if got := trackerNotifyDoneColumn(shipped); got != trackerColDeploy {
		t.Fatalf("shipped text → deploy, not done: %s", got)
	}
}

func TestTrackerShouldAdvanceFromResult(t *testing.T) {
	waiting := database.TrackerTask{
		Status:    "pending",
		DevColumn: trackerColTodo,
		Result:    "⏰ Задача #236 выполнена.\n\nГотово.\n- Подпись теперь только «сгорит через …».",
	}
	if !trackerShouldAdvanceFromResult(waiting) {
		t.Fatal("waiting + выполнена must leave Ожидает")
	}
	doing := waiting
	doing.Status = "running"
	doing.DevColumn = trackerColDoing
	if !trackerShouldAdvanceFromResult(doing) {
		t.Fatal("doing + выполнена must leave В работе")
	}
	review := waiting
	review.Status = "reviewing"
	review.DevColumn = trackerColReview
	if trackerShouldAdvanceFromResult(review) {
		t.Fatal("already on review must stay")
	}
	shipped := review
	shipped.Result = trackerFullyDoneNote(database.TrackerTask{Num: 236})
	if !trackerShouldAdvanceFromResult(shipped) {
		t.Fatal("railway ship from review must go to deploy, not stay")
	}
	empty := database.TrackerTask{Status: "pending", DevColumn: trackerColTodo}
	if trackerShouldAdvanceFromResult(empty) {
		t.Fatal("no result must not move")
	}
}

func TestApplyBoardNotifyFindsOpenTaskWhenForeignID(t *testing.T) {
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatal(err)
	}
	defer sqlDB.Close()
	now := time.Date(2026, 8, 20, 9, 49, 0, 0, time.UTC)
	b := &Bot{db: database.NewForTest(sqlDB)}

	mock.ExpectQuery(`WHERE t.id = \$1`).
		WithArgs(int64(236)).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`WHERE t.num = \$1`).
		WithArgs(236).
		WillReturnError(sql.ErrNoRows)
	mock.ExpectQuery(`t.last_run_at DESC`).
		WillReturnRows(sqlmock.NewRows(trackerListColumns()).AddRow(
			int64(11), 1, "починить статус на доске", now, "20.08 09:19", "разово", "task",
			"running", "doing", nil, nil, false,
			false, false, false, true,
			false, []byte("[]"),
			"", "", []byte(`["Взяли в работу по расписанию"]`), int64(42),
			now, now, now, 0,
		))
	mock.ExpectExec(`UPDATE pack_tracker_tasks SET`).
		WillReturnResult(sqlmock.NewResult(0, 1))

	id, ship, err := b.ApplyBoardNotify(236, "⏰ Задача #236 выполнена.\n\nГотово.")
	if err != nil {
		t.Fatal(err)
	}
	if id != 11 || ship {
		t.Fatalf("id=%d ship=%v", id, ship)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatal(err)
	}
}
