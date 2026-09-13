package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"leo-bot/internal/database"
)

var trackerNotifyNumRe = regexp.MustCompile(`(?i)задач[аиеу]?\s*#\s*(\d+)`)

const trackerNotifyResultMax = 4000

// ApplyBoardNotify — входящее «задача выполнена» должно сдвинуть карточку
// на доске. Раньше писали только в личку, а колонка так и оставалась «В работе».
func (b *Bot) ApplyBoardNotify(taskID int64, text string) (localID int64, ship bool, err error) {
	text = strings.TrimSpace(text)
	if b == nil || b.db == nil || text == "" {
		return 0, false, nil
	}
	kind := trackerNotifyKind(text)
	t, err := b.findTrackerTaskForNotify(taskID, trackerNotifyTaskNum(text))
	if err != nil {
		return 0, false, err
	}
	if t.ID <= 0 {
		return 0, false, nil
	}
	from := strings.ToLower(strings.TrimSpace(t.DevColumn))
	applyTrackerNotify(&t, kind, text)
	if kind == "done" {
		applyTrackerPhaseVerdict(&t, from, text)
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return t.ID, false, err
	}
	if trackerShouldKickAfterNotify(kind, from, text) {
		b.kickTrackerPipeline(t)
	}
	return t.ID, trackerShouldShipAfterNotify(t), nil
}

func (b *Bot) findTrackerTaskForNotify(id int64, num int) (database.TrackerTask, error) {
	var empty database.TrackerTask
	if b == nil || b.db == nil {
		return empty, nil
	}
	if id > 0 {
		if t, ok := trackerTaskIfFound(b.db.GetTrackerTask(id)); ok {
			return t, nil
		}
		if t, ok := trackerTaskIfFound(b.db.GetTrackerTaskByNum(int(id))); ok {
			return t, nil
		}
		if t, ok := trackerTaskIfFound(b.findTrackerTaskByRemoteID(id)); ok {
			return t, nil
		}
	}
	if num > 0 && int64(num) != id {
		if t, ok := trackerTaskIfFound(b.db.GetTrackerTaskByNum(num)); ok {
			return t, nil
		}
		if t, ok := trackerTaskIfFound(b.findTrackerTaskByRemoteID(int64(num))); ok {
			return t, nil
		}
	}
	return b.db.FindOpenTrackerTask()
}

func (b *Bot) findTrackerTaskByRemoteID(remoteID int64) (database.TrackerTask, error) {
	var empty database.TrackerTask
	if b == nil || b.db == nil || remoteID <= 0 {
		return empty, fmt.Errorf("задача не найдена")
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return empty, err
	}
	for _, t := range list {
		if trackerStepRemoteID(t.Steps) == remoteID {
			return t, nil
		}
	}
	return empty, fmt.Errorf("задача не найдена")
}

func trackerTaskIfFound(t database.TrackerTask, err error) (database.TrackerTask, bool) {
	if err != nil || t.ID <= 0 {
		return database.TrackerTask{}, false
	}
	return t, true
}

// TrackerNotifyIsFullyShipped — в личку пишем только финал: задача уже
// на проде в ветке main. Промежуточные статусы крутятся на доске молча.
func TrackerNotifyIsFullyShipped(text string) bool {
	low := strings.ToLower(strings.TrimSpace(text))
	if low == "" {
		return false
	}
	if strings.Contains(low, "запушь") || strings.Contains(text, "TRACKER_NO_CODE") ||
		strings.Contains(low, "началась") || strings.Contains(low, "можно на тест") ||
		strings.Contains(low, "ревью не принято") || strings.Contains(low, "тест не прошёл") ||
		strings.Contains(low, "тест не прошел") || strings.Contains(low, "кода нет") ||
		strings.Contains(low, "не попал в github") {
		return false
	}
	hasProd := strings.Contains(low, "railway") || strings.Contains(low, "на прод")
	hasMain := strings.Contains(low, "ветке main") || strings.Contains(low, "ветки main") ||
		strings.Contains(low, "ветка main") || strings.Contains(low, "в main") ||
		strings.Contains(low, "(main)") || strings.Contains(low, "railway main")
	hasDeployed := strings.Contains(low, "задепло") || strings.Contains(low, "выехал") ||
		(strings.Contains(low, "на railway") || strings.Contains(low, "на прод")) &&
			(strings.Contains(low, "выполнен") || strings.Contains(low, "готово"))
	return hasProd && hasMain && hasDeployed
}

