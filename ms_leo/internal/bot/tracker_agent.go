package bot

import (
	"bytes"
	"encoding/json"
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
var trackerCommitRe = regexp.MustCompile(`(?i)коммит(?:\s+выполнения)?\s+([0-9a-f]{7,40})`)

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
		return "Composer-ревью"
	case "test":
		return "Composer-тест"
	default:
		return "Агент"
	}
}

const trackerAgentKickCooldown = 90 * time.Second
const trackerAgentClaimWait = 45 * time.Second
const trackerAgentKickMax = 5
const trackerAgentWaitingStep = "ждёт очередь"

func trackerLastStep(t database.TrackerTask) string {
	if n := len(t.Steps); n > 0 {
		return strings.TrimSpace(t.Steps[n-1])
	}
	return ""
}

func trackerAgentKickCount(t database.TrackerTask) int {
	n := 0
	for _, s := range t.Steps {
		if strings.Contains(strings.ToLower(s), "снова запускаем агента") {
			n++
		}
	}
	return n
}

// trackerAgentStartFailed — агент уже отработал и сорвался. Старый агент:#N
// после этого мёртв: иначе kick видит remote id и больше не стартует.
func trackerAgentStartFailed(t database.TrackerTask) bool {
	err := strings.ToLower(t.Error)
	last := strings.ToLower(trackerLastStep(t))
	if strings.Contains(err, "агент не стартовал") || last == "агент не стартовал" {
		return true
	}
	if strings.Contains(err, "только заметка") || strings.Contains(err, "нет правок") {
		return true
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	return status == "error" && (col == trackerColDoing || col == "")
}

// trackerNeedsAgentKick — карточка в «В работе», а живого агента нет:
// срыв старта (в том числе «только заметка» при уже записанном агент:#N),
// два аппрува без запуска, или очередь так и не дошла.
func trackerNeedsAgentKick(t database.TrackerTask, now time.Time, force bool) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col != trackerColDoing && status != "running" {
		return false
	}
	if col != "" && col != trackerColDoing {
		return false
	}
	failed := trackerAgentStartFailed(t)
	if trackerStepRemoteID(t.Steps) > 0 && !failed {
		return false
	}
	if !force && trackerAgentKickCount(t) >= trackerAgentKickMax {
		return false
	}
	if failed {
		if force {
			return true
		}
		return !t.HasLastRun || now.Sub(t.LastRunAt) >= trackerAgentKickCooldown
	}
	// Remote ещё нет: аппрув, «ждёт очередь» или create ещё летит.
	// Пустую карточку без last_run не трогаем — так выглядит свежий claim
	// в том же тике, пока шаги ещё не доехали в list.
	if !t.HasLastRun {
		last := strings.ToLower(trackerLastStep(t))
		return strings.Contains(last, "в работу") ||
			last == strings.ToLower(trackerAgentWaitingStep) ||
			strings.Contains(last, "агент: запустили")
	}
	return now.Sub(t.LastRunAt) >= trackerAgentClaimWait
}

