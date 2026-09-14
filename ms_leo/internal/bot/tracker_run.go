package bot

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"leo-bot/internal/database"
)

var (
	trackerSprintPrefixRe = regexp.MustCompile(`(?i)^\[Спринт\s+\d+\]\s*`)
	trackerTaskNumLineRe  = regexp.MustCompile(`(?i)^Задача\s*#\s*\d+\.?$`)
)

// Как часто смотрим, не пора ли снять карточку с «Ожидает».
const trackerDueTick = 15 * time.Second

// Раз в сутки перезапускаем застрявших агентов в обход лимита попыток:
// карточка, у которой сгорели все 5 попыток, иначе висит «В работе» вечно.
const trackerStuckDailyKick = 24 * time.Hour

// trackerTaskDueForStart — та же развилка, что в ClaimDueTrackerTasks:
// только очередь и только если when_at уже наступил.
func trackerTaskDueForStart(t database.TrackerTask, now time.Time) bool {
	if trackerAwaitingApproval(t) {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	if status != "pending" && status != "scheduled" {
		return false
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col == "" {
		col = trackerColTodo
	}
	if col != trackerColTodo {
		return false
	}
	if t.WhenAt.IsZero() {
		return true
	}
	return !t.WhenAt.After(now)
}

func trackerDueStartedNote(t database.TrackerTask) string {
	n := t.Num
	if n <= 0 {
		n = int(t.ID)
	}
	prompt := strings.TrimSpace(t.Prompt)
	if runes := []rune(prompt); len(runes) > 180 {
		prompt = string(runes[:180]) + "…"
	}
	text := fmt.Sprintf("🔧 %s началась.", trackerNotifyLabel(n, trackerTaskTitle(t.Prompt)))
	if prompt != "" {
		text += "\n" + prompt
	}
	return text
}

// startTrackerDueScheduler — фоновый цикл: созревшие карточки сами уходят
// из «Ожидает» в «В работе». Первый прогон сразу после старта, чтобы уже
// просроченные задачи (как #1 с 09:19) не ждали ещё один тик.
func (b *Bot) startTrackerDueScheduler(ctx context.Context) {
	b.runDueTrackerTasks()
	ticker := time.NewTicker(trackerDueTick)
	defer ticker.Stop()
	daily := time.NewTicker(trackerStuckDailyKick)
	defer daily.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runDueTrackerTasks()
		case <-daily.C:
			if n := b.kickStuckTrackerAgents(true); n > 0 && b.logger != nil {
				b.logger.Infof("трекер: суточный перезапуск застрявших агентов: %d", n)
			}
		}
	}
}

func (b *Bot) runDueTrackerTasks() {
	_, _ = b.claimAndNotifyDueTrackerTasks()
}

// claimAndNotifyDueTrackerTasks — то, что делает «Обновить» на доске:
// снимает созревшие с «Ожидает». В личку не пишет.
func (b *Bot) claimAndNotifyDueTrackerTasks() (int, error) {
	return b.claimAndKickTrackerTasks(false)
}

func (b *Bot) claimAndKickTrackerTasks(forceStuck bool) (int, error) {
	if b == nil || b.db == nil {
		return 0, fmt.Errorf("база недоступна")
	}
	due, err := b.db.ClaimDueTrackerTasks(time.Now())
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не забрать созревшие задачи: %v", err)
		}
		return 0, err
	}
	healed, herr := b.healTrackerCardsFromStoredResult()
	if herr != nil && b.logger != nil {
		b.logger.Warnf("трекер: не додвинуть карточки по сохранённому результату: %v", herr)
	}
	for _, t := range due {
		if b.logger != nil {
			b.logger.Infof("трекер: задача #%d началась по расписанию", trackerDueNum(t))
		}
		// В личку не пишем: старт — промежуточный статус. Уведомление
		// будет только когда задача выедет на Railway в main.
		b.dispatchTrackerAgent(t, "doing")
	}
	kicked := b.kickStuckTrackerAgents(forceStuck)
	resumed := b.resumeTrackerStandBuilds()
	return len(due) + healed + kicked + resumed, nil
}

func (b *Bot) resumeTrackerStandBuilds() int {
	if b == nil || b.db == nil {
		return 0
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не прочитать сборки: %v", err)
		}
		return 0
	}
	n := 0
	for _, t := range list {
		col := strings.ToLower(strings.TrimSpace(t.DevColumn))
		if col != trackerColDeploy {
			continue
		}
		if t.Status == "canceled" || t.Status == "done" {
			continue
		}
		if !trackerTaskHasCode(t) && !trackerTaskShippedToStand(t) {
			continue
		}
		if b.logger != nil {
			b.logger.Infof("трекер: дожимаем сборку #%d", trackerDueNum(t))
		}
		go b.finishTrackerBuild(t.ID)
		n++
	}
	return n
}