func trackerDoneExecutionSummary(t database.TrackerTask) string {
	done := trackerStepPrefixedSummary(t.Steps, "сделано:")
	if done == "" {
		done = trackerExtractAgentNote(t.Result)
	}
	if done == "" {
		done = trackerTaskTitle(t.Prompt)
	}
	return clipDoneSummary(done)
}

// trackerFullyDoneNote — короткое DM о выкате. Полный чеклист из 5 пунктов
// (выполнение → ревью → тест → сборка → уведомление) только на карточке
// задачи в done_summary, не в телеграме.
func trackerFullyDoneNote(t database.TrackerTask) string {
	parts := []string{
		fmt.Sprintf("✅ %s выполнена.", trackerNotifyHeading(t)),
		trackerDeploySummary(t),
	}
	if summary := trackerDoneExecutionSummary(t); summary != "" {
		parts = append(parts, summary)
	}
	return strings.Join(parts, "\n\n")
}

const trackerDoneSummaryMax = 400

func clipDoneSummary(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, part := range strings.Split(s, "\n\n") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		low := strings.ToLower(part)
		if strings.HasPrefix(low, "локальный коммит") || strings.HasPrefix(low, "следующий шаг") {
			continue
		}
		s = part
		break
	}
	r := []rune(s)
	if len(r) <= trackerDoneSummaryMax {
		return string(r)
	}
	return string(r[:trackerDoneSummaryMax]) + "…"
}

func trackerStepPrefixedSummary(steps []string, prefix string) string {
	prefix = strings.ToLower(strings.TrimSpace(prefix))
	for i := len(steps) - 1; i >= 0; i-- {
		s := strings.TrimSpace(steps[i])
		if strings.HasPrefix(strings.ToLower(s), prefix) {
			return strings.TrimSpace(s[len(prefix):])
		}
	}
	return ""
}

func trackerPhaseCommit(steps []string, label string) string {
	label = strings.ToLower(strings.TrimSpace(label))
	for i := len(steps) - 1; i >= 0; i-- {
		low := strings.ToLower(strings.TrimSpace(steps[i]))
		if strings.Contains(low, "коммит ") && strings.HasSuffix(low, " "+label) {
			if m := trackerCommitRe.FindStringSubmatch(steps[i]); len(m) > 1 {
				return m[1]
			}
		}
	}
	return ""
}

func trackerExtractAgentNote(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	var body []string
	pastHeader := false
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			if pastHeader && len(body) > 0 {
				body = append(body, "")
			}
			continue
		}
		low := strings.ToLower(line)
		if !pastHeader {
			if strings.HasPrefix(low, "задача #") ||
				strings.Contains(low, "коммит выполнения") ||
				strings.Contains(low, "⏰ задача #") {
				pastHeader = strings.Contains(low, "коммит выполнения") ||
					strings.Contains(low, "выполнена")
				continue
			}
		}
		if strings.Contains(low, "следующий шаг") ||
			strings.Contains(text, "TRACKER_NO_CODE") ||
			strings.Contains(low, "можно на тест") ||
			strings.Contains(low, "тест пройден") {
			break
		}
		pastHeader = true
		body = append(body, line)
	}
	return strings.TrimSpace(strings.Join(body, "\n"))
}

func trackerPhasePassed(t database.TrackerTask, label string) bool {
	if trackerPhaseCommit(t.Steps, label) != "" {
		return true
	}
	low := strings.ToLower(t.Result)
	switch label {
	case "ревью":
		if strings.Contains(low, "можно на тест") ||
			strings.Contains(low, "composer: ревью") ||
			strings.Contains(low, "ревью: пройден") {
			return true
		}
	case "тест":
		if strings.Contains(low, "тест пройден") ||
			strings.Contains(low, "composer: тест") ||
			strings.Contains(low, "тест: пройден") {
			return true
		}
	}
	for _, step := range t.Steps {
		lowStep := strings.ToLower(step)
		switch label {
		case "ревью":
			if strings.Contains(lowStep, "composer: ревью принято") ||
				strings.HasPrefix(lowStep, "ревью:") {
				return true
			}
		case "тест":
			if strings.Contains(lowStep, "composer: тест принято") ||
				strings.HasPrefix(lowStep, "тест:") ||
				strings.Contains(lowStep, "qa принял") {
				return true
			}
		}
	}
	return false
}

