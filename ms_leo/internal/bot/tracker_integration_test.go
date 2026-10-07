package bot

import (
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
)

const (
	trAdminA = int64(556001)
	trAdminB = int64(556002)
	trAdminC = int64(556003)
)

// fakeTracker — ms_tracker для тестов: принимает задачи в очередь и отвечает
// на «спросить Claude» тем, что задано в ask.
type fakeTracker struct {
	mu       sync.Mutex
	asked    []string // вопросы к Claude
	queued   int      // сколько задач ушло исполнителю
	ask      string   // ответ Claude; пусто — ошибка
	askFails bool
}

func newFakeTracker(t *testing.T) (*fakeTracker, func(*config.Config)) {
	t.Helper()
	f := &fakeTracker{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		defer f.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/api/ask":
			var body struct{ Prompt string }
			_ = json.Unmarshal(raw, &body)
			f.asked = append(f.asked, body.Prompt)
			if f.askFails || f.ask == "" {
				w.WriteHeader(http.StatusBadGateway)
				_, _ = w.Write([]byte(`{"ok":false,"error":"лимит подписки"}`))
				return
			}
			out, _ := json.Marshal(map[string]any{"ok": true, "text": f.ask})
			_, _ = w.Write(out)
		case r.URL.Path == "/api/scheduled" && r.Method == http.MethodPost:
			f.queued++
			_, _ = w.Write([]byte(`{"ok":true,"id":1}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"tasks":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return f, func(c *config.Config) {
		c.BoardURL = srv.URL
		c.BoardSecret = "board-secret"
		c.AdminIDs = []int64{trAdminA, trAdminB, trAdminC}
	}
}

func (f *fakeTracker) askCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.asked)
}

func boardCall(t *testing.T, b *Bot, op string, taskID int64, payload map[string]any, userID int64) map[string]any {
	t.Helper()
	raw, err := b.trackerRequest(op, taskID, payload, userID, "admin")
	if err != nil {
		t.Fatalf("доска %s: %v", op, err)
	}
	out := map[string]any{}
	_ = json.Unmarshal(raw, &out)
	return out
}

// createApprovalCard ставит карточку, которой нужны два аппрува, от имени author.
func createApprovalCard(t *testing.T, b *Bot, author int64, prompt string) database.TrackerTask {
	t.Helper()
	out := boardCall(t, b, "create", 0, map[string]any{"prompt": prompt, "when": "сейчас", "needs_approval": true}, author)
	id, _ := out["id"].(float64)
	task, err := b.db.GetTrackerTask(int64(id))
	if err != nil {
		t.Fatalf("карточка не создалась: %v (%v)", err, out)
	}
	return task
}

func reloadTask(t *testing.T, b *Bot, id int64) database.TrackerTask {
	t.Helper()
	task, err := b.db.GetTrackerTask(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// ageApproval делает вид, что карточка ждёт аппрувов уже age.
func ageApproval(t *testing.T, db *sql.DB, id int64, age time.Duration) {
	t.Helper()
	if _, err := db.Exec(`UPDATE pack_tracker_tasks SET approval_notified_at = NOW() - make_interval(secs => $2), created_at = NOW() - make_interval(secs => $2) WHERE id = $1`, id, age.Seconds()); err != nil {
		t.Fatal(err)
	}
}

// Карточка с аппрувами: уведомления админам, автор не голосует за себя,
// два голоса отправляют задачу в работу, на доске видно, кто одобрил.
func TestTrackerApprovalLifecycle(t *testing.T) {
	tracker, tune := newFakeTracker(t)
	b, tg, _ := newIntegrationBot(t, tune)

	task := createApprovalCard(t, b, trAdminA, "Добавить кнопку «Поделиться» в профиль")
	if task.DevColumn != trackerColApprove || !task.NeedsApproval || len(task.Approvals) != 0 {
		t.Fatalf("новая карточка ждёт аппрувов: %+v", task)
	}
	// Уведомление — всем админам, кроме автора.
	for id, want := range map[int64]int{trAdminA: 0, trAdminB: 1, trAdminC: 1, itAdmin: 1} {
		if got := len(tg.textsTo(id)); got != want {
			t.Errorf("уведомлений админу %d: %d, ждали %d", id, got, want)
		}
	}

	if _, err := b.trackerRequest("approve", task.ID, map[string]any{"action": "approve"}, trAdminA, "a"); err == nil || !strings.Contains(err.Error(), "автор не может") {
		t.Errorf("автор не одобряет свою задачу: %v", err)
	}
	view := boardCall(t, b, "approve", task.ID, map[string]any{"action": "approve"}, trAdminB)["task"].(map[string]any)
	if view["approvals_count"] != float64(1) || view["dev_column"] != trackerColApprove {
		t.Fatalf("после первого голоса: %v", view)
	}
	if ids, _ := view["approver_ids"].([]any); len(ids) != 1 || ids[0] != float64(trAdminB) {
		t.Fatalf("на карточке видно, кто одобрил: %v", view["approver_ids"])
	}
	boardCall(t, b, "approve", task.ID, map[string]any{"action": "approve"}, trAdminB) // повтор не считается
	if got := reloadTask(t, b, task.ID); len(got.Approvals) != 1 {
		t.Fatalf("повторный голос того же админа: %+v", got.Approvals)
	}

	boardCall(t, b, "approve", task.ID, map[string]any{"action": "approve"}, trAdminC)
	got := reloadTask(t, b, task.ID)
	if len(got.Approvals) != 2 || got.DevColumn != trackerColDoing {
		t.Fatalf("два голоса — в работу: колонка %s, голоса %v", got.DevColumn, got.Approvals)
	}
	if !trackerHasStep(got, "Два аппрува — в работу") {
		t.Errorf("шаги карточки: %v", got.Steps)
	}
	_ = tracker
}

// Отказ админа отменяет карточку и сообщает автору причину.
func TestTrackerApprovalRejectCancelsCard(t *testing.T) {
	_, tune := newFakeTracker(t)
	b, tg, _ := newIntegrationBot(t, tune)
	task := createApprovalCard(t, b, trAdminA, "Переписать всё на Rust")
	tg.reset()

	boardCall(t, b, "approve", task.ID, map[string]any{"action": "reject", "comment": "слишком дорого"}, trAdminB)

	got := reloadTask(t, b, task.ID)
	if got.DevColumn != trackerColCanceled {
		t.Fatalf("после отказа карточка отменена: %s", got.DevColumn)
	}
	texts := tg.textsTo(trAdminA)
	if len(texts) != 1 || !strings.Contains(texts[0], "отменена") || !strings.Contains(texts[0], "слишком дорого") {
		t.Errorf("автору — причина отказа: %q", texts)
	}
	if _, err := b.trackerRequest("approve", task.ID, map[string]any{"action": "approve"}, trAdminC, "c"); err == nil {
		t.Error("отменённую карточку одобрить нельзя")
	}
}

// Голос Лео: карточка с одним голосом, висящая больше суток, уходит на оценку
// в Claude. «За» — второй аппрув и задача в работе; «против» — причина в
// истории; Claude недоступен — Лео не голосует и не дёргает его каждый проход.
func TestTrackerLeoVotesAfterADay(t *testing.T) {
	tracker, tune := newFakeTracker(t)
	b, _, db := newIntegrationBot(t, tune)
	now := time.Now()

	approved := createApprovalCard(t, b, trAdminA, "Показывать стрик друга в списке друзей")
	boardCall(t, b, "approve", approved.ID, map[string]any{"action": "approve"}, trAdminB)

	// Меньше суток — Лео молчит.
	tracker.ask = `{"approve": true, "reason": "Понятная и полезная задача"}`
	b.runTrackerApprovalAgingSweep(now)
	if tracker.askCount() != 0 || len(reloadTask(t, b, approved.ID).Approvals) != 1 {
		t.Fatal("раньше суток Лео не голосует")
	}

	ageApproval(t, db, approved.ID, 25*time.Hour)
	b.runTrackerApprovalAgingSweep(now)
	got := reloadTask(t, b, approved.ID)
	if len(got.Approvals) != 2 || got.Approvals[1] != database.TrackerLeoAuthorID || got.DevColumn != trackerColDoing {
		t.Fatalf("голос «за»: голоса %v, колонка %s", got.Approvals, got.DevColumn)
	}
	if !strings.Contains(strings.Join(got.Steps, "\n"), "Аппрув от Лео: Понятная и полезная задача") {
		t.Errorf("причина голоса в истории: %v", got.Steps)
	}
	if tracker.askCount() != 1 || !strings.Contains(tracker.asked[0], "Показывать стрик друга") {
		t.Errorf("Claude получил текст задачи: %v", tracker.asked)
	}
	if ids := trackerTaskView(got, false)["approver_ids"].([]int64); len(ids) != 2 || ids[1] != database.TrackerLeoAuthorID {
		t.Errorf("Лео среди одобривших на доске: %v", ids)
	}

	// Голос «против».
	rejected := createApprovalCard(t, b, trAdminA, "Сделать красиво")
	boardCall(t, b, "approve", rejected.ID, map[string]any{"action": "approve"}, trAdminB)
	ageApproval(t, db, rejected.ID, 30*time.Hour)
	tracker.ask = `{"approve": false, "reason": "Формулировка расплывчата"}`
	b.runTrackerApprovalAgingSweep(now)
	b.runTrackerApprovalAgingSweep(now)
	got = reloadTask(t, b, rejected.ID)
	if len(got.Approvals) != 1 || got.DevColumn != trackerColApprove {
		t.Fatalf("голос «против» карточку не двигает: %v %s", got.Approvals, got.DevColumn)
	}
	if !trackerHasStep(got, "Лео не поддержал: Формулировка расплывчата") {
		t.Errorf("причина отказа в истории: %v", got.Steps)
	}
	if tracker.askCount() != 2 {
		t.Errorf("в одном круге Лео спрашивает Claude один раз, вопросов всего: %d", tracker.askCount())
	}

	// После правки описания — новый круг, Лео голосует снова.
	boardCall(t, b, "prompt", rejected.ID, map[string]any{"prompt": "В профиле выровнять аватар по центру"}, trAdminA)
	if got = reloadTask(t, b, rejected.ID); len(got.Approvals) != 0 {
		t.Fatalf("правка описания сбрасывает голоса: %v", got.Approvals)
	}
	boardCall(t, b, "approve", rejected.ID, map[string]any{"action": "approve"}, trAdminC)
	ageApproval(t, db, rejected.ID, 26*time.Hour)
	tracker.ask = `{"approve": true, "reason": "Теперь понятно"}`
	b.runTrackerApprovalAgingSweep(now)
	if got = reloadTask(t, b, rejected.ID); len(got.Approvals) != 2 {
		t.Fatalf("в новом круге Лео голосует снова: %v, шаги %v", got.Approvals, got.Steps)
	}

	// Claude недоступен — голоса нет, повторный проход его не дёргает.
	down := createApprovalCard(t, b, trAdminA, "Добавить тёмную тему в ленту")
	boardCall(t, b, "approve", down.ID, map[string]any{"action": "approve"}, trAdminB)
	ageApproval(t, db, down.ID, 25*time.Hour)
	tracker.askFails = true
	before := tracker.askCount()
	b.runTrackerApprovalAgingSweep(now)
	b.runTrackerApprovalAgingSweep(now.Add(5 * time.Minute))
	if got = reloadTask(t, b, down.ID); len(got.Approvals) != 1 || got.DevColumn != trackerColApprove {
		t.Fatalf("без Claude Лео не голосует: %v", got.Approvals)
	}
	if tracker.askCount()-before != 1 {
		t.Errorf("при недоступном Claude — одна попытка в час, было: %d", tracker.askCount()-before)
	}
	// Через час пробует снова.
	tracker.askFails = false
	b.runTrackerApprovalAgingSweep(now.Add(61 * time.Minute))
	if got = reloadTask(t, b, down.ID); len(got.Approvals) != 2 {
		t.Errorf("через час Лео проголосовал: %v", got.Approvals)
	}
}

// Свои задачи Лео не одобряет; непонятный ответ Claude голосом не считается.
func TestTrackerLeoVoteLimits(t *testing.T) {
	tracker, tune := newFakeTracker(t)
	b, _, db := newIntegrationBot(t, tune)
	tracker.ask = `{"approve": true, "reason": "да"}`

	out := boardCall(t, b, "create", 0, map[string]any{"prompt": "Идея Лео: недельные дуэли", "when": "сейчас", "needs_approval": true, "leo": true}, trAdminA)
	own := reloadTask(t, b, int64(out["id"].(float64)))
	boardCall(t, b, "approve", own.ID, map[string]any{"action": "approve"}, trAdminB)
	ageApproval(t, db, own.ID, 48*time.Hour)
	b.runTrackerApprovalAgingSweep(time.Now())
	if tracker.askCount() != 0 || len(reloadTask(t, b, own.ID).Approvals) != 1 {
		t.Fatal("за свою задачу Лео не голосует")
	}

	garbled := createApprovalCard(t, b, trAdminA, "Починить поиск в чате")
	boardCall(t, b, "approve", garbled.ID, map[string]any{"action": "approve"}, trAdminB)
	ageApproval(t, db, garbled.ID, 48*time.Hour)
	tracker.ask = "Думаю, стоит взять."
	b.runTrackerApprovalAgingSweep(time.Now())
	got := reloadTask(t, b, garbled.ID)
	if len(got.Approvals) != 1 || trackerHasStep(got, trackerLeoVoteStep(1)) {
		t.Fatalf("ответ не по формату — не голос: %v %v", got.Approvals, got.Steps)
	}
}

// Карточка без единого голоса пять дней отменяется сама, автор получает
// уведомление; карточка с голосом — остаётся.
func TestTrackerApprovalExpiresAfterFiveDays(t *testing.T) {
	_, tune := newFakeTracker(t)
	b, tg, db := newIntegrationBot(t, tune)

	stale := createApprovalCard(t, b, trAdminA, "Старая идея")
	fresh := createApprovalCard(t, b, trAdminA, "Свежая идея")
	voted := createApprovalCard(t, b, trAdminA, "Идея с голосом")
	boardCall(t, b, "approve", voted.ID, map[string]any{"action": "approve"}, trAdminB)
	ageApproval(t, db, stale.ID, 6*24*time.Hour)
	ageApproval(t, db, fresh.ID, 4*24*time.Hour)
	ageApproval(t, db, voted.ID, 20*time.Hour)
	tg.reset()

	b.runTrackerApprovalAgingSweep(time.Now())
	b.runTrackerApprovalAgingSweep(time.Now())

	if got := reloadTask(t, b, stale.ID); got.DevColumn != trackerColCanceled || !trackerHasStep(got, trackerApprovalExpiredStep) {
		t.Fatalf("пять дней без голосов — отмена: %s %v", got.DevColumn, got.Steps)
	}
	if got := reloadTask(t, b, fresh.ID); got.DevColumn != trackerColApprove {
		t.Errorf("четыре дня — ещё ждём: %s", got.DevColumn)
	}
	if got := reloadTask(t, b, voted.ID); got.DevColumn != trackerColApprove {
		t.Errorf("карточку с голосом не отменяем: %s", got.DevColumn)
	}
	texts := tg.textsTo(trAdminA)
	if len(texts) != 1 || !strings.Contains(texts[0], "отменена автоматически") {
		t.Errorf("автору — одно уведомление об отмене: %q", texts)
	}
}

// Напоминание об аппруве уходит через час только тем, кто ещё не голосовал.
func TestTrackerApprovalReminder(t *testing.T) {
	_, tune := newFakeTracker(t)
	b, tg, db := newIntegrationBot(t, tune)
	task := createApprovalCard(t, b, trAdminA, "Напомнить про аппрув")
	boardCall(t, b, "approve", task.ID, map[string]any{"action": "approve"}, trAdminB)
	tg.reset()

	b.runTrackerApprovalReminderSweep()
	if len(tg.sent("sendMessage")) != 0 {
		t.Fatal("раньше часа напоминаний нет")
	}
	if _, err := db.Exec(`UPDATE pack_tracker_tasks SET approval_notified_at = NOW() - INTERVAL '2 hours' WHERE id = $1`, task.ID); err != nil {
		t.Fatal(err)
	}
	b.runTrackerApprovalReminderSweep()
	b.runTrackerApprovalReminderSweep()
	for id, want := range map[int64]int{trAdminA: 0, trAdminB: 0, trAdminC: 1, itAdmin: 1} {
		texts := tg.textsTo(id)
		if len(texts) != want {
			t.Errorf("напоминаний админу %d: %d, ждали %d", id, len(texts), want)
		}
		if want == 1 && !strings.Contains(texts[0], "Напоминание") {
			t.Errorf("текст напоминания: %q", texts[0])
		}
	}
}