func (b *Bot) kickStuckTrackerAgents(force bool) int {
	if b == nil || b.db == nil {
		return 0
	}
	list, err := b.db.ListTrackerTasks()
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не прочитать зависшие задачи: %v", err)
		}
		return 0
	}
	now := time.Now()
	n := 0
	for _, t := range list {
		if trackerNeedsPhaseKick(t, now, force) {
			if b.logger != nil {
				b.logger.Infof("трекер: снова запускаем %s #%d", t.DevColumn, trackerDueNum(t))
			}
			b.kickTrackerPipeline(t)
			n++
			continue
		}
		if !trackerNeedsAgentKick(t, now, force) {
			continue
		}
		if b.trackerPipelineBusy(t.ID) {
			b.dispatchTrackerAgent(t, "doing")
			continue
		}
		t.Error = ""
		_ = applyTrackerColumn(&t, trackerColDoing)
		appendTrackerStep(&t, "Снова запускаем агента")
		if err := b.db.SaveTrackerTask(t); err != nil {
			if b.logger != nil {
				b.logger.Warnf("трекер: не перезапустить #%d: %v", trackerDueNum(t), err)
			}
			continue
		}
		if b.logger != nil {
			b.logger.Infof("трекер: снова запускаем агента #%d", trackerDueNum(t))
		}
		b.dispatchTrackerAgent(t, "doing")
		n++
	}
	return n
}

// Ревью/тест без агента: карточка приехала, а старт упёрся в занятый
// конвейер. Повтора не было — #107 стояла в «Ревью» и держала всю очередь.
const trackerPhaseKickWait = 10 * time.Minute

// trackerNeedsPhaseKick — карточка в «Ревью»/«Тесте», агент фазы после
// сдачи не запускался. «не принято» ждёт человека — не трогаем.
func trackerNeedsPhaseKick(t database.TrackerTask, now time.Time, force bool) bool {
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col != trackerColReview && col != trackerColTest {
		return false
	}
	status := strings.ToLower(strings.TrimSpace(t.Status))
	if status == "done" || status == "canceled" {
		return false
	}
	if col == trackerColTest && t.ManualQa {
		return false
	}
	arrived := false
	for i := len(t.Steps) - 1; i >= 0; i-- {
		low := strings.ToLower(strings.TrimSpace(t.Steps[i]))
		if strings.Contains(low, "запустили") || strings.Contains(low, "не принято") {
			return false
		}
		if low == "агент сдал результат" {
			arrived = true
			break
		}
	}
	if !arrived {
		return false
	}
	if force || !t.HasLastRun {
		return true
	}
	return now.Sub(t.LastRunAt) >= trackerPhaseKickWait
}

func trackerDueNum(t database.TrackerTask) int {
	if t.Num > 0 {
		return t.Num
	}
	return int(t.ID)
}

// trackerTaskTitle — короткое название для уведомлений: первая строка prompt.
func trackerTaskTitle(prompt string) string {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" {
		return ""
	}
	if m := trackerSprintPrefixRe.FindString(prompt); m != "" {
		prompt = strings.TrimSpace(prompt[len(m):])
	}
	for _, line := range strings.Split(prompt, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if trackerTaskNumLineRe.MatchString(line) {
			continue
		}
		return clipTrackerNotifyTitle(line)
	}
	return clipTrackerNotifyTitle(prompt)
}

func trackerNotifyLabel(n int, title string) string {
	title = strings.TrimSpace(title)
	if title == "" {
		return fmt.Sprintf("Задача #%d", n)
	}
	return fmt.Sprintf("Задача #%d: %s", n, title)
}

func trackerNotifyHeading(t database.TrackerTask) string {
	return trackerNotifyLabel(trackerDueNum(t), trackerTaskTitle(t.Prompt))
}

// trackerTaskAuthorLabel — кто поставил задачу: Лео, админ или «из чата».
func (b *Bot) trackerTaskAuthorLabel(t database.TrackerTask) string {
	if !t.HasAuthor {
		return "Из чата"
	}
	if t.AuthorID == database.TrackerLeoAuthorID || t.Kind == "leo_task" {
		return "Лео"
	}
	if t.AuthorID <= 0 {
		return "Из чата"
	}
	if b != nil && b.db != nil {
		people, err := b.db.AdminPeopleByIDs(b.adminPackChatID(), []int64{t.AuthorID})
		if err == nil && len(people) > 0 {
			name := strings.TrimSpace(people[0].DisplayName)
			if name == "" {
				name = strings.TrimSpace(people[0].Username)
			}
			if name != "" {
				if !strings.HasPrefix(name, "@") && adminLooksLikeTelegramHandle(name) {
					return "@" + name
				}
				return name
			}
		}
	}
	return fmt.Sprintf("id %d", t.AuthorID)
}

func clipTrackerNotifyTitle(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= 80 {
		return string(r)
	}
	return string(r[:80]) + "…"
}

// kickTrackerDueIfReady — после постановки/переноса: если срок уже сейчас,
// не ждём тик (иначе «дата в прошлом» висела бы до 15 секунд).
func (b *Bot) kickTrackerDueIfReady(at time.Time) {
	if b == nil || at.After(time.Now()) {
		return
	}
	go b.runDueTrackerTasks()
}
