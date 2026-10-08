package bot

import (
	"context"
	"errors"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/utils"
)

// PackWeeklyGoalStep — на сколько тренировок растёт цель стаи после закрытой недели.
const PackWeeklyGoalStep = 5

// packWeekSummarySince — первая неделя, по которой Лео подводит итоги. Прошлые недели
// шли по старой цели: их итоги не показываем и цель по ним не поднимаем.
const packWeekSummarySince = PackWeeklyWorkoutGoalSince

const packWeekSummaryTick = 5 * time.Minute

// ErrPackWeekSummaryNotFound — итогов такой недели нет.
var ErrPackWeekSummaryNotFound = errors.New("pack week summary not found")

// nextPackWeeklyGoal — цель следующей недели: закрыли — на PackWeeklyGoalStep больше, нет — та же.
func nextPackWeeklyGoal(goal int, reached bool) int {
	if !reached {
		return goal
	}
	next := goal + PackWeeklyGoalStep
	if next > maxPackWeeklyWorkoutGoal {
		next = maxPackWeeklyWorkoutGoal
	}
	return next
}

// previousWeekMSK — понедельник и воскресенье прошлой недели (МСК) относительно now.
func previousWeekMSK(now time.Time) (start, end string) {
	monday, err := time.ParseInLocation("2006-01-02", utils.WeekStartMondayMSK(now), moscowLocation())
	if err != nil {
		return "", ""
	}
	return monday.AddDate(0, 0, -7).Format("2006-01-02"), monday.AddDate(0, 0, -1).Format("2006-01-02")
}

// EnsurePackWeekSummary подводит итоги прошлой недели стаи, если их ещё нет: сколько
// тренировок, закрыта ли цель, сколько участников. Закрытая неделя поднимает цель
// на PackWeeklyGoalStep — ровно один раз. nil — итогов нет (неделя до старта фичи).
func (b *Bot) EnsurePackWeekSummary(packChatID int64, now time.Time) (*database.PackWeekSummary, error) {
	if b == nil || b.db == nil || packChatID == 0 {
		return nil, nil
	}
	start, end := previousWeekMSK(now)
	if start == "" {
		return nil, nil
	}
	if s, err := b.db.GetPackWeekSummary(packChatID, start); err != nil || s != nil {
		return s, err
	}
	if start < packWeekSummarySince {
		return nil, nil
	}
	workouts, err := b.db.CountPackTrainingSessionsInDateRange(packChatID, start, end)
	if err != nil {
		return nil, err
	}
	participants, err := b.db.CountPackWeekParticipants(packChatID, start, end)
	if err != nil {
		return nil, err
	}
	// Цель на эту минуту ещё та, что действовала всю прошлую неделю: поднимаем её только здесь.
	goal := b.packWeeklyWorkoutGoal(packChatID, start)
	reached := workouts >= goal
	s := database.PackWeekSummary{
		PackChatID:   packChatID,
		WeekStart:    start,
		Workouts:     workouts,
		Goal:         goal,
		GoalReached:  reached,
		NextGoal:     nextPackWeeklyGoal(goal, reached),
		Participants: participants,
	}
	inserted, err := b.db.InsertPackWeekSummary(s)
	if err != nil {
		return nil, err
	}
	if inserted && b.logger != nil {
		b.logger.Infof("pack week summary pack=%d week=%s workouts=%d goal=%d reached=%v next_goal=%d participants=%d",
			packChatID, start, workouts, goal, reached, s.NextGoal, participants)
	}
	return b.db.GetPackWeekSummary(packChatID, start)
}

// ensurePackWeekSummaryQuiet — подвести итоги прошлой недели перед расчётом текущей:
// иначе в понедельник прогресс считался бы по ещё не поднятой цели.
func (b *Bot) ensurePackWeekSummaryQuiet(packChatID int64) {
	if _, err := b.EnsurePackWeekSummary(packChatID, time.Now()); err != nil && b.logger != nil {
		b.logger.Warnf("pack week summary pack=%d: %v", packChatID, err)
	}
}

// MiniappPackWeekSummary — модалка «Итоги недели стаи».
type MiniappPackWeekSummary struct {
	WeekStart    string `json:"week_start"`
	WeekEnd      string `json:"week_end"`
	Workouts     int    `json:"workouts"`
	Goal         int    `json:"goal"`
	GoalReached  bool   `json:"goal_reached"`
	NextGoal     int    `json:"next_goal"`
	Participants int    `json:"participants"`
	MyWorkouts   int    `json:"my_workouts"`
}

// GetPendingPackWeekSummaryForAPI — итоги прошлой недели для участника общего зачёта,
// если он их ещё не видел. nil — показывать нечего.
func (b *Bot) GetPendingPackWeekSummaryForAPI(userID, packChatID int64, now time.Time) (*MiniappPackWeekSummary, error) {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return nil, nil
	}
	s, err := b.EnsurePackWeekSummary(packChatID, now)
	if err != nil || s == nil || s.Participants == 0 {
		return nil, err
	}
	seen, err := b.db.IsPackWeekSummarySeen(userID, packChatID, s.WeekStart)
	if err != nil || seen {
		return nil, err
	}
	_, end := previousWeekMSK(now)
	mine, err := b.db.CountUserPackTrainingSessionsInDateRange(userID, packChatID, s.WeekStart, end)
	if err != nil || mine == 0 {
		return nil, err
	}
	return &MiniappPackWeekSummary{
		WeekStart:    s.WeekStart,
		WeekEnd:      end,
		Workouts:     s.Workouts,
		Goal:         s.Goal,
		GoalReached:  s.GoalReached,
		NextGoal:     s.NextGoal,
		Participants: s.Participants,
		MyWorkouts:   mine,
	}, nil
}

// MarkPackWeekSummarySeenForAPI — участник закрыл модалку итогов недели.
func (b *Bot) MarkPackWeekSummarySeenForAPI(userID, packChatID int64, weekStart string) error {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return ErrPackWeekSummaryNotFound
	}
	if _, err := time.Parse("2006-01-02", weekStart); err != nil {
		return ErrPackWeekSummaryNotFound
	}
	s, err := b.db.GetPackWeekSummary(packChatID, weekStart)
	if err != nil {
		return err
	}
	if s == nil {
		return ErrPackWeekSummaryNotFound
	}
	return b.db.MarkPackWeekSummarySeen(userID, packChatID, weekStart)
}

// startPackWeekSummaryScheduler — в понедельник сразу после полуночи (МСК) Лео подводит
// итоги прошлой недели и поднимает цель; остальные тики ничего не делают.
func (b *Bot) startPackWeekSummaryScheduler(ctx context.Context) {
	packChatID := b.MonetizedChatID()
	if packChatID == 0 {
		return
	}
	b.ensurePackWeekSummaryQuiet(packChatID)
	ticker := time.NewTicker(packWeekSummaryTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.ensurePackWeekSummaryQuiet(packChatID)
		}
	}
}
