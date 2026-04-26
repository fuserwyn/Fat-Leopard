package database

import (
	"leo-bot/internal/domain"
	"leo-bot/internal/utils"
)

// SaveTrainingSession сохраняет запись о конкретной тренировочной сессии.
func (d *Database) SaveTrainingSession(session *domain.TrainingSession) error {
	query := `
		INSERT INTO training_sessions (
			user_id, chat_id, session_date, message_text, trainings_count, cups_added, is_bonus, created_at
		)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())

	_, err := d.db.Exec(
		query,
		session.UserID,
		session.ChatID,
		session.SessionDate,
		session.MessageText,
		session.TrainingsCount,
		session.CupsAdded,
		session.IsBonus,
		moscowTime,
	)
	return err
}

// CountTrainingSessionsInDateRange считает количество сессий пользователя в диапазоне дат (включительно).
func (d *Database) CountTrainingSessionsInDateRange(userID, chatID int64, startDate, endDate string) (int, error) {
	query := `
		SELECT COUNT(*)
		FROM training_sessions
		WHERE user_id = $1
		  AND chat_id = $2
		  AND session_date >= $3
		  AND session_date <= $4
		  AND is_bonus = FALSE
		  AND trainings_count > 0
	`

	var count int
	err := d.db.QueryRow(query, userID, chatID, startDate, endDate).Scan(&count)
	if err != nil {
		return 0, err
	}
	return count, nil
}
