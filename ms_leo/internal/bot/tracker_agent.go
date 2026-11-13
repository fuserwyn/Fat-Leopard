package bot

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/database"
)

// Ревью и тест гоняет Cursor Composer: он читает код в репозитории,
// а не только формулировку карточки. Реализацию пишет модель доски
// (BOARD_MODEL) или та, что настроена у владельца.
const trackerComposerModelKey = "cursor-composer"

const trackerAgentHTTPTimeout = 45 * time.Second
const trackerShipHTTPTimeout = 90 * time.Second

var trackerBranchRe = regexp.MustCompile(`tracker/\d+-\d+`)
var trackerCommitRe = regexp.MustCompile(`(?i)(?:commit|коммит)(?:\s+(?:execution|выполнения))?\s+([0-9a-f]{7,40})`)

func trackerComposerModel(b *Bot) string {
	if b != nil && b.config != nil {
		if m := strings.TrimSpace(b.config.BoardReviewModel); m != "" {
			return m
		}
	}
	return trackerComposerModelKey
}

func trackerImplModel(b *Bot) string {
	if b != nil && b.config != nil {
		return strings.TrimSpace(b.config.BoardModel)
	}
	return ""
}

// trackerDoingModelKey — модель фазы «в работе».
// По умолчанию и при выборе Claude стартует Claude. Cursor — если админ выбрал
// его на карточке или карточку передали ему после сбоев.
func trackerDoingModelKey(agent string) string {
	if normalizeTrackerAgent(agent) == "cursor" {
		return "cursor"
	}
	return "claude"
}

func normalizeTrackerAgent(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "claude", "cursor":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "auto"
	}
}

func trackerAgentChoiceLabel(agent string) string {
	switch normalizeTrackerAgent(agent) {
	case "claude":
		return "Claude"
	case "cursor":
		return "Cursor"
	default:
		return "Auto"
	}
}

// trackerAgentBoardUserID — кто в сессии create на внешней доске.
// Всегда владелец: иначе гостевой SSO отвечает unauthorized.
func (b *Bot) trackerAgentBoardUserID() int64 {
	if b == nil {
		return 0
	}
	return b.leoBoardUserID()
}

func trackerAgentName(phase string) string {
	switch phase {
	case "review":
		return "Composer review"
	case "test":
		return "Composer test"
	default:
		return "Agent"
	}
}

const trackerAgentKickCooldown = 90 * time.Second
const trackerAgentClaimWait = 45 * time.Second

// Сорванный старт (нет токена, лимит подписки, лежит исполнитель) сам
// перезапускаем не больше trackerAttemptCap раз, пауза растёт до получаса.
// Дальше карточка ждёт человека: #167 сутки крутилась каждые 30 минут и жгла
// токены. «Запустить снова» и «Обновить» (force) перезапускают сразу.
const trackerAgentKickCooldownMax = 30 * time.Minute

// trackerAgentLimitWait — пауза перед повтором, когда Claude упёрся в лимит
// подписки: сессия сбрасывается раз в 5 часов, и повторы через минуты сгорали
// впустую. Три повтора по 2 часа ждут сброса, потом карточка уходит Cursor.
const trackerAgentLimitWait = 2 * time.Hour

