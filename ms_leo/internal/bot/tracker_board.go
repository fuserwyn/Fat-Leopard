package bot

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/database"
)

// Своя доска: те же карточки, что рисует TrackerScreen, но данные из нашей базы.
// Коммиты на ветке задачи: выполнение до review, ревью до теста.
// Пуш в main и колонка «Сборка» — только после теста. Выполнено — когда
// стенд собрался.

const (
	trackerColTodo     = "todo"
	trackerColApprove  = "approve"
	trackerColDoing    = "doing"
	trackerColReview   = "review"
	trackerColTest     = "test"
	trackerColDeploy   = "deploy"
	trackerColDone     = "done"
	trackerColCanceled = "canceled"
)

const trackerApprovalRequired = 2

var trackerNextColumn = map[string]string{
	trackerColTodo:    trackerColDoing,
	trackerColApprove: trackerColDoing,
	trackerColDoing:   trackerColReview,
	trackerColReview:  trackerColTest,
	trackerColTest:    trackerColDeploy,
	trackerColDeploy:  trackerColDone,
}

func applyTrackerColumn(t *database.TrackerTask, col string) error {
	col = strings.ToLower(strings.TrimSpace(col))
	switch col {
	case trackerColTodo:
		t.Status = "pending"
		t.DevColumn = trackerColTodo
		t.HandedToQa = false
		t.QaColumn = ""
		t.QaStatus = ""
		t.Error = ""
	case trackerColApprove:
		t.Status = "pending"
		t.DevColumn = trackerColApprove
		t.HandedToQa = false
		t.QaColumn = ""
		t.QaStatus = ""
		t.Error = ""
	case trackerColDoing:
		t.Status = "running"
		t.DevColumn = trackerColDoing
		t.HasLastRun = true
		t.LastRunAt = time.Now()
	case trackerColReview:
		t.Status = "reviewing"
		t.DevColumn = trackerColReview
	case trackerColTest:
		t.Status = "holding"
		t.DevColumn = trackerColTest
		t.HandedToQa = true
		if t.QaColumn == "" || t.QaColumn == "done" {
			t.QaColumn = trackerColTodo
		}
		if t.QaStatus == "pass" {
			t.QaStatus = ""
		}
	case trackerColDeploy:
		t.Status = "holding"
		t.DevColumn = trackerColDeploy
	case trackerColDone:
		t.Status = "done"
		t.DevColumn = trackerColDone
		t.Error = ""
	case trackerColCanceled:
		t.Status = "canceled"
		t.DevColumn = trackerColCanceled
	default:
		return fmt.Errorf("нет такой колонки")
	}
	return nil
}

// trackerAwaitingApproval — задача ждёт ещё аппрувы, пока не набрано нужное число.
func trackerAwaitingApproval(t database.TrackerTask) bool {
	if !t.NeedsApproval || len(t.Approvals) >= trackerApprovalRequired {
		return false
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col == trackerColDone || col == trackerColCanceled {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	return status != "done" && status != "canceled" && status != "cancelled"
}

// trackerEffectiveDevColumn — колонка на доске: без аппрува карточка в «Аппрув»,
// даже если в базе ещё todo (старые записи или ручная правка).
func trackerEffectiveDevColumn(t database.TrackerTask) string {
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col == "" {
		col = trackerColTodo
	}
	if !trackerAwaitingApproval(t) {
		return col
	}
	switch col {
	case trackerColDoing, trackerColReview, trackerColTest, trackerColDeploy:
		return col
	default:
		return trackerColApprove
	}
}

func appendTrackerStep(t *database.TrackerTask, step string) {
	step = strings.TrimSpace(step)
	if step == "" {
		return
	}
	t.Steps = append(t.Steps, step)
	if len(t.Steps) > 80 {
		t.Steps = t.Steps[len(t.Steps)-80:]
	}
}

func trackerStatusMeta(status, col string) (label, icon, phase string) {
	col = strings.ToLower(strings.TrimSpace(col))
	if col == trackerColApprove {
		return "Аппрув", "👍", "approve"
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "running":
		return "В работе", "🔧", "doing"
	case "reviewing":
		return "Review", "👀", "review"
	case "holding":
		if col == trackerColTest {
			return "Тест", "🧪", "test"
		}
		return "Сборка", "🚀", "deploy"
	case "done", "completed":
		return "Выполнено", "✅", "done"
	case "canceled", "cancelled":
		return "Отменено", "⛔", "canceled"
	case "error":
		return "Ошибка", "⚠️", "todo"
	default:
		return "Ожидает", "⏳", "todo"
	}
}

// trackerCanRestart — админ может снова пустить агента: завершённые, с ошибкой,
// зависшие без живого remote job, или ещё не стартовавшие в очереди.
func trackerCanRestart(t database.TrackerTask) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if status == "done" || col == trackerColDone {
		return true
	}
	if status == "canceled" || col == trackerColCanceled {
		return true
	}
	if status == "error" || strings.TrimSpace(t.Error) != "" {
		return true
	}
	if trackerAgentStartFailed(t) {
		return true
	}
	if status == "running" || status == "reviewing" {
		if trackerStepRemoteID(t.Steps) > 0 && !trackerAgentStartFailed(t) {
			return false
		}
		return true
	}
	if status == "holding" {
		return true
	}
	if col == trackerColReview || col == trackerColTest || col == trackerColDeploy {
		return true
	}
	if (status == "pending" || status == "scheduled") &&
		(col == trackerColTodo || col == trackerColApprove || col == "") {
		return true
	}
	return false
}

// trackerCanEditPrompt — формулировку можно править, пока агент не взял задачу.
func trackerCanEditPrompt(t database.TrackerTask) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if status == "done" || col == trackerColDone {
		return false
	}
	if status == "canceled" || col == trackerColCanceled {
		return false
	}
	if status == "running" || status == "reviewing" || status == "holding" {
		return false
	}
	switch col {
	case trackerColDoing, trackerColReview, trackerColTest, trackerColDeploy, trackerColDone, trackerColCanceled:
		return false
	case trackerColTodo, trackerColApprove, "":
		return true
	default:
		return false
	}
}