func trackerPhaseBriefLine(t database.TrackerTask, label, fallback string) string {
	prefix := label + ":"
	if note := trackerStepPrefixedSummary(t.Steps, prefix); note != "" {
		return note
	}
	if sha := trackerPhaseCommit(t.Steps, label); sha != "" {
		return fallback + " (коммит " + sha + ")"
	}
	return fallback
}

func trackerDeploySummary(t database.TrackerTask) string {
	for _, step := range t.Steps {
		low := strings.ToLower(strings.TrimSpace(step))
		if strings.Contains(low, "стенд собрался") {
			return "Выехала на прод (ветка main)."
		}
		if strings.HasPrefix(low, "пуш в ") {
			return "Выехала на прод (ветка main)."
		}
	}
	return "Выехала на прод (ветка main)."
}

func trackerDoneBrief(t database.TrackerTask) string {
	lines := make([]string, 0, 5)

	lines = append(lines, "1. Выполнение: "+trackerDoneExecutionSummary(t))

	if !t.FastTrack && trackerPhasePassed(t, "ревью") {
		lines = append(lines, "2. Ревью: "+trackerPhaseBriefLine(t, "ревью", "пройдено"))
	}

	if trackerPhasePassed(t, "тест") {
		lines = append(lines, fmt.Sprintf("%d. Тест: %s", len(lines)+1, trackerPhaseBriefLine(t, "тест", "пройден")))
	} else if t.ManualQa {
		for _, step := range t.Steps {
			if strings.EqualFold(strings.TrimSpace(step), "QA принял") {
				lines = append(lines, fmt.Sprintf("%d. Тест: ручное QA пройдено", len(lines)+1))
				break
			}
		}
	}

	lines = append(lines, fmt.Sprintf("%d. Сборка: %s", len(lines)+1, strings.TrimSuffix(trackerDeploySummary(t), ".")))
	lines = append(lines, fmt.Sprintf("%d. Уведомление админам отправлено.", len(lines)+1))
	return strings.Join(lines, "\n")
}

func trackerRecordPhaseSummary(t *database.TrackerTask, fromCol, text string) {
	if t == nil {
		return
	}
	switch fromCol {
	case trackerColDoing:
		if summary := trackerExtractAgentNote(text); summary != "" {
			appendTrackerStep(t, "сделано: "+clipDoneSummary(summary))
		}
	case trackerColReview:
		if trackerComposerPassed(trackerColReview, text) {
			appendTrackerStep(t, "ревью: "+trackerPhaseBriefLine(*t, "ревью", "пройдено"))
		}
	case trackerColTest:
		if trackerComposerPassed(trackerColTest, text) {
			appendTrackerStep(t, "тест: "+trackerPhaseBriefLine(*t, "тест", "пройден"))
		}
	}
}

const trackerShipNotifiedStep = "уведомили о выкате"

// trackerShipNotifyInflight — один DM на карточку: finishTrackerBuild и
// resume после рестарта ms_leo могут сойтись на одном task_id.
var trackerShipNotifyInflight sync.Map

func trackerAlreadyNotifiedShip(t database.TrackerTask) bool {
	for _, step := range t.Steps {
		if strings.TrimSpace(step) == trackerShipNotifiedStep {
			return true
		}
	}
	return false
}

func trackerShouldKickAfterNotify(kind, from, text string) bool {
	if kind != "done" {
		return false
	}
	// Текст «на прод» не закрывает карточку. Запускаем ожидание стенда —
	// kick смотрит уже новую колонку (сборка), а не ревью.
	if TrackerNotifyIsFullyShipped(text) {
		return true
	}
	// Тест провален — ждём человека или повторный клик. Ревью провалено —
	// агент снова пишет код во «В работе».
	if from == trackerColTest && !trackerComposerPassed(from, text) {
		return false
	}
	return true
}

// applyTrackerPhaseVerdict — отказ ревью возвращает карточку в работу,
// чтобы агент правил по замечаниям. Отказ теста остаётся на тесте.
func applyTrackerPhaseVerdict(t *database.TrackerTask, from, text string) {
	if t == nil {
		return
	}
	if from != trackerColReview && from != trackerColTest {
		return
	}
	if trackerComposerPassed(from, text) {
		return
	}
	t.Error = clipNotifyText(text)
	if from == trackerColReview {
		_ = applyTrackerColumn(t, trackerColDoing)
		appendTrackerStep(t, "Вернули в работу: ревью не принято")
		return
	}
	_ = applyTrackerColumn(t, from)
	appendTrackerStep(t, trackerAgentName(from)+" не принято")
}