// trackerAgentHitLimit — агент сорвался на лимите подписки или тарифа.
func trackerAgentHitLimit(errText string) bool {
	low := strings.ToLower(errText)
	for _, s := range []string{"hit your", "session limit", "weekly limit", "usage limit", "rate limit"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// trackerAgentRetryWait — сколько ждать перед повтором упавшей карточки.
func trackerAgentRetryWait(t database.TrackerTask) time.Duration {
	wait := trackerAgentKickWait(trackerStageKickCount(t))
	if trackerAgentHitLimit(t.Error) && wait < trackerAgentLimitWait {
		return trackerAgentLimitWait
	}
	return wait
}

// trackerAttemptCap — сколько раз сами перезапускаем упавшего исполнителя.
// Дальше автору уходит сигнал, лог пишется в карточку, и если это был не Cursor —
// карточку один раз отдаём Cursor, тоже не больше чем на столько повторов.
const trackerAttemptCap = 3

const (
	trackerCursorHandoffStep     = "Handed off to Cursor after errors"
	trackerAttemptsExhaustedStep = "Attempts exhausted"
	trackerAgentChosenStepPrefix = "Assignee: "
)

// trackerAgentKickWait — сколько ждать перед очередным перезапуском агента.
func trackerAgentKickWait(kicks int) time.Duration {
	wait := trackerAgentKickCooldown
	for i := 0; i < kicks && wait < trackerAgentKickCooldownMax; i++ {
		wait *= 2
	}
	if wait > trackerAgentKickCooldownMax {
		return trackerAgentKickCooldownMax
	}
	return wait
}

const trackerAgentWaitingStep = "waiting for queue"

func trackerLastStep(t database.TrackerTask) string {
	if n := len(t.Steps); n > 0 {
		return strings.TrimSpace(t.Steps[n-1])
	}
	return ""
}

func trackerStepHas(t database.TrackerTask, needle string) bool {
	for _, s := range t.Steps {
		if strings.Contains(s, needle) {
			return true
		}
	}
	return false
}

// trackerCurrentAgentSteps — шаги с момента, когда карточка досталась текущему
// исполнителю: после передачи Cursor или смены исполнителя вручную.
func trackerCurrentAgentSteps(t database.TrackerTask) []string {
	for i := len(t.Steps) - 1; i >= 0; i-- {
		if strings.Contains(t.Steps[i], trackerCursorHandoffStep) || strings.Contains(t.Steps[i], trackerAgentChosenStepPrefix) {
			return t.Steps[i+1:]
		}
	}
	return t.Steps
}

// trackerStageKickCount — повторы текущего исполнителя: новому даём весь лимит заново.
func trackerStageKickCount(t database.TrackerTask) int {
	n := 0
	for _, s := range trackerCurrentAgentSteps(t) {
		if trackerLoHas(s, "restarting agent", "снова запускаем агента") {
			n++
		}
	}
	return n
}

// trackerAgentAttemptsExhausted — текущий исполнитель уже исчерпал повторы и автор об этом знает.
func trackerAgentAttemptsExhausted(t database.TrackerTask) bool {
	for _, s := range trackerCurrentAgentSteps(t) {
		if strings.Contains(s, trackerAttemptsExhaustedStep) {
			return true
		}
	}
	return false
}

// trackerAgentStartFailed — агент уже отработал и сорвался. Старый агент:#N
// после этого мёртв: иначе kick видит remote id и больше не стартует.
func trackerAgentStartFailed(t database.TrackerTask) bool {
	err := strings.ToLower(t.Error)
	last := strings.ToLower(trackerLastStep(t))
	if trackerLoHas(err, "agent failed to start", "агент не стартовал") || trackerLoEq(last, "agent failed to start", "агент не стартовал") {
		return true
	}
	if trackerLoHas(err, "note only", "только заметка") || trackerLoHas(err, "no repo changes", "нет правок") {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	return status == "error" && (col == trackerColDoing || col == "")
}

// trackerNeedsAgentKick — карточка в «В работе», а живого агента нет:
// срыв старта (в том числе «только заметка» при уже записанном агент:#N),
// два аппрува без запуска, или очередь так и не дошла.
// trackerCardInDoing — карточка в «В работе»: отменённые, закрытые и стоящие
// на сборке агента не ждут.
func trackerCardInDoing(t database.TrackerTask) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col != trackerColDoing && status != "running" {
		return false
	}
	return col == "" || col == trackerColDoing
}

func trackerNeedsAgentKick(t database.TrackerTask, now time.Time, force bool) bool {
	if !trackerCardInDoing(t) {
		return false
	}
	failed := trackerAgentStartFailed(t)
	// «ждёт очередь» после прошлой попытки: агент:#N остался в шагах, а живого
	// агента нет. Без этого #104/#105 не будил даже «Обновить».
	waiting := strings.EqualFold(trackerLastStep(t), trackerAgentWaitingStep)
	if trackerStepRemoteID(t.Steps) > 0 && !failed && !waiting {
		return false
	}
	if failed {
		if !force && trackerStageKickCount(t) >= trackerAttemptCap {
			return false
		}
		if force {
			return true
		}
		return !t.HasLastRun || now.Sub(t.LastRunAt) >= trackerAgentRetryWait(t)
	}
	// Remote ещё нет: аппрув, «ждёт очередь» или create ещё летит.
	// Пустую карточку без last_run не трогаем — так выглядит свежий claim
	// в том же тике, пока шаги ещё не доехали в list.
	if !t.HasLastRun {
		last := strings.ToLower(trackerLastStep(t))
		return trackerLoHas(last, "into doing", "в работу") ||
			last == strings.ToLower(trackerAgentWaitingStep) ||
			trackerLoHas(last, "agent: started", "агент: запустили")
	}
	return now.Sub(t.LastRunAt) >= trackerAgentClaimWait
}

func trackerStepRemoteID(steps []string) int64 {
	for i := len(steps) - 1; i >= 0; i-- {
		s := strings.TrimSpace(steps[i])
		if !strings.HasPrefix(s, "agent:#") && !strings.HasPrefix(s, "агент:#") {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimPrefix(s, "agent:#"), "агент:#"), 10, 64)
		if err == nil && n > 0 {
			return n
		}
	}
	return 0
}

func trackerAgentPrompt(t database.TrackerTask, phase string) string {
	n := trackerDueNum(t)
	prompt := strings.TrimSpace(t.Prompt)
	result := strings.TrimSpace(t.Result)
	switch phase {
	case "review":
		return fmt.Sprintf(`Review task #%d: is this the requested feature, not just any change?

Task:
%s

Agent submission:
%s

Reply "ok for test" only if the code includes what was asked (for donations — tier in config.go, not a stub file).
Reply "review rejected" if there is no commit, note only, config.go is truncated, or the task tier is missing.`, n, prompt, result)
	case "test":
		return fmt.Sprintf(`Test task #%d: is the feature from the task really in the code?

Task:
%s

Result and review:
%s

Reply "test passed" only if config.go has the needed tier and builds (parseAmountTiers and getEnv exist).
Reply "test failed" if the branch is missing, the file is a stub, or the task tier is missing.`, n, prompt, result)
	default:
		text := prompt
		if n > 0 {
			text = fmt.Sprintf("Task #%d.\n\n%s", n, prompt)
		}
		if result != "" {
			resultLow := strings.ToLower(result)
			if strings.Contains(result, "Railway build logs") ||
				strings.Contains(result, "Логи сборки Railway") ||
				strings.Contains(resultLow, "stand build failed") ||
				strings.Contains(resultLow, "сборка на стенде не прошла") {
				text += "\n\nRailway build failed. Fix the code from the logs with tools; do not return full file contents as JSON.\n" + result
			} else if strings.Contains(result, trackerTestFailedMarker) {
				text += "\n\nCode is on the task branch, but pre-ship checks rejected it. Fix surgically, rerun the same checks yourself, and resubmit.\n" + result
			} else {
				text += "\n\nPrevious result / review notes:\n" + result
			}
		}
		return text
	}
}

// trackerComposerPassed — вердикт Composer по тексту сдачи.
// Явный провал важнее общего «готово» от доски.
// trackerVerdictIsFakePass — старый трекер закрывал карточку, если есть ветка.
// Так #29/#30/#31 стали «готово» без фичи. Такие тексты больше не двигают доску.
func trackerVerdictIsFakePass(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(low, "посредственн") || strings.Contains(low, "mediocre") ||
		strings.Contains(low, "минимальный тест") || strings.Contains(low, "minimal test") ||
		strings.Contains(low, "дымовая") || strings.Contains(low, "smoke") ||
		strings.Contains(low, "только заметка") || strings.Contains(low, "note only")
}

func trackerComposerPassed(phase, text string) bool {
	low := strings.ToLower(text)
	if trackerVerdictIsFakePass(text) {
		return false
	}
	switch phase {
	case "review":
		if trackerLoHas(text, "review rejected", "ревью не принято") || strings.Contains(low, `"pass":false`) ||
			trackerLoHas(text, "not ok for test", "нельзя на тест") {
			return false
		}
		return trackerLoHas(text, "ok for test", "можно на тест")
	case "test":
		if trackerLoHas(text, "test failed", "тест не прошёл") || trackerLoHas(text, "test failed", "тест не прошел") ||
			strings.Contains(low, `"pass":false`) {
			return false
		}
		return trackerLoHas(text, "test passed", "тест пройден")
	}
	if strings.Contains(low, "блокер") || strings.Contains(low, "blocker") ||
		trackerLoHas(text, "not accepted", "не принято") ||
		(strings.Contains(low, "верн") || strings.Contains(low, "return")) && strings.Contains(low, "работ") {
		return false
	}
	return trackerLoHas(text, "completed", "выполнен") || trackerLoHas(text, "ready", "готово") ||
		strings.Contains(low, "completed") || strings.Contains(text, "✅")
}

func trackerPipelineNotify(t database.TrackerTask) string {
	head := trackerNotifyHeading(t)
	if trackerTaskHasCode(t) {
		return fmt.Sprintf("%s: code is in the repo; card is ready to ship.\nTo deploy on Railway, send «push».", head)
	}
	return fmt.Sprintf("%s did not ship to Railway.\nThe agent only submitted a plan — no test or build.\nTo ship code, commit first, then «push».", head)
}

func trackerLooksLikeNoteOnly(text string) bool {
	low := strings.ToLower(text)
	return strings.Contains(text, "TRACKER_NO_CODE") ||
		strings.Contains(low, "только заметка") ||
		strings.Contains(low, "нет правок приложения") ||
		strings.Contains(low, "репозиторий не менялся") ||
		strings.Contains(low, "кода нет")
}

func trackerTaskHasCode(t database.TrackerTask) bool {
	for _, step := range t.Steps {
		low := strings.ToLower(step)
		if strings.Contains(low, "коммит ") || strings.Contains(low, "ветка ") ||
			strings.HasPrefix(strings.TrimSpace(step), "ветка") {
			return true
		}
	}
	low := strings.ToLower(t.Result)
	return strings.Contains(low, "ветка:") || strings.Contains(low, "код в ветке") ||
		strings.Contains(low, "коммит выполнения") || trackerCommitRe.FindString(t.Result) != ""
}

func trackerTaskCommit(t database.TrackerTask) string {
	for i := len(t.Steps) - 1; i >= 0; i-- {
		if m := trackerCommitRe.FindStringSubmatch(t.Steps[i]); len(m) > 1 {
			return m[1]
		}
	}
	if m := trackerCommitRe.FindStringSubmatch(t.Result); len(m) > 1 {
		return m[1]
	}
	return ""
}

// trackerTaskInPipeline — карточка уже в конвейере (не в «Ожидает»).
// trackerTaskInPipeline — карточка держит конвейер. «В работе» держит только
// с живым агентом: ждущая очередь или сорвавшаяся раньше тоже считалась
// занятой, и карточки ждали друг друга вечно (ревью #107 ждало #104–#106,
// а те — ревью).
func trackerTaskInPipeline(t database.TrackerTask) bool {
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	st := strings.ToLower(strings.TrimSpace(t.Status))
	if st == "done" || st == "canceled" {
		return false
	}
	switch col {
	case trackerColReview, trackerColTest, trackerColDeploy:
		return true
	case trackerColDoing:
		if st == "error" || trackerAgentStartFailed(t) {
			return false
		}
		return !strings.EqualFold(trackerLastStep(t), trackerAgentWaitingStep)
	default:
		return false
	}
}

func (b *Bot) trackerPipelineBusy(exceptID int64) bool {
	if b == nil || b.db == nil {
		return false
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return false
	}
	for _, t := range list {
		if t.ID == exceptID {
			continue
		}
		if trackerTaskInPipeline(t) {
			return true
		}
	}
	return false
}

// trackerKeepInWaitingQueue — конвейер занят: карточка остаётся в «Ожидает».
func trackerKeepInWaitingQueue(t *database.TrackerTask, step string) {
	if t == nil {
		return
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col == trackerColDoing {
		_ = applyTrackerColumn(t, trackerColTodo)
	}
	if step != "" {
		appendTrackerStep(t, step)
		return
	}
	if !strings.EqualFold(trackerLastStep(*t), trackerAgentWaitingStep) {
		appendTrackerStep(t, trackerAgentWaitingStep)
	}
}

// trackerPipelineBusyForPhase — занят ли конвейер для этой фазы карточки.
// Ревью/тест не ждут «В работе» (ms_tracker и так гонит задачи по одной), а
// среди ревью/теста/сборки первой идёт более старая карточка. Иначе две
// карточки в «Ревью» (#104 и #111) ждали друг друга вечно.
func (b *Bot) trackerPipelineBusyForPhase(t database.TrackerTask, phase string) bool {
	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase != "review" && phase != "test" {
		return b.trackerPipelineBusy(t.ID)
	}
	if b == nil || b.db == nil {
		return false
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		return false
	}
	return trackerLaterPhaseBlocks(t, list)
}

func trackerLaterPhaseBlocks(t database.TrackerTask, list []database.TrackerTask) bool {
	for _, other := range list {
		if other.ID == t.ID || other.ID > t.ID {
			continue
		}
		st := strings.ToLower(strings.TrimSpace(other.Status))
		if st == "done" || st == "canceled" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(other.DevColumn)) {
		case trackerColReview, trackerColTest, trackerColDeploy:
			return true
		}
	}
	return false
}

// dispatchTrackerAgent — поставить агенту работу по фазе карточки.
// Код пишет внешняя доска (сессия BOARD_SSO_SECRET); ревью и тест — Composer.
func (b *Bot) dispatchTrackerAgent(t database.TrackerTask, phase string) {
	if b == nil || t.ID <= 0 || b.config == nil {
		return
	}
	if b.trackerPipelineBusyForPhase(t, phase) {
		if (phase == "" || phase == "doing") && b.db != nil {
			trackerKeepInWaitingQueue(&t, "")
			_ = b.db.SaveTrackerTask(t)
		}
		if b.logger != nil {
			b.logger.Infof("трекер: #%d (%s) ждёт очередь", trackerDueNum(t), phase)
		}
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil && b.logger != nil {
				b.logger.Errorf("трекер: паника агента #%d (%s): %v", trackerDueNum(t), phase, rec)
			}
		}()
		if err := b.runTrackerAgent(t.ID, phase); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: агент #%d (%s): %v", trackerDueNum(t), phase, err)
		}
	}()
}