func trackerQaMeta(status, col string, handed bool) (label, icon string) {
	if !handed {
		return "", ""
	}
	switch {
	case status == "pass" || col == trackerColDone:
		return "Принято", "✅"
	case status == "fail":
		return "Вернули", "↩️"
	case col == trackerColDoing:
		return "В тестировании", "🧪"
	default:
		return "К тестированию", "🧪"
	}
}

func trackerTaskView(t database.TrackerTask, withAtts bool) map[string]any {
	devColumn := trackerEffectiveDevColumn(t)
	label, icon, phase := trackerStatusMeta(t.Status, devColumn)
	qaLabel, qaIcon := trackerQaMeta(t.QaStatus, t.QaColumn, t.HandedToQa)
	done := t.Status == "done" || t.DevColumn == trackerColDone
	canceled := t.Status == "canceled" || t.DevColumn == trackerColCanceled
	active := !done && !canceled
	canDelete := canceled || done || t.DevColumn == trackerColTodo || t.DevColumn == trackerColApprove || t.Status == "pending" || t.Status == "error"
	canRestart := trackerCanRestart(t)
	canEditPrompt := trackerCanEditPrompt(t)
	when := strings.TrimSpace(t.WhenLabel)
	if when == "" {
		when = formatTrackerWhen(t.WhenAt)
	}
	var author any
	if t.HasAuthor {
		author = t.AuthorID
	} else {
		author = nil
	}
	var qaCol any
	if t.QaColumn != "" {
		qaCol = t.QaColumn
	} else if t.HandedToQa {
		qaCol = trackerColTodo
	} else {
		qaCol = nil
	}
	var qaStatus any
	if t.QaStatus != "" {
		qaStatus = t.QaStatus
	} else {
		qaStatus = nil
	}
	live := ""
	if n := len(t.Steps); n > 0 {
		live = t.Steps[n-1]
	}
	doneSummary := ""
	if done {
		doneSummary = trackerDoneBrief(t)
	}
	out := map[string]any{
		"id":                t.ID,
		"num":               t.Num,
		"prompt":            t.Prompt,
		"repo":              "",
		"when":              when,
		"repeat":            t.Repeat,
		"kind":              t.Kind,
		"status":            t.Status,
		"status_label":      label,
		"status_icon":       icon,
		"done":              done,
		"active":            active,
		"can_delete":        canDelete,
		"can_restart":       canRestart,
		"can_edit_prompt":   canEditPrompt,
		"auto_review":       t.AutoReview,
		"manual_qa":         t.ManualQa,
		"fast_track":        t.FastTrack,
		"error":             t.Error,
		"has_result":        strings.TrimSpace(t.Result) != "",
		"phase":             phase,
		"qa_status":         qaStatus,
		"qa_label":          qaLabel,
		"qa_icon":           qaIcon,
		"auto_qa_running":   false,
		"dev_column":        devColumn,
		"qa_column":         qaCol,
		"handed_to_qa":      t.HandedToQa,
		"attachments_count": t.AttachmentsCount,
		"has_attachments":   t.AttachmentsCount > 0,
		"auto_push":         t.AutoPush,
		"needs_approval":    t.NeedsApproval,
		"approvals_count":   len(t.Approvals),
		"approvals_needed":  trackerApprovalRequired,
		"author_id":         author,
		"steps":             t.Steps,
		"steps_running": t.Status == "running" || t.Status == "reviewing" ||
			(t.Status == "holding" && (t.DevColumn == trackerColTest && !t.ManualQa || t.DevColumn == trackerColDeploy)),
		"model_key":  "",
		"live_step":  live,
		"result":       t.Result,
		"done_summary": doneSummary,
		"created_at":   t.CreatedAt.Format(time.RFC3339),
		"commit":     trackerTaskCommit(t),
		"branch":     trackerTaskBranch(t),
	}
	if t.HasLastRun {
		out["last_run_at"] = t.LastRunAt.Format(time.RFC3339)
	}
	if withAtts {
		atts := make([]map[string]any, 0, len(t.Attachments))
		for _, a := range t.Attachments {
			atts = append(atts, map[string]any{
				"id":   a.ID,
				"name": a.Name,
				"mime": a.Mime,
				"size": a.Size,
				"url":  "",
			})
		}
		out["attachments"] = atts
	}
	return out
}