func (b *Bot) notifyTrackerShippedOnce(t database.TrackerTask) {
	if b == nil || t.ID <= 0 {
		return
	}
	if _, loaded := trackerShipNotifyInflight.LoadOrStore(t.ID, struct{}{}); loaded {
		return
	}
	defer trackerShipNotifyInflight.Delete(t.ID)
	if b.db != nil {
		if fresh, err := b.db.GetTrackerTask(t.ID); err == nil && fresh.ID > 0 {
			t = fresh
		}
	}
	if trackerAlreadyNotifiedShip(t) {
		return
	}
	note := trackerFullyDoneNote(t)
	appendTrackerStep(&t, trackerShipNotifiedStep)
	if !strings.Contains(t.Result, note) {
		t.Result = strings.TrimSpace(strings.TrimSpace(t.Result) + "\n\n" + note)
	}
	if b.db != nil {
		if err := b.db.SaveTrackerTask(t); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить уведомление о выкате #%d: %v", trackerDueNum(t), err)
			return
		}
	}
	if err := b.NotifyTrackerResult(trackerNotifyAuthor(t), note); err != nil && b.logger != nil {
		b.logger.Warnf("трекер: не сообщить о выкате #%d: %v", trackerDueNum(t), err)
	}
}

func (b *Bot) NotifyTrackerShippedIfNeeded(taskID int64, text string) {
	if b == nil || !TrackerNotifyIsFullyShipped(text) {
		return
	}
	t, err := b.findTrackerTaskForNotify(taskID, trackerNotifyTaskNum(text))
	if err != nil || t.ID <= 0 {
		return
	}
	b.notifyTrackerShippedOnce(t)
}

func trackerNotifyKind(text string) string {
	low := strings.ToLower(text)
	switch {
	case strings.Contains(text, "TRACKER_NO_CODE") || strings.Contains(low, "кода нет") ||
		strings.Contains(low, "репозиторий не менялся"):
		return "plan"
	case strings.Contains(low, "отменен") || strings.Contains(low, "cancelled") || strings.Contains(low, "canceled"):
		return "canceled"
	case strings.Contains(low, "агент не стартовал") || strings.Contains(low, "openrouter"):
		return "error"
	case strings.Contains(low, "выполнен") || strings.Contains(low, "completed") ||
		strings.Contains(low, "готово") || strings.Contains(text, "✅") ||
		strings.Contains(low, "можно на тест") || strings.Contains(low, "тест пройден") ||
		strings.Contains(low, "ревью не принято") || strings.Contains(low, "тест не прошёл") ||
		strings.Contains(low, "тест не прошел"):
		return "done"
	case strings.Contains(low, "ошибк") || strings.Contains(low, "не удалось") || strings.Contains(low, "срыв"):
		return "error"
	case strings.Contains(low, "началась") || strings.Contains(low, "взяли в работу"):
		return "started"
	default:
		return ""
	}
}

