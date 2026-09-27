package database

import (
	"database/sql"
	"strings"
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

// InsertPackWeeklyGoalMemberCups фиксирует кубки, выданные участнику за неделю стаи.
// Повтор той же недели не задваивает запись.
func (d *Database) InsertPackWeeklyGoalMemberCups(userID, packChatID int64, weekStart string, cups int, createdAt time.Time) error {
	if d == nil || userID == 0 || packChatID == 0 || cups == 0 || strings.TrimSpace(weekStart) == "" {
		return nil
	}
	if createdAt.IsZero() {
		createdAt = time.Now().UTC()
	}
	query := `
		INSERT INTO pack_weekly_goal_member_cups (user_id, pack_chat_id, week_start_date, cups, created_at)
		VALUES ($1, $2, $3::date, $4, $5)
		ON CONFLICT (user_id, pack_chat_id, week_start_date) DO NOTHING
	`
	_, err := d.db.Exec(query, userID, packChatID, weekStart, cups, createdAt.UTC())
	return err
}

// PackWeeklyGoalMemberCupsRow — одно начисление кубков за недельное достижение стаи.
type PackWeeklyGoalMemberCupsRow struct {
	Cups      int
	CreatedAt time.Time
}

// ListPackWeeklyGoalMemberCups — начисления кубков за недели стаи, новые сверху.
func (d *Database) ListPackWeeklyGoalMemberCups(userID, packChatID int64, limit int) ([]PackWeeklyGoalMemberCupsRow, error) {
	if d == nil || userID == 0 || packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 200
	}
	query := `
		SELECT cups, created_at
		FROM pack_weekly_goal_member_cups
		WHERE user_id = $1
		  AND pack_chat_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`
	rows, err := d.db.Query(query, userID, packChatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PackWeeklyGoalMemberCupsRow
	for rows.Next() {
		var row PackWeeklyGoalMemberCupsRow
		if err := rows.Scan(&row.Cups, &row.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
