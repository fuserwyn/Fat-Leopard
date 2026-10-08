package database

import (
	"database/sql"
	"fmt"
)

// AdminUserPurgeRow — сколько строк пользователя нашлось (превью) или удалилось (итог) в одной таблице.
type AdminUserPurgeRow struct {
	Label string
	Table string
	Rows  int64
}

// AdminUserPurgeReport — полный след пользователя в БД. Одна и та же структура работает
// и как превью перед удалением, и как отчёт после него.
type AdminUserPurgeReport struct {
	UserID int64
	Rows   []AdminUserPurgeRow // в порядке каскада
	Total  int64
}

// NonEmpty — только таблицы, где что-то есть: админу показываем компактный список.
func (r AdminUserPurgeReport) NonEmpty() []AdminUserPurgeRow {
	out := make([]AdminUserPurgeRow, 0, len(r.Rows))
	for _, row := range r.Rows {
		if row.Rows > 0 {
			out = append(out, row)
		}
	}
	return out
}

// userPurgeSpec — шаг каскада: по одному и тому же условию сначала считаем, потом удаляем.
type userPurgeSpec struct {
	label string
	table string
	where string // $1 = user_id, плейсхолдер может повторяться
}

// Подзапросы «свой контент»: посты в ленте, сообщения чата стаи, комментарии
// (свои + чужие под своими постами — они уезжают вместе с постом).
const (
	purgeOwnFeedPostsSQL = `SELECT id FROM user_messages WHERE user_id = $1`
	purgeOwnPackMsgsSQL  = `SELECT id FROM miniapp_pack_group_chat WHERE from_user_id = $1`
	purgeOwnThreadsSQL   = `SELECT id FROM miniapp_training_feed_thread
	                          WHERE from_user_id = $1
	                             OR user_message_id IN (SELECT id FROM user_messages WHERE user_id = $1)`
)

// purgeUserReportsWhere — жалобы, где юзер автор, нарушитель или владелец контента.
const purgeUserReportsWhere = `reporter_user_id = $1
	OR target_user_id = $1
	OR user_message_id IN (` + purgeOwnFeedPostsSQL + `)
	OR (target_type = 'pack_group_message' AND user_message_id IN (` + purgeOwnPackMsgsSQL + `))
	OR (thread_reply_id > 0 AND thread_reply_id IN (` + purgeOwnThreadsSQL + `))`

// adminUserPurgeSpecs — весь каскад в порядке удаления: зависимые строки идут раньше
// своих владельцев, поэтому ON DELETE CASCADE ничего не добирает молча и счётчики честные.
//
// Область — все чаты сразу (и pack-строка, и приватная строка в личке), чтобы после
// удаления пользователь пришёл в бота как совершенно новый.
func adminUserPurgeSpecs() []userPurgeSpec {
	return []userPurgeSpec{
		{"непрочитанные комментарии", "miniapp_training_thread_unread",
			`recipient_user_id = $1 OR thread_reply_id IN (` + purgeOwnThreadsSQL + `)`},
		{"лайки комментариев", "miniapp_training_feed_thread_likes",
			`user_id = $1 OR thread_reply_id IN (` + purgeOwnThreadsSQL + `)`},
		{"уведомления админам о жалобах", "miniapp_feed_report_admin_notifies",
			`admin_user_id = $1 OR report_id IN (SELECT id FROM miniapp_feed_reports WHERE ` + purgeUserReportsWhere + `)`},
		{"жалобы на контент", "miniapp_feed_reports", purgeUserReportsWhere},
		{"комментарии в ленте", "miniapp_training_feed_thread",
			`from_user_id = $1 OR user_message_id IN (` + purgeOwnFeedPostsSQL + `)`},
		{"реакции в ленте", "miniapp_training_feed_reactions",
			`user_id = $1 OR user_message_id IN (` + purgeOwnFeedPostsSQL + `)`},
		{"голоса в опросах", "miniapp_feed_poll_votes",
			`user_id = $1 OR user_message_id IN (` + purgeOwnFeedPostsSQL + `)`},
		{"лог уведомлений о лайках", "miniapp_feed_like_notify_log",
			`recipient_user_id = $1 OR liker_user_id = $1`},
		{"посты в ленте и сообщения", "user_messages", `user_id = $1`},
		{"непрочитанное в чате стаи", "miniapp_pack_group_unread",
			`recipient_user_id = $1 OR pack_message_id IN (` + purgeOwnPackMsgsSQL + `)`},
		{"реакции в чате стаи", "miniapp_pack_group_reactions",
			`user_id = $1 OR pack_message_id IN (` + purgeOwnPackMsgsSQL + `)`},
		{"сообщения в чате стаи", "miniapp_pack_group_chat", `from_user_id = $1`},
		{"лайки в чате с Лео", "miniapp_personal_chat_likes",
			`user_id = $1 OR message_id IN (SELECT id FROM miniapp_personal_chat WHERE user_id = $1)`},
		{"чат с Лео", "miniapp_personal_chat", `user_id = $1`},
		{"переписка с поддержкой", "miniapp_support_chat", `user_id = $1`},
		{"профиль мини-аппа", "miniapp_user_profile", `user_id = $1`},
		{"напоминания о тренировке", "miniapp_workout_reminders", `user_id = $1`},
		{"подписка на мудрость дня", "miniapp_wisdom_subscriptions", `user_id = $1`},
		{"настройка уведомлений о лайках", "miniapp_like_notifications", `user_id = $1`},
		{"контакты, присланные боту", "user_contacts", `owner_user_id = $1 OR contact_user_id = $1`},
		{"лог уведомлений о вступлении контактов", "contact_join_notify_log",
			`owner_user_id = $1 OR contact_user_id = $1`},
		{"настройка уведомлений о вступлении контактов", "miniapp_contact_join_notifications", `user_id = $1`},
		{"подписки на друзей", "miniapp_friend_subscriptions", `subscriber_id = $1 OR target_id = $1`},
		{"приглашения в челленджи", "challenge_invites", `user_id = $1`},
		{"участие в челленджах", "challenge_participants", `user_id = $1`},
		// Участия других в его челленджах уходят каскадом: название — его текст.
		{"свои челленджи", "challenges", `author_user_id = $1`},
		{"кубки за неделю стаи", "pack_weekly_goal_member_cups", `user_id = $1`},
		{"просмотренные итоги недели стаи", "pack_week_summary_seen", `user_id = $1`},
		{"тренировки", "training_sessions", `user_id = $1`},
		{"заявки на оплату доступа", "paywall_access_requests", `user_id = $1`},
		{"события удаления из стаи", "deletion_events", `user_id = $1`},
		{"визиты в бота", "bot_visits", `user_id = $1`},
		{"аналитические события", "events", `user_id = $1 OR telegram_id = $1`},
		// Незакрытый outbox воскресил бы профиль после удаления (paywall_access_restore_requested).
		{"очередь outbox", "outbox_events", `payload->>'user_id' = $1::bigint::text`},
		{"неопубликованные отложенные посты", "scheduled_admin_posts",
			`created_by = $1 AND published_at IS NULL AND canceled_at IS NULL`},
		{"права администратора", "dynamic_admins", `user_id = $1`},
		{"профиль стаи и личка (training_state)", "training_state", `user_id = $1`},
	}
}

