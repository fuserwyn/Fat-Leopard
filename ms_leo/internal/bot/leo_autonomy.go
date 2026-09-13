package bot

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/database"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Автономный Лео: раз в несколько часов он сам придумывает спринт — тему и
// несколько задач — и ставит их на нашу доску. На карточках его аватарка.
//
// Зачем состояние в базе, а не флаг в окружении: включает и выключает режим
// админ прямо из трекера, с выбором, сколько дней Лео работает сам. Срок —
// обязательная часть: эксперимент, который некому остановить, останавливать
// приходится руками в базе.

const (
	// Проверяем чаще, чем интервал: иначе включённый режим ждал бы полный
	// интервал впустую после рестарта сервиса.
	leoAutonomyTick = 5 * time.Minute
	// Больше двух недель подряд без человека — это уже не эксперимент.
	leoAutonomyMaxDays  = 14
	leoAutonomyMaxTasks = 5
)

const leoAutonomySprintPrompt = `Ты — Лео, суровый и остроумный леопард, тренер стаи Fat Leopard.
Ты сам ведёшь продукт: мини-апп стаи (лента тренировок, комментарии, стрики,
ачивки, чат, профиль, админка) и бот.

Придумай короткий спринт: одну тему и %d задач, которые реально улучшат
приложение для участников стаи.

Ответь JSON без обрамления и пояснений:
{"theme": "...", "reply": "...", "tasks": ["...", "..."]}

theme — 2–5 слов, о чём спринт.
reply — 1–2 предложения твоим голосом: дерзко, с юмором, по делу.
tasks — формулировки для разработчика: что сделать и зачем, каждая одним
абзацем до 400 символов, без эмодзи и без обращения к человеку. Задачи должны
быть разными и не ломать существующие сценарии.
Не предлагай переписать проект целиком, не трогай оплату и удаление данных.`

// startLeoAutonomyScheduler — фоновый цикл автономного режима.
func (b *Bot) startLeoAutonomyScheduler(ctx context.Context) {
	ticker := time.NewTicker(leoAutonomyTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runLeoAutonomyIfDue()
		}
	}
}

// runLeoAutonomyIfDue — пора ли Лео придумывать очередной спринт.
func (b *Bot) runLeoAutonomyIfDue() {
	if b == nil || b.db == nil {
		return
	}
	state, err := b.db.GetLeoAutonomy()
	if err != nil {
		b.logger.Warnf("автономный Лео: не прочитать состояние: %v", err)
		return
	}
	if !state.Active() {
		return
	}
	if due := state.DueAt(); due.After(time.Now()) {
		return
	}
	if err := b.runLeoSprint(state); err != nil {
		b.logger.Warnf("автономный Лео: спринт не вышел: %v", err)
		// Отметку о прогоне ставим и при неудаче: иначе сломанный ключ модели
		// заставил бы нас долбиться в неё каждые пять минут.
		_ = b.db.MarkLeoAutonomyRun("не получилось: " + err.Error())
	}
}

// runLeoSprint — спросить у Лео спринт и поставить задачи на доску.
func (b *Bot) runLeoSprint(state database.LeoAutonomy) error {
	if b.aiClient == nil {
		return fmt.Errorf("не настроен OpenRouter")
	}
	count := state.TasksPerRun
	if count <= 0 {
		count = 3
	}
	if count > leoAutonomyMaxTasks {
		count = leoAutonomyMaxTasks
	}

	raw, err := b.aiClient.Chat([]ai.ChatMessage{
		{Role: "system", Content: fmt.Sprintf(leoAutonomySprintPrompt, count)},
		{Role: "user", Content: leoSprintUserHint(state)},
	}, "")
	if err != nil {
		return fmt.Errorf("Лео не ответил: %w", err)
	}
	var parsed struct {
		Theme string   `json:"theme"`
		Reply string   `json:"reply"`
		Tasks []string `json:"tasks"`
	}
	block := leoJSONBlock.FindString(raw)
	if block == "" {
		return fmt.Errorf("Лео ответил не JSON")
	}
	if err := json.Unmarshal([]byte(block), &parsed); err != nil {
		return fmt.Errorf("ответ Лео не разобрался: %w", err)
	}

	theme := strings.TrimSpace(parsed.Theme)
	created, failed := b.createLeoSprintTasks(theme, parsed.Tasks, count)
	if created == 0 {
		return fmt.Errorf("ни одной задачи не поставилось: %s", failed)
	}

	note := fmt.Sprintf("%s — задач: %d", theme, created)
	if failed != "" {
		note += "; не удалось: " + failed
	}
	if err := b.db.MarkLeoAutonomyRun(note); err != nil {
		b.logger.Warnf("автономный Лео: не записать прогон: %v", err)
	}
	b.announceLeoSprint(theme, strings.TrimSpace(parsed.Reply), created, state)
	return nil
}

// leoSprintUserHint — чтобы спринты не повторялись, показываем Лео прошлую тему.
func leoSprintUserHint(state database.LeoAutonomy) string {
	if note := strings.TrimSpace(state.LastNote); note != "" {
		return "Прошлый твой спринт: " + note + ". Возьми другую тему."
	}
	return "Первый спринт: возьми то, что важнее всего для участников стаи."
}