func trackerStepRemoteID(steps []string) int64 {
	for i := len(steps) - 1; i >= 0; i-- {
		s := strings.TrimSpace(steps[i])
		if !strings.HasPrefix(s, "агент:#") {
			continue
		}
		n, err := strconv.ParseInt(strings.TrimPrefix(s, "агент:#"), 10, 64)
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
		return fmt.Sprintf(`Ревью задачи #%d: принята ли именно эта фича, а не любая правка.

Формулировка:
%s

Что сдал агент:
%s

«можно на тест» только если в коде есть то, что просили (для доната — номинал в config.go, файл не заглушка).
«ревью не принято» если коммита нет, только заметка, config.go обрезан или номинала из задачи нет.`, n, prompt, result)
	case "test":
		return fmt.Sprintf(`Тест задачи #%d: фича из формулировки реально в коде.

Формулировка:
%s

Результат и ревью:
%s

«тест пройден» только если в config.go есть нужный номинал и файл собирается (есть parseAmountTiers и getEnv).
«тест не прошёл» если ветки нет, файла-заглушка или номинала из задачи нет.`, n, prompt, result)
	default:
		text := prompt
		if n > 0 {
			text = fmt.Sprintf("Задача #%d.\n\n%s", n, prompt)
		}
		if result != "" {
			if strings.Contains(result, "Логи сборки Railway") ||
				strings.Contains(strings.ToLower(result), "сборка на стенде не прошла") {
				text += "\n\nСборка Railway упала. Почини код по логам точечно, инструментами. Не возвращай полный текст файлов JSON-ом.\n" + result
			} else {
				text += "\n\nПрошлый результат / замечания ревью:\n" + result
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
	return strings.Contains(low, "посредственн") ||
		strings.Contains(low, "минимальный тест") ||
		strings.Contains(low, "дымовая") ||
		strings.Contains(low, "только заметка")
}

func trackerComposerPassed(phase, text string) bool {
	low := strings.ToLower(text)
	if trackerVerdictIsFakePass(text) {
		return false
	}
	switch phase {
	case "review":
		if strings.Contains(low, "ревью не принято") || strings.Contains(low, `"pass":false`) ||
			strings.Contains(low, "нельзя на тест") {
			return false
		}
		return strings.Contains(low, "можно на тест")
	case "test":
		if strings.Contains(low, "тест не прошёл") || strings.Contains(low, "тест не прошел") ||
			strings.Contains(low, `"pass":false`) {
			return false
		}
		return strings.Contains(low, "тест пройден")
	}
	if strings.Contains(low, "блокер") || strings.Contains(low, "не принято") ||
		strings.Contains(low, "верн") && strings.Contains(low, "работ") {
		return false
	}
	return strings.Contains(low, "выполнен") || strings.Contains(low, "готово") ||
		strings.Contains(low, "completed") || strings.Contains(text, "✅")
}

func trackerPipelineNotify(t database.TrackerTask) string {
	head := trackerNotifyHeading(t)
	if trackerTaskHasCode(t) {
		return fmt.Sprintf("%s: код в репозитории, карточка на публикации.\nЧтобы выкатить на Railway, напиши «запушь».", head)
	}
	return fmt.Sprintf("%s не выехала на Railway.\nАгент сдал только план — теста и сборки не было.\nЧтобы выкатить код, сначала нужен коммит, потом «запушь».", head)
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

// dispatchTrackerAgent — поставить агенту работу по фазе карточки.
// Код пишет внешняя доска (сессия BOARD_SSO_SECRET); ревью и тест — Composer.
func (b *Bot) dispatchTrackerAgent(t database.TrackerTask, phase string) {
	if b == nil || t.ID <= 0 || b.config == nil {
		return
	}
	if b.trackerPipelineBusy(t.ID) {
		if (phase == "" || phase == "doing") && b.db != nil &&
			!strings.EqualFold(trackerLastStep(t), trackerAgentWaitingStep) {
			appendTrackerStep(&t, trackerAgentWaitingStep)
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
		return fmt.Errorf("задача не найдена")
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
	}
	if phase == "doing" {
		_ = applyTrackerColumn(&t, trackerColDoing)
	}
	t.Error = ""
	t.HasLastRun = true
	t.LastRunAt = time.Now()
	appendTrackerStep(&t, trackerAgentName(phase)+": запустили")
	_ = b.db.SaveTrackerTask(t)

	remoteID, raw, err := b.remoteTrackerCreate(t, phase, model)
	if err != nil {
		if phase == "review" || phase == "test" {
			return b.finishTrackerComposerLocal(t, phase, err)
		}
		t.Error = clipNotifyText("Агент не стартовал: " + err.Error())
		appendTrackerStep(&t, "Агент не стартовал")
		return b.db.SaveTrackerTask(t)
	}
	if remoteID > 0 {
		appendTrackerStep(&t, fmt.Sprintf("агент:#%d", remoteID))
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
	trackerRemoteWhen = "сейчас"
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
		return nil, fmt.Errorf("доска агента недоступна: %w", err)
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
				msg = fmt.Sprintf("доска ответила %d", resp.StatusCode)
			}
			return nil, fmt.Errorf("%s", msg)
		}
		if len(env.Data) > 0 {
			return env.Data, nil
		}
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("доска агента: HTTP %d", resp.StatusCode)
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
			{Role: "system", Content: `Ты — ревьюер/тестировщик Fat Leopard.
Ответь JSON без обрамления: {"pass":true/false,"note":"1–3 предложения без эмодзи"}.
pass true только если в коде есть фича из формулировки (для доната — номинал в config.go, не заглушка).
pass false если нет коммита, только заметка, config.go обрезан или номинала нет.`},
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
			note = "Локальный вердикт недоступен: " + err.Error()
			passed = false
		}
	}
	if note == "" {
		note = "Composer недоступен: " + startErr.Error()
		passed = false
	}
	if len([]rune(note)) > 1200 {
		note = string([]rune(note)[:1200])
	}
	label := "Composer: ревью"
	if phase == "test" {
		label = "Composer: тест"
	}
	t.Result = strings.TrimSpace(t.Result + "\n\n" + label + ":\n" + note)
	if passed {
		appendTrackerStep(&t, label+" принято")
		if phase == "review" || phase == "test" {
			stampPhase := phase
			stampLabel := "ревью"
			if phase == "test" {
				stampLabel = "тест"
			}
			if sha, berr, serr := b.stampTrackerCommit(t, stampPhase, note); serr != nil {
				t.Error = "нет коммита " + stampLabel + ": " + serr.Error()
				appendTrackerStep(&t, "коммит "+stampLabel+" не вышел")
				return b.db.SaveTrackerTask(t)
			} else {
				appendTrackerStep(&t, "коммит "+sha+" "+stampLabel)
				if berr != "" {
					appendTrackerStep(&t, "ветка "+berr)
				}
			}
		}
		next := trackerColTest
		if phase == "test" {
			next = trackerColDeploy
		}
		_ = applyTrackerColumn(&t, next)
		if err := b.db.SaveTrackerTask(t); err != nil {
			return err
		}
		b.kickTrackerPipeline(t)
		return nil
	}
	t.Error = clipNotifyText(note)
	appendTrackerStep(&t, label+" не принято")
	if phase == "review" {
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Вернули в работу: ревью не принято")
		if err := b.db.SaveTrackerTask(t); err != nil {
			return err
		}
		b.kickTrackerPipeline(t)
		return nil
	}
	return b.db.SaveTrackerTask(t)
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
		t.Error = "Сборка не запускалась: нет коммита выполнения"
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Сборка не запускалась")
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
		if err != nil {
			t.Error = "не влили в main: " + err.Error()
			t.Result = strings.TrimSpace(t.Result + "\n\nПуш в main не вышел: " + err.Error())
			appendTrackerStep(&t, "пуш на стенд не вышел")
			if trackerFailLooksLikeStub(err.Error(), "") {
				appendTrackerStep(&t, "не вернули в работу: ветка обрезала файлы")
				if serr := b.db.SaveTrackerTask(t); serr != nil && b.logger != nil {
					b.logger.Warnf("трекер: не сохранить срыв пуша #%d: %v", trackerDueNum(t), serr)
				}
				return
			}
			_ = applyTrackerColumn(&t, trackerColDoing)
			appendTrackerStep(&t, "вернули в работу: пуш не вышел")
			if serr := b.db.SaveTrackerTask(t); serr != nil && b.logger != nil {
				b.logger.Warnf("трекер: не сохранить срыв пуша #%d: %v", trackerDueNum(t), serr)
			}
			b.kickTrackerPipeline(t)
			return
		}
		appendTrackerStep(&t, "пуш в "+base)
		appendTrackerStep(&t, "ждём сборку на стенде")
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
				appendTrackerStep(&t, "Railway: собираем "+name)
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
		b.returnTrackerFromFailedStand(&t, fmt.Errorf("нет заказанной сборки Railway"))
		return
	}
	if err := b.waitStandBuild(started, order.Pinned); err != nil {
		b.clearTrackerDeployOrder(t.ID)
		b.returnTrackerFromFailedStand(&t, err)
		return
	}
	b.clearTrackerDeployOrder(t.ID)
	_ = applyTrackerColumn(&t, trackerColDone)
	appendTrackerStep(&t, "стенд собрался")
	t.Error = ""
	if err := b.db.SaveTrackerTask(t); err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не закрыть #%d после стенда: %v", trackerDueNum(t), err)
		}
		return
	}
	b.notifyTrackerShippedOnce(t)
}

