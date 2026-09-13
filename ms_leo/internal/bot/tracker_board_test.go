package bot

import (
	"testing"
	"time"

	"leo-bot/internal/database"
)

func TestPayloadBoolOrAutoPushDefault(t *testing.T) {
	if !payloadBoolOr(nil, "auto_push", true) {
		t.Fatal("nil payload ships")
	}
	if !payloadBoolOr(map[string]any{}, "auto_push", true) {
		t.Fatal("omitted auto_push ships")
	}
	if payloadBoolOr(map[string]any{"auto_push": false}, "auto_push", true) {
		t.Fatal("explicit false must stay off")
	}
	if !payloadBoolOr(map[string]any{"auto_push": true}, "auto_push", false) {
		t.Fatal("explicit true")
	}
}

func TestApplyTrackerColumnFlow(t *testing.T) {
	var task database.TrackerTask
	if err := applyTrackerColumn(&task, "todo"); err != nil {
		t.Fatal(err)
	}
	if task.Status != "pending" || task.DevColumn != "todo" || task.HandedToQa {
		t.Fatalf("todo: %+v", task)
	}
	if err := applyTrackerColumn(&task, "doing"); err != nil {
		t.Fatal(err)
	}
	if task.Status != "running" || !task.HasLastRun {
		t.Fatalf("doing: %+v", task)
	}
	if err := applyTrackerColumn(&task, "test"); err != nil {
		t.Fatal(err)
	}
	if !task.HandedToQa || task.QaColumn != "todo" || task.DevColumn != "test" {
		t.Fatalf("test: %+v", task)
	}
	if err := applyTrackerColumn(&task, "done"); err != nil {
		t.Fatal(err)
	}
	if task.Status != "done" || task.DevColumn != "done" {
		t.Fatalf("done: %+v", task)
	}
	if err := applyTrackerColumn(&task, "nope"); err == nil {
		t.Fatal("unknown column must fail")
	}
}

func TestTrackerStatusMeta(t *testing.T) {
	label, icon, phase := trackerStatusMeta("pending", "todo")
	if label != "Ожидает" || icon != "⏳" || phase != "todo" {
		t.Fatalf("pending: %s %s %s", label, icon, phase)
	}
	label, _, phase = trackerStatusMeta("holding", "test")
	if label != "Тест" || phase != "test" {
		t.Fatalf("test holding: %s %s", label, phase)
	}
	label, _, phase = trackerStatusMeta("holding", "deploy")
	if label != "Сборка" || phase != "deploy" {
		t.Fatalf("deploy: %s %s", label, phase)
	}
}

func TestTrackerQaMeta(t *testing.T) {
	if label, _ := trackerQaMeta("", "todo", false); label != "" {
		t.Fatalf("not handed: %q", label)
	}
	if label, _ := trackerQaMeta("pass", "done", true); label != "Принято" {
		t.Fatalf("pass: %q", label)
	}
	if label, _ := trackerQaMeta("", "doing", true); label != "В тестировании" {
		t.Fatalf("doing: %q", label)
	}
}

func TestTrackerEffectiveDevColumnAwaitingApproval(t *testing.T) {
	task := database.TrackerTask{
		Status:        "pending",
		DevColumn:     trackerColTodo,
		NeedsApproval: true,
		Approvals:     []int64{100},
	}
	if col := trackerEffectiveDevColumn(task); col != trackerColApprove {
		t.Fatalf("todo+needs_approval: got %q", col)
	}
	view := trackerTaskView(task, false)
	if view["dev_column"] != trackerColApprove || view["phase"] != "approve" || view["status_label"] != "Аппрув" {
		t.Fatalf("view awaiting approval: %#v", view)
	}
	done := task
	done.DevColumn = trackerColDone
	done.Status = "done"
	if col := trackerEffectiveDevColumn(done); col != trackerColDone {
		t.Fatalf("done must stay done: %q", col)
	}
}

func TestTrackerTaskViewLeoTaskKind(t *testing.T) {
	// leo_task: на карточке Лео, author_id — админ, выставивший на аппрув.
	view := trackerTaskView(database.TrackerTask{
		ID:            9,
		Num:           2,
		Prompt:        "улучшить профиль",
		Kind:          "leo_task",
		Status:        "pending",
		DevColumn:     trackerColApprove,
		NeedsApproval: true,
		HasAuthor:     true,
		AuthorID:      42,
		Approvals:     []int64{100},
	}, false)
	if view["kind"] != "leo_task" || view["author_id"] != int64(42) {
		t.Fatalf("leo_task view: %#v", view)
	}
	if view["needs_approval"] != true || view["approvals_count"] != 1 {
		t.Fatalf("approval flags: %#v", view)
	}
}

