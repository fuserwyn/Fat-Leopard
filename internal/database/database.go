package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/logger"
	"leo-bot/internal/models"
	"leo-bot/internal/utils"

	_ "github.com/lib/pq"
)

type Database struct {
	db     *sql.DB
	logger logger.Logger
}

func New(databaseURL string) (*Database, error) {
	// Если databaseURL пустой, используем дефолтное значение
	if databaseURL == "" {
		databaseURL = "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable"
	}

	db, err := sql.Open("postgres", databaseURL)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Устанавливаем таймауты для подключения
	db.SetConnMaxLifetime(5 * time.Minute)
	db.SetMaxOpenConns(25)
	db.SetMaxIdleConns(5)

	// Проверяем соединение с таймаутом
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := db.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("failed to ping database (check DATABASE_URL and network connectivity): %w", err)
	}

	return &Database{
		db: db,
	}, nil
}

func (d *Database) Close() error {
	return d.db.Close()
}

// CreateTables создает таблицы в базе данных, если они не существуют
func (d *Database) CreateTables() error {
	queries := []string{
		`CREATE TABLE IF NOT EXISTS message_log (
			user_id BIGINT,
			username TEXT DEFAULT '',
			chat_id BIGINT,
			calories INTEGER DEFAULT 0,
			streak_days INTEGER DEFAULT 0,
			last_training_date TEXT,
			last_message TEXT NOT NULL,
			has_training_done BOOLEAN DEFAULT FALSE,
			has_sick_leave BOOLEAN DEFAULT FALSE,
			has_healthy BOOLEAN DEFAULT FALSE,
			is_deleted BOOLEAN DEFAULT FALSE,
			is_exempt_from_deletion BOOLEAN DEFAULT FALSE,
			timer_start_time TEXT,
			sick_leave_start_time TEXT,
			sick_leave_end_time TEXT,
			sick_time TEXT,
			rest_time_till_del TEXT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, chat_id)
		)`,
		`CREATE TABLE IF NOT EXISTS training_log (
			user_id BIGINT PRIMARY KEY,
			username TEXT DEFAULT '',
			last_report TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)`,
	}

	for _, query := range queries {
		if _, err := d.db.Exec(query); err != nil {
			return fmt.Errorf("failed to create table: %w", err)
		}
	}

	// Запускаем миграции для обновления схемы
	if err := d.RunMigrations(); err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}

	return nil
}

// SaveMessageLog сохраняет информацию о сообщении
func (d *Database) SaveMessageLog(msg *models.MessageLog) error {
	query := `
		INSERT INTO message_log (user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion, timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20)
		ON CONFLICT (user_id, chat_id) 
		DO UPDATE SET 
			username = EXCLUDED.username,
			calories = EXCLUDED.calories,
			streak_days = EXCLUDED.streak_days,
			calorie_streak_days = EXCLUDED.calorie_streak_days,
			cups_earned = EXCLUDED.cups_earned,
			last_training_date = EXCLUDED.last_training_date,
			last_message = EXCLUDED.last_message,
			has_training_done = EXCLUDED.has_training_done,
			has_sick_leave = EXCLUDED.has_sick_leave,
			has_healthy = EXCLUDED.has_healthy,
			is_deleted = EXCLUDED.is_deleted,
			is_exempt_from_deletion = EXCLUDED.is_exempt_from_deletion,
			timer_start_time = EXCLUDED.timer_start_time,
			sick_leave_start_time = EXCLUDED.sick_leave_start_time,
			sick_leave_end_time = EXCLUDED.sick_leave_end_time,
			sick_time = EXCLUDED.sick_time,
			rest_time_till_del = EXCLUDED.rest_time_till_del,
			updated_at = $20
	`

	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())

	// Временное логирование для отладки
	fmt.Printf("DEBUG: Saving to DB - UserID: %d, TimerStartTime: %v, SickLeaveStartTime: %v, RestTimeTillDel: %v\n",
		msg.UserID, msg.TimerStartTime, msg.SickLeaveStartTime, msg.RestTimeTillDel)

	result, err := d.db.Exec(query,
		msg.UserID, msg.Username, msg.ChatID, msg.Calories, msg.StreakDays, msg.CalorieStreakDays, msg.CupsEarned, msg.LastTrainingDate, msg.LastMessage, msg.HasTrainingDone,
		msg.HasSickLeave, msg.HasHealthy, msg.IsDeleted, msg.IsExemptFromDeletion, msg.TimerStartTime, msg.SickLeaveStartTime, msg.SickLeaveEndTime, msg.SickTime, msg.RestTimeTillDel, moscowTime)

	if err != nil {
		fmt.Printf("DEBUG: Save error: %v\n", err)
		return err
	}

	// Проверяем, что именно произошло (INSERT или UPDATE)
	rowsAffected, _ := result.RowsAffected()
	fmt.Printf("DEBUG: Rows affected: %d\n", rowsAffected)

	return err
}