// AdminCountUserFootprint — превью перед полным удалением: сколько строк лежит по таблицам.
func (d *Database) AdminCountUserFootprint(userID int64) (AdminUserPurgeReport, error) {
	if d == nil || userID == 0 {
		return AdminUserPurgeReport{UserID: userID}, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return AdminUserPurgeReport{}, err
	}
	defer func() { _ = tx.Rollback() }()
	return countUserPurgeTargets(tx, userID)
}

// AdminPurgeUserEverywhere — полное каскадное удаление пользователя из всех таблиц бота
// (профиль стаи, лента, чаты, оплаты, аналитика, права админа) одной транзакцией.
// Возвращает отчёт: сколько строк было в каждой таблице на момент удаления.
//
// После него пользователь для бота не существует: следующий /start и заход в мини-апп
// пройдут путь новичка с нуля.
func (d *Database) AdminPurgeUserEverywhere(userID int64) (AdminUserPurgeReport, error) {
	if d == nil || userID == 0 {
		return AdminUserPurgeReport{UserID: userID}, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return AdminUserPurgeReport{}, err
	}
	defer func() { _ = tx.Rollback() }()

	before, err := countUserPurgeTargets(tx, userID)
	if err != nil {
		return AdminUserPurgeReport{}, err
	}

	for _, spec := range adminUserPurgeSpecs() {
		q := fmt.Sprintf(`DELETE FROM %s WHERE %s`, spec.table, spec.where)
		if _, err := tx.Exec(q, userID); err != nil {
			return AdminUserPurgeReport{}, fmt.Errorf("purge %s: %w", spec.table, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return AdminUserPurgeReport{}, err
	}
	return before, nil
}

func countUserPurgeTargets(tx *sql.Tx, userID int64) (AdminUserPurgeReport, error) {
	specs := adminUserPurgeSpecs()
	report := AdminUserPurgeReport{UserID: userID, Rows: make([]AdminUserPurgeRow, 0, len(specs))}
	for _, spec := range specs {
		var n int64
		q := fmt.Sprintf(`SELECT COUNT(*) FROM %s WHERE %s`, spec.table, spec.where)
		if err := tx.QueryRow(q, userID).Scan(&n); err != nil {
			return AdminUserPurgeReport{}, fmt.Errorf("count %s: %w", spec.table, err)
		}
		report.Rows = append(report.Rows, AdminUserPurgeRow{Label: spec.label, Table: spec.table, Rows: n})
		report.Total += n
	}
	return report, nil
}
