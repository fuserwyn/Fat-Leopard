package bot

import (
	"time"

	"leo-bot/internal/utils"
)

// PackWeeklyWorkoutGoal — цель тренировок стаи за неделю (сброс каждое воскресенье).
const PackWeeklyWorkoutGoal = 100

// PackBonusThemeDuration — эксклюзивная тема на сутки после достижения цели.
const PackBonusThemeDuration = 24 * time.Hour

// MiniappPackWeeklyProgress — недельный прогресс стаи для мини-аппа.
type MiniappPackWeeklyProgress struct {
	WorkoutsWeek       int
	Goal               int
	WeekStart          string
	WeekEnd            string
	GoalReached        bool
	BonusActive        bool
	BonusActiveUntil   string // RFC3339; пусто, если бонус не активен
}

// GetMiniappPackWeeklyProgressForAPI — суммарные тренировки стаи с воскресенья по сегодня (МСК).
func (b *Bot) GetMiniappPackWeeklyProgressForAPI(packChatID int64) MiniappPackWeeklyProgress {
	out := MiniappPackWeeklyProgress{Goal: PackWeeklyWorkoutGoal}
	if b == nil || b.db == nil || packChatID == 0 {
		return out
	}
	now := utils.GetMoscowTime()
	out.WeekStart = utils.WeekStartSundayMSK(now)
	out.WeekEnd = utils.WeekEndSaturdayMSK(now)
	today := now.Format("2006-01-02")
	count, err := b.db.CountPackTrainingSessionsInDateRange(packChatID, out.WeekStart, today)
	if err != nil {
		b.logger.Warnf("pack weekly count pack=%d: %v", packChatID, err)
		return out
	}
	out.WorkoutsWeek = count
	out.GoalReached = count >= out.Goal
	if until, ok, err := b.db.GetActivePackWeeklyGoalBonusUntil(packChatID, time.Now().UTC()); err == nil && ok {
		out.BonusActive = true
		out.BonusActiveUntil = until.UTC().Format(time.RFC3339)
	}
	return out
}

// IsPackBonusThemeActive — временная тема «Стая» доступна после достижения недельной цели.
func (b *Bot) IsPackBonusThemeActive(packChatID int64) bool {
	if b == nil || b.db == nil || packChatID == 0 {
		return false
	}
	_, ok, err := b.db.GetActivePackWeeklyGoalBonusUntil(packChatID, time.Now().UTC())
	return err == nil && ok
}

// MaybeGrantPackWeeklyGoalBonus проверяет цель стаи и выдаёт бонусную тему на сутки.
func (b *Bot) MaybeGrantPackWeeklyGoalBonus(packChatID int64) {
	if b == nil || b.db == nil || packChatID == 0 {
		return
	}
	now := utils.GetMoscowTime()
	weekStart := utils.WeekStartSundayMSK(now)
	today := now.Format("2006-01-02")
	count, err := b.db.CountPackTrainingSessionsInDateRange(packChatID, weekStart, today)
	if err != nil {
		b.logger.Warnf("pack weekly bonus count pack=%d: %v", packChatID, err)
		return
	}
	if count < PackWeeklyWorkoutGoal {
		return
	}
	bonusUntil := time.Now().UTC().Add(PackBonusThemeDuration)
	granted, err := b.db.TryInsertPackWeeklyGoalBonus(packChatID, weekStart, bonusUntil)
	if err != nil {
		b.logger.Warnf("pack weekly bonus grant pack=%d: %v", packChatID, err)
		return
	}
	if granted {
		b.logger.Infof("pack weekly goal reached pack=%d week=%s count=%d bonus_until=%s",
			packChatID, weekStart, count, bonusUntil.Format(time.RFC3339))
	}
}