func (b *Bot) runTrackerAgent(taskID int64, phase string) error {
	if b == nil || b.db == nil || taskID <= 0 {
		return fmt.Errorf("task not found")
	}
	t, err := b.db.GetTrackerTask(taskID)
	if err != nil {
		return err
	}
	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase == "" {
		phase = "doing"
	}
	model := trackerImplModel(b)
	if phase == "review" || phase == "test" {
		model = trackerComposerModel(b)
	} else if phase == "doing" || phase == "" {
		model = trackerDoingModelKey(t.Agent)
	}
	if phase == "doing" {
		_ = applyTrackerColumn(&t, trackerColDoing)
	}
	t.Error = ""
	t.HasLastRun = true
	t.LastRunAt = time.Now()
	appendTrackerStep(&t, trackerAgentName(phase)+": started")
	_ = b.db.SaveTrackerTask(t)

	remoteID, raw, err := b.remoteTrackerCreate(t, phase, model)
	if err != nil {
		if phase == "review" || phase == "test" {
			return b.finishTrackerComposerLocal(t, phase, err)
		}
		t.Error = clipNotifyText("Agent failed to start: " + err.Error())
		appendTrackerStep(&t, "Agent failed to start")
		return b.db.SaveTrackerTask(t)
	}
	if remoteID > 0 {
		appendTrackerStep(&t, fmt.Sprintf("agent:#%d", remoteID))
	}
	if note := strings.TrimSpace(raw); note != "" && phase == "doing" {
		t.Error = ""
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return err
	}
	if b.logger != nil {
		b.logger.Infof("трекер: #%d фаза %s ушла агенту (remote=%d)", trackerDueNum(t), phase, remoteID)
	}
	return nil
}

