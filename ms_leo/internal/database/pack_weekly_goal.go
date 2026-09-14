package database

import (
	"database/sql"
	"time"
)

// CountPackTrainingSessionsInDateRange — все зачтённые тренировки стаи за диапазон дат (включительно).
func (d *Database) CountPackTrainingSessionsInDateRange(packChatID int64, startDate, endDate string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM training_sessions
		WHERE chat_id = $1
		  AND session_date >= $2
		  AND session_date <= $3
		  AND is_bonus = FALSE
		  AND trainings_count > 0
	`
	var count int
	err := d.db.QueryRow(query, packChatID, startDate, endDate).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}

// TryInsertPackWeeklyGoalBonus записывает бонус за достижение цели стаи (один раз на неделю).
func (d *Database) TryInsertPackWeeklyGoalBonus(packChatID int64, weekStart string, bonusUntil time.Time) (bool, error) {
	query := `
		INSERT INTO pack_weekly_goal_bonus (pack_chat_id, week_start_date, bonus_until)
		VALUES ($1, $2::date, $3)
		ON CONFLICT (pack_chat_id, week_start_date) DO NOTHING
	`
	res, err := d.db.Exec(query, packChatID, weekStart, bonusUntil.UTC())
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n > 0, nil
}

// GetActivePackWeeklyGoalBonusUntil — bonus_until, если бонус ещё активен.
func (d *Database) GetActivePackWeeklyGoalBonusUntil(packChatID int64, now time.Time) (time.Time, bool, error) {
	query := `
		SELECT bonus_until
		FROM pack_weekly_goal_bonus
		WHERE pack_chat_id = $1
		  AND bonus_until > $2
		ORDER BY bonus_until DESC
		LIMIT 1
	`
	var until time.Time
	err := d.db.QueryRow(query, packChatID, now.UTC()).Scan(&until)
	if err != nil {
		if err == sql.ErrNoRows {
			return time.Time{}, false, nil
		}
		return time.Time{}, false, err
	}
	return until, true, nil
}
