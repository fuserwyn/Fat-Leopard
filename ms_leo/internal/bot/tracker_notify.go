package bot

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"leo-bot/internal/database"
)

var trackerNotifyNumRe = regexp.MustCompile(`(?i)(?:task|задач[аиеу]?)\s*#\s*(\d+)`)

// Шаблон агента «Сделано для задачи #N.» / «Done for task #N.» — в Telegram-DM не нужен.
var trackerDoneBoilerplateRe = regexp.MustCompile(`(?i)^(?:Сделано\s+для\s+задачи|Done\s+for\s+task)\s+#\d+[.:]?\s*$`)

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
	// Проваленный тест вернул карточку в работу — агента надо запустить снова.
	testReturned := from == trackerColTest && t.DevColumn == trackerColDoing
	if trackerShouldKickAfterNotify(kind, from, text) || testReturned {
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
		return empty, fmt.Errorf("task not found")
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
	return empty, fmt.Errorf("task not found")
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
	if trackerLoHas(text, "push", "запушь") || strings.Contains(text, "TRACKER_NO_CODE") ||
		trackerLoHas(text, "started", "началась") || trackerLoHas(text, "ok for test", "можно на тест") ||
		trackerLoHas(text, "review rejected", "ревью не принято") ||
		trackerLoHas(text, "test failed", "тест не прошёл") || trackerLoHas(text, "test failed", "тест не прошел") ||
		trackerLoHas(text, "no code", "кода нет") ||
		trackerLoHas(text, "did not reach github", "не попал в github") {
		return false
	}
	hasProd := strings.Contains(low, "railway") || trackerLoHas(text, "prod", "на прод") ||
		strings.Contains(low, "shipped to prod")
	hasMain := trackerLoHas(text, "main branch", "ветке main") || trackerLoHas(text, "main branch", "ветки main") ||
		trackerLoHas(text, "main branch", "ветка main") || trackerLoHas(text, "main", "в main") ||
		strings.Contains(low, "(main)") || strings.Contains(low, "railway main")
	hasDeployed := trackerLoHas(text, "deployed", "задепло") || trackerLoHas(text, "shipped", "выехал") ||
		strings.Contains(low, "shipped to prod") ||
		((strings.Contains(low, "railway") || trackerLoHas(text, "prod", "на прод")) &&
			(trackerLoHas(text, "completed", "выполнен") || trackerLoHas(text, "ready", "готово")))
	return hasProd && hasMain && hasDeployed
}

func trackerEffectiveDoneSummary(s string) string {
	s = clipDoneSummary(s)
	if s == "" || trackerDoneBoilerplateRe.MatchString(s) {
		return ""
	}
	return s
}

func trackerDoneExecutionSummary(t database.TrackerTask) string {
	if done := trackerEffectiveDoneSummary(trackerStepPrefixedSummary(t.Steps, "done:")); done != "" {
		return done
	}
	if done := trackerEffectiveDoneSummary(trackerStepPrefixedSummary(t.Steps, "сделано:")); done != "" {
		return done
	}
	if done := trackerEffectiveDoneSummary(trackerExtractAgentNote(t.Result)); done != "" {
		return done
	}
	return trackerEffectiveDoneSummary(trackerTaskTitle(t.Prompt))
}