const (
	trackerRemoteWhen = "now"
	trackerBoardAPI   = "/api/scheduled"
)

func (b *Bot) remoteTrackerCreate(t database.TrackerTask, phase, model string) (remoteID int64, note string, err error) {
	payload := map[string]any{
		"when":           trackerRemoteWhen,
		"prompt":         trackerAgentPrompt(t, phase),
		"auto_review":    false,
		"auto_push":      true,
		"phase":          phase,
		"source_task_id": t.ID,
		"source_num":     trackerDueNum(t),
		"branch":         trackerTaskBranch(t),
	}
	if t.HasAuthor {
		payload["author_id"] = t.AuthorID
	}
	if model != "" {
		payload["model_key"] = model
	}
	userID := b.trackerAgentBoardUserID()
	raw, err := b.remoteTrackerRequest("create", 0, payload, userID, trackerAgentName(phase))
	if err != nil {
		return 0, "", err
	}
	var parsed struct {
		ID   int64  `json:"id"`
		When string `json:"when"`
	}
	if json.Unmarshal(raw, &parsed) == nil && parsed.ID > 0 {
		return parsed.ID, parsed.When, nil
	}
	var wrap struct {
		Data struct {
			ID int64 `json:"id"`
		} `json:"data"`
		ID int64 `json:"id"`
	}
	if json.Unmarshal(raw, &wrap) == nil {
		if wrap.Data.ID > 0 {
			return wrap.Data.ID, "", nil
		}
		if wrap.ID > 0 {
			return wrap.ID, "", nil
		}
	}
	return 0, string(raw), nil
}