func trackerJSON(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	return raw, nil
}

func payloadString(p map[string]any, key string) string {
	if p == nil {
		return ""
	}
	switch v := p[key].(type) {
	case string:
		return strings.TrimSpace(v)
	case fmt.Stringer:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

func payloadBool(p map[string]any, key string) bool {
	if p == nil {
		return false
	}
	switch v := p[key].(type) {
	case bool:
		return v
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	default:
		return false
	}
}

// payloadBoolOr — как payloadBool, но без ключа берём запасное значение.
// Админские задачи без явного auto_push катим на Railway сами.
func payloadBoolOr(p map[string]any, key string, fallback bool) bool {
	if p == nil {
		return fallback
	}
	if _, ok := p[key]; !ok {
		return fallback
	}
	return payloadBool(p, key)
}

func (b *Bot) localTrackerList() (json.RawMessage, error) {
	// Тихий опрос доски тоже снимает созревшие: иначе «Обновить» и автообновление
	// показывали бы одну и ту же карточку в «Ожидает» после срока.
	started, _ := b.claimAndNotifyDueTrackerTasks()
	return b.localTrackerBoard(started)
}

// localTrackerRefresh — кнопка «Обновить»: если созревшие не снялись,
// админ видит ошибку, а не ту же очередь.
func (b *Bot) localTrackerRefresh() (json.RawMessage, error) {
	started, err := b.claimAndKickTrackerTasks(true)
	if err != nil {
		return nil, err
	}
	return b.localTrackerBoard(started)
}

func (b *Bot) localTrackerBoard(started int) (json.RawMessage, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, 0, len(list))
	for _, t := range list {
		tasks = append(tasks, trackerTaskView(t, false))
	}
	return trackerJSON(map[string]any{"tasks": tasks, "repo": nil, "started": started})
}

func (b *Bot) localTrackerTask(taskID int64, payload map[string]any) (json.RawMessage, error) {
	id := trackerPayloadTaskID(taskID, payload)
	t, err := b.db.GetTrackerTask(id)
	if err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"task": trackerTaskView(t, true)})
}