func TestTrackerTaskViewAuthorAndShipShape(t *testing.T) {
	trow := database.TrackerTask{
		ID:         7,
		Num:        3,
		Prompt:     "починить стрик",
		Status:     "holding",
		DevColumn:  "test",
		HandedToQa: true,
		QaColumn:   "todo",
		HasAuthor:  true,
		AuthorID:   database.TrackerLeoAuthorID,
		Steps:      []string{"Поставили", "На тест"},
	}
	view := trackerTaskView(trow, false)
	if view["num"] != 3 || view["author_id"] != database.TrackerLeoAuthorID {
		t.Fatalf("view: %#v", view)
	}
	if view["dev_column"] != "test" || view["handed_to_qa"] != true {
		t.Fatalf("qa flags: %#v", view)
	}
	if view["live_step"] != "На тест" {
		t.Fatalf("live: %#v", view["live_step"])
	}
	if _, ok := view["attachments"]; ok {
		t.Fatal("list view must not embed attachments")
	}
}

func TestTrackerNextColumnMap(t *testing.T) {
	if trackerNextColumn["todo"] != "doing" || trackerNextColumn["deploy"] != "done" {
		t.Fatalf("%#v", trackerNextColumn)
	}
	if _, ok := trackerNextColumn["done"]; ok {
		t.Fatal("done has no next")
	}
}

func TestApplyTrackerQaFlow(t *testing.T) {
	var task database.TrackerTask
	if err := applyTrackerColumn(&task, trackerColReview); err != nil {
		t.Fatal(err)
	}
	if err := applyTrackerQa(&task, "start"); err != nil {
		t.Fatal(err)
	}
	if !task.HandedToQa || task.QaColumn != trackerColDoing || task.DevColumn != trackerColTest {
		t.Fatalf("start: %+v", task)
	}
	if err := applyTrackerQa(&task, "pass"); err != nil {
		t.Fatal(err)
	}
	if task.QaStatus != "pass" || task.DevColumn != trackerColDeploy || task.QaColumn != trackerColDone {
		t.Fatalf("pass: %+v", task)
	}
	if err := applyTrackerQa(&task, "fail"); err != nil {
		t.Fatal(err)
	}
	if task.HandedToQa || task.DevColumn != trackerColDoing || task.QaStatus != "fail" {
		t.Fatalf("fail: %+v", task)
	}
	if err := applyTrackerQa(&task, "reset"); err != nil {
		t.Fatal(err)
	}
	if !task.HandedToQa || task.QaColumn != trackerColTodo || task.DevColumn != trackerColTest {
		t.Fatalf("reset: %+v", task)
	}
	if err := applyTrackerQa(&task, "nope"); err == nil {
		t.Fatal("unknown qa action must fail")
	}
}

// TestAdminBoardFullCycle — админ прогоняет карточку по всей доске:
// поставить → «Обновить» снимает с очереди → в работе → review → тест →
// тестировщик принимает → к публикации → выполнено. Без базы: те же
// правила, что дергают кнопки мини-аппа.
func TestAdminBoardFullCycle(t *testing.T) {
	now := time.Date(2026, 8, 20, 9, 20, 0, 0, time.UTC)
	adminID := int64(42)
	task := database.TrackerTask{
		ID:        11,
		Num:       1,
		Prompt:    "починить кнопку обновить на доске",
		Status:    "pending",
		DevColumn: trackerColTodo,
		WhenAt:    now.Add(-time.Minute),
		WhenLabel: "20.08 09:19",
		HasAuthor: true,
		AuthorID:  adminID,
		Steps:     []string{"Поставлена на доску стаи"},
	}

	if !trackerTaskDueForStart(task, now) {
		t.Fatal("просроченная задача админа должна стартовать по «Обновить»")
	}
	if err := applyTrackerColumn(&task, trackerColDoing); err != nil {
		t.Fatal(err)
	}
	appendTrackerStep(&task, "Взяли в работу по расписанию")
	view := trackerTaskView(task, false)
	if view["status"] != "running" || view["dev_column"] != trackerColDoing || view["phase"] != "doing" {
		t.Fatalf("после обновления: %#v", view)
	}
	if view["author_id"] != adminID || view["active"] != true || view["steps_running"] != true {
		t.Fatalf("карточка админа в работе: %#v", view)
	}
	if view["live_step"] != "Взяли в работу по расписанию" {
		t.Fatalf("live: %#v", view["live_step"])
	}

	for _, col := range []string{trackerColReview, trackerColTest} {
		next, ok := trackerNextColumn[task.DevColumn]
		if !ok || next != col {
			t.Fatalf("next after %s: %s", task.DevColumn, next)
		}
		if err := applyTrackerColumn(&task, col); err != nil {
			t.Fatal(err)
		}
	}
	if task.Status != "holding" || !task.HandedToQa || task.QaColumn != trackerColTodo {
		t.Fatalf("на тесте: %+v", task)
	}
	view = trackerTaskView(task, false)
	if view["phase"] != "test" || view["qa_label"] != "К тестированию" {
		t.Fatalf("вид теста: %#v", view)
	}

	if err := applyTrackerQa(&task, "start"); err != nil {
		t.Fatal(err)
	}
	qaLabel, _ := trackerQaMeta(task.QaStatus, task.QaColumn, task.HandedToQa)
	if task.QaColumn != trackerColDoing || qaLabel != "В тестировании" {
		t.Fatalf("тестировщик взял: %+v label=%q", task, qaLabel)
	}

	if err := applyTrackerQa(&task, "pass"); err != nil {
		t.Fatal(err)
	}
	if task.DevColumn != trackerColDeploy || task.QaStatus != "pass" || task.QaColumn != trackerColDone {
		t.Fatalf("после приёма: %+v", task)
	}
	view = trackerTaskView(task, false)
	if view["phase"] != "deploy" || view["qa_label"] != "Принято" {
		t.Fatalf("вид публикации: %#v", view)
	}

	snap := trackerTaskSnapshot{
		Status:     task.Status,
		DevColumn:  task.DevColumn,
		QaColumn:   task.QaColumn,
		QaStatus:   task.QaStatus,
		HandedToQa: task.HandedToQa,
	}
	if !trackerTaskReadyToShip(snap) {
		t.Fatal("после QA pass задача готова к публикации")
	}
	if err := applyTrackerColumn(&task, trackerColDone); err != nil {
		t.Fatal(err)
	}
	appendTrackerStep(&task, "Отметили к публикации")
	view = trackerTaskView(task, false)
	if view["done"] != true || view["status"] != "done" || view["active"] != false {
		t.Fatalf("выполнено: %#v", view)
	}
	if view["can_delete"] != true {
		t.Fatal("готовую карточку админ может снять")
	}
}