func (b *Bot) remoteTrackerRequest(op string, taskID int64, payload map[string]any, userID int64, name string) (json.RawMessage, error) {
	if b == nil || b.config == nil {
		return nil, ErrTrackerNotConfigured
	}
	_ = op
	_ = taskID
	secret := strings.TrimSpace(b.config.BoardSecret)
	if secret == "" || strings.TrimSpace(b.config.BoardURL) == "" {
		return nil, ErrTrackerNotConfigured
	}
	if err := b.pingTrackerWire(); err != nil {
		return nil, err
	}
	_ = userID
	_ = name
	if payload == nil {
		payload = map[string]any{}
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(b.config.BoardURL, "/") + trackerBoardAPI
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	client := &http.Client{Timeout: trackerAgentHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("agent board unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	var env struct {
		OK      bool            `json:"ok"`
		Data    json.RawMessage `json:"data"`
		Error   string          `json:"error"`
		Message string          `json:"message"`
	}
	if json.Unmarshal(raw, &env) == nil {
		if env.OK && len(env.Data) > 0 {
			return env.Data, nil
		}
		if !env.OK {
			msg := strings.TrimSpace(env.Message)
			if msg == "" {
				msg = strings.TrimSpace(env.Error)
			}
			if msg == "" {
				msg = fmt.Sprintf("board responded %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if len(env.Data) > 0 {
			return env.Data, nil
		}
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("agent board: HTTP %d", resp.StatusCode)
	}
	return json.RawMessage(raw), nil
}

// finishTrackerComposerLocal — если внешняя доска не взяла ревью/тест,
// вердикт пишет Лео и ставит тот же коммит фазы, что и агент.
func (b *Bot) finishTrackerComposerLocal(t database.TrackerTask, phase string, startErr error) error {
	note := ""
	passed := false
	if b.aiClient != nil {
		raw, err := b.aiClient.Chat([]ai.ChatMessage{
			{Role: "system", Content: `You are the Fat Leopard reviewer/tester.
Reply with JSON only: {"pass":true/false,"note":"1–3 sentences, no emoji"}.
pass true only if the code has the feature from the task (for donations — tier in config.go, not a stub).
pass false if there is no commit, note only, config.go is truncated, or the tier is missing.`},
			{Role: "user", Content: trackerAgentPrompt(t, phase)},
		}, "")
		if err == nil {
			note = strings.TrimSpace(raw)
			if block := leoJSONBlock.FindString(raw); block != "" {
				var parsed struct {
					Pass bool   `json:"pass"`
					Note string `json:"note"`
				}
				if json.Unmarshal([]byte(block), &parsed) == nil {
					passed = parsed.Pass
					if strings.TrimSpace(parsed.Note) != "" {
						note = strings.TrimSpace(parsed.Note)
					}
				}
			}
			if !passed {
				passed = trackerComposerPassed(phase, note)
			}
		} else {
			note = "Local verdict unavailable: " + err.Error()
			passed = false
		}
	}
	if note == "" {
		note = "Composer unavailable: " + startErr.Error()
		passed = false
	}
	if len([]rune(note)) > 1200 {
		note = string([]rune(note)[:1200])
	}
	label := "Composer: review"
	if phase == "test" {
		label = "Composer: test"
	}
	t.Result = strings.TrimSpace(t.Result + "\n\n" + label + ":\n" + note)
	if passed {
		appendTrackerStep(&t, label+" accepted")
		if phase == "review" || phase == "test" {
			stampPhase := phase
			stampLabel := "review"
			if phase == "test" {
				stampLabel = "test"
			}
			if sha, berr, serr := b.stampTrackerCommit(t, stampPhase, note); serr != nil {
				t.Error = "no " + stampLabel + " commit: " + serr.Error()
				appendTrackerStep(&t, "commit "+stampLabel+" failed")
				return b.db.SaveTrackerTask(t)
			} else {
				appendTrackerStep(&t, "commit "+sha+" "+stampLabel)
				if berr != "" {
					appendTrackerStep(&t, "branch "+berr)
				}
			}
		}
		next := trackerColDeploy
		if phase == "review" {
			next = trackerColumnAfterReview(t)
		}
		_ = applyTrackerColumn(&t, next)
		if err := b.db.SaveTrackerTask(t); err != nil {
			return err
		}
		b.kickTrackerPipeline(t)
		return nil
	}
	t.Error = clipNotifyText(note)
	if phase == "review" {
		appendTrackerStep(&t, label+" not accepted")
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Returned to doing: review rejected")
		if err := b.db.SaveTrackerTask(t); err != nil {
			return err
		}
		b.kickTrackerPipeline(t)
		return nil
	}
	returnTrackerFromFailedTest(&t, note)
	if err := b.db.SaveTrackerTask(t); err != nil {
		return err
	}
	b.kickTrackerPipeline(t)
	return nil
}

// kickTrackerPipeline — следующая фаза после сдачи: Composer или сборка.
func (b *Bot) kickTrackerPipeline(t database.TrackerTask) {
	if b == nil || t.ID <= 0 {
		return
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	switch col {
	case trackerColDoing:
		b.dispatchTrackerAgent(t, "doing")
	case trackerColReview:
		b.dispatchTrackerAgent(t, "review")
	case trackerColTest:
		if t.ManualQa {
			return
		}
		b.dispatchTrackerAgent(t, "test")
	case trackerColDeploy:
		if b.config == nil {
			return
		}
		go b.finishTrackerBuild(t.ID)
	}
}

func (b *Bot) finishTrackerBuild(taskID int64) {
	if b == nil || b.db == nil || taskID <= 0 {
		return
	}
	if !tryBeginTrackerStand(taskID) {
		return
	}
	defer endTrackerStand(taskID)
	defer func() {
		if rec := recover(); rec != nil && b.logger != nil {
			b.logger.Errorf("трекер: паника сборки #%d: %v", taskID, rec)
		}
	}()
	t, err := b.db.GetTrackerTask(taskID)
	if err != nil {
		return
	}
	if t.Status == "canceled" || t.DevColumn == trackerColCanceled {
		return
	}
	if t.Status == "done" || t.DevColumn == trackerColDone {
		return
	}
	if !trackerTaskHasCode(t) {
		t.Error = "Build did not start: no execution commit"
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Build was not started")
		if err := b.db.SaveTrackerTask(t); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не вернуть #%d с фейковой сборки: %v", trackerDueNum(t), err)
		}
		return
	}
	_ = applyTrackerColumn(&t, trackerColDeploy)
	started := time.Now()
	var shippedPinned map[string]string
	if !trackerTaskShippedToStand(t) {
		base, pinned, err := b.shipTrackerToMain(t)
		shippedPinned = pinned
		// Ветка уже в main, не вышел только заказ сборки (нет access к Railway):
		// код не виноват, в работу не возвращаем — дальше проверяем стенд сами.
		var deployErr *trackerShipDeployError
		if errors.As(err, &deployErr) {
			base = deployErr.Base
			appendTrackerStep(&t, "Railway rejected build order: "+deployErr.Msg)
			err = nil
		}
		if err != nil {
			t.Error = "merge to main failed: " + err.Error()
			t.Result = strings.TrimSpace(t.Result + "\n\nPush to main failed: " + err.Error())
			appendTrackerStep(&t, "stand push failed")
			if trackerFailLooksLikeStub(err.Error(), "") {
				appendTrackerStep(&t, "kept in deploy: branch trimmed files")
				if serr := b.db.SaveTrackerTask(t); serr != nil && b.logger != nil {
					b.logger.Warnf("трекер: не сохранить срыв пуша #%d: %v", trackerDueNum(t), serr)
				}
				return
			}
			_ = applyTrackerColumn(&t, trackerColDoing)
			appendTrackerStep(&t, "returned to doing: push failed")
			if serr := b.db.SaveTrackerTask(t); serr != nil && b.logger != nil {
				b.logger.Warnf("трекер: не сохранить срыв пуша #%d: %v", trackerDueNum(t), serr)
			}
			b.kickTrackerPipeline(t)
			return
		}
		appendTrackerStep(&t, "push to "+base)
		appendTrackerStep(&t, "waiting for stand build")
		t.Error = ""
		if err := b.db.SaveTrackerTask(t); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить сборку #%d: %v", trackerDueNum(t), err)
		}
	}
	// Сборку заказываем сами, а не ждём вебхук GitHub → Railway: он мог быть
	// снят вместе с автосборкой сервиса, и тогда ждать было бы нечего.
	// Заказ помним: сюда же возвращается планировщик каждые 15 секунд, а
	// пересборка ms_leo перезапускает нас самих — иначе вышел бы вечный круг.
	order := b.loadTrackerDeployOrder(t.ID)
	if !order.Ordered {
		if len(shippedPinned) > 0 {
			order = trackerDeployOrder{Ordered: true, Pinned: shippedPinned}
			for name := range shippedPinned {
				appendTrackerStep(&t, "Railway: building "+name)
			}
		} else {
			order = b.startTrackerDeploy(&t)
		}
		b.saveTrackerDeployOrder(t.ID, order)
		if err := b.db.SaveTrackerTask(t); err != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить заказ сборки #%d: %v", trackerDueNum(t), err)
		}
	}
	if len(order.Pinned) == 0 {
		b.clearTrackerDeployOrder(t.ID)
		b.closeTrackerShippedUnwatched(&t)
		return
	}
	if err := b.waitStandBuild(started, order.Pinned); err != nil {
		b.clearTrackerDeployOrder(t.ID)
		b.stopTrackerOnFailedStand(&t, err)
		return
	}
	b.clearTrackerDeployOrder(t.ID)
	_ = applyTrackerColumn(&t, trackerColDone)
	appendTrackerStep(&t, "stand build succeeded")
	t.Error = ""
	if err := b.db.SaveTrackerTask(t); err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не закрыть #%d после стенда: %v", trackerDueNum(t), err)
		}
		return
	}
	b.notifyTrackerShippedOnce(t)
}