func (b *Bot) localTrackerCreate(payload map[string]any, userID int64) (json.RawMessage, error) {
	prompt := payloadString(payload, "prompt")
	at, label, err := parseTrackerWhen(payloadString(payload, "when"))
	if err != nil {
		return nil, err
	}
	isLeo := payloadBool(payload, "leo")
	needsApproval := payloadBool(payload, "needs_approval")
	t := database.TrackerTask{
		Prompt:     prompt,
		WhenAt:     at,
		WhenLabel:  label,
		Repeat:     "разово",
		Kind:       "task",
		Status:     "pending",
		DevColumn:  trackerColTodo,
		AutoReview: true,
		ManualQa:   payloadBool(payload, "manual_qa"),
		FastTrack:  payloadBool(payload, "fast_track"),
		AutoPush:   payloadBoolOr(payload, "auto_push", true),
		Steps:      []string{"Поставлена на доску стаи"},
	}
	if needsApproval {
		t.NeedsApproval = true
		t.DevColumn = trackerColApprove
		if isLeo {
			t.Steps = []string{"Задача от Лео — ждёт аппрува всех админов"}
			if userID != 0 {
				t.Steps = append(t.Steps, fmt.Sprintf("На доску вынес админ %d", userID))
			}
		} else {
			t.Steps = []string{"Ждёт аппрува других админов"}
		}
	}
	if _, ok := payload["auto_review"]; ok {
		t.AutoReview = payloadBool(payload, "auto_review")
	}
	if isLeo {
		// На карточке — Лео; аппрув могут ставить все админы, в том числе вынесший на доску.
		t.AuthorID = database.TrackerLeoAuthorID
		t.HasAuthor = true
		if needsApproval {
			t.Kind = "leo_task"
		}
	} else if userID != 0 {
		t.AuthorID = userID
		t.HasAuthor = true
	}
	created, err := b.db.CreateTrackerTask(t)
	if err != nil {
		return nil, err
	}
	if created.NeedsApproval {
		b.notifyTrackerApprovalsNeeded(created)
		return trackerJSON(map[string]any{"id": created.ID, "when": created.WhenLabel})
	}
	// Срок «сейчас» — забираем в этом же запросе, не в горутине: иначе
	// следующая отрисовка доски ещё покажет карточку в «Ожидает».
	if trackerTaskDueForStart(created, time.Now()) {
		_, _ = b.claimAndNotifyDueTrackerTasks()
	}
	return trackerJSON(map[string]any{"id": created.ID, "when": created.WhenLabel})
}

func (b *Bot) localTrackerLoad(taskID int64, payload map[string]any) (database.TrackerTask, error) {
	id := trackerPayloadTaskID(taskID, payload)
	return b.db.GetTrackerTask(id)
}

func (b *Bot) localTrackerCancel(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if err := applyTrackerColumn(&t, trackerColCanceled); err != nil {
		return nil, err
	}
	appendTrackerStep(&t, "Отменена")
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"ok": true})
}

func (b *Bot) localTrackerDelete(taskID int64, payload map[string]any) (json.RawMessage, error) {
	id := trackerPayloadTaskID(taskID, payload)
	if err := b.db.DeleteTrackerTask(id); err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"ok": true})
}

// trackerTaskClearable — выполненные, отменённые и упавшие карточки можно
// снять с доски пакетом, не трогая очередь и живую работу агента.
func trackerTaskClearable(t database.TrackerTask) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if status == "done" || status == "completed" || col == trackerColDone {
		return true
	}
	if status == "canceled" || status == "cancelled" || col == trackerColCanceled {
		return true
	}
	if status == "error" {
		return true
	}
	if strings.TrimSpace(t.Error) == "" {
		return false
	}
	if status == "running" || status == "reviewing" {
		if trackerStepRemoteID(t.Steps) > 0 && !trackerAgentStartFailed(t) {
			return false
		}
	}
	return true
}

func (b *Bot) localTrackerClearFinished() (json.RawMessage, error) {
	if b == nil || b.db == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return nil, err
	}
	deleted := 0
	for _, t := range list {
		if !trackerTaskClearable(t) {
			continue
		}
		if err := b.db.DeleteTrackerTask(t.ID); err != nil {
			return nil, err
		}
		deleted++
	}
	fresh, err := b.db.ListTrackerTasks()
	if err != nil {
		return nil, err
	}
	tasks := make([]map[string]any, 0, len(fresh))
	for _, t := range fresh {
		tasks = append(tasks, trackerTaskView(t, false))
	}
	return trackerJSON(map[string]any{"ok": true, "deleted": deleted, "tasks": tasks, "repo": nil})
}

