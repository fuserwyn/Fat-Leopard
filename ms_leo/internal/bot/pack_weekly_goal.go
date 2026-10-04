package bot

import (
	"fmt"
	"time"

	"leo-bot/internal/utils"
)

// PackWeeklyWorkoutGoal — цель тренировок стаи за неделю (сброс каждый понедельник 00:00 МСК).
const PackWeeklyWorkoutGoal = 75

// packWeeklyWorkoutGoalLegacy — прежняя цель, действует для недель до PackWeeklyWorkoutGoalSince.
const packWeeklyWorkoutGoalLegacy = 50

// PackWeeklyWorkoutGoalSince — понедельник (МСК), с которого действует новая цель:
// текущая неделя досчитывается по старой цели, новая — со следующего пересчёта.
const PackWeeklyWorkoutGoalSince = "2026-10-05"

// PackWeeklyWorkoutGoalForWeek — цель стаи для недели, начинающейся weekStart (YYYY-MM-DD).
func PackWeeklyWorkoutGoalForWeek(weekStart string) int {
	if weekStart != "" && weekStart < PackWeeklyWorkoutGoalSince {
		return packWeeklyWorkoutGoalLegacy
	}
	return PackWeeklyWorkoutGoal
}

const (
	minPackWeeklyWorkoutGoal = 1
	maxPackWeeklyWorkoutGoal = 10_000
)

func parsePackWeeklyWorkoutGoal(goal int) (int, error) {
	if goal < minPackWeeklyWorkoutGoal || goal > maxPackWeeklyWorkoutGoal {
		return 0, fmt.Errorf("цель должна быть от %d до %d тренировок", minPackWeeklyWorkoutGoal, maxPackWeeklyWorkoutGoal)
	}
	return goal, nil
}

// packWeeklyWorkoutGoalOverride — цель, которую админ выставил в мини-аппе (0 — не задана).
func (b *Bot) packWeeklyWorkoutGoalOverride(packChatID int64) int {
	if b == nil || b.db == nil || packChatID == 0 {
		return 0
	}
	n, ok, err := b.db.GetPackWeeklyWorkoutGoal(packChatID)
	if err != nil || !ok || n <= 0 {
		return 0
	}
	return n
}

// packWeeklyWorkoutGoal — действующая цель стаи: оверрайд админа или значение по умолчанию.
// Оверрайд действует сразу, в том числе на текущую неделю.
func (b *Bot) packWeeklyWorkoutGoal(packChatID int64, weekStart string) int {
	if n := b.packWeeklyWorkoutGoalOverride(packChatID); n > 0 {
		return n
	}
	return PackWeeklyWorkoutGoalForWeek(weekStart)
}

// PackWeeklyGoalCupsBonus — кубки каждому участнику стаи при достижении недельной цели.
const PackWeeklyGoalCupsBonus = 50

// PackBonusThemeDuration — эксклюзивная тема на сутки после достижения цели.
const PackBonusThemeDuration = 24 * time.Hour

// MiniappPackWeeklyProgress — недельный прогресс стаи для мини-аппа.
type MiniappPackWeeklyProgress struct {
	WorkoutsWeek     int
	Goal             int
	WeekStart        string
	WeekEnd          string
	GoalReached      bool
	BonusActive      bool
	BonusActiveUntil string // RFC3339; пусто, если бонус не активен
}

// GetMiniappPackWeeklyProgressForAPI — суммарные тренировки стаи с понедельника по сегодня (МСК).
func (b *Bot) GetMiniappPackWeeklyProgressForAPI(packChatID int64) MiniappPackWeeklyProgress {
	out := MiniappPackWeeklyProgress{Goal: PackWeeklyWorkoutGoal}
	if b == nil || b.db == nil || packChatID == 0 {
		return out
	}
	now := utils.GetMoscowTime()
	out.WeekStart = utils.WeekStartMondayMSK(now)
	out.WeekEnd = utils.WeekEndSundayMSK(now)
	out.Goal = b.packWeeklyWorkoutGoal(packChatID, out.WeekStart)
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
	weekStart := utils.WeekStartMondayMSK(now)
	today := now.Format("2006-01-02")
	count, err := b.db.CountPackTrainingSessionsInDateRange(packChatID, weekStart, today)
	if err != nil {
		b.logger.Warnf("pack weekly bonus count pack=%d: %v", packChatID, err)
		return
	}
	goal := b.packWeeklyWorkoutGoal(packChatID, weekStart)
	if count < goal {
		return
	}
	bonusUntil := time.Now().UTC().Add(PackBonusThemeDuration)
	granted, err := b.db.TryInsertPackWeeklyGoalBonus(packChatID, weekStart, bonusUntil)
	if err != nil {
		b.logger.Warnf("pack weekly bonus grant pack=%d: %v", packChatID, err)
		return
	}
	if granted {
		b.grantPackWeeklyGoalMemberCups(packChatID, weekStart)
		b.savePackRoarPackFeed(packWeeklyGoalAchievedFeedMessage(goal))
		b.logger.Infof("pack weekly goal reached pack=%d week=%s count=%d bonus_until=%s cups=%d",
			packChatID, weekStart, count, bonusUntil.Format(time.RFC3339), PackWeeklyGoalCupsBonus)
	}
}

func packWeeklyGoalAchievedFeedMessage(goal int) string {
	return fmt.Sprintf(
		"Цель недели стаи достигнута — %d тренировок за неделю! Каждому участнику начислено по %d кубков. Так держать, леопарды!",
		goal,
		PackWeeklyGoalCupsBonus,
	)
}

func (b *Bot) grantPackWeeklyGoalMemberCups(packChatID int64, weekStart string) {
	if b == nil || b.db == nil || packChatID == 0 {
		return
	}
	users, err := b.db.GetUsersByChatID(packChatID)
	if err != nil {
		b.logger.Warnf("pack weekly cups members pack=%d: %v", packChatID, err)
		return
	}
	awardedAt := time.Now().UTC()
	for _, user := range users {
		if user == nil || user.UserID == 0 {
			continue
		}
		if err := b.db.AddCups(user.UserID, packChatID, PackWeeklyGoalCupsBonus); err != nil {
			b.logger.Warnf("pack weekly cups user=%d pack=%d: %v", user.UserID, packChatID, err)
			continue
		}
		if err := b.db.InsertPackWeeklyGoalMemberCups(user.UserID, packChatID, weekStart, PackWeeklyGoalCupsBonus, awardedAt); err != nil {
			b.logger.Warnf("pack weekly cups history user=%d pack=%d: %v", user.UserID, packChatID, err)
		}
	}
}
