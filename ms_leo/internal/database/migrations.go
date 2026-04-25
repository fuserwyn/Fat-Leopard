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
			DO $migrate1$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'message_log'
					  AND column_name = 'created_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE message_log ALTER COLUMN created_at TYPE TIMESTAMP WITH TIME ZONE;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'message_log'
					  AND column_name = 'updated_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE message_log ALTER COLUMN updated_at TYPE TIMESTAMP WITH TIME ZONE;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_log'
					  AND column_name = 'created_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE training_log ALTER COLUMN created_at TYPE TIMESTAMP WITH TIME ZONE;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_log'
					  AND column_name = 'updated_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE training_log ALTER COLUMN updated_at TYPE TIMESTAMP WITH TIME ZONE;
				END IF;
			END
			$migrate1$;

			ALTER TABLE message_log
			ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
			ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');

			ALTER TABLE training_log
			ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
			ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');
		`,
		DownSQL: `
			DO $migrate1down$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'message_log'
					  AND column_name = 'created_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE message_log ALTER COLUMN created_at TYPE TIMESTAMP;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'message_log'
					  AND column_name = 'updated_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE message_log ALTER COLUMN updated_at TYPE TIMESTAMP;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_log'
					  AND column_name = 'created_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE training_log ALTER COLUMN created_at TYPE TIMESTAMP;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_log'
					  AND column_name = 'updated_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE training_log ALTER COLUMN updated_at TYPE TIMESTAMP;
				END IF;
			END
			$migrate1down$;

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
			ALTER TABLE message_log 
			ADD COLUMN IF NOT EXISTS cups_earned INTEGER DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE message_log 
			DROP COLUMN IF EXISTS cups_earned;
		`,
	},
	{
		Version:     3,
		Description: "Add calorie_streak_days field to message_log table",
		UpSQL: `
			ALTER TABLE message_log 
			ADD COLUMN IF NOT EXISTS calorie_streak_days INTEGER DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE message_log 
			DROP COLUMN IF EXISTS calorie_streak_days;
		`,
	},
	{
		Version:     4,
		Description: "Add is_exempt_from_deletion field to message_log table",
		UpSQL: `
			ALTER TABLE message_log 
			ADD COLUMN IF NOT EXISTS is_exempt_from_deletion BOOLEAN DEFAULT FALSE;
		`,
		DownSQL: `
			ALTER TABLE message_log 
			DROP COLUMN IF EXISTS is_exempt_from_deletion;
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
			ALTER TABLE message_log 
			ADD COLUMN IF NOT EXISTS gender TEXT DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE message_log 
			DROP COLUMN IF EXISTS gender;
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
		Description: "Create chat_types table for storing chat type (training; legacy writing normalized to training)",
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
	{
		Version:     11,
		Description: "Add timezone_offset_from_moscow field to message_log",
		UpSQL: `
			ALTER TABLE message_log
			ADD COLUMN IF NOT EXISTS timezone_offset_from_moscow INTEGER NOT NULL DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE message_log
			DROP COLUMN IF EXISTS timezone_offset_from_moscow;
		`,
	},
	{
		Version:     12,
		Description: "Paywall: access requests linked to Telegram invoice payload",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS paywall_access_requests (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				monetized_chat_id BIGINT NOT NULL,
				status VARCHAR(32) NOT NULL DEFAULT 'pending',
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				completed_at TIMESTAMP WITH TIME ZONE,
				telegram_payment_charge_id TEXT,
				total_amount_minor INTEGER,
				currency VARCHAR(10)
			);
			CREATE INDEX IF NOT EXISTS idx_paywall_requests_user_chat
				ON paywall_access_requests (user_id, monetized_chat_id);
			CREATE INDEX IF NOT EXISTS idx_paywall_requests_status
				ON paywall_access_requests (status);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS paywall_access_requests;
		`,
	},
	{
		Version:     13,
		Description: "Paywall: monthly access expiration",
		UpSQL: `
			ALTER TABLE paywall_access_requests
			ADD COLUMN IF NOT EXISTS access_expires_at TIMESTAMP WITH TIME ZONE;
		`,
		DownSQL: `
			ALTER TABLE paywall_access_requests
			DROP COLUMN IF EXISTS access_expires_at;
		`,
	},
	{
		Version:     14,
		Description: "training_sessions.session_date TEXT -> DATE (YYYY-MM-DD)",
		UpSQL: `
			ALTER TABLE training_sessions
			ALTER COLUMN session_date TYPE DATE USING (
				CASE
					WHEN trim(session_date) ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$' THEN trim(session_date)::date
					ELSE DATE '2000-01-01'
				END
			);
		`,
		DownSQL: `
			ALTER TABLE training_sessions
			ALTER COLUMN session_date TYPE TEXT USING session_date::text;
		`,
	},
	{
		Version:     15,
		Description: "training_log: composite PK (user_id, chat_id); legacy rows chat_id=0",
		UpSQL: `
			ALTER TABLE training_log ADD COLUMN IF NOT EXISTS chat_id BIGINT NOT NULL DEFAULT 0;
			ALTER TABLE training_log DROP CONSTRAINT IF EXISTS training_log_pkey;
			ALTER TABLE training_log ADD PRIMARY KEY (user_id, chat_id);
		`,
		DownSQL: `
			ALTER TABLE training_log DROP CONSTRAINT IF EXISTS training_log_pkey;
			ALTER TABLE training_log DROP COLUMN IF EXISTS chat_id;
			ALTER TABLE training_log ADD PRIMARY KEY (user_id);
		`,
	},
	{
		Version:     16,
		Description: "Paywall: YooKassa payment id for API sync when webhook fails",
		UpSQL: `
			ALTER TABLE paywall_access_requests
			ADD COLUMN IF NOT EXISTS yookassa_payment_id TEXT;
		`,
		DownSQL: `
			ALTER TABLE paywall_access_requests
			DROP COLUMN IF EXISTS yookassa_payment_id;
		`,
	},
	{
		Version:     17,
		Description: "Leopard Money Model: XP achievements, freeze, daily XP cursor",
		UpSQL: `
			ALTER TABLE message_log ADD COLUMN IF NOT EXISTS achievement_count INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE message_log ADD COLUMN IF NOT EXISTS xp_freeze_until TIMESTAMP WITH TIME ZONE;
			ALTER TABLE message_log ADD COLUMN IF NOT EXISTS last_daily_xp_msk_date DATE;
			ALTER TABLE message_log ADD COLUMN IF NOT EXISTS leopard_starter_bonus_applied BOOLEAN NOT NULL DEFAULT FALSE;
			ALTER TABLE message_log ADD COLUMN IF NOT EXISTS last_achievement_streak_level INTEGER NOT NULL DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE message_log DROP COLUMN IF EXISTS last_achievement_streak_level;
			ALTER TABLE message_log DROP COLUMN IF EXISTS leopard_starter_bonus_applied;
			ALTER TABLE message_log DROP COLUMN IF EXISTS last_daily_xp_msk_date;
			ALTER TABLE message_log DROP COLUMN IF EXISTS xp_freeze_until;
			ALTER TABLE message_log DROP COLUMN IF EXISTS achievement_count;
		`,
	},
	{
		Version:     18,
		Description: "Deletion events audit log for DM delivery status",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS deletion_events (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				chat_id BIGINT NOT NULL,
				dm_status VARCHAR(32) NOT NULL,
				error_text TEXT,
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);

			CREATE INDEX IF NOT EXISTS idx_deletion_events_user_chat_created
				ON deletion_events (user_id, chat_id, created_at DESC);
			CREATE INDEX IF NOT EXISTS idx_deletion_events_status
				ON deletion_events (dm_status);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_deletion_events_status;
			DROP INDEX IF EXISTS idx_deletion_events_user_chat_created;
			DROP TABLE IF EXISTS deletion_events;
		`,
	},
	{
		Version:     19,
		Description: "Return analytics and lifecycle state in message_log",
		UpSQL: `
			ALTER TABLE message_log
			ADD COLUMN IF NOT EXISTS return_count INTEGER NOT NULL DEFAULT 0,
			ADD COLUMN IF NOT EXISTS returned_at TIMESTAMP WITH TIME ZONE,
			ADD COLUMN IF NOT EXISTS lifecycle_status TEXT NOT NULL DEFAULT 'active';
		`,
		DownSQL: `
			ALTER TABLE message_log
			DROP COLUMN IF EXISTS lifecycle_status,
			DROP COLUMN IF EXISTS returned_at,
			DROP COLUMN IF EXISTS return_count;
		`,
	},
	{
		Version:     20,
		Description: "Outbox events for reliable async delivery",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS outbox_events (
				id BIGSERIAL PRIMARY KEY,
				event_type TEXT NOT NULL,
				aggregate_key TEXT NOT NULL,
				payload JSONB NOT NULL,
				status TEXT NOT NULL DEFAULT 'pending',
				attempts INTEGER NOT NULL DEFAULT 0,
				next_attempt_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				last_error TEXT,
				locked_at TIMESTAMP WITH TIME ZONE,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);

			CREATE INDEX IF NOT EXISTS idx_outbox_events_ready
				ON outbox_events (status, next_attempt_at, id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_outbox_events_ready;
			DROP TABLE IF EXISTS outbox_events;
		`,
	},
	{
		Version:     21,
		Description: "Mini app shared pack group chat (messages, Leo via @leo in app only)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_pack_group_chat (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				from_user_id BIGINT NOT NULL,
				username TEXT NOT NULL DEFAULT '',
				is_leo BOOLEAN NOT NULL DEFAULT FALSE,
				message_text TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_pack_group_chat_pack_created
				ON miniapp_pack_group_chat (pack_chat_id, created_at DESC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_pack_group_chat_pack_created;
			DROP TABLE IF EXISTS miniapp_pack_group_chat;
		`,
	},
	{
		Version:     22,
		Description: "Pack group chat: telegram_message_id for TG sync with mini app",
		UpSQL: `
			ALTER TABLE miniapp_pack_group_chat
				ADD COLUMN IF NOT EXISTS telegram_message_id BIGINT;
			CREATE UNIQUE INDEX IF NOT EXISTS uq_miniapp_pack_group_telegram_msg
				ON miniapp_pack_group_chat (pack_chat_id, telegram_message_id)
				WHERE telegram_message_id IS NOT NULL;
		`,
		DownSQL: `
			DROP INDEX IF EXISTS uq_miniapp_pack_group_telegram_msg;
			ALTER TABLE miniapp_pack_group_chat DROP COLUMN IF EXISTS telegram_message_id;
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
