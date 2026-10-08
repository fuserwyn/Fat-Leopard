package database

import (
	"database/sql"
	"fmt"
	"time"
)

// PackWeekSummary — итоги закрытой недели стаи (снимок, который Лео собирает в понедельник).
type PackWeekSummary struct {
	PackChatID   int64
	WeekStart    string // YYYY-MM-DD, понедельник
	Workouts     int
	Goal         int
	GoalReached  bool
	NextGoal     int // цель следующей недели на момент подведения итогов
	Participants int
	CreatedAt    time.Time
}

const packWeekSummaryColumns = `pack_chat_id, week_start_date::text, workouts, goal, goal_reached, next_goal, participants, created_at`

func scanPackWeekSummary(sc interface{ Scan(...any) error }) (PackWeekSummary, error) {
	var s PackWeekSummary
	err := sc.Scan(&s.PackChatID, &s.WeekStart, &s.Workouts, &s.Goal, &s.GoalReached, &s.NextGoal, &s.Participants, &s.CreatedAt)
	return s, err
}

// GetPackWeekSummary — итоги недели стаи; nil, если их ещё не подводили.
func (d *Database) GetPackWeekSummary(packChatID int64, weekStart string) (*PackWeekSummary, error) {
	row := d.db.QueryRow(`SELECT `+packWeekSummaryColumns+`
		FROM pack_week_summaries
		WHERE pack_chat_id = $1 AND week_start_date = $2::date`, packChatID, weekStart)
	s, err := scanPackWeekSummary(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pack week summary: %w", err)
	}
	return &s, nil
}

// ListPackWeekSummaries — итоги недель стаи начиная с недели from (включительно), старые сверху.
func (d *Database) ListPackWeekSummaries(packChatID int64, from string) ([]PackWeekSummary, error) {
	rows, err := d.db.Query(`SELECT `+packWeekSummaryColumns+`
		FROM pack_week_summaries
		WHERE pack_chat_id = $1 AND week_start_date >= $2::date
		ORDER BY week_start_date`, packChatID, from)
	if err != nil {
		return nil, fmt.Errorf("list pack week summaries: %w", err)
	}
	defer rows.Close()
	var out []PackWeekSummary
	for rows.Next() {
		s, err := scanPackWeekSummary(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// InsertPackWeekSummary записывает итоги недели один раз. Если записали именно сейчас
// и цель закрыта, в той же транзакции цель стаи поднимается до s.NextGoal —
// повторный вызов ничего не меняет, так что цель растёт ровно один раз за неделю.
func (d *Database) InsertPackWeekSummary(s PackWeekSummary) (inserted bool, err error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	res, err := tx.Exec(`
		INSERT INTO pack_week_summaries
			(pack_chat_id, week_start_date, workouts, goal, goal_reached, next_goal, participants, created_at)
		VALUES ($1, $2::date, $3, $4, $5, $6, $7, NOW())
		ON CONFLICT (pack_chat_id, week_start_date) DO NOTHING
	`, s.PackChatID, s.WeekStart, s.Workouts, s.Goal, s.GoalReached, s.NextGoal, s.Participants)
	if err != nil {
		return false, fmt.Errorf("insert pack week summary: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n > 0 && s.GoalReached && s.NextGoal > 0 && s.NextGoal != s.Goal {
		if _, err = tx.Exec(`
			INSERT INTO pack_weekly_goal_settings (pack_chat_id, goal, updated_by, updated_at)
			VALUES ($1, $2, 0, NOW())
			ON CONFLICT (pack_chat_id) DO UPDATE
			  SET goal = EXCLUDED.goal,
			      updated_by = 0,
			      updated_at = NOW()
		`, s.PackChatID, s.NextGoal); err != nil {
			return false, fmt.Errorf("raise pack weekly goal: %w", err)
		}
	}
	if err = tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// CountPackWeekParticipants — сколько участников стаи отчитались о тренировке за диапазон дат.
func (d *Database) CountPackWeekParticipants(packChatID int64, startDate, endDate string) (int, error) {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(DISTINCT user_id)
		FROM training_sessions
		WHERE chat_id = $1
		  AND session_date >= $2
		  AND session_date <= $3
		  AND is_bonus = FALSE
		  AND trainings_count > 0
	`, packChatID, startDate, endDate).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count pack week participants: %w", err)
	}
	return n, nil
}

// CountUserPackTrainingSessionsInDateRange — вклад участника в общий зачёт стаи за диапазон дат.
func (d *Database) CountUserPackTrainingSessionsInDateRange(userID, packChatID int64, startDate, endDate string) (int, error) {
	var n int
	err := d.db.QueryRow(`
		SELECT COUNT(*)
		FROM training_sessions
		WHERE user_id = $1
		  AND chat_id = $2
		  AND session_date >= $3
		  AND session_date <= $4
		  AND is_bonus = FALSE
		  AND trainings_count > 0
	`, userID, packChatID, startDate, endDate).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count user pack sessions: %w", err)
	}
	return n, nil
}

// IsPackWeekSummarySeen — участник уже видел итоги этой недели.
func (d *Database) IsPackWeekSummarySeen(userID, packChatID int64, weekStart string) (bool, error) {
	var seen bool
	err := d.db.QueryRow(`
		SELECT EXISTS (
			SELECT 1 FROM pack_week_summary_seen
			WHERE user_id = $1 AND pack_chat_id = $2 AND week_start_date = $3::date
		)
	`, userID, packChatID, weekStart).Scan(&seen)
	if err != nil {
		return false, fmt.Errorf("pack week summary seen: %w", err)
	}
	return seen, nil
}

// MarkPackWeekSummarySeen — участник закрыл модалку итогов недели.
func (d *Database) MarkPackWeekSummarySeen(userID, packChatID int64, weekStart string) error {
	_, err := d.db.Exec(`
		INSERT INTO pack_week_summary_seen (user_id, pack_chat_id, week_start_date, seen_at)
		VALUES ($1, $2, $3::date, NOW())
		ON CONFLICT (user_id, pack_chat_id, week_start_date) DO NOTHING
	`, userID, packChatID, weekStart)
	if err != nil {
		return fmt.Errorf("mark pack week summary seen: %w", err)
	}
	return nil
}