// createLeoSprintTasks — поставить задачи на нашу доску от имени Лео.
// Запуски разносим по времени, чтобы на карточках не стояло одно и то же.
func (b *Bot) createLeoSprintTasks(theme string, tasks []string, limit int) (created int, failed string) {
	var problems []string
	for i, task := range tasks {
		if i >= limit {
			break
		}
		text := strings.TrimSpace(task)
		if text == "" {
			continue
		}
		if theme != "" {
			text = "[" + theme + "] " + text
		}
		when := fmt.Sprintf("через %d мин", 5+i*20)
		if _, err := b.trackerRequest("create", 0, map[string]any{
			"when":           when,
			"prompt":         text,
			"leo":            true,
			"needs_approval": true,
			"auto_push":      true,
		}, b.leoBoardUserID(), "Лео"); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		created++
	}
	return created, strings.Join(problems, "; ")
}

// leoBoardUserID — от чьего имени открываем сессию доски. Доска знает гостей
// по telegram_id, поэтому берём владельца; авторство самой задачи отдельным
// флагом leo, чтобы на карточке был Лео, а не человек.
func (b *Bot) leoBoardUserID() int64 {
	if b.config == nil {
		return 0
	}
	if b.config.OwnerID != 0 {
		return b.config.OwnerID
	}
	if ids := b.config.AdminTelegramUserIDs(); len(ids) > 0 {
		return ids[0]
	}
	return 0
}

// announceLeoSprint — рассказать админам, что Лео придумал на этот раз.
func (b *Bot) announceLeoSprint(theme, reply string, created int, state database.LeoAutonomy) {
	text := fmt.Sprintf("🐆 Лео придумал спринт: %s\nЗадач поставлено: %d", theme, created)
	if reply != "" {
		text += "\n\n" + reply
	}
	text += fmt.Sprintf("\n\nСам работает до %s.", state.ActiveUntil.Format("02.01 15:04"))
	if err := b.NotifyTrackerResult(0, text); err != nil {
		b.logger.Warnf("автономный Лео: не сообщить админам: %v", err)
	}
}

// MiniappLeoAutonomyState — что показываем в трекере.
type MiniappLeoAutonomyState struct {
	Active      bool   `json:"active"`
	ActiveUntil string `json:"active_until"`
	NextRunAt   string `json:"next_run_at"`
	EveryHours  int    `json:"every_hours"`
	TasksPerRun int    `json:"tasks_per_run"`
	LastRunAt   string `json:"last_run_at"`
	LastNote    string `json:"last_note"`
	MaxDays     int    `json:"max_days"`
}

// MiniappLeoAutonomy — статус и управление режимом из трекера.
// action: status | start | stop. days — сколько дней Лео работает сам.
func (b *Bot) MiniappLeoAutonomy(
	viewerUserID int64, initD initdata.InitData, action string, days, everyHours, tasks int,
) (MiniappLeoAutonomyState, error) {
	var out MiniappLeoAutonomyState
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	// Режим тратит деньги владельца (Лео ходит в модель), поэтому только
	// настоящие админы (OWNER_ID + ADMIN_IDS), а не любой админ чата стаи.
	if b.config == nil || !b.config.IsAdminTelegramUser(viewerUserID) {
		return out, ErrPackFeedForbidden
	}
	if b.db == nil {
		return out, fmt.Errorf("база недоступна")
	}

	switch strings.TrimSpace(action) {
	case "", "status":
	case "stop":
		if err := b.db.SetLeoAutonomy(time.Time{}, everyHours, tasks, viewerUserID); err != nil {
			return out, err
		}
	case "start":
		if days <= 0 {
			days = 1
		}
		if days > leoAutonomyMaxDays {
			days = leoAutonomyMaxDays
		}
		if everyHours <= 0 {
			everyHours = 4
		}
		if tasks <= 0 {
			tasks = 3
		}
		if tasks > leoAutonomyMaxTasks {
			tasks = leoAutonomyMaxTasks
		}
		until := time.Now().Add(time.Duration(days) * 24 * time.Hour)
		if err := b.db.SetLeoAutonomy(until, everyHours, tasks, viewerUserID); err != nil {
			return out, err
		}
	default:
		return out, ErrAdminActionInvalid
	}

	state, err := b.db.GetLeoAutonomy()
	if err != nil {
		return out, err
	}
	return leoAutonomyView(state), nil
}

func leoAutonomyView(state database.LeoAutonomy) MiniappLeoAutonomyState {
	out := MiniappLeoAutonomyState{
		Active:      state.Active(),
		EveryHours:  state.EveryHours,
		TasksPerRun: state.TasksPerRun,
		LastNote:    state.LastNote,
		MaxDays:     leoAutonomyMaxDays,
	}
	if out.EveryHours <= 0 {
		out.EveryHours = 4
	}
	if out.TasksPerRun <= 0 {
		out.TasksPerRun = 3
	}
	if !state.ActiveUntil.IsZero() {
		out.ActiveUntil = state.ActiveUntil.Format(time.RFC3339)
	}
	if !state.LastRunAt.IsZero() {
		out.LastRunAt = state.LastRunAt.Format(time.RFC3339)
	}
	if due := state.DueAt(); !due.IsZero() {
		out.NextRunAt = due.Format(time.RFC3339)
	}
	return out
}