// GetMessageLog получает информацию о сообщении пользователя
func (d *Database) GetMessageLog(userID, chatID int64) (*models.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, created_at, updated_at
		FROM message_log 
		WHERE user_id = $1 AND chat_id = $2
	`

	var msg models.MessageLog
	err := d.db.QueryRow(query, userID, chatID).Scan(
		&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
		&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel,
		&msg.CreatedAt, &msg.UpdatedAt)
	if err != nil {
		return nil, err
	}

	// Временное логирование для отладки
	fmt.Printf("DEBUG: Retrieved from DB - UserID: %d, HasSickLeave: %t, TimerStartTime: %v, SickLeaveStartTime: %v, RestTimeTillDel: %v\n",
		msg.UserID, msg.HasSickLeave, msg.TimerStartTime, msg.SickLeaveStartTime, msg.RestTimeTillDel)

	return &msg, nil
}

// GetUsersByChatID получает всех пользователей в чате
func (d *Database) GetUsersByChatID(chatID int64) ([]*models.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, created_at, updated_at
		FROM message_log 
		WHERE chat_id = $1 AND is_deleted = FALSE
		ORDER BY calories DESC, last_message DESC
	`

	rows, err := d.db.Query(query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.MessageLog
	for rows.Next() {
		var msg models.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel,
			&msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &msg)
	}

	return users, nil
}

// GetUserIDByUsername получает user_id по username в конкретном чате
// Поддерживает поиск по разным форматам: @username, username, "Имя Фамилия"
func (d *Database) GetUserIDByUsername(username string, chatID int64) (int64, error) {
	// Сначала пробуем точное совпадение
	query := `
		SELECT user_id FROM message_log 
		WHERE username = $1 AND chat_id = $2
	`
	var userID int64
	err := d.db.QueryRow(query, username, chatID).Scan(&userID)
	if err == nil {
		return userID, nil
	}

	// Если не найдено, пробуем поиск с @
	if !strings.HasPrefix(username, "@") {
		query = `
			SELECT user_id FROM message_log 
			WHERE username = $1 AND chat_id = $2
		`
		err = d.db.QueryRow(query, "@"+username, chatID).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}

	// Если не найдено, пробуем поиск без @
	if strings.HasPrefix(username, "@") {
		query = `
			SELECT user_id FROM message_log 
			WHERE username = $1 AND chat_id = $2
		`
		err = d.db.QueryRow(query, username[1:], chatID).Scan(&userID)
		if err == nil {
			return userID, nil
		}
	}

	// Если все еще не найдено, пробуем поиск по частичному совпадению (для случаев типа "OggO Logos")
	query = `
		SELECT user_id FROM message_log 
		WHERE username ILIKE $1 AND chat_id = $2
	`
	err = d.db.QueryRow(query, "%"+username+"%", chatID).Scan(&userID)
	if err == nil {
		return userID, nil
	}

	return 0, fmt.Errorf("user not found")
}

// SaveTrainingLog сохраняет отчет о тренировке
func (d *Database) SaveTrainingLog(training *models.TrainingLog) error {
	query := `
		INSERT INTO training_log (user_id, username, last_report, updated_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (user_id) 
		DO UPDATE SET 
			username = EXCLUDED.username,
			last_report = EXCLUDED.last_report,
			updated_at = $4
	`

	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, training.UserID, training.Username, training.LastReport, moscowTime)
	return err
}

// GetDatabaseStats получает статистику базы данных
func (d *Database) GetDatabaseStats() (map[string]interface{}, error) {
	query := `
		SELECT 
			COUNT(*) as total_users,
			COUNT(CASE WHEN has_training_done = true THEN 1 END) as training_done,
			COUNT(CASE WHEN has_sick_leave = true THEN 1 END) as sick_leave,
			COUNT(CASE WHEN has_healthy = true THEN 1 END) as healthy
		FROM message_log
	`

	var stats struct {
		TotalUsers   int `db:"total_users"`
		TrainingDone int `db:"training_done"`
		SickLeave    int `db:"sick_leave"`
		Healthy      int `db:"healthy"`
	}

	err := d.db.QueryRow(query).Scan(&stats.TotalUsers, &stats.TrainingDone, &stats.SickLeave, &stats.Healthy)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"total_users":   stats.TotalUsers,
		"training_done": stats.TrainingDone,
		"sick_leave":    stats.SickLeave,
		"healthy":       stats.Healthy,
	}, nil
}