func trackerNotifyTaskNum(text string) int {
	m := trackerNotifyNumRe.FindStringSubmatch(text)
	if len(m) < 2 {
		return 0
	}
	n, err := strconv.Atoi(m[1])
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

func clipNotifyText(s string) string {
	s = strings.TrimSpace(s)
	if r := []rune(s); len(r) > trackerNotifyResultMax {
		return string(r[:trackerNotifyResultMax])
	}
	return s
}

func trackerNotifyDoneColumn(t database.TrackerTask) string {
	// Текст «выехала на прод» карточку не закрывает — как myvibelab:
	// «выполнено» только после SUCCESS заказанной сборки Railway.
	if TrackerNotifyIsFullyShipped(t.Result) {
		return trackerColDeploy
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	// Сдача на review/тесте — это вердикт Composer, двигаем дальше.
	// Ручное QA на тесте не перескакиваем: человек ещё смотрит.
	if t.ManualQa && col == trackerColTest {
		return trackerColTest
	}
	switch col {
	case trackerColReview:
		return trackerColTest
	case trackerColTest:
		return trackerColDeploy
	case trackerColDeploy, trackerColDone:
		return col
	}
	if t.FastTrack {
		return trackerColDeploy
	}
	return trackerColReview
}

func trackerShouldShipAfterNotify(t database.TrackerTask) bool {
	// Обычный путь: kickTrackerPipeline сам закрывает сборку.
	// Fast-track сразу к публикации — пусть старый ship тоже сработает.
	if !t.FastTrack {
		return false
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	return col == trackerColDeploy || col == trackerColDone
}

// healTrackerCardsFromStoredResult — «выполнена» уже лежит в result, а колонка
// всё ещё «Ожидает» / «В работе». Так бывает, если уведомление пришло, пока
// карточка ждала срок, или claim из-за TZ её не снял. Двигаем по сохранённому
// тексту, не дожидаясь второго вебхука.
func (b *Bot) healTrackerCardsFromStoredResult() (int, error) {
	if b == nil || b.db == nil {
		return 0, nil
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return 0, err
	}
	moved := 0
	for _, t := range list {
		if !trackerShouldAdvanceFromResult(t) {
			continue
		}
		kind := trackerNotifyKind(t.Result)
		applyTrackerNotify(&t, kind, t.Result)
		if err := b.db.SaveTrackerTask(t); err != nil {
			return moved, err
		}
		if kind == "done" && !trackerComposerFailedResult(t.Result) &&
			trackerShouldKickAfterNotify(kind, t.DevColumn, t.Result) {
			b.kickTrackerPipeline(t)
		}
		moved++
	}
	return moved, nil
}

// trackerShouldAdvanceFromResult — в result уже «готово», а колонка не там.
func trackerComposerFailedResult(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "ревью не принято") ||
		strings.Contains(low, "тест не прошёл") ||
		strings.Contains(low, "тест не прошел") ||
		strings.Contains(low, "агент не стартовал") ||
		trackerVerdictIsFakePass(text)
}

func trackerShouldAdvanceFromResult(t database.TrackerTask) bool {
	kind := trackerNotifyKind(t.Result)
	if kind == "" || kind == "started" || kind == "error" {
		return false
	}
	if trackerComposerFailedResult(t.Result) {
		return false
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	status := strings.ToLower(strings.TrimSpace(t.Status))
	if status == "done" || status == "canceled" || status == "cancelled" ||
		col == trackerColDone || col == trackerColCanceled {
		return false
	}
	switch kind {
	case "done":
		if TrackerNotifyIsFullyShipped(t.Result) {
			return col != trackerColDone && col != trackerColCanceled
		}
		want := trackerNotifyDoneColumn(t)
		if col == want && status != "pending" && status != "scheduled" && status != "running" {
			return false
		}
		return col == "" || col == trackerColTodo || col == trackerColDoing ||
			status == "pending" || status == "scheduled" || status == "running"
	case "error":
		return status != "error"
	case "canceled":
		return col != trackerColCanceled
	default:
		return false
	}
}

func applyTrackerNotify(t *database.TrackerTask, kind, text string) {
	if t == nil {
		return
	}
	text = clipNotifyText(text)
	if text != "" && kind != "error" {
		t.Result = text
	}
	if br := trackerBranchRe.FindString(text); br != "" {
		appendTrackerStep(t, "ветка "+br)
	}
	if m := trackerCommitRe.FindStringSubmatch(text); len(m) > 1 {
		label := "выполнение"
		lowText := strings.ToLower(text)
		if strings.Contains(lowText, "ревью") {
			label = "ревью"
		} else if strings.Contains(lowText, "тест") {
			label = "тест"
		}
		appendTrackerStep(t, "коммит "+m[1]+" "+label)
	}
	switch kind {
	case "plan":
		_ = applyTrackerColumn(t, trackerColDoing)
		t.Error = "Кода в репозитории нет"
		appendTrackerStep(t, "Агент сдал план без кода")
	case "canceled":
		_ = applyTrackerColumn(t, trackerColCanceled)
		appendTrackerStep(t, "Отменена по уведомлению")
	case "done":
		fromCol := strings.ToLower(strings.TrimSpace(t.DevColumn))
		trackerRecordPhaseSummary(t, fromCol, text)
		col := trackerNotifyDoneColumn(*t)
		if col != fromCol {
			_ = applyTrackerColumn(t, col)
		}
		t.Error = ""
		appendTrackerStep(t, "Агент сдал результат")
	case "error":
		t.Status = "error"
		if text != "" {
			t.Error = text
		}
		appendTrackerStep(t, "Ошибка")
	case "started":
		status := strings.ToLower(strings.TrimSpace(t.Status))
		if t.DevColumn == trackerColTodo || status == "pending" || status == "scheduled" {
			_ = applyTrackerColumn(t, trackerColDoing)
			appendTrackerStep(t, "Взяли в работу по уведомлению")
		}
	default:
		if text != "" {
			appendTrackerStep(t, "Обновили результат")
		}
	}
}
