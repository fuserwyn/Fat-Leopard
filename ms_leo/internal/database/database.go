package database

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
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
			timezone_offset_from_moscow INTEGER NOT NULL DEFAULT 0,
			sick_approval_pending BOOLEAN DEFAULT FALSE,
			sick_approval_deadline TIMESTAMP WITH TIME ZONE,
			sick_approval_message_id BIGINT,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, chat_id)
		)`,
		`CREATE TABLE IF NOT EXISTS training_log (
			user_id BIGINT NOT NULL,
			chat_id BIGINT NOT NULL DEFAULT 0,
			username TEXT DEFAULT '',
			last_report TEXT NOT NULL,
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (user_id, chat_id)
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
func (d *Database) SaveMessageLog(msg *domain.MessageLog) error {
	query := `
		INSERT INTO message_log (user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion, timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, gender, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id, achievement_count, xp_freeze_until, last_daily_xp_msk_date, leopard_starter_bonus_applied, last_achievement_streak_level, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24, $25, $26, $27, $28, $29, $30)
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
			gender = CASE WHEN EXCLUDED.gender != '' THEN EXCLUDED.gender ELSE message_log.gender END,
			timezone_offset_from_moscow = EXCLUDED.timezone_offset_from_moscow,
			sick_approval_pending = EXCLUDED.sick_approval_pending,
			sick_approval_deadline = EXCLUDED.sick_approval_deadline,
			sick_approval_message_id = EXCLUDED.sick_approval_message_id,
			achievement_count = EXCLUDED.achievement_count,
			xp_freeze_until = EXCLUDED.xp_freeze_until,
			last_daily_xp_msk_date = EXCLUDED.last_daily_xp_msk_date,
			leopard_starter_bonus_applied = EXCLUDED.leopard_starter_bonus_applied,
			last_achievement_streak_level = EXCLUDED.last_achievement_streak_level,
			updated_at = $30
	`

	// Используем московское время
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())

	// Временное логирование для отладки
	fmt.Printf("DEBUG: Saving to DB - UserID: %d, TimerStartTime: %v, SickLeaveStartTime: %v, RestTimeTillDel: %v\n",
		msg.UserID, msg.TimerStartTime, msg.SickLeaveStartTime, msg.RestTimeTillDel)

	result, err := d.db.Exec(query,
		msg.UserID, msg.Username, msg.ChatID, msg.Calories, msg.StreakDays, msg.CalorieStreakDays, msg.CupsEarned, msg.LastTrainingDate, msg.LastMessage, msg.HasTrainingDone,
		msg.HasSickLeave, msg.HasHealthy, msg.IsDeleted, msg.IsExemptFromDeletion, msg.TimerStartTime, msg.SickLeaveStartTime, msg.SickLeaveEndTime, msg.SickTime, msg.RestTimeTillDel, msg.Gender, msg.TimezoneOffsetFromMoscow,
		msg.SickApprovalPending, msg.SickApprovalDeadline, msg.SickApprovalMessageID,
		msg.AchievementCount, msg.XpFreezeUntil, msg.LastDailyXPMskDate, msg.LeopardStarterBonusApplied, msg.LastAchievementStreakLevel,
		moscowTime)

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
func (d *Database) GetMessageLog(userID, chatID int64) (*domain.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, gender, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id,
		       achievement_count, xp_freeze_until, last_daily_xp_msk_date, leopard_starter_bonus_applied, last_achievement_streak_level, created_at, updated_at
		FROM message_log 
		WHERE user_id = $1 AND chat_id = $2 AND is_deleted = FALSE
		ORDER BY updated_at DESC
		LIMIT 1
	`

	var msg domain.MessageLog
	var lastDaily sql.NullString
	err := d.db.QueryRow(query, userID, chatID).Scan(
		&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
		&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel, &msg.Gender, &msg.TimezoneOffsetFromMoscow,
		&msg.SickApprovalPending, &msg.SickApprovalDeadline, &msg.SickApprovalMessageID,
		&msg.AchievementCount, &msg.XpFreezeUntil, &lastDaily, &msg.LeopardStarterBonusApplied, &msg.LastAchievementStreakLevel, &msg.CreatedAt, &msg.UpdatedAt)
	if lastDaily.Valid {
		s := lastDaily.String
		msg.LastDailyXPMskDate = &s
	}
	if err != nil {
		return nil, err
	}

	// Временное логирование для отладки
	fmt.Printf("DEBUG: Retrieved from DB - UserID: %d, Username: %s, Gender: '%s', HasSickLeave: %t, TimerStartTime: %v, SickLeaveStartTime: %v, RestTimeTillDel: %v\n",
		msg.UserID, msg.Username, msg.Gender, msg.HasSickLeave, msg.TimerStartTime, msg.SickLeaveStartTime, msg.RestTimeTillDel)

	return &msg, nil
}

// UserHasActiveMessageLogInChat — в message_log есть живая запись пользователя в этом чате (учитываем для paywall, если getChatMember недоступен).
func (d *Database) UserHasActiveMessageLogInChat(userID, chatID int64) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM message_log
			WHERE user_id = $1 AND chat_id = $2 AND is_deleted = FALSE
		)`
	var ok bool
	if err := d.db.QueryRow(q, userID, chatID).Scan(&ok); err != nil {
		return false, fmt.Errorf("exists message_log user/chat: %w", err)
	}
	return ok, nil
}

// GetUsersByChatID получает всех пользователей в чате
func (d *Database) GetUsersByChatID(chatID int64) ([]*domain.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, gender, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id,
		       achievement_count, xp_freeze_until, last_daily_xp_msk_date, leopard_starter_bonus_applied, last_achievement_streak_level, created_at, updated_at
		FROM message_log 
		WHERE chat_id = $1 AND is_deleted = FALSE
		ORDER BY calories DESC, last_message DESC
	`

	rows, err := d.db.Query(query, chatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.MessageLog
	for rows.Next() {
		var msg domain.MessageLog
		var lastDaily2 sql.NullString
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel, &msg.Gender, &msg.TimezoneOffsetFromMoscow,
			&msg.SickApprovalPending, &msg.SickApprovalDeadline, &msg.SickApprovalMessageID,
			&msg.AchievementCount, &msg.XpFreezeUntil, &lastDaily2, &msg.LeopardStarterBonusApplied, &msg.LastAchievementStreakLevel, &msg.CreatedAt, &msg.UpdatedAt)
		if lastDaily2.Valid {
			s := lastDaily2.String
			msg.LastDailyXPMskDate = &s
		}
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

// SaveTrainingLog сохраняет последний отчёт о тренировке (по паре user_id + chat_id).
func (d *Database) SaveTrainingLog(training *domain.TrainingLog) error {
	query := `
		INSERT INTO training_log (user_id, chat_id, username, last_report, updated_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_id, chat_id)
		DO UPDATE SET
			username = EXCLUDED.username,
			last_report = EXCLUDED.last_report,
			updated_at = $5
	`

	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(query, training.UserID, training.ChatID, training.Username, training.LastReport, moscowTime)
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

// CountUsersWithCups получает количество пользователей с указанным количеством кубков или больше
// Включает удаленных пользователей, если у них есть нужное количество кубков
func (d *Database) CountUsersWithCups(chatID int64, minCups int) (int, error) {
	query := `
		SELECT COUNT(DISTINCT user_id)
		FROM message_log 
		WHERE chat_id = $1 AND cups_earned >= $2
	`

	var count int
	err := d.db.QueryRow(query, chatID, minCups).Scan(&count)
	if err != nil {
		return 0, err
	}

	return count, nil
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

// LogDeletionEvent пишет событие удаления пользователя и статус доставки DM.
func (d *Database) LogDeletionEvent(userID, chatID int64, dmStatus, errorText string) error {
	const query = `
		INSERT INTO deletion_events (user_id, chat_id, dm_status, error_text)
		VALUES ($1, $2, $3, NULLIF($4, ''))
	`
	_, err := d.db.Exec(query, userID, chatID, dmStatus, strings.TrimSpace(errorText))
	return err
}

// ReactivateReturnedUser переводит удаленного пользователя в активное состояние возврата.
// Возвращает false, если запись пользователя в чате не найдена (data inconsistency).
func (d *Database) ReactivateReturnedUser(userID, chatID int64, username string) (bool, error) {
	const q = `
		UPDATE message_log
		SET is_deleted = FALSE,
		    lifecycle_status = 'active',
		    calories = 42,
		    achievement_count = 0,
		    has_training_done = FALSE,
		    has_sick_leave = FALSE,
		    has_healthy = FALSE,
		    timer_start_time = $4,
		    returned_at = (NOW() AT TIME ZONE 'Europe/Moscow'),
		    return_count = COALESCE(return_count, 0) + 1,
		    username = CASE WHEN NULLIF($3, '') IS NULL THEN username ELSE $3 END,
		    updated_at = $4
		WHERE user_id = $1 AND chat_id = $2
	`
	now := utils.FormatMoscowTime(utils.GetMoscowTime())
	res, err := d.db.Exec(q, userID, chatID, strings.TrimSpace(username), now)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 1 {
		return true, nil
	}

	// Если записи нет вообще (первый платный вход), создаём профиль в активном состоянии.
	const insertQ = `
		INSERT INTO message_log (
			user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned,
			last_message, has_training_done, has_sick_leave, has_healthy, is_deleted,
			timer_start_time, timezone_offset_from_moscow, achievement_count, return_count,
			returned_at, lifecycle_status, created_at, updated_at
		) VALUES (
			$1, NULLIF($2, ''), $3, 42, 0, 0, 0,
			$4, FALSE, FALSE, FALSE, FALSE,
			$4, 0, 0, 1,
			(NOW() AT TIME ZONE 'Europe/Moscow'), 'active', $4, $4
		)
	`
	if _, err := d.db.Exec(insertQ, userID, strings.TrimSpace(username), chatID, now); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Database) GetUserReturnCount(userID, chatID int64) (int, error) {
	const q = `SELECT COALESCE(return_count, 0) FROM message_log WHERE user_id = $1 AND chat_id = $2`
	var cnt int
	if err := d.db.QueryRow(q, userID, chatID).Scan(&cnt); err != nil {
		return 0, err
	}
	return cnt, nil
}

// GetTopUsers получает топ пользователей по калориям
func (d *Database) GetTopUsers(chatID int64, limit int) ([]*domain.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id, created_at, updated_at
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

	var users []*domain.MessageLog
	for rows.Next() {
		var msg domain.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel, &msg.TimezoneOffsetFromMoscow,
			&msg.SickApprovalPending, &msg.SickApprovalDeadline, &msg.SickApprovalMessageID, &msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &msg)
	}

	return users, nil
}

// GetAllUsersWithTimers получает всех пользователей с активными таймерами
func (d *Database) GetAllUsersWithTimers() ([]*domain.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id, created_at, updated_at
		FROM message_log 
		WHERE timer_start_time IS NOT NULL AND is_deleted = FALSE
		ORDER BY timer_start_time ASC
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.MessageLog
	for rows.Next() {
		var msg domain.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel, &msg.TimezoneOffsetFromMoscow,
			&msg.SickApprovalPending, &msg.SickApprovalDeadline, &msg.SickApprovalMessageID, &msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		users = append(users, &msg)
	}

	return users, nil
}

// GetPendingSickApprovals возвращает пользователей с ожидающим подтверждением больничного
func (d *Database) GetPendingSickApprovals() ([]*domain.MessageLog, error) {
	query := `
		SELECT user_id, username, chat_id, calories, streak_days, calorie_streak_days, cups_earned, last_training_date, last_message, has_training_done, has_sick_leave, has_healthy, is_deleted, is_exempt_from_deletion,
		       timer_start_time, sick_leave_start_time, sick_leave_end_time, sick_time, rest_time_till_del, gender, timezone_offset_from_moscow, sick_approval_pending, sick_approval_deadline, sick_approval_message_id, created_at, updated_at
		FROM message_log
		WHERE sick_approval_pending = TRUE AND is_deleted = FALSE
	`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var approvals []*domain.MessageLog
	for rows.Next() {
		var msg domain.MessageLog
		err := rows.Scan(
			&msg.UserID, &msg.Username, &msg.ChatID, &msg.Calories, &msg.StreakDays, &msg.CalorieStreakDays, &msg.CupsEarned, &msg.LastTrainingDate, &msg.LastMessage, &msg.HasTrainingDone,
			&msg.HasSickLeave, &msg.HasHealthy, &msg.IsDeleted, &msg.IsExemptFromDeletion, &msg.TimerStartTime, &msg.SickLeaveStartTime, &msg.SickLeaveEndTime, &msg.SickTime, &msg.RestTimeTillDel, &msg.Gender, &msg.TimezoneOffsetFromMoscow,
			&msg.SickApprovalPending, &msg.SickApprovalDeadline, &msg.SickApprovalMessageID, &msg.CreatedAt, &msg.UpdatedAt)
		if err != nil {
			return nil, err
		}
		approvals = append(approvals, &msg)
	}

	return approvals, nil
}

// GetRecentUserMessages получает последние сообщения ВСЕХ участников чата для контекста
// Для всех типов чатов использует полную историю из user_messages всех пользователей
// Включает все сообщения, включая ответы бота (ai_reply) для полного контекста диалога
func (d *Database) GetRecentUserMessages(userID, chatID int64, limit int) ([]string, error) {
	// Для всех типов чатов используем полную историю из user_messages всех пользователей
	// Получаем последние сообщения ВСЕХ участников чата, включая ответы бота
	query := `
		SELECT message_text
		FROM user_messages
		WHERE chat_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`

	rows, err := d.db.Query(query, chatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var messages []string
	for rows.Next() {
		var msgText string
		if err := rows.Scan(&msgText); err != nil {
			continue
		}
		if msgText != "" && strings.TrimSpace(msgText) != "" {
			messages = append(messages, msgText)
		}
	}

	// Разворачиваем список для хронологического порядка (от старых к новым)
	for i, j := 0, len(messages)-1; i < j; i, j = i+1, j-1 {
		messages[i], messages[j] = messages[j], messages[i]
	}

	return messages, nil
}

// GetChatContext получает контекст чата: информацию о других участниках и их последних сообщениях
// Возвращает []*domain.MessageLog для обратной совместимости
// Теперь всегда использует user_messages для получения полного контекста всех пользователей
func (d *Database) GetChatContext(chatID int64, excludeUserID int64, limit int) ([]*domain.MessageLog, error) {
	// Получаем последние сообщения всех пользователей из user_messages
	// Используем подзапрос для получения последнего сообщения каждого пользователя
	query := `
		SELECT user_id, username, chat_id, message_text as last_message
		FROM (
			SELECT user_id, username, chat_id, message_text,
			       ROW_NUMBER() OVER (PARTITION BY user_id ORDER BY created_at DESC) as rn
			FROM user_messages
			WHERE chat_id = $1 AND user_id != $2
		) AS ranked
		WHERE rn = 1
		ORDER BY user_id DESC
		LIMIT $3
	`

	rows, err := d.db.Query(query, chatID, excludeUserID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []*domain.MessageLog
	for rows.Next() {
		var msg domain.MessageLog
		err := rows.Scan(&msg.UserID, &msg.Username, &msg.ChatID, &msg.LastMessage)
		if err != nil {
			continue
		}
		// Получаем полную информацию о пользователе из message_log
		fullUserLog, err := d.GetMessageLog(msg.UserID, chatID)
		if err == nil {
			// Копируем последнее сообщение из user_messages
			fullUserLog.LastMessage = msg.LastMessage
			users = append(users, fullUserLog)
		} else {
			// Если нет записи в message_log, создаем минимальную
			msg.ChatID = chatID
			users = append(users, &msg)
		}
	}

	return users, nil
}

// GetChatWritingContext — устаревшее имя: контекст чата для режима «writing» совпадает с общим контекстом (тип чата унифицирован с training).
func (d *Database) GetChatWritingContext(chatID int64, excludeUserID int64, limit int) ([]*domain.MessageLog, error) {
	return d.GetChatContext(chatID, excludeUserID, limit)
}

// GetChatType получает тип чата; по умолчанию "training". Устаревшее значение writing нормализуется в training.
func (d *Database) GetChatType(chatID int64) (string, error) {
	query := `SELECT chat_type FROM chat_types WHERE chat_id = $1`
	var chatType string
	err := d.db.QueryRow(query, chatID).Scan(&chatType)
	if err != nil {
		// Если запись не найдена, возвращаем тип по умолчанию
		return "training", nil
	}
	if chatType == "writing" {
		return "training", nil
	}
	return chatType, nil
}

// SetChatType устанавливает тип чата (только training)
func (d *Database) SetChatType(chatID int64, chatType string) error {
	if chatType == "writing" {
		chatType = "training"
	}
	if chatType != "training" {
		return fmt.Errorf("invalid chat type: %s (must be 'training')", chatType)
	}

	query := `
		INSERT INTO chat_types (chat_id, chat_type, updated_at)
		VALUES ($1, $2, NOW() AT TIME ZONE 'Europe/Moscow')
		ON CONFLICT (chat_id) 
		DO UPDATE SET chat_type = $2, updated_at = NOW() AT TIME ZONE 'Europe/Moscow'
	`

	_, err := d.db.Exec(query, chatID, chatType)
	return err
}