// AddCalories добавляет калории пользователю
func (d *Database) AddCalories(userID, chatID int64, calories int) error {
	query := `
		UPDATE message_log 
		SET calories = calories + $3, updated_at = $4
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, calories, moscowTime)
	return err
}

// GetUserCalories получает калории пользователя
func (d *Database) GetUserCalories(userID, chatID int64) (int, error) {
	query := `
		SELECT calories FROM message_log 
		WHERE user_id = $1 AND chat_id = $2
	`
	var calories int
	err := d.db.QueryRow(query, userID, chatID).Scan(&calories)
	if err != nil {
		return 0, err
	}
	return calories, nil
}

// UpdateStreak обновляет серию тренировок пользователя
func (d *Database) UpdateStreak(userID, chatID int64, streakDays int, lastTrainingDate string) error {
	query := `
		UPDATE message_log 
		SET streak_days = $3, last_training_date = $4, updated_at = $5
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, streakDays, lastTrainingDate, moscowTime)
	return err
}

// ResetStreakDays сбрасывает только серию дней, не трогая last_training_date
func (d *Database) ResetStreakDays(userID, chatID int64) error {
	query := `
		UPDATE message_log 
		SET streak_days = 0, updated_at = $3
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, moscowTime)
	return err
}

// UpdateCalorieStreak обновляет серию дней для калорий
func (d *Database) UpdateCalorieStreak(userID, chatID int64, calorieStreakDays int) error {
	query := `
		UPDATE message_log 
		SET calorie_streak_days = $3, updated_at = $4
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, calorieStreakDays, moscowTime)
	return err
}

// UpdateCalorieStreakWithDate обновляет серию дней для калорий с датой последней тренировки
func (d *Database) UpdateCalorieStreakWithDate(userID, chatID int64, calorieStreakDays int, lastTrainingDate string) error {
	query := `
		UPDATE message_log 
		SET calorie_streak_days = $3, last_training_date = $4, updated_at = $5
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, calorieStreakDays, lastTrainingDate, moscowTime)
	return err
}

// ResetCalorieStreak сбрасывает серию дней для калорий
func (d *Database) ResetCalorieStreak(userID, chatID int64) error {
	query := `
		UPDATE message_log 
		SET calorie_streak_days = 0, updated_at = $3
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, moscowTime)
	return err
}

// AddCups добавляет кубки пользователю
func (d *Database) AddCups(userID, chatID int64, cups int) error {
	query := `
		UPDATE message_log 
		SET cups_earned = cups_earned + $3, updated_at = $4
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, cups, moscowTime)
	return err
}

// GetUserCups получает количество заработанных кубков пользователя
func (d *Database) GetUserCups(userID, chatID int64) (int, error) {
	query := `
		SELECT COALESCE(cups_earned, 0) 
		FROM message_log 
		WHERE user_id = $1 AND chat_id = $2
	`

	var cups int
	err := d.db.QueryRow(query, userID, chatID).Scan(&cups)
	if err != nil {
		return 0, err
	}

	return cups, nil
}

// MarkUserAsDeleted помечает пользователя как удаленного
func (d *Database) MarkUserAsDeleted(userID, chatID int64) error {
	query := `
		UPDATE message_log 
		SET is_deleted = TRUE, updated_at = $3
		WHERE user_id = $1 AND chat_id = $2
	`
	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, userID, chatID, moscowTime)
	return err
}

// GetTopUsers получает топ пользователей по калориям
func (d *Database) GetTopUsers(chatID int64, limit int) ([]*models.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, created_at, updated_at
		FROM message_log 
		WHERE chat_id = $1 AND calories > 0 AND is_deleted = FALSE
		ORDER BY calories DESC, last_message DESC
		LIMIT $2
	`

	rows, err := d.db.Query(query, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.MessageLog
	for rows.Next() {
		var msg models.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel,
			&msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &msg)
	}

	return users, nil
}

// GetAllUsersWithTimers получает всех пользователей с активными таймерами
func (d *Database) GetAllUsersWithTimers() ([]*models.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, created_at, updated_at
		FROM message_log 
		WHERE timer_start_time IS NOT NULL AND is_deleted = FALSE
		ORDER BY timer_start_time ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*models.MessageLog
	for rows.Next() {
		var msg models.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel,
			&msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &msg)
	}

	return users, nil
}
