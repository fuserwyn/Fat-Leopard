package database

import (
	"time"

	"leo-bot/internal/models"
	"leo-bot/internal/utils"
)

// SaveUserMessage сохраняет сообщение пользователя для RAG контекста
func (d *Database) SaveUserMessage(msg *models.UserMessage) error {
	query := `
		INSERT INTO user_messages (user_id, chat_id, username, message_text, message_type, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	createdAt, err := time.Parse("2006-01-02 15:04:05", moscowTime)
	if err != nil {
		createdAt = time.Now()
	}

	_, err = d.db.Exec(query, msg.UserID, msg.ChatID, msg.Username, msg.MessageText, msg.MessageType, createdAt)
	return err
}

// GetUserMessages получает сообщения пользователя за указанный период
func (d *Database) GetUserMessages(userID, chatID int64, startTime, endTime time.Time) ([]*models.UserMessage, error) {
	query := `
		SELECT id, user_id, chat_id, username, message_text, message_type, created_at
		FROM user_messages
		WHERE user_id = $1 AND chat_id = $2 
		AND created_at >= $3 AND created_at <= $4
		ORDER BY created_at ASC
	`

	rows, err := d.db.Query(query, userID, chatID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.UserMessage
	for rows.Next() {
		var msg models.UserMessage
		err := rows.Scan(&msg.ID, &msg.UserID, &msg.ChatID, &msg.Username, &msg.MessageText, &msg.MessageType, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}

	return messages, nil
}

// GetDailyMessages получает все сообщения за указанный день для всех пользователей в чате
func (d *Database) GetDailyMessages(chatID int64, date time.Time) ([]*models.UserMessage, error) {
	startTime := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endTime := startTime.Add(24 * time.Hour)

	query := `
		SELECT id, user_id, chat_id, username, message_text, message_type, created_at
		FROM user_messages
		WHERE chat_id = $1 
		AND created_at >= $2 AND created_at < $3
		ORDER BY created_at ASC
	`

	rows, err := d.db.Query(query, chatID, startTime, endTime)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.UserMessage
	for rows.Next() {
		var msg models.UserMessage
		err := rows.Scan(&msg.ID, &msg.UserID, &msg.ChatID, &msg.Username, &msg.MessageText, &msg.MessageType, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}

	return messages, nil
}

// GetUserTrainingHistory получает историю тренировок пользователя для RAG контекста
func (d *Database) GetUserTrainingHistory(userID, chatID int64, limit int) ([]*models.UserMessage, error) {
	if limit <= 0 {
		limit = 50 // По умолчанию 50 последних сообщений
	}

	query := `
		SELECT id, user_id, chat_id, username, message_text, message_type, created_at
		FROM user_messages
		WHERE user_id = $1 AND chat_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`

	rows, err := d.db.Query(query, userID, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []*models.UserMessage
	for rows.Next() {
		var msg models.UserMessage
		err := rows.Scan(&msg.ID, &msg.UserID, &msg.ChatID, &msg.Username, &msg.MessageText, &msg.MessageType, &msg.CreatedAt)
		if err != nil {
			return nil, err
		}
		messages = append(messages, &msg)
	}

	// Разворачиваем список для хронологического порядка
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetAllChatIDs получает все уникальные chat_id из базы данных
func (d *Database) GetAllChatIDs() ([]int64, error) {
	query := `SELECT DISTINCT chat_id FROM message_log WHERE is_deleted = FALSE`
	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chatIDs []int64
	for rows.Next() {
		var chatID int64
		if err := rows.Scan(&chatID); err != nil {
			continue
		}
		chatIDs = append(chatIDs, chatID)
	}

	return chatIDs, nil
}

// HasAnyMessages проверяет, есть ли хотя бы одно сообщение в таблице user_messages
func (d *Database) HasAnyMessages() (bool, error) {
	query := `SELECT COUNT(*) FROM user_messages LIMIT 1`
	var count int
	err := d.db.QueryRow(query).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}
