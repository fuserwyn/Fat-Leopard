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
					WHERE table_schema = 'public' AND table_name = 'training_state'
					  AND column_name = 'created_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE training_state ALTER COLUMN created_at TYPE TIMESTAMP WITH TIME ZONE;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state'
					  AND column_name = 'updated_at' AND udt_name = 'timestamp') THEN
					ALTER TABLE training_state ALTER COLUMN updated_at TYPE TIMESTAMP WITH TIME ZONE;
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

			ALTER TABLE training_state
			ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
			ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');

			DO $migrate1_defaults$
			BEGIN
				-- training_log — легаси таблица; после полной очистки схемы может отсутствовать.
				IF to_regclass('public.training_log') IS NOT NULL THEN
					ALTER TABLE training_log
					ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
					ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');
				END IF;
			END
			$migrate1_defaults$;
		`,
		DownSQL: `
			DO $migrate1down$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state'
					  AND column_name = 'created_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE training_state ALTER COLUMN created_at TYPE TIMESTAMP;
				END IF;
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state'
					  AND column_name = 'updated_at' AND udt_name = 'timestamptz') THEN
					ALTER TABLE training_state ALTER COLUMN updated_at TYPE TIMESTAMP;
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

			ALTER TABLE training_state
			ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
			ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;

			DO $migrate1down_defaults$
			BEGIN
				IF to_regclass('public.training_log') IS NOT NULL THEN
					ALTER TABLE training_log
					ALTER COLUMN created_at SET DEFAULT CURRENT_TIMESTAMP,
					ALTER COLUMN updated_at SET DEFAULT CURRENT_TIMESTAMP;
				END IF;
			END
			$migrate1down_defaults$;
		`,
	},
	{
		Version:     2,
		Description: "Add cups_earned field to training_state table",
		UpSQL: `
			ALTER TABLE training_state 
			ADD COLUMN IF NOT EXISTS cups_earned INTEGER DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state 
			DROP COLUMN IF EXISTS cups_earned;
		`,
	},
	{
		Version:     3,
		Description: "Add calorie_streak_days field to training_state table",
		UpSQL: `
			ALTER TABLE training_state 
			ADD COLUMN IF NOT EXISTS calorie_streak_days INTEGER DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state 
			DROP COLUMN IF EXISTS calorie_streak_days;
		`,
	},
	{
		Version:     4,
		Description: "Add is_exempt_from_deletion field to training_state table",
		UpSQL: `
			ALTER TABLE training_state 
			ADD COLUMN IF NOT EXISTS is_exempt_from_deletion BOOLEAN DEFAULT FALSE;
		`,
		DownSQL: `
			ALTER TABLE training_state 
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
		Description: "Add gender field to training_state table",
		UpSQL: `
			ALTER TABLE training_state 
			ADD COLUMN IF NOT EXISTS gender TEXT DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE training_state 
			DROP COLUMN IF EXISTS gender;
		`,
	},
	{
		Version:     7,
		Description: "Add sick leave approval fields to training_state",
		UpSQL: `
			ALTER TABLE training_state 
			ADD COLUMN IF NOT EXISTS sick_approval_pending BOOLEAN DEFAULT FALSE,
			ADD COLUMN IF NOT EXISTS sick_approval_deadline TIMESTAMP WITH TIME ZONE,
			ADD COLUMN IF NOT EXISTS sick_approval_message_id BIGINT;
		`,
		DownSQL: `
			ALTER TABLE training_state 
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
		Description: "Add timezone_offset_from_moscow field to training_state",
		UpSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS timezone_offset_from_moscow INTEGER NOT NULL DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state
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
			DO $migrate15$
			BEGIN
				IF to_regclass('public.training_log') IS NOT NULL THEN
					ALTER TABLE training_log ADD COLUMN IF NOT EXISTS chat_id BIGINT NOT NULL DEFAULT 0;
					ALTER TABLE training_log DROP CONSTRAINT IF EXISTS training_log_pkey;
					ALTER TABLE training_log ADD PRIMARY KEY (user_id, chat_id);
				END IF;
			END
			$migrate15$;
		`,
		DownSQL: `
			DO $migrate15down$
			BEGIN
				IF to_regclass('public.training_log') IS NOT NULL THEN
					ALTER TABLE training_log DROP CONSTRAINT IF EXISTS training_log_pkey;
					ALTER TABLE training_log DROP COLUMN IF EXISTS chat_id;
					ALTER TABLE training_log ADD PRIMARY KEY (user_id);
				END IF;
			END
			$migrate15down$;
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
			ALTER TABLE training_state ADD COLUMN IF NOT EXISTS achievement_count INTEGER NOT NULL DEFAULT 0;
			ALTER TABLE training_state ADD COLUMN IF NOT EXISTS xp_freeze_until TIMESTAMP WITH TIME ZONE;
			ALTER TABLE training_state ADD COLUMN IF NOT EXISTS last_daily_xp_msk_date DATE;
			ALTER TABLE training_state ADD COLUMN IF NOT EXISTS leopard_starter_bonus_applied BOOLEAN NOT NULL DEFAULT FALSE;
			ALTER TABLE training_state ADD COLUMN IF NOT EXISTS last_achievement_streak_level INTEGER NOT NULL DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state DROP COLUMN IF EXISTS last_achievement_streak_level;
			ALTER TABLE training_state DROP COLUMN IF EXISTS leopard_starter_bonus_applied;
			ALTER TABLE training_state DROP COLUMN IF EXISTS last_daily_xp_msk_date;
			ALTER TABLE training_state DROP COLUMN IF EXISTS xp_freeze_until;
			ALTER TABLE training_state DROP COLUMN IF EXISTS achievement_count;
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
		Description: "Return analytics and lifecycle state in training_state",
		UpSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS return_count INTEGER NOT NULL DEFAULT 0,
			ADD COLUMN IF NOT EXISTS returned_at TIMESTAMP WITH TIME ZONE,
			ADD COLUMN IF NOT EXISTS lifecycle_status TEXT NOT NULL DEFAULT 'active';
		`,
		DownSQL: `
			ALTER TABLE training_state
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
	{
		Version:     24,
		Description: "Drop miniapp_personal_reply_queue (replies live in bot process memory)",
		UpSQL: `
			DROP INDEX IF EXISTS idx_miniapp_personal_reply_user_id_id;
			DROP TABLE IF EXISTS miniapp_personal_reply_queue;
		`,
		DownSQL: `SELECT 1`,
	},
	{
		Version:     25,
		Description: "Rename legacy table message_log to training_state (idempotent)",
		UpSQL: `
			DO $rename_ml$
			BEGIN
				IF to_regclass('public.message_log') IS NOT NULL
					AND to_regclass('public.training_state') IS NULL THEN
					ALTER TABLE message_log RENAME TO training_state;
				END IF;
			END
			$rename_ml$;
		`,
		DownSQL: `
			DO $rename_back$
			BEGIN
				IF to_regclass('public.training_state') IS NOT NULL
					AND to_regclass('public.message_log') IS NULL THEN
					ALTER TABLE training_state RENAME TO message_log;
				END IF;
			END
			$rename_back$;
		`,
	},
	{
		Version:     26,
		Description: "Rename training_state.calories to xp (Leopard Money)",
		UpSQL: `
			DO $m26$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'calories'
				) AND NOT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'xp'
				) THEN
					ALTER TABLE training_state RENAME COLUMN calories TO xp;
				END IF;
			END
			$m26$;
		`,
		DownSQL: `
			DO $m26d$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'xp'
				) AND NOT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'calories'
				) THEN
					ALTER TABLE training_state RENAME COLUMN xp TO calories;
				END IF;
			END
			$m26d$;
		`,
	},
	{
		Version:     27,
		Description: "Drop legacy chat_types (only training mode remains)",
		UpSQL: `
			DROP INDEX IF EXISTS idx_chat_types_chat_type;
			DROP TABLE IF EXISTS chat_types;
		`,
		DownSQL: `
			CREATE TABLE IF NOT EXISTS chat_types (
				chat_id BIGINT PRIMARY KEY,
				chat_type TEXT NOT NULL DEFAULT 'training',
				created_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				updated_at TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			CREATE INDEX IF NOT EXISTS idx_chat_types_chat_type ON chat_types (chat_type);
		`,
	},
	{
		Version:     28,
		Description: "outbox_events: fix next_attempt_at default (claim uses NOW(); MSK-naive default broke UTC DBs)",
		UpSQL: `
			ALTER TABLE outbox_events
				ALTER COLUMN next_attempt_at SET DEFAULT (NOW()),
				ALTER COLUMN created_at SET DEFAULT (NOW()),
				ALTER COLUMN updated_at SET DEFAULT (NOW());
			UPDATE outbox_events
			SET next_attempt_at = LEAST(next_attempt_at, NOW())
			WHERE status IN ('pending', 'retry')
			  AND next_attempt_at > NOW();
		`,
		DownSQL: `
			ALTER TABLE outbox_events
				ALTER COLUMN next_attempt_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				ALTER COLUMN created_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				ALTER COLUMN updated_at SET DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow');
		`,
	},
	{
		Version:     29,
		Description: "Mini app feed: reactions and thread replies on training_done",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_training_feed_reactions (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				user_message_id BIGINT NOT NULL,
				user_id BIGINT NOT NULL,
				username TEXT NOT NULL DEFAULT '',
				emoji TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (user_message_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_feed_react_msg
				ON miniapp_training_feed_reactions (user_message_id);
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_feed_react_pack
				ON miniapp_training_feed_reactions (pack_chat_id);

			CREATE TABLE IF NOT EXISTS miniapp_training_feed_thread (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				user_message_id BIGINT NOT NULL,
				from_user_id BIGINT NOT NULL,
				username TEXT NOT NULL DEFAULT '',
				message_text TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_feed_thr_msg_created
				ON miniapp_training_feed_thread (user_message_id, created_at ASC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_tr_feed_thr_msg_created;
			DROP TABLE IF EXISTS miniapp_training_feed_thread;
			DROP INDEX IF EXISTS idx_miniapp_tr_feed_react_pack;
			DROP INDEX IF EXISTS idx_miniapp_tr_feed_react_msg;
			DROP TABLE IF EXISTS miniapp_training_feed_reactions;
		`,
	},
	{
		Version:     30,
		Description: "Miniapp user profile: gender, display name, age (optional)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_user_profile (
				user_id BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				gender TEXT NOT NULL DEFAULT '',
				display_name TEXT NOT NULL DEFAULT '',
				age_years INT NULL,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				PRIMARY KEY (user_id, pack_chat_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_user_profile_user
				ON miniapp_user_profile (user_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_user_profile_user;
			DROP TABLE IF EXISTS miniapp_user_profile;
		`,
	},
	{
		Version: 31,
		// Серверное хранение личного чата юзера с Лео (приходит из мини-аппа и из ЛС с ботом).
		// Раньше история жила в localStorage браузера/устройства — между Telegram Desktop и iPhone
		// синхронизация ломалась (см. требование пользователя «история чата должна быть общая на
		// всех устройствах у одного юзера»). Теперь источник правды — БД.
		// role: 'user' — юзер пишет Лео; 'leo' — Лео отвечает (включая push-уведомления о warning'ах).
		Description: "Personal Leo chat history (cross-device sync)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_personal_chat (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				role TEXT NOT NULL CHECK (role IN ('user','leo')),
				message_text TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_personal_chat_user_pack_id
				ON miniapp_personal_chat (user_id, pack_chat_id, id DESC);
			CREATE INDEX IF NOT EXISTS idx_miniapp_personal_chat_user_pack_created
				ON miniapp_personal_chat (user_id, pack_chat_id, created_at DESC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_personal_chat_user_pack_created;
			DROP INDEX IF EXISTS idx_miniapp_personal_chat_user_pack_id;
			DROP TABLE IF EXISTS miniapp_personal_chat;
		`,
	},
	{
		Version: 32,
		// Доступ к мини-аппу — разовая покупка без подписочного срока. Раньше completion заявки
		// ставила access_expires_at = NOW() + INTERVAL '30 days'; пользователи теряли доступ через
		// 30 дней даже при ежедневных тренировках. Новый код пишет 'infinity'::timestamptz, и
		// миграция продлевает ВСЕХ уже-completed-юзеров с ещё активной 30-дневкой до infinity,
		// чтобы они не потеряли доступ при выкатывании. Кикает теперь только ExpirePaywallAccessForUser
		// (8 дней неактивности) — он по-прежнему ставит access_expires_at = NOW().
		Description: "Paywall access becomes lifetime (infinity); extend currently active completed rows",
		UpSQL: `
			UPDATE paywall_access_requests
			SET access_expires_at = 'infinity'::timestamptz
			WHERE status = 'completed'
			  AND access_expires_at IS NOT NULL
			  AND access_expires_at > NOW();
		`,
		DownSQL: `
			-- Откат: вернуть expires к NOW()+30d для тех, кому мы только что поставили infinity.
			-- Безопасно как noop для уже истёкших или pending-записей; это лучшее, что мы можем
			-- без хранения исходного значения.
			UPDATE paywall_access_requests
			SET access_expires_at = NOW() + INTERVAL '30 days'
			WHERE status = 'completed'
			  AND access_expires_at = 'infinity'::timestamptz;
		`,
	},
	{
		Version:     33,
		Description: "miniapp_user_profile.telegram_photo_url from WebApp init (feed avatars)",
		UpSQL: `
			ALTER TABLE miniapp_user_profile
			ADD COLUMN IF NOT EXISTS telegram_photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_user_profile DROP COLUMN IF EXISTS telegram_photo_url;
		`,
	},
	{
		Version:     34,
		Description: "user_messages.training_photo_url for workout photo in pack feed",
		UpSQL: `
			ALTER TABLE user_messages ADD COLUMN IF NOT EXISTS training_photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE user_messages DROP COLUMN IF EXISTS training_photo_url;
		`,
	},
	{
		Version:     35,
		Description: "miniapp_training_feed_thread.reply_to_id for thread replies",
		UpSQL: `
			ALTER TABLE miniapp_training_feed_thread ADD COLUMN IF NOT EXISTS reply_to_id BIGINT NULL
				REFERENCES miniapp_training_feed_thread(id) ON DELETE SET NULL;
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_feed_thr_reply_to ON miniapp_training_feed_thread (reply_to_id)
				WHERE reply_to_id IS NOT NULL;
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_tr_feed_thr_reply_to;
			ALTER TABLE miniapp_training_feed_thread DROP COLUMN IF EXISTS reply_to_id;
		`,
	},
	{
		Version:     36,
		Description: "miniapp unread markers for comments on author's training_done (feed tab badge)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_training_thread_unread (
				id BIGSERIAL PRIMARY KEY,
				recipient_user_id BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				thread_reply_id BIGINT NOT NULL UNIQUE,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_thread_unread_recipient
				ON miniapp_training_thread_unread (recipient_user_id, pack_chat_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_tr_thread_unread_recipient;
			DROP TABLE IF EXISTS miniapp_training_thread_unread;
		`,
	},
	{
		Version:     37,
		Description: "Likes for thread comments and personal Leo chat",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_training_feed_thread_likes (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				thread_reply_id BIGINT NOT NULL REFERENCES miniapp_training_feed_thread(id) ON DELETE CASCADE,
				user_id BIGINT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (thread_reply_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_tr_feed_thread_likes_reply
				ON miniapp_training_feed_thread_likes (thread_reply_id);

			CREATE TABLE IF NOT EXISTS miniapp_personal_chat_likes (
				id BIGSERIAL PRIMARY KEY,
				message_id BIGINT NOT NULL REFERENCES miniapp_personal_chat(id) ON DELETE CASCADE,
				user_id BIGINT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (message_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_personal_chat_likes_msg
				ON miniapp_personal_chat_likes (message_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_personal_chat_likes_msg;
			DROP TABLE IF EXISTS miniapp_personal_chat_likes;
			DROP INDEX IF EXISTS idx_miniapp_tr_feed_thread_likes_reply;
			DROP TABLE IF EXISTS miniapp_training_feed_thread_likes;
		`,
	},
	{
		Version:     38,
		Description: "training_state.max_streak_days for personal records",
		UpSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS max_streak_days INTEGER NOT NULL DEFAULT 0;
			UPDATE training_state
			SET max_streak_days = GREATEST(COALESCE(max_streak_days, 0), COALESCE(streak_days, 0));
		`,
		DownSQL: `
			ALTER TABLE training_state
			DROP COLUMN IF EXISTS max_streak_days;
		`,
	},
	{
		Version:     39,
		Description: "Paywall: idempotent post-payment welcome DM (retry / outbox)",
		UpSQL: `
			ALTER TABLE paywall_access_requests
			ADD COLUMN IF NOT EXISTS post_payment_welcome_sent_at TIMESTAMP WITH TIME ZONE;
		`,
		DownSQL: `
			ALTER TABLE paywall_access_requests
			DROP COLUMN IF EXISTS post_payment_welcome_sent_at;
		`,
	},
	{
		Version:     40,
		Description: "Drop duplicate training_state rows where chat_id = user_id if pack row exists (Telegram pack chat_id < 0)",
		UpSQL: `
			DELETE FROM training_state ts
			WHERE ts.user_id > 0
			  AND ts.chat_id = ts.user_id
			  AND EXISTS (
			    SELECT 1 FROM training_state p
			    WHERE p.user_id = ts.user_id AND p.chat_id <> ts.chat_id AND p.chat_id < 0
			  );
		`,
		DownSQL: `SELECT 1;`,
	},
	{
		Version:     41,
		Description: "Drop legacy training_state.xp column",
		UpSQL: `
			DO $m41$
			BEGIN
				IF EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'xp'
				) THEN
					ALTER TABLE training_state DROP COLUMN xp;
				END IF;
			END
			$m41$;
		`,
		DownSQL: `
			DO $m41d$
			BEGIN
				IF NOT EXISTS (
					SELECT 1 FROM information_schema.columns
					WHERE table_schema = 'public' AND table_name = 'training_state' AND column_name = 'xp'
				) THEN
					ALTER TABLE training_state ADD COLUMN xp INTEGER DEFAULT 0;
					UPDATE training_state SET xp = COALESCE(cups_earned, 0);
				END IF;
			END
			$m41d$;
		`,
	},
	{
		Version:     42,
		Description: "Separate miniapp support chat from Leo personal chat",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_support_chat (
				id BIGSERIAL PRIMARY KEY,
				user_id BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				role TEXT NOT NULL CHECK (role IN ('user','support')),
				message_text TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_support_chat_user_pack_id
				ON miniapp_support_chat (user_id, pack_chat_id, id DESC);
			CREATE INDEX IF NOT EXISTS idx_miniapp_support_chat_user_pack_created
				ON miniapp_support_chat (user_id, pack_chat_id, created_at DESC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_support_chat_user_pack_created;
			DROP INDEX IF EXISTS idx_miniapp_support_chat_user_pack_id;
			DROP TABLE IF EXISTS miniapp_support_chat;
		`,
	},
	{
		Version:     43,
		Description: "Miniapp feed poll votes for admin polls",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_feed_poll_votes (
				id BIGSERIAL PRIMARY KEY,
				user_message_id BIGINT NOT NULL REFERENCES user_messages(id) ON DELETE CASCADE,
				user_id BIGINT NOT NULL,
				option_index INTEGER NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (user_message_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_feed_poll_votes_message
				ON miniapp_feed_poll_votes (user_message_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_feed_poll_votes_message;
			DROP TABLE IF EXISTS miniapp_feed_poll_votes;
		`,
	},
	{
		Version:     44,
		Description: "Drop legacy training_state.calorie_streak_days",
		UpSQL: `
			ALTER TABLE training_state
			DROP COLUMN IF EXISTS calorie_streak_days;
		`,
		DownSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS calorie_streak_days INTEGER DEFAULT 0;
		`,
	},
	{
		Version:     45,
		Description: "Streak save attempts: lifetime counter for manual saves",
		UpSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS streak_save_attempts_used INTEGER NOT NULL DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state
			DROP COLUMN IF EXISTS streak_save_attempts_used;
		`,
	},
	{
		Version:     46,
		Description: "Miniapp feed content reports for admin moderation",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_feed_reports (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				reporter_user_id BIGINT NOT NULL,
				target_type TEXT NOT NULL CHECK (target_type IN ('feed_post', 'thread_reply')),
				user_message_id BIGINT NOT NULL,
				thread_reply_id BIGINT NOT NULL DEFAULT 0,
				target_user_id BIGINT NOT NULL,
				target_text TEXT NOT NULL,
				status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'dismissed')),
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (reporter_user_id, user_message_id, thread_reply_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_feed_reports_pack_status_created
				ON miniapp_feed_reports (pack_chat_id, status, created_at DESC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_feed_reports_pack_status_created;
			DROP TABLE IF EXISTS miniapp_feed_reports;
		`,
	},
	{
		Version:     47,
		Description: "Pack group chat replies and unread markers for reply notifications",
		UpSQL: `
			ALTER TABLE miniapp_pack_group_chat ADD COLUMN IF NOT EXISTS reply_to_id BIGINT NULL
				REFERENCES miniapp_pack_group_chat(id) ON DELETE SET NULL;
			CREATE INDEX IF NOT EXISTS idx_miniapp_pack_group_reply_to ON miniapp_pack_group_chat (reply_to_id)
				WHERE reply_to_id IS NOT NULL;

			CREATE TABLE IF NOT EXISTS miniapp_pack_group_unread (
				id BIGSERIAL PRIMARY KEY,
				recipient_user_id BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				pack_message_id BIGINT NOT NULL UNIQUE,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_pack_group_unread_recipient
				ON miniapp_pack_group_unread (recipient_user_id, pack_chat_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_pack_group_unread_recipient;
			DROP TABLE IF EXISTS miniapp_pack_group_unread;
			DROP INDEX IF EXISTS idx_miniapp_pack_group_reply_to;
			ALTER TABLE miniapp_pack_group_chat DROP COLUMN IF EXISTS reply_to_id;
		`,
	},
	{
		Version:     48,
		Description: "UGC moderation: soft hide, mute, violation counter",
		UpSQL: `
			ALTER TABLE training_state
				ADD COLUMN IF NOT EXISTS ugc_muted_until TIMESTAMPTZ,
				ADD COLUMN IF NOT EXISTS ugc_violation_count INTEGER NOT NULL DEFAULT 0;

			ALTER TABLE user_messages
				ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;

			ALTER TABLE miniapp_training_feed_thread
				ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;

			ALTER TABLE miniapp_pack_group_chat
				ADD COLUMN IF NOT EXISTS is_hidden BOOLEAN NOT NULL DEFAULT FALSE;

			CREATE INDEX IF NOT EXISTS idx_user_messages_feed_visible
				ON user_messages (chat_id, created_at DESC)
				WHERE is_hidden = FALSE;
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_user_messages_feed_visible;
			ALTER TABLE miniapp_pack_group_chat DROP COLUMN IF EXISTS is_hidden;
			ALTER TABLE miniapp_training_feed_thread DROP COLUMN IF EXISTS is_hidden;
			ALTER TABLE user_messages DROP COLUMN IF EXISTS is_hidden;
			ALTER TABLE training_state DROP COLUMN IF EXISTS ugc_muted_until;
			ALTER TABLE training_state DROP COLUMN IF EXISTS ugc_violation_count;
		`,
	},
	{
		Version:     50,
		Description: "Pack group chat emoji reactions",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_pack_group_reactions (
				id BIGSERIAL PRIMARY KEY,
				pack_chat_id BIGINT NOT NULL,
				pack_message_id BIGINT NOT NULL REFERENCES miniapp_pack_group_chat(id) ON DELETE CASCADE,
				user_id BIGINT NOT NULL,
				username TEXT NOT NULL DEFAULT '',
				emoji TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				UNIQUE (pack_message_id, user_id)
			);
			CREATE INDEX IF NOT EXISTS idx_miniapp_pg_react_msg
				ON miniapp_pack_group_reactions (pack_message_id);
			CREATE INDEX IF NOT EXISTS idx_miniapp_pg_react_pack
				ON miniapp_pack_group_reactions (pack_chat_id);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_miniapp_pg_react_pack;
			DROP INDEX IF EXISTS idx_miniapp_pg_react_msg;
			DROP TABLE IF EXISTS miniapp_pack_group_reactions;
		`,
	},
	{
		Version:     51,
		Description: "Feed reports: allow pack group chat messages",
		UpSQL: `
			ALTER TABLE miniapp_feed_reports DROP CONSTRAINT IF EXISTS miniapp_feed_reports_target_type_check;
			ALTER TABLE miniapp_feed_reports ADD CONSTRAINT miniapp_feed_reports_target_type_check
				CHECK (target_type IN ('feed_post', 'thread_reply', 'pack_group_message'));
		`,
		DownSQL: `
			ALTER TABLE miniapp_feed_reports DROP CONSTRAINT IF EXISTS miniapp_feed_reports_target_type_check;
			ALTER TABLE miniapp_feed_reports ADD CONSTRAINT miniapp_feed_reports_target_type_check
				CHECK (target_type IN ('feed_post', 'thread_reply'));
		`,
	},
	{
		Version:     52,
		Description: "Dynamic admins and bot visits tracking",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS dynamic_admins (
				user_id      BIGINT PRIMARY KEY,
				username     TEXT    NOT NULL DEFAULT '',
				added_by     BIGINT  NOT NULL DEFAULT 0,
				added_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);

			CREATE TABLE IF NOT EXISTS bot_visits (
				id           BIGSERIAL PRIMARY KEY,
				user_id      BIGINT  NOT NULL,
				username     TEXT    NOT NULL DEFAULT '',
				first_name   TEXT    NOT NULL DEFAULT '',
				last_name    TEXT    NOT NULL DEFAULT '',
				visited_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS idx_bot_visits_user_id    ON bot_visits (user_id);
			CREATE INDEX IF NOT EXISTS idx_bot_visits_visited_at ON bot_visits (visited_at DESC);
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_bot_visits_visited_at;
			DROP INDEX IF EXISTS idx_bot_visits_user_id;
			DROP TABLE IF EXISTS bot_visits;
			DROP TABLE IF EXISTS dynamic_admins;
		`,
	},
	{
		Version:     53,
		Description: "Pack group chat: edited_at marker for edited messages",
		UpSQL: `
			ALTER TABLE miniapp_pack_group_chat
				ADD COLUMN IF NOT EXISTS edited_at TIMESTAMP WITH TIME ZONE NULL;
		`,
		DownSQL: `
			ALTER TABLE miniapp_pack_group_chat DROP COLUMN IF EXISTS edited_at;
		`,
	},
	{
		Version:     54,
		Description: "Pack group chat: photo attachments (photo_url)",
		UpSQL: `
			ALTER TABLE miniapp_pack_group_chat
				ADD COLUMN IF NOT EXISTS photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_pack_group_chat DROP COLUMN IF EXISTS photo_url;
		`,
	},
	{
		Version:     55,
		Description: "Analytics: events table (analytics_BT_v1) — funnel/KPI event log",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS events (
				id              BIGSERIAL PRIMARY KEY,
				event_name      VARCHAR(64) NOT NULL,
				user_id         BIGINT,
				telegram_id     BIGINT,
				occurred_at     TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				payload         JSONB,
				session_id      UUID,
				source          VARCHAR(32),
				app_version     VARCHAR(16),
				idempotency_key VARCHAR(128),
				created_at      TIMESTAMP WITH TIME ZONE DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			CREATE INDEX IF NOT EXISTS idx_events_name_time ON events (event_name, occurred_at);
			CREATE INDEX IF NOT EXISTS idx_events_user_time ON events (user_id, occurred_at);
			CREATE INDEX IF NOT EXISTS idx_events_tg_name  ON events (telegram_id, event_name);
			-- Идемпотентность финансовых событий (§9.2): payment_id и т.п. в idempotency_key.
			CREATE UNIQUE INDEX IF NOT EXISTS uq_events_idempotency
				ON events (idempotency_key) WHERE idempotency_key IS NOT NULL;
		`,
		DownSQL: `
			DROP TABLE IF EXISTS events;
		`,
	},
	{
		Version:     56,
		Description: "Analytics: events.is_alpha — пометка событий альфа-тестеров (§10)",
		UpSQL: `
			ALTER TABLE events ADD COLUMN IF NOT EXISTS is_alpha BOOLEAN NOT NULL DEFAULT FALSE;
			CREATE INDEX IF NOT EXISTS idx_events_alpha_name ON events (is_alpha, event_name) WHERE is_alpha;
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_events_alpha_name;
			ALTER TABLE events DROP COLUMN IF EXISTS is_alpha;
		`,
	},
	{
		Version:     57,
		Description: "Scheduled admin feed posts (отложенные посты + автор Лео/Админ)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS scheduled_admin_posts (
				id           BIGSERIAL PRIMARY KEY,
				chat_id      BIGINT NOT NULL,
				author       VARCHAR(16) NOT NULL DEFAULT 'admin',
				message_text TEXT NOT NULL,
				scheduled_at TIMESTAMP WITH TIME ZONE NOT NULL,
				created_by   BIGINT NOT NULL,
				published_at TIMESTAMP WITH TIME ZONE,
				canceled_at  TIMESTAMP WITH TIME ZONE,
				created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
			CREATE INDEX IF NOT EXISTS idx_sched_admin_posts_due
				ON scheduled_admin_posts (scheduled_at)
				WHERE published_at IS NULL AND canceled_at IS NULL;
		`,
		DownSQL: `
			DROP TABLE IF EXISTS scheduled_admin_posts;
		`,
	},
	{
		Version:     58,
		Description: "Личный чат с Лео: фото к сообщению (vision-рекомендации)",
		UpSQL: `
			ALTER TABLE miniapp_personal_chat ADD COLUMN IF NOT EXISTS photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_personal_chat DROP COLUMN IF EXISTS photo_url;
		`,
	},
	{
		Version:     59,
		Description: "Закреп объявлений админа в ленте стаи (user_messages.pinned_at)",
		UpSQL: `
			ALTER TABLE user_messages ADD COLUMN IF NOT EXISTS pinned_at TIMESTAMP WITH TIME ZONE;
			CREATE INDEX IF NOT EXISTS idx_user_messages_pinned
				ON user_messages (chat_id, pinned_at DESC)
				WHERE pinned_at IS NOT NULL;
		`,
		DownSQL: `
			DROP INDEX IF EXISTS idx_user_messages_pinned;
			ALTER TABLE user_messages DROP COLUMN IF EXISTS pinned_at;
		`,
	},
	{
		Version:     60,
		Description: "Напоминания внести тренировку + подписки на друзей стаи",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_workout_reminders (
				user_id            BIGINT NOT NULL,
				pack_chat_id       BIGINT NOT NULL,
				enabled            BOOLEAN NOT NULL DEFAULT TRUE,
				remind_hour        SMALLINT NOT NULL DEFAULT 19,
				last_sent_msk_date DATE,
				updated_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				PRIMARY KEY (user_id, pack_chat_id)
			);

			CREATE TABLE IF NOT EXISTS miniapp_friend_subscriptions (
				subscriber_id BIGINT NOT NULL,
				target_id     BIGINT NOT NULL,
				pack_chat_id  BIGINT NOT NULL,
				created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				PRIMARY KEY (subscriber_id, target_id, pack_chat_id)
			);
			CREATE INDEX IF NOT EXISTS idx_friend_subs_target
				ON miniapp_friend_subscriptions (target_id, pack_chat_id);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS miniapp_friend_subscriptions;
			DROP TABLE IF EXISTS miniapp_workout_reminders;
		`,
	},
	{
		Version:     61,
		Description: "Фото в комментариях ленты стаи (miniapp_training_feed_thread.photo_url) + vision Лео",
		UpSQL: `
			ALTER TABLE miniapp_training_feed_thread ADD COLUMN IF NOT EXISTS photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_training_feed_thread DROP COLUMN IF EXISTS photo_url;
		`,
	},
	{
		Version:     62,
		Description: "Голос комментария админа: miniapp_training_feed_thread.posted_as (self/leo/admin)",
		UpSQL: `
			ALTER TABLE miniapp_training_feed_thread ADD COLUMN IF NOT EXISTS posted_as TEXT NOT NULL DEFAULT 'self';
		`,
		DownSQL: `
			ALTER TABLE miniapp_training_feed_thread DROP COLUMN IF EXISTS posted_as;
		`,
	},
	{
		Version:     63,
		Description: "Уведомления о тренировках леопардов из «Слежу за»",
		UpSQL: `
			ALTER TABLE miniapp_friend_subscriptions
				ADD COLUMN IF NOT EXISTS notify_workouts BOOLEAN NOT NULL DEFAULT TRUE;
		`,
		DownSQL: `
			ALTER TABLE miniapp_friend_subscriptions DROP COLUMN IF EXISTS notify_workouts;
		`,
	},
	{
		Version:     64,
		Description: "edited_at для комментариев ленты и постов user_messages",
		UpSQL: `
			ALTER TABLE miniapp_training_feed_thread
				ADD COLUMN IF NOT EXISTS edited_at TIMESTAMP WITH TIME ZONE;
			ALTER TABLE user_messages
				ADD COLUMN IF NOT EXISTS edited_at TIMESTAMP WITH TIME ZONE;
		`,
		DownSQL: `
			ALTER TABLE miniapp_training_feed_thread DROP COLUMN IF EXISTS edited_at;
			ALTER TABLE user_messages DROP COLUMN IF EXISTS edited_at;
		`,
	},
	{
		Version:     65,
		Description: "DM-уведомления админам о жалобах в ленте (для кнопки «Решено»)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_feed_report_admin_notifies (
				report_id BIGINT NOT NULL REFERENCES miniapp_feed_reports(id) ON DELETE CASCADE,
				admin_user_id BIGINT NOT NULL,
				chat_id BIGINT NOT NULL,
				message_id BIGINT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				PRIMARY KEY (report_id, admin_user_id)
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS miniapp_feed_report_admin_notifies;
		`,
	},
	{
		Version:     66,
		Description: "Напоминание о тренировке выключено по умолчанию (opt-in)",
		UpSQL: `
			ALTER TABLE miniapp_workout_reminders ALTER COLUMN enabled SET DEFAULT FALSE;
		`,
		DownSQL: `
			ALTER TABLE miniapp_workout_reminders ALTER COLUMN enabled SET DEFAULT TRUE;
		`,
	},
	{
		Version:     67,
		Description: "Сброс существующих напоминаний в выкл: opt-in для всех (галочка отжата по умолчанию)",
		// Колонка enabled исторически была DEFAULT TRUE, поэтому у части пользователей
		// строки уже стоят с enabled=TRUE, хотя они не включали галочку осознанно.
		// Разово гасим всё — дальше включение только вручную.
		UpSQL: `
			UPDATE miniapp_workout_reminders SET enabled = FALSE WHERE enabled = TRUE;
		`,
		DownSQL: ``,
	},
	{
		Version:     68,
		Description: "Снапшот кубков при удалении (cups_at_deletion) — для восстановления аккаунта админом",
		UpSQL: `
			ALTER TABLE training_state
			ADD COLUMN IF NOT EXISTS cups_at_deletion INTEGER DEFAULT 0;
		`,
		DownSQL: `
			ALTER TABLE training_state
			DROP COLUMN IF EXISTS cups_at_deletion;
		`,
	},
	{
		Version:     69,
		Description: "Причина скрытия поста (hidden_reason) — разделение «удалено админом» / «скрыто по жалобе»",
		UpSQL: `
			ALTER TABLE user_messages
			ADD COLUMN IF NOT EXISTS hidden_reason TEXT;
		`,
		DownSQL: `
			ALTER TABLE user_messages
			DROP COLUMN IF EXISTS hidden_reason;
		`,
	},
	{
		Version:     70,
		Description: "Подписка на «мудрость дня» в личку бота + суточный лог сгенерированной мудрости",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS daily_wisdom_log (
				msk_date   DATE PRIMARY KEY,
				text       TEXT NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);

			CREATE TABLE IF NOT EXISTS miniapp_wisdom_subscriptions (
				user_id            BIGINT NOT NULL,
				pack_chat_id       BIGINT NOT NULL,
				enabled            BOOLEAN NOT NULL DEFAULT FALSE,
				remind_hour        SMALLINT NOT NULL DEFAULT 9,
				last_sent_msk_date DATE,
				updated_at         TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				PRIMARY KEY (user_id, pack_chat_id)
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS miniapp_wisdom_subscriptions;
			DROP TABLE IF EXISTS daily_wisdom_log;
		`,
	},
	{
		Version:     71,
		Description: "Единая лента: baseline id общего чата (старые сообщения в ленту не тянем)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS unified_feed_meta (
				id                       INT PRIMARY KEY DEFAULT 1,
				pack_message_baseline_id BIGINT NOT NULL DEFAULT 0,
				CONSTRAINT unified_feed_meta_singleton CHECK (id = 1)
			);
			-- Фиксируем текущий максимум id чата: в объединённую ленту попадут только
			-- сообщения, созданные после накатывания этой миграции (id > baseline).
			INSERT INTO unified_feed_meta (id, pack_message_baseline_id)
			SELECT 1, COALESCE(MAX(id), 0) FROM miniapp_pack_group_chat
			ON CONFLICT (id) DO NOTHING;
		`,
		DownSQL: `
			DROP TABLE IF EXISTS unified_feed_meta;
		`,
	},
	{
		Version:     72,
		Description: "Уведомления о лайках в ленте: настройка вкл/выкл + лог отправленных (идемпотентность)",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS miniapp_like_notifications (
				user_id      BIGINT NOT NULL,
				pack_chat_id BIGINT NOT NULL,
				enabled      BOOLEAN NOT NULL DEFAULT FALSE,
				updated_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				PRIMARY KEY (user_id, pack_chat_id)
			);

			-- Один DM на пару (получатель, лайкнувший, цель): переставленный лайк не спамит.
			CREATE TABLE IF NOT EXISTS miniapp_feed_like_notify_log (
				recipient_user_id BIGINT NOT NULL,
				pack_chat_id      BIGINT NOT NULL,
				liker_user_id     BIGINT NOT NULL,
				target_kind       TEXT   NOT NULL,
				target_id         BIGINT NOT NULL,
				created_at        TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				PRIMARY KEY (recipient_user_id, liker_user_id, target_kind, target_id)
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS miniapp_feed_like_notify_log;
			DROP TABLE IF EXISTS miniapp_like_notifications;
		`,
	},
	{
		Version:     73,
		Description: "Донаты из профиля мини-аппа: добровольная поддержка (Telegram Stars / ЮKassa)",
		UpSQL: `
			-- Донат не даёт доступ и не связан с paywall_access_requests: это отдельная сущность,
			-- чтобы добровольная поддержка не влияла на вход в стаю и на кик за неактивность.
			CREATE TABLE IF NOT EXISTS donations (
				id                         BIGSERIAL PRIMARY KEY,
				user_id                    BIGINT NOT NULL,
				provider                   VARCHAR(16) NOT NULL,
				status                     VARCHAR(16) NOT NULL DEFAULT 'pending',
				amount_minor               INTEGER NOT NULL,
				currency                   VARCHAR(10) NOT NULL,
				telegram_payment_charge_id TEXT,
				yookassa_payment_id        TEXT,
				thanks_sent_at             TIMESTAMP WITH TIME ZONE,
				created_at                 TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
				completed_at               TIMESTAMP WITH TIME ZONE
			);

			CREATE INDEX IF NOT EXISTS donations_user_status_idx ON donations (user_id, status);
			-- Повторное уведомление ЮKassa по тому же платежу не должно создавать второй донат.
			CREATE UNIQUE INDEX IF NOT EXISTS donations_yookassa_payment_id_key
				ON donations (yookassa_payment_id) WHERE yookassa_payment_id IS NOT NULL;
		`,
		DownSQL: `
			DROP TABLE IF EXISTS donations;
		`,
	},
	{
		Version:     74,
		Description: "Цена доступа в стаю, которую админ задаёт из мини-аппа",
		UpSQL: `
			-- Одна строка на стаю: если её нет — берём сумму из env (PAYMENT_AMOUNT_* / YOOKASSA).
			-- amount_minor — копейки RUB. Звёзды Telegram по-прежнему из env.
			CREATE TABLE IF NOT EXISTS pack_paywall_settings (
				pack_chat_id BIGINT PRIMARY KEY,
				amount_minor INTEGER NOT NULL CHECK (amount_minor > 0 AND amount_minor <= 10000000),
				updated_by   BIGINT  NOT NULL DEFAULT 0,
				updated_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow')
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS pack_paywall_settings;
		`,
	},
	{
		Version:     75,
		Description: "Вход в десктопное приложение через подтверждение в чате",
		UpSQL: `
			-- В обычном окне на компьютере объекта Telegram.WebApp нет, подписанного
			-- initData взять неоткуда. Приложение открывает t.me/<bot>?start=auth_<nonce>,
			-- человек жмёт «Войти» в чате, и мы выдаём токен сессии.
			-- token_plain — одноразовая ячейка выдачи: живёт минуты и стирается при
			-- первой же отдаче приложению.
			CREATE TABLE IF NOT EXISTS desktop_logins (
				nonce       TEXT PRIMARY KEY,
				user_id     BIGINT,
				status      TEXT NOT NULL DEFAULT 'pending',
				token_plain TEXT,
				created_at  TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				expires_at  TIMESTAMP WITH TIME ZONE NOT NULL
			);

			-- На руках у приложения токен, у нас только его хеш: дамп базы не должен
			-- давать возможность зайти под чужим аккаунтом.
			CREATE TABLE IF NOT EXISTS desktop_sessions (
				token_hash   TEXT PRIMARY KEY,
				user_id      BIGINT NOT NULL,
				created_at   TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				last_used_at TIMESTAMP WITH TIME ZONE,
				revoked_at   TIMESTAMP WITH TIME ZONE
			);
			CREATE INDEX IF NOT EXISTS idx_desktop_sessions_user
				ON desktop_sessions (user_id, created_at DESC);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS desktop_sessions;
			DROP TABLE IF EXISTS desktop_logins;
		`,
	},
	{
		Version:     76,
		Description: "Автономный режим Лео: он сам придумывает задачи трекера",
		UpSQL: `
			-- Лео сам ставит задачи раз в несколько часов, пока включён режим.
			-- Одна строка на окружение: включение — это не событие, а состояние,
			-- и админам важно видеть, до какого числа он ещё «сам себе продукт».
			CREATE TABLE IF NOT EXISTS leo_autonomy (
				id            BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (id),
				active_until  TIMESTAMP WITH TIME ZONE,
				every_hours   INT NOT NULL DEFAULT 4,
				tasks_per_run INT NOT NULL DEFAULT 3,
				last_run_at   TIMESTAMP WITH TIME ZONE,
				last_note     TEXT,
				updated_by    BIGINT,
				updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS leo_autonomy;
		`,
	},
	{
		Version:     77,
		Description: "Своя доска задач в админке: задачи и вложения",
		UpSQL: `
			-- Доска живёт у нас, а не на чужом трекере: карточки, QA и фото
			-- остаются в базе стаи и не зависят от гостевой сессии снаружи.
			CREATE TABLE IF NOT EXISTS pack_tracker_tasks (
				id            BIGSERIAL PRIMARY KEY,
				num           INT NOT NULL,
				prompt        TEXT NOT NULL,
				when_at       TIMESTAMP WITH TIME ZONE NOT NULL,
				when_label    TEXT NOT NULL DEFAULT '',
				repeat        TEXT NOT NULL DEFAULT 'разово',
				kind          TEXT NOT NULL DEFAULT 'task',
				status        TEXT NOT NULL DEFAULT 'pending',
				dev_column    TEXT NOT NULL DEFAULT 'todo',
				qa_column     TEXT,
				qa_status     TEXT,
				handed_to_qa  BOOLEAN NOT NULL DEFAULT FALSE,
				auto_review   BOOLEAN NOT NULL DEFAULT FALSE,
				manual_qa     BOOLEAN NOT NULL DEFAULT FALSE,
				fast_track    BOOLEAN NOT NULL DEFAULT FALSE,
				auto_push     BOOLEAN NOT NULL DEFAULT FALSE,
				error         TEXT NOT NULL DEFAULT '',
				result        TEXT NOT NULL DEFAULT '',
				steps         JSONB NOT NULL DEFAULT '[]'::jsonb,
				author_id     BIGINT,
				created_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				last_run_at   TIMESTAMP WITH TIME ZONE,
				updated_at    TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE UNIQUE INDEX IF NOT EXISTS pack_tracker_tasks_num_uidx
				ON pack_tracker_tasks (num);
			CREATE INDEX IF NOT EXISTS pack_tracker_tasks_board_idx
				ON pack_tracker_tasks (dev_column, created_at DESC);

			CREATE TABLE IF NOT EXISTS pack_tracker_attachments (
				id         TEXT PRIMARY KEY,
				task_id    BIGINT NOT NULL REFERENCES pack_tracker_tasks(id) ON DELETE CASCADE,
				name       TEXT NOT NULL,
				mime       TEXT NOT NULL,
				size       INT NOT NULL,
				data       BYTEA NOT NULL,
				created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
			CREATE INDEX IF NOT EXISTS pack_tracker_attachments_task_idx
				ON pack_tracker_attachments (task_id, created_at);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS pack_tracker_attachments;
			DROP TABLE IF EXISTS pack_tracker_tasks;
		`,
	},
	{
		Version:     78,
		Description: "Переопределения промптов Леопарда из админки",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS leo_prompt_overrides (
				key        TEXT PRIMARY KEY,
				body       TEXT NOT NULL,
				filename   TEXT NOT NULL DEFAULT '',
				updated_by BIGINT,
				updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW()
			);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS leo_prompt_overrides;
		`,
	},
	{
		Version:     79,
		Description: "miniapp_user_profile.theme — тема мини-аппа переживает закрытие WebView",
		UpSQL: `
			ALTER TABLE miniapp_user_profile
			ADD COLUMN IF NOT EXISTS theme TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_user_profile DROP COLUMN IF EXISTS theme;
		`,
	},
	{
		Version:     80,
		Description: "miniapp_support_chat.photo_url — фото в чате поддержки",
		UpSQL: `
			ALTER TABLE miniapp_support_chat
			ADD COLUMN IF NOT EXISTS photo_url TEXT NOT NULL DEFAULT '';
		`,
		DownSQL: `
			ALTER TABLE miniapp_support_chat DROP COLUMN IF EXISTS photo_url;
		`,
	},
	{
		Version:     81,
		Description: "pack_weekly_goal_bonus — бонусная тема стаи после недельной цели",
		UpSQL: `
			CREATE TABLE IF NOT EXISTS pack_weekly_goal_bonus (
				pack_chat_id    BIGINT NOT NULL,
				week_start_date DATE NOT NULL,
				bonus_until     TIMESTAMP WITH TIME ZONE NOT NULL,
				created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
				PRIMARY KEY (pack_chat_id, week_start_date)
			);
			CREATE INDEX IF NOT EXISTS pack_weekly_goal_bonus_active_idx
				ON pack_weekly_goal_bonus (pack_chat_id, bonus_until DESC);
		`,
		DownSQL: `
			DROP TABLE IF EXISTS pack_weekly_goal_bonus;
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
