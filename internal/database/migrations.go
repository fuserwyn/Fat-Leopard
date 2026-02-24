package database

import (
	"fmt"
)

// Migration представляет миграцию базы данных
type Migration struct {
	Version     int
	Description string
	UpSQL       string
	DownSQL     string
}

// Migrations содержит все миграции в порядке версий
var Migrations = []Migration{
	{
		Version:     1,
		Description: "Update timestamp fields to use Moscow timezone",
		UpSQL: `
			-- Обновляем поля created_at и updated_at в message_log
			ALTER TABLE message_log 
			ALTER COLUMN created_at TYPE TIMESTAMP WITH TIME ZONE,
			ALTER COLUMN updated_at TYPE TIMESTAMP WITH TIME ZONE;
			
			-- Обновляем поля created_at и updated_at в training_log
			ALTER TABLE training_log 
			ALTER COLUMN created_at TYPE TIMESTAMP WITH TIME ZONE,
			ALTER COLUMN updated_at TYPE TIMESTAMP WITH TIME ZONE;
			
			-- Устанавливаем значения по умолчанию для новых записей
			ALTER TABLE message_log 
			ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
			ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');
			
			ALTER TABLE training_log 
			ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
			ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');
		`,
		DownSQL: `
			-- Откатываем изменения для message_log
			ALTER TABLE message_log 
			ALTER COLUMN created_at TYPE TIMESTAMP,
			ALTER COLUMN updated_at TYPE TIMESTAMP;
			
			-- Откатываем изменения для training_log
			ALTER TABLE training_log 
			ALTER COLUMN created_at TYPE TIMESTAMP,
			ALTER COLUMN updated_at TYPE TIMESTAMP;
			
			-- Устанавливаем старые значения по умолчанию
			ALTER TABLE message_log 
			ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
			ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
			
			ALTER TABLE training_log 
			ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
			ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
		`,
	},
	{
		Version:     2,
		Description: "Add cups_earned field to message_log table",
		UpSQL: `
			-- Добавляем поле cups_earned в таблицу message_log
			ALTER TABLE message_log 
			ADD COLUMN cups_earned INTEGER DEFAULT 0;
		`,
		DownSQL: `
			-- Удаляем поле cups_earned из таблицы message_log
			ALTER TABLE message_log 
			DROP COLUMN cups_earned;
		`,
	},
	{
		Version:     3,
		Description: "Add calorie_streak_days field to message_log table",
		UpSQL: `
			-- Добавляем поле calorie_streak_days в таблицу message_log
			ALTER TABLE message_log 
			ADD COLUMN calorie_streak_days INTEGER DEFAULT 0;
		`,
		DownSQL: `
			-- Удаляем поле calorie_streak_days из таблицы message_log
			ALTER TABLE message_log 
			DROP COLUMN calorie_streak_days;
		`,
	},
	{
		Version:     4,
		Description: "Add is_exempt_from_deletion field to message_log table",
		UpSQL: `
			-- Добавляем поле is_exempt_from_deletion в таблицу message_log
			ALTER TABLE message_log 
			ADD COLUMN is_exempt_from_deletion BOOLEAN DEFAULT FALSE;
		`,
		DownSQL: `
			-- Удаляем поле is_exempt_from_deletion из таблицы message_log
			ALTER TABLE message_log 
			DROP COLUMN is_exempt_from_deletion;
		`,
	},
	{
		Version:     5,
		Description: "Create user_messages table for RAG context storage",
		UpSQL: `
			-- Создаем таблицу для хранения сообщений пользователей (RAG контекст)
			CREATE TABLE IF NOT EXISTS user_messages (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				chat_id BIGINT NOT NULL,
				username TEXT DEFAULT '',
				message_text TEXT NOT NULL,
				message_type TEXT DEFAULT 'general',
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			
			-- Создаем индексы для быстрого поиска
			CREATE INDEX IF NOT EXISTS idx_user_messages_user_chat ON user_messages (user_id, chat_id);
			CREATE INDEX IF NOT EXISTS idx_user_messages_created_at ON user_messages (created_at);
		`,
		DownSQL: `
			-- Удаляем индексы
			DROP INDEX IF EXISTS idx_user_messages_created_at;
			DROP INDEX IF EXISTS idx_user_messages_user_chat;
			-- Удаляем таблицу user_messages
			DROP TABLE IF EXISTS user_messages;
		`,
	},
	{
		Version:     6,
		Description: "Add gender field to message_log table",
		UpSQL: `
			-- Добавляем поле gender в таблицу message_log
			ALTER TABLE message_log 
			ADD COLUMN gender TEXT DEFAULT '';
		`,
		DownSQL: `
			-- Удаляем поле gender из таблицы message_log
			ALTER TABLE message_log 
			DROP COLUMN gender;
		`,
	},
	{
		Version:     7,
		Description: "Add sick leave approval fields to message_log",
		UpSQL: `
			ALTER TABLE message_log 
			ADD COLUMN IF NOT EXISTS sick_approval_pending BOOLEAN DEFAULT FALSE,
			ADD COLUMN IF NOT EXISTS sick_approval_deadline TIMESTAMP WITH TIME ZONE,
			ADD COLUMN IF NOT EXISTS sick_approval_message_id BIGINT;
		`,
		DownSQL: `
			ALTER TABLE message_log 
			DROP COLUMN IF EXISTS sick_approval_pending,
			DROP COLUMN IF EXISTS sick_approval_deadline,
			DROP COLUMN IF EXISTS sick_approval_message_id;
		`,
	},
	{
		Version:     8,
		Description: "Create chat_types table for storing chat type (training/writing)",
		UpSQL: `
			-- Создаем таблицу для хранения типов чатов
			CREATE TABLE IF NOT EXISTS chat_types (
				chat_id BIGINT PRIMARY KEY,
				chat_type TEXT NOT NULL DEFAULT 'training',
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				updated_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			
			-- Создаем индекс для быстрого поиска
			CREATE INDEX IF NOT EXISTS idx_chat_types_chat_type ON chat_types (chat_type);
		`,
		DownSQL: `
			-- Удаляем индекс
			DROP INDEX IF EXISTS idx_chat_types_chat_type;
			-- Удаляем таблицу chat_types
			DROP TABLE IF EXISTS chat_types;
		`,
	},
	{
		Version:     9,
		Description: "Create training_sessions table for per-session analytics",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS training_sessions (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				chat_id BIGINT NOT NULL,
				session_date TEXT NOT NULL,
				message_text TEXT DEFAULT '',
				trainings_count INTEGER NOT NULL DEFAULT 1,
				cups_added INTEGER NOT NULL DEFAULT 0,
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);

			CREATE INDEX IF NOT EXISTS idx_training_sessions_user_chat_date
				ON training_sessions (user_id, chat_id, session_date);
			CREATE INDEX IF NOT EXISTS idx_training_sessions_chat_date
				ON training_sessions (chat_id, session_date);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_training_sessions_chat_date;
			DROP INDEX IF EXISTS idx_training_sessions_user_chat_date;
			DROP TABLE IF EXISTS training_sessions;
		`,
	},
	{
		Version:     10,
		Description: "Add is_bonus field to training_sessions",
		UpSQL: `
			ALTER TABLE training_sessions
			ADD COLUMN IF NOT EXISTS is_bonus BOOLEAN NOT NULL DEFAULT FALSE;

			CREATE INDEX IF NOT EXISTS idx_training_sessions_bonus_date
				ON training_sessions (user_id, chat_id, session_date, is_bonus);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_training_sessions_bonus_date;
			ALTER TABLE training_sessions
			DROP COLUMN IF EXISTS is_bonus;
		`,
	},
}

// MigrationRecord представляет запись о выполненной миграции
type MigrationRecord struct {
	Version     int    `db:"version"`
	Description string `db:"description"`
	AppliedAt   string `db:"applied_at"`
}

// CreateMigrationsTable создает таблицу для отслеживания миграций
func (d *Database) CreateMigrationsTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS migrations (
			version INTEGER PRIMARY KEY,
			description TEXT NOT NULL,
			applied_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
		)
	`

	_, err := d.db.Exec(query)
	if err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	return nil
}

// GetAppliedMigrations получает список уже примененных миграций
func (d *Database) GetAppliedMigrations() ([]MigrationRecord, error) {
	query := `SELECT version, description, applied_at FROM migrations ORDER BY version`

	rows, err := d.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("failed to query migrations: %w", err)
	}
	defer rows.Close()

	var migrations []MigrationRecord
	for rows.Next() {
		var migration MigrationRecord
		err := rows.Scan(&migration.Version, &migration.Description, &migration.AppliedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan migration: %w", err)
		}
		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// ApplyMigration применяет миграцию
func (d *Database) ApplyMigration(migration Migration) error {
	// Начинаем транзакцию
	tx, err := d.db.Begin()
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer tx.Rollback() // Откатываем в случае ошибки

	// Выполняем SQL миграции
	_, err = tx.Exec(migration.UpSQL)
	if err != nil {
		return fmt.Errorf("failed to execute migration %d: %w", migration.Version, err)
	}

	// Записываем информацию о примененной миграции
	insertQuery := `INSERT INTO migrations (version, description) VALUES ($1, $2)`
	_, err = tx.Exec(insertQuery, migration.Version, migration.Description)
	if err != nil {
		return fmt.Errorf("failed to record migration %d: %w", migration.Version, err)
	}

	// Подтверждаем транзакцию
	err = tx.Commit()
	if err != nil {
		return fmt.Errorf("failed to commit migration %d: %w", migration.Version, err)
	}

	return nil
}

// RunMigrations выполняет все необходимые миграции
func (d *Database) RunMigrations() error {
	// Создаем таблицу миграций
	if err := d.CreateMigrationsTable(); err != nil {
		return fmt.Errorf("failed to create migrations table: %w", err)
	}

	// Получаем уже примененные миграции
	appliedMigrations, err := d.GetAppliedMigrations()
	if err != nil {
		return fmt.Errorf("failed to get applied migrations: %w", err)
	}

	// Создаем map для быстрого поиска
	appliedMap := make(map[int]bool)
	for _, migration := range appliedMigrations {
		appliedMap[migration.Version] = true
	}

	// Применяем новые миграции
	for _, migration := range Migrations {
		if !appliedMap[migration.Version] {
			fmt.Printf("Applying migration %d: %s\n", migration.Version, migration.Description)

			if err := d.ApplyMigration(migration); err != nil {
				return fmt.Errorf("failed to apply migration %d: %w", migration.Version, err)
			}

			fmt.Printf("Successfully applied migration %d\n", migration.Version)
		} else {
			fmt.Printf("Migration %d already applied, skipping\n", migration.Version)
		}
	}

	return nil
}