func (b *Bot) returnTrackerFromFailedStand(t *database.TrackerTask, waitErr error) {
	if t == nil {
		return
	}
	reason := "сборка не прошла"
	if waitErr != nil && strings.TrimSpace(waitErr.Error()) != "" {
		reason = waitErr.Error()
	}
	logs := ""
	if id := standBuildDeployID(waitErr); id != "" {
		logs = b.railwayDeployLogs(id)
	}
	note := "Сборка на стенде не прошла: " + reason
	if logs != "" {
		note += "\n\nЛоги сборки Railway:\n" + logs
	}
	t.Error = clipNotifyText("сборка на стенде: " + reason)
	t.Result = strings.TrimSpace(t.Result + "\n\n" + note)
	appendTrackerStep(t, "сборка на стенде не прошла")
	if trackerStandFailCount(*t) >= trackerStandMaxRetries {
		appendTrackerStep(t, "сборка не чинится после нескольких попыток")
		if serr := b.db.SaveTrackerTask(*t); serr != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить срыв стенда #%d: %v", trackerDueNum(*t), serr)
		}
		return
	}
	if trackerFailLooksLikeStub(reason, logs) {
		appendTrackerStep(t, "не вернули в работу: сборка упала на заглушке")
		if serr := b.db.SaveTrackerTask(*t); serr != nil && b.logger != nil {
			b.logger.Warnf("трекер: не сохранить срыв стенда #%d: %v", trackerDueNum(*t), serr)
		}
		return
	}
	_ = applyTrackerColumn(t, trackerColDoing)
	appendTrackerStep(t, "вернули в работу: сборка не прошла")
	if serr := b.db.SaveTrackerTask(*t); serr != nil && b.logger != nil {
		b.logger.Warnf("трекер: не сохранить возврат #%d со стенда: %v", trackerDueNum(*t), serr)
		return
	}
	b.kickTrackerPipeline(*t)
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
		return "", "", fmt.Errorf("трекер не настроен")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if secret == "" || baseURL == "" {
		return "", "", fmt.Errorf("трекер не настроен")
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
		return "", "", fmt.Errorf("трекер недоступен: %w", err)
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
		return false, fmt.Errorf("трекер не настроен")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	branch := trackerTaskBranch(t)
	if secret == "" || baseURL == "" || branch == "" {
		return false, fmt.Errorf("нет ветки задачи")
	}
	req, err := http.NewRequest(http.MethodGet, baseURL+"/api/inspect?branch="+url.QueryEscape(branch), nil)
	if err != nil {
		return false, err
	}
	req.Header.Set("X-Tracker-Secret", secret)
	req.Header.Set("Authorization", "Bearer "+secret)
	resp, err := (&http.Client{Timeout: trackerAgentHTTPTimeout}).Do(req)
	if err != nil {
		return false, fmt.Errorf("трекер недоступен: %w", err)
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
		return "", nil, fmt.Errorf("трекер не настроен")
	}
	secret := strings.TrimSpace(b.config.BoardSecret)
	baseURL := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if secret == "" || baseURL == "" {
		return "", nil, fmt.Errorf("трекер не настроен")
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
		return "", nil, fmt.Errorf("трекер недоступен: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	var parsed struct {
		OK     bool              `json:"ok"`
		Base   string            `json:"base"`
		Head   string            `json:"head"`
		Error  string            `json:"error"`
		Pinned map[string]string `json:"pinned"`
	}
	_ = json.Unmarshal(raw, &parsed)
	if resp.StatusCode >= 300 || !parsed.OK {
		msg := strings.TrimSpace(parsed.Error)
		if msg == "" {
			msg = fmt.Sprintf("HTTP %d", resp.StatusCode)
		}
		return "", nil, fmt.Errorf("%s", msg)
	}
	if len(parsed.Pinned) == 0 {
		return "", nil, fmt.Errorf("трекер влил в main, но не заказал сборку Railway")
	}
	base := strings.TrimSpace(parsed.Base)
	if base == "" {
		base = "main"
	}
	return base, parsed.Pinned, nil
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
		return nil, fmt.Errorf("нет такой фазы")
	}
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.dispatchTrackerAgent(t, phase)
	return trackerJSON(map[string]any{"ok": true, "task": trackerTaskView(t, false)})
}