func (b *Bot) localTrackerRestart(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if !trackerCanRestart(t) {
		return nil, fmt.Errorf("агент ещё работает — сначала останови задачу")
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if status == "done" || col == trackerColDone || status == "canceled" || col == trackerColCanceled {
		t.Result = ""
	}
	t.Error = ""
	t.HandedToQa = false
	t.QaColumn = ""
	t.QaStatus = ""
	now := time.Now()
	t.WhenAt = now
	t.WhenLabel = "сейчас"
	if err := applyTrackerColumn(&t, trackerColDoing); err != nil {
		return nil, err
	}
	appendTrackerStep(&t, "Перезапускаем задачу")
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.dispatchTrackerAgent(t, "doing")
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}

func (b *Bot) localTrackerReschedule(taskID int64, payload map[string]any) (json.RawMessage, error) {
	normalizeTrackerReschedule("reschedule", payload)
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	at, label, err := parseTrackerWhen(payloadString(payload, "when"))
	if err != nil {
		return nil, err
	}
	t.WhenAt = at
	t.WhenLabel = label
	// «Запустить снова» / when=сейчас. Перенос на будущее не должен
	// перехватывать карточку и снова слать агента.
	if !at.After(time.Now()) && trackerNeedsAgentKick(t, time.Now(), true) {
		t.Error = ""
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Снова запускаем агента")
		if err := b.db.SaveTrackerTask(t); err != nil {
			return nil, err
		}
		b.dispatchTrackerAgent(t, "doing")
		return trackerJSON(map[string]any{"ok": true})
	}
	if t.Status == "done" || t.Status == "canceled" || t.Status == "error" ||
		t.DevColumn == trackerColDone || t.DevColumn == trackerColCanceled {
		if err := applyTrackerColumn(&t, trackerColTodo); err != nil {
			return nil, err
		}
		appendTrackerStep(&t, "Вернули в ожидание на "+label)
	} else {
		appendTrackerStep(&t, "Перенесли на "+label)
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	if trackerTaskDueForStart(t, time.Now()) {
		_, _ = b.claimAndNotifyDueTrackerTasks()
	}
	return trackerJSON(map[string]any{"ok": true})
}

func (b *Bot) localTrackerMove(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	col := payloadString(payload, "column")
	if col == "" || col == "next" {
		next, ok := trackerNextColumn[t.DevColumn]
		if !ok {
			return nil, fmt.Errorf("дальше этой карточке идти некуда")
		}
		col = next
	}
	if t.DevColumn == trackerColApprove && col == trackerColDoing && len(t.Approvals) < trackerApprovalRequired {
		return nil, fmt.Errorf("нужно %d аппрува, сейчас %d", trackerApprovalRequired, len(t.Approvals))
	}
	if err := applyTrackerColumn(&t, col); err != nil {
		return nil, err
	}
	label, _, _ := trackerStatusMeta(t.Status, t.DevColumn)
	appendTrackerStep(&t, "Колонка: "+label)
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.kickTrackerPipeline(t)
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}

func applyTrackerQa(t *database.TrackerTask, action string) error {
	if t == nil {
		return fmt.Errorf("задача не найдена")
	}
	switch strings.ToLower(strings.TrimSpace(action)) {
	case "start":
		t.HandedToQa = true
		t.QaColumn = trackerColDoing
		t.QaStatus = "start"
		if t.DevColumn == trackerColTodo || t.DevColumn == trackerColDoing || t.DevColumn == trackerColReview {
			_ = applyTrackerColumn(t, trackerColTest)
		}
		appendTrackerStep(t, "Взяли в тест")
	case "pass":
		t.HandedToQa = true
		t.QaColumn = trackerColDone
		t.QaStatus = "pass"
		t.Error = ""
		_ = applyTrackerColumn(t, trackerColDeploy)
		appendTrackerStep(t, "QA принял")
	case "fail":
		t.QaColumn = trackerColTodo
		t.QaStatus = "fail"
		t.HandedToQa = false
		_ = applyTrackerColumn(t, trackerColDoing)
		appendTrackerStep(t, "QA вернул в работу")
	case "reset":
		t.HandedToQa = true
		t.QaColumn = trackerColTodo
		t.QaStatus = ""
		_ = applyTrackerColumn(t, trackerColTest)
		appendTrackerStep(t, "QA снова в очереди")
	default:
		return fmt.Errorf("такое действие доске недоступно")
	}
	return nil
}

func (b *Bot) localTrackerQa(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if err := applyTrackerQa(&t, payloadString(payload, "action")); err != nil {
		return nil, err
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	if t.DevColumn == trackerColDeploy {
		b.kickTrackerPipeline(t)
	}
	return trackerJSON(map[string]any{"ok": true})
}

func (b *Bot) localTrackerAutoQa(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if b.aiClient == nil {
		return nil, fmt.Errorf("Лео сейчас недоступен: не настроен OpenRouter")
	}
	raw, err := b.aiClient.Chat([]ai.ChatMessage{
		{Role: "system", Content: `Ты — Лео, тестировщик приложения стаи Fat Leopard.
Прочитай формулировку задачи и коротко скажи, что проверить руками.
Ответь JSON без обрамления: {"note":"..."} 
note — 2–5 предложений, без эмодзи, конкретно: что открыть и что должно получиться.`},
		{Role: "user", Content: t.Prompt},
	}, "")
	if err != nil {
		return nil, fmt.Errorf("AI-тест не вышел: %w", err)
	}
	note := strings.TrimSpace(raw)
	if block := leoJSONBlock.FindString(raw); block != "" {
		var parsed struct {
			Note string `json:"note"`
		}
		if json.Unmarshal([]byte(block), &parsed) == nil && strings.TrimSpace(parsed.Note) != "" {
			note = strings.TrimSpace(parsed.Note)
		}
	}
	if len([]rune(note)) > 1200 {
		note = string([]rune(note)[:1200])
	}
	t.HandedToQa = true
	if t.QaColumn == "" {
		t.QaColumn = trackerColTodo
	}
	t.Result = strings.TrimSpace(strings.TrimSpace(t.Result) + "\n\nAI-тест Лео:\n" + note)
	appendTrackerStep(&t, "Лео написал чек-лист теста")
	if t.DevColumn != trackerColTest && t.DevColumn != trackerColDeploy && t.DevColumn != trackerColDone {
		_ = applyTrackerColumn(&t, trackerColTest)
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"ok": true})
}

func (b *Bot) localTrackerPrompt(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if !trackerCanEditPrompt(t) {
		return nil, fmt.Errorf("Задача уже началась — формулировку не меняем")
	}
	prompt := payloadString(payload, "prompt")
	if prompt == "" {
		return nil, fmt.Errorf("опиши задачу")
	}
	t.Prompt = prompt
	appendTrackerStep(&t, "Формулировку обновили")
	if t.DevColumn == trackerColApprove {
		t.Approvals = nil
		appendTrackerStep(&t, "Аппрувы сброшены после правки")
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	if t.DevColumn == trackerColApprove && t.NeedsApproval {
		b.notifyTrackerApprovalsNeeded(t)
	}
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}

func (b *Bot) localTrackerShip(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		// Старый вебхук чужой доски мог прислать id, которого у нас нет.
		if strings.Contains(err.Error(), "не найдена") {
			return trackerJSON(map[string]any{"ok": true, "skipped": true})
		}
		return nil, err
	}
	snap := trackerTaskSnapshot{
		Status:     t.Status,
		Error:      t.Error,
		Done:       t.Status == "done" || t.DevColumn == trackerColDone,
		DevColumn:  t.DevColumn,
		QaColumn:   t.QaColumn,
		QaStatus:   t.QaStatus,
		HandedToQa: t.HandedToQa,
	}
	if !trackerTaskReadyToShip(snap) {
		return trackerJSON(map[string]any{"ok": true, "skipped": true})
	}
	_ = applyTrackerColumn(&t, trackerColDeploy)
	appendTrackerStep(&t, "К сборке: пуш после теста")
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.kickTrackerPipeline(t)
	return trackerJSON(map[string]any{
		"ok":       true,
		"promoted": false,
		"pushed":   true,
		"deployed": false,
	})
}

func (b *Bot) localTrackerPromoteRevert(what string) (json.RawMessage, error) {
	return nil, fmt.Errorf("%s на своей доске не нужен: код уже в этом проекте. Чтобы выкатить — напиши «запушь»", what)
}

func (b *Bot) localTrackerSprintIdeas(payload map[string]any) (json.RawMessage, error) {
	if b.aiClient == nil {
		return nil, fmt.Errorf("Лео сейчас недоступен: не настроен OpenRouter")
	}
	hint := payloadString(payload, "hint")
	if hint == "" {
		return nil, fmt.Errorf("напиши тему спринта")
	}
	raw, err := b.aiClient.Chat([]ai.ChatMessage{
		{Role: "system", Content: `Ты продуктовый лид Fat Leopard (мини-апп стаи: лента, стрики, чат, админка).
Предложи 4 идеи спринта по теме человека.
Ответь JSON без обрамления:
{"ideas":[{"id":"1","title":"...","summary":"..."}],"recommended_id":"1"}
title до 60 символов, summary до 180, без эмодзи.`},
		{Role: "user", Content: hint},
	}, "")
	if err != nil {
		return nil, fmt.Errorf("не удалось предложить идеи: %w", err)
	}
	var parsed struct {
		Ideas         []map[string]any `json:"ideas"`
		RecommendedID string           `json:"recommended_id"`
	}
	if block := leoJSONBlock.FindString(raw); block != "" {
		_ = json.Unmarshal([]byte(block), &parsed)
	}
	if len(parsed.Ideas) == 0 {
		return nil, fmt.Errorf("Лео не предложил идей, попробуй другую тему")
	}
	return trackerJSON(map[string]any{
		"ideas":          parsed.Ideas,
		"recommended_id": parsed.RecommendedID,
	})
}

func (b *Bot) localTrackerSprintGenerate(payload map[string]any) (json.RawMessage, error) {
	if b.aiClient == nil {
		return nil, fmt.Errorf("Лео сейчас недоступен: не настроен OpenRouter")
	}
	hint := payloadString(payload, "hint")
	sprintCount := payloadInt(payload, "sprint_count", 1)
	per := payloadInt(payload, "tasks_per_sprint", 5)
	if sprintCount < 1 {
		sprintCount = 1
	}
	if sprintCount > 8 {
		sprintCount = 8
	}
	if per < 1 {
		per = 3
	}
	if per > 12 {
		per = 12
	}
	idea := payload["idea"]
	ideaJSON, _ := json.Marshal(idea)
	raw, err := b.aiClient.Chat([]ai.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(`Ты нарезаешь спринт для Fat Leopard.
Сделай %d спринт(ов) по %d задач.
Ответь JSON без обрамления:
{"features":[{"title":"...","prompt":"...","sprint":1}]}
prompt — что сделать и зачем, до 400 символов, без эмодзи.`, sprintCount, per)},
		{Role: "user", Content: "Тема: " + hint + "\nИдея: " + string(ideaJSON)},
	}, "")
	if err != nil {
		return nil, fmt.Errorf("не удалось собрать план: %w", err)
	}
	var parsed struct {
		Features []map[string]any `json:"features"`
	}
	if block := leoJSONBlock.FindString(raw); block != "" {
		_ = json.Unmarshal([]byte(block), &parsed)
	}
	if len(parsed.Features) == 0 {
		return nil, fmt.Errorf("план пустой, попробуй ещё раз")
	}
	return trackerJSON(map[string]any{"features": parsed.Features})
}