// closeTrackerShippedUnwatched — код уже в main, но заказать или проверить сборку
// на Railway не вышло (нет access). Возвращать в работу нечего: агент
// переделывал готовое, ревью не находило нового коммита, и карточка крутилась (#171).
func (b *Bot) closeTrackerShippedUnwatched(t *database.TrackerTask) {
	if t == nil {
		return
	}
	_ = applyTrackerColumn(t, trackerColDone)
	appendTrackerStep(t, trackerShippedUnwatchedStep)
	t.Error = ""
	t.Result = strings.TrimSpace(t.Result + "\n\nMerged to main. Tracker could not verify the Railway build: " +
		"Railway will build from the push to main. Check RAILWAY_API_TOKEN for ms_leo and ms-tracker.")
	if err := b.db.SaveTrackerTask(*t); err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не закрыть #%d без стенда: %v", trackerDueNum(*t), err)
		}
		return
	}
	b.notifyTrackerShippedOnce(*t)
}

const trackerShippedUnwatchedStep = "merged to main, Railway build unchecked"

// trackerShipDeployError — трекер влил ветку в main, но сборку на Railway не заказал.
type trackerShipDeployError struct {
	Base string
	Msg  string
}

func (e *trackerShipDeployError) Error() string {
	return "merged into " + e.Base + ", but Railway build was not ordered: " + e.Msg
}