// trackerFullyDoneNote — короткое DM о выкате. Полный чеклист из 5 пунктов
// (выполнение → ревью → тест → сборка → уведомление) только на карточке
// задачи в done_summary, не в телеграме.
func trackerFullyDoneNote(t database.TrackerTask) string {
	parts := []string{
		fmt.Sprintf("✅ %s completed.", trackerNotifyHeading(t)),
		trackerDeploySummary(t),
	}
	// Если агент ничего внятного не написал, итог откатывается к формулировке
	// задачи — а она уже стоит в заголовке, повторять её незачем.
	if summary := trackerDoneExecutionSummary(t); summary != "" &&
		summary != trackerEffectiveDoneSummary(trackerTaskTitle(t.Prompt)) {
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
		if strings.HasPrefix(low, "локальный коммит") || strings.HasPrefix(low, "local commit") ||
			strings.HasPrefix(low, "следующий шаг") || strings.HasPrefix(low, "next step") {
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
		enLabel := label
		switch label {
		case "ревью":
			enLabel = "review"
		case "тест":
			enLabel = "test"
		case "выполнение":
			enLabel = "execution"
		}
		if (strings.Contains(low, "commit ") || strings.Contains(low, "коммит ")) &&
			(strings.HasSuffix(low, " "+label) || strings.HasSuffix(low, " "+enLabel)) {
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
			if strings.HasPrefix(low, "task #") || strings.HasPrefix(low, "задача #") ||
				strings.Contains(low, "commit execution") || strings.Contains(low, "коммит выполнения") ||
				strings.Contains(low, "⏰ task #") || strings.Contains(low, "⏰ задача #") {
				pastHeader = strings.Contains(low, "commit execution") ||
					strings.Contains(low, "коммит выполнения") ||
					strings.Contains(low, "completed") || strings.Contains(low, "выполнена")
				continue
			}
		}
		if strings.Contains(low, "next step") || strings.Contains(low, "следующий шаг") ||
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
	ruLabel := label
	switch label {
	case "review":
		ruLabel = "ревью"
	case "test":
		ruLabel = "тест"
	}
	if trackerPhaseCommit(t.Steps, label) != "" || trackerPhaseCommit(t.Steps, ruLabel) != "" {
		return true
	}
	low := strings.ToLower(t.Result)
	switch label {
	case "review", "ревью":
		if trackerLoHas(t.Result, "ok for test", "можно на тест") ||
			strings.Contains(low, "composer: review") ||
			strings.Contains(low, "composer: ревью") ||
			trackerLoHas(t.Result, "review: passed", "ревью: пройден") {
			return true
		}
	case "test", "тест":
		if trackerLoHas(t.Result, "test passed", "тест пройден") ||
			strings.Contains(low, "composer: test") ||
			strings.Contains(low, "composer: тест") ||
			trackerLoHas(t.Result, "test: passed", "тест: пройден") {
			return true
		}
	}
	for _, step := range t.Steps {
		lowStep := strings.ToLower(step)
		switch label {
		case "review", "ревью":
			if strings.Contains(lowStep, "composer: review accepted") ||
				strings.Contains(lowStep, "composer: ревью принято") ||
				strings.HasPrefix(lowStep, "review:") ||
				strings.HasPrefix(lowStep, "ревью:") {
				return true
			}
		case "test", "тест":
			if strings.Contains(lowStep, "composer: test accepted") ||
				strings.Contains(lowStep, "composer: тест принято") ||
				strings.HasPrefix(lowStep, "test:") ||
				strings.HasPrefix(lowStep, "тест:") ||
				trackerLoHas(step, "qa accepted", "qa принял") {
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
		return fallback + " (commit " + sha + ")"
	}
	return fallback
}

func trackerDeploySummary(t database.TrackerTask) string {
	for _, step := range t.Steps {
		low := strings.ToLower(strings.TrimSpace(step))
		if trackerLoHas(step, "stand build succeeded", "стенд собрался") {
			return "Shipped to prod (main branch)."
		}
		if strings.HasPrefix(low, "push to ") || strings.HasPrefix(low, "пуш в ") {
			return "Shipped to prod (main branch)."
		}
	}
	return "Shipped to prod (main branch)."
}

func trackerDoneBrief(t database.TrackerTask) string {
	lines := make([]string, 0, 5)

	lines = append(lines, "1. Execution: "+trackerDoneExecutionSummary(t))

	if !t.FastTrack && trackerPhasePassed(t, "review") {
		lines = append(lines, "2. Review: "+trackerPhaseBriefLine(t, "review", "passed"))
	}

	if trackerPhasePassed(t, "test") {
		lines = append(lines, fmt.Sprintf("%d. Test: %s", len(lines)+1, trackerPhaseBriefLine(t, "test", "passed")))
	} else if t.ManualQa {
		for _, step := range t.Steps {
			if strings.EqualFold(strings.TrimSpace(step), "QA accepted") {
				lines = append(lines, fmt.Sprintf("%d. Test: manual QA passed", len(lines)+1))
				break
			}
		}
	}

	lines = append(lines, fmt.Sprintf("%d. Deploy: %s", len(lines)+1, strings.TrimSuffix(trackerDeploySummary(t), ".")))
	lines = append(lines, fmt.Sprintf("%d. Admin notification sent.", len(lines)+1))
	return strings.Join(lines, "\n")
}

func trackerRecordPhaseSummary(t *database.TrackerTask, fromCol, text string) {
	if t == nil {
		return
	}
	switch fromCol {
	case trackerColDoing:
		if summary := trackerEffectiveDoneSummary(trackerExtractAgentNote(text)); summary != "" {
			appendTrackerStep(t, "done: "+summary)
		}
	case trackerColReview:
		if trackerComposerPassed(trackerColReview, text) {
			appendTrackerStep(t, "review: "+trackerPhaseBriefLine(*t, "review", "passed"))
		}
	case trackerColTest:
		if trackerComposerPassed(trackerColTest, text) {
			appendTrackerStep(t, "test: "+trackerPhaseBriefLine(*t, "test", "passed"))
		}
	}
}

const trackerShipNotifiedStep = "ship notification sent"

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
	// Тест провален: карточку возвращает в работу returnTrackerFromFailedTest,
	// агента после этого запускает вызывающий.
	if from == trackerColTest && !trackerComposerPassed(from, text) {
		return false
	}
	return true
}

// applyTrackerPhaseVerdict — отказ ревью или теста возвращает карточку в
// работу, чтобы агент правил по замечаниям.
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
		appendTrackerStep(t, "Returned to doing: review rejected")
		return
	}
	returnTrackerFromFailedTest(t, text)
}

// Тест проверяет сборку и прогоняет тесты по-настоящему, так что отказы —
// обычное дело. Карточка не ждёт человека: возвращается агенту вместе с
// причиной и идёт по кругу «работа → ревью → тест», пока тест не пройдёт.
// Лимита попыток нет намеренно — так решил владелец.
const (
	trackerTestReturnedStep = "Returned to doing: test failed"
	trackerTestFailedMarker = "Test failed — fix and resubmit:"
)

// returnTrackerFromFailedTest возвращает карточку в работу после отказа теста.
func returnTrackerFromFailedTest(t *database.TrackerTask, text string) {
	if t == nil {
		return
	}
	t.Error = clipNotifyText(text)
	appendTrackerStep(t, trackerAgentName(trackerColTest)+": not accepted")
	// Причину отказа кладём в результат: он уходит агенту в задании на доработку.
	// Прошлую причину заменяем, а не копим.
	result := t.Result
	if i := strings.Index(result, trackerTestFailedMarker); i >= 0 {
		result = result[:i]
	}
	reason := strings.TrimSpace(text)
	// Уведомление трекера уже записало вердикт в результат — второй раз его не кладём.
	result = strings.Replace(result, reason, "", 1)
	if r := []rune(reason); len(r) > 2000 {
		reason = string(r[:2000]) + "…"
	}
	t.Result = strings.TrimSpace(strings.TrimSpace(result) + "\n\n" + trackerTestFailedMarker + "\n" + reason)
	_ = applyTrackerColumn(t, trackerColDoing)
	appendTrackerStep(t, trackerTestReturnedStep)
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
	// Отметку ставим атомарно в базе: при выкатке ms_leo старый и новый
	// контейнеры оба дожимают сборку, sync.Map живёт в одном процессе, и
	// автор получал два «выполнена» (#105).
	if b.db != nil {
		claimed, err := b.db.ClaimTrackerStep(t.ID, trackerShipNotifiedStep)
		if err != nil {
			if b.logger != nil {
				b.logger.Warnf("трекер: не отметить уведомление о выкате #%d: %v", trackerDueNum(t), err)
			}
			return
		}
		if !claimed {
			return
		}
		// Шаг уже в базе — перечитываем, чтобы сохранение ниже его не стёрло.
		if fresh, err := b.db.GetTrackerTask(t.ID); err == nil && fresh.ID > 0 {
			t = fresh
		} else {
			appendTrackerStep(&t, trackerShipNotifiedStep)
		}
	} else {
		appendTrackerStep(&t, trackerShipNotifiedStep)
	}
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
	case trackerLoHas(text, "started", "началась") || trackerLoHas(text, "moved to doing", "взяли в работу"):
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
		return trackerColumnAfterReview(t)
	case trackerColTest:
		return trackerColDeploy
	case trackerColDeploy, trackerColDone:
		return col
	}
	return trackerColumnAfterDoing(t)
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
		appendTrackerStep(t, "branch "+br)
	}
	if m := trackerCommitRe.FindStringSubmatch(text); len(m) > 1 {
		label := "execution"
		lowText := strings.ToLower(text)
		if strings.Contains(lowText, "review") || strings.Contains(lowText, "ревью") {
			label = "review"
		} else if strings.Contains(lowText, "test") || strings.Contains(lowText, "тест") {
			label = "test"
		}
		appendTrackerStep(t, "commit "+m[1]+" "+label)
	}
	switch kind {
	case "plan":
		_ = applyTrackerColumn(t, trackerColDoing)
		t.Error = "No code in repository"
		appendTrackerStep(t, "Agent submitted plan without code")
	case "canceled":
		_ = applyTrackerColumn(t, trackerColCanceled)
		appendTrackerStep(t, "Cancelled via notification")
	case "done":
		fromCol := strings.ToLower(strings.TrimSpace(t.DevColumn))
		trackerRecordPhaseSummary(t, fromCol, text)
		col := trackerNotifyDoneColumn(*t)
		if col != fromCol {
			_ = applyTrackerColumn(t, col)
		}
		t.Error = ""
		appendTrackerStep(t, "Agent delivered result")
	case "error":
		t.Status = "error"
		if text != "" {
			t.Error = text
		}
		appendTrackerStep(t, "Error")
	case "started":
		status := strings.ToLower(strings.TrimSpace(t.Status))
		if t.DevColumn == trackerColTodo || status == "pending" || status == "scheduled" {
			_ = applyTrackerColumn(t, trackerColDoing)
			appendTrackerStep(t, "Moved to doing via notification")
		}
	default:
		if text != "" {
			appendTrackerStep(t, "Updated result")
		}
	}
}
