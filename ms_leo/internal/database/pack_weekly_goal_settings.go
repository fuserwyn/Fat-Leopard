package database

import (
	"database/sql"
	"fmt"
)

// GetPackWeeklyWorkoutGoal — недельная цель стаи, заданная админом.
// ok=false, если строки нет: тогда вызывающий берёт цель по умолчанию.
func (d *Database) GetPackWeeklyWorkoutGoal(packChatID int64) (goal int, ok bool, err error) {
	if d == nil || packChatID == 0 {
		return 0, false, nil
	}
	err = d.db.QueryRow(
		`SELECT goal FROM pack_weekly_goal_settings WHERE pack_chat_id = $1`,
		packChatID,
	).Scan(&goal)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get pack weekly goal: %w", err)
	}
	return goal, goal > 0, nil
}

// SetPackWeeklyWorkoutGoal — админ выставляет недельную цель стаи.
func (d *Database) SetPackWeeklyWorkoutGoal(packChatID int64, goal int, updatedBy int64) error {
	if packChatID == 0 {
		return fmt.Errorf("pack not configured")
	}
	if goal <= 0 || goal > 100_000 {
		return fmt.Errorf("invalid pack weekly goal")
	}
	_, err := d.db.Exec(`
		INSERT INTO pack_weekly_goal_settings (pack_chat_id, goal, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (pack_chat_id) DO UPDATE
		  SET goal = EXCLUDED.goal,
		      updated_by = EXCLUDED.updated_by,
		      updated_at = NOW()
	`, packChatID, goal, updatedBy)
	if err != nil {
		return fmt.Errorf("set pack weekly goal: %w", err)
	}
	return nil
}

// ClearPackWeeklyWorkoutGoal — вернуть недельную цель к значению по умолчанию.
func (d *Database) ClearPackWeeklyWorkoutGoal(packChatID int64) error {
	if packChatID == 0 {
		return fmt.Errorf("pack not configured")
	}
	_, err := d.db.Exec(`DELETE FROM pack_weekly_goal_settings WHERE pack_chat_id = $1`, packChatID)
	if err != nil {
		return fmt.Errorf("clear pack weekly goal: %w", err)
	}
	return nil
}