func (b *Bot) localTrackerSprintApply(payload map[string]any, userID int64) (json.RawMessage, error) {
	feats, _ := payload["features"].([]any)
	if len(feats) == 0 {
		return nil, fmt.Errorf("отметь хотя бы одну задачу")
	}
	isLeo := payloadBool(payload, "leo")
	needsApproval := payloadBool(payload, "needs_approval")
	if isLeo {
		needsApproval = true
	}
	created := 0
	for i, raw := range feats {
		feat, _ := raw.(map[string]any)
		if feat == nil {
			continue
		}
		title := payloadString(feat, "title")
		prompt := payloadString(feat, "prompt")
		if prompt == "" {
			prompt = title
		}
		if prompt == "" {
			continue
		}
		sprint := payloadInt(feat, "sprint", 1)
		if sprint > 0 {
			prompt = fmt.Sprintf("[Спринт %d] %s", sprint, prompt)
		}
		when := "сейчас"
		if i > 0 {
			when = fmt.Sprintf("через %d мин", i)
		}
		createPayload := map[string]any{
			"when":   when,
			"prompt": prompt,
		}
		if isLeo {
			createPayload["leo"] = true
		}
		if needsApproval {
			createPayload["needs_approval"] = true
		}
		if _, ok := payload["auto_push"]; ok {
			createPayload["auto_push"] = payloadBool(payload, "auto_push")
		}
		if _, err := b.localTrackerCreate(createPayload, userID); err != nil {
			return nil, err
		}
		created++
	}
	if created == 0 {
		return nil, fmt.Errorf("ни одной задачи не поставилось")
	}
	return trackerJSON(map[string]any{"ok": true, "created": created})
}

func payloadInt(p map[string]any, key string, fallback int) int {
	if p == nil {
		return fallback
	}
	switch v := p[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return fallback
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return fallback
		}
		return n
	default:
		return fallback
	}
}