// trackerStandFailedStep — сборка на стенде упала, карточка ждёт человека.
const trackerStandFailedStep = "build failed: awaiting human"

// stopTrackerOnFailedStand — сборка на стенде упала. Задачу заново не делаем:
// агент переписывал готовый код по логам сборки и крутил карточку. Карточка
// остаётся в «Сборке» с ошибкой, автору уходит сообщение с логами, дальше —
// «Запустить снова» руками.
func (b *Bot) stopTrackerOnFailedStand(t *database.TrackerTask, waitErr error) {
	if t == nil {
		return
	}
	reason := "build failed"
	if waitErr != nil && strings.TrimSpace(waitErr.Error()) != "" {
		reason = waitErr.Error()
	}
	logs := ""
	if id := standBuildDeployID(waitErr); id != "" {
		logs = b.railwayDeployLogs(id)
	}
	note := "Stand build failed: " + reason
	if logs != "" {
		note += "\n\nRailway build logs:\n" + logs
	}
	t.Error = clipNotifyText("stand build: " + reason)
	t.Result = strings.TrimSpace(t.Result + "\n\n" + note)
	appendTrackerStep(t, "stand build failed")
	if trackerFailLooksLikeStub(reason, logs) {
		appendTrackerStep(t, "kept in deploy: stub build failed")
		if serr := b.db.SaveTrackerTask(*t); serr != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить срыв стенда #%d: %v", trackerDueNum(*t), serr)
		}
		return
	}
	t.Status = "error"
	appendTrackerStep(t, trackerStandFailedStep)
	if serr := b.db.SaveTrackerTask(*t); serr != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не сохранить срыв стенда #%d: %v", trackerDueNum(*t), serr)
		}
		return
	}
	b.notifyTrackerAttempt(*t, trackerStandFailedNotice(trackerDueNum(*t), reason, logs))
}

// trackerStandFailedNotice — сообщение автору: сборка упала, код уже в main.
func trackerStandFailedNotice(num int, reason, logs string) string {
	text := fmt.Sprintf("Task #%d: stand build failed — %s.\n\n"+
		"Code is already on main; Railway kept the previous version. The task won't auto-restart: "+
		"fix the build or tap «Run again» on the card.", num, reason)
	if strings.TrimSpace(logs) != "" {
		text += "\n\nRailway build logs:\n" + logs
	}
	return clipNotifyText(text)
}

func trackerTaskBranch(t database.TrackerTask) string {
	if m := trackerBranchRe.FindAllString(t.Result, -1); len(m) > 0 {
		return m[len(m)-1]
	}
	for i := len(t.Steps) - 1; i >= 0; i-- {
		if m := trackerBranchRe.FindString(t.Steps[i]); m != "" {
			return m
		}
	}
	return ""
}