func TestTrackerRequestRefreshIsBoardOp(t *testing.T) {
	b := &Bot{}
	_, listErr := b.trackerRequest("list", 0, nil, 42, "Admin")
	_, refreshErr := b.trackerRequest("refresh", 0, nil, 42, "Admin")
	if listErr == nil || refreshErr == nil {
		t.Fatal("без базы list и refresh должны упасть")
	}
	if listErr.Error() != refreshErr.Error() {
		t.Fatalf("refresh — та же доска, что list: list=%v refresh=%v", listErr, refreshErr)
	}
}

func TestTrackerCanRestart(t *testing.T) {
	done := database.TrackerTask{Status: "done", DevColumn: trackerColDone}
	if !trackerCanRestart(done) {
		t.Fatal("done must restart")
	}
	failed := database.TrackerTask{
		Status:    "running",
		DevColumn: trackerColDoing,
		Error:     "Агент не стартовал",
		Steps:     []string{"агент:#88", "Ошибка"},
	}
	if !trackerCanRestart(failed) {
		t.Fatal("failed agent must restart")
	}
	live := database.TrackerTask{
		Status:     "running",
		DevColumn:  trackerColDoing,
		HasLastRun: true,
		Steps:      []string{"Агент: запустили", "агент:#88"},
	}
	if trackerCanRestart(live) {
		t.Fatal("live remote must not restart")
	}
	review := database.TrackerTask{Status: "reviewing", DevColumn: trackerColReview}
	if !trackerCanRestart(review) {
		t.Fatal("review must restart")
	}
}

func TestTrackerTaskViewCanRestart(t *testing.T) {
	view := trackerTaskView(database.TrackerTask{
		ID: 5, Num: 2, Status: "done", DevColumn: trackerColDone,
	}, false)
	if view["can_restart"] != true {
		t.Fatalf("done: %#v", view["can_restart"])
	}
	view = trackerTaskView(database.TrackerTask{
		ID: 6, Status: "running", DevColumn: trackerColDoing,
		Steps: []string{"Агент: запустили", "агент:#1"},
	}, false)
	if view["can_restart"] != false {
		t.Fatalf("live: %#v", view["can_restart"])
	}
}

func TestPayloadHelpers(t *testing.T) {
	p := map[string]any{"prompt": "  hello ", "leo": true, "sprint": float64(2), "on": "true"}
	if payloadString(p, "prompt") != "hello" {
		t.Fatal(payloadString(p, "prompt"))
	}
	if !payloadBool(p, "leo") || !payloadBool(p, "on") {
		t.Fatal("bool")
	}
	if payloadInt(p, "sprint", 1) != 2 {
		t.Fatal(payloadInt(p, "sprint", 1))
	}
	if payloadInt(nil, "sprint", 4) != 4 {
		t.Fatal("fallback")
	}
}