func (b *Bot) stampTrackerCommit(t database.TrackerTask, phase, note string) (sha, branch string, err error) {
	if b == nil || b.config == nil {
		return "", "", fmt.Errorf("tracker not configured")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if secret == "" || baseURL == "" {
		return "", "", fmt.Errorf("tracker not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"source_task_id": t.ID,
		"source_num":     trackerDueNum(t),
		"phase":          phase,
		"note":           note,
		"prompt":         t.Prompt,
		"branch":         trackerTaskBranch(t),
	})
	if err != nil {
		return "", "", err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/stamp", bytes.NewReader(payload))
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	client := &http.Client{Timeout: trackerShipHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("tracker unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK        bool   `json:"ok"`
		Commit    string `json:"commit"`
		Branch    string `json:"branch"`
		Committed bool   `json:"committed"`
		Error     string `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode >= 300 || !parsed.OK || !parsed.Committed {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", "", fmt.Errorf("%s", msg)
	}
	return parsed.Commit, parsed.Branch, nil
}

func (b *Bot) inspectTrackerBranch(t database.TrackerTask) (bool, error) {
	if b == nil || b.config == nil {
		return false, fmt.Errorf("tracker not configured")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	branch := trackerTaskBranch(t)
	if secret == "" || baseURL == "" || branch == "" {
		return false, fmt.Errorf("no task branch")
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/inspect?branch="+url.QueryEscape(branch), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: trackerAgentHTTPTimeout}).Do(req)
	if err != nil {
		return false, fmt.Errorf("tracker unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK      bool   `json:"ok"`
		HasImpl bool   `json:"has_impl"`
		Exists  bool   `json:"exists"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return false, err
	}
	if resp.StatusCode >= 300 || !parsed.OK {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return false, fmt.Errorf("%s", msg)
	}
	return parsed.Exists && parsed.HasImpl, nil
}

func (b *Bot) shipTrackerToMain(t database.TrackerTask) (string, map[string]string, error) {
	if b == nil || b.config == nil {
		return "", nil, fmt.Errorf("tracker not configured")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if secret == "" || baseURL == "" {
		return "", nil, fmt.Errorf("tracker not configured")
	}
	payload, err := json.Marshal(map[string]any{
		"source_task_id": t.ID,
		"source_num":     trackerDueNum(t),
		"branch":         trackerTaskBranch(t),
		"prompt":         t.Prompt,
	})
	if err != nil {
		return "", nil, err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/ship", bytes.NewReader(payload))
	if err != nil {
		return "", nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	client := &http.Client{Timeout: trackerShipHTTPTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return "", nil, fmt.Errorf("tracker unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK     bool              `json:"ok"`
		Base   string            `json:"base"`
		Head   string            `json:"head"`
		Error  string            `json:"error"`
		Merged bool              `json:"merged"`
		Pinned map[string]string `json:"pinned"`
	}
	_ = json.Unmarshal(raw, &parsed)
	base := strings.TrimSpace(parsed.Base)
	if base == "" {
		base = "main"
	}
	if resp.StatusCode >= 300 || !parsed.OK {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		if parsed.Merged {
			return "", nil, &trackerShipDeployError{Base: base, Msg: msg}
		}
		return "", nil, fmt.Errorf("%s", msg)
	}
	if len(parsed.Pinned) == 0 {
		return "", nil, &trackerShipDeployError{Base: base, Msg: "Railway returned no build ids"}
	}
	return base, parsed.Pinned, nil
}

// trackerAskHTTPTimeout — чуть больше потолка ответа в трекере (3 минуты).
const trackerAskHTTPTimeout = 200 * time.Second

// trackerAskClaude — один ответ Claude текстом через ms_tracker (/api/ask):
// там стоит Claude Agent SDK с доступом по подписке.
func (b *Bot) trackerAskClaude(system, prompt, model string) (string, error) {
	if b == nil || b.config == nil {
		return "", fmt.Errorf("tracker not configured")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if secret == "" || baseURL == "" {
		return "", fmt.Errorf("tracker not configured")
	}
	payload, err := json.Marshal(map[string]string{"system": system, "prompt": prompt, "model": model})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/api/ask", bytes.NewReader(payload))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: trackerAskHTTPTimeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("tracker unavailable: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK    bool   `json:"ok"`
		Text  string `json:"text"`
		Error string `json:"error"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode >= 300 || !parsed.OK {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", fmt.Errorf("%s", msg)
	}
	text := strings.TrimSpace(parsed.Text)
	if text == "" {
		return "", fmt.Errorf("empty response")
	}
	return text, nil
}

func trackerNotifyAuthor(t database.TrackerTask) int64 {
	if t.HasAuthor {
		return t.AuthorID
	}
	return 0
}

func (b *Bot) localTrackerComposer(taskID int64, payload map[string]any, phase string) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	switch phase {
	case "review":
		_ = applyTrackerColumn(&t, trackerColReview)
	case "test":
		_ = applyTrackerColumn(&t, trackerColTest)
	default:
		return nil, fmt.Errorf("unknown phase")
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.dispatchTrackerAgent(t, phase)
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}
