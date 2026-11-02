package database

import (
	"database/sql"
	"fmt"
)

// WorkoutReminderSettings — настройки напоминания «внеси тренировку» для пары (user, стая).
type WorkoutReminderSettings struct {
	Enabled    bool
	RemindHour int // 0–23, локальный час пользователя (TZ из training_state.timezone_offset_from_moscow)
}

// WorkoutReminderDue — кандидат на напоминание: всё, что нужно планировщику, в одной строке.
type WorkoutReminderDue struct {
	UserID                   int64
	PackChatID               int64
	Username                 string
	RemindHour               int
	TimezoneOffsetFromMoscow int
	LastTrainingDate         sql.NullString // YYYY-MM-DD в локальном TZ юзера
	LastSentMskDate          sql.NullString // YYYY-MM-DD последней отправки в локальном TZ юзера (идемпотентность; имя колонки историческое)
}

// GetWorkoutReminderSettings — настройки напоминания. Если строки нет — дефолт (ВЫКЛ, 19:00 локально).
// Напоминание по умолчанию выключено: планировщик шлёт только тем, у кого есть строка с enabled=TRUE,
// поэтому витрина должна честно показывать «выкл», пока пользователь сам не включит галочку.
func (d *Database) GetWorkoutReminderSettings(userID, packChatID int64) (WorkoutReminderSettings, error) {
	def := WorkoutReminderSettings{Enabled: false, RemindHour: 19}
	if d == nil || userID == 0 || packChatID == 0 {
		return def, nil
	}
	const q = `SELECT enabled, remind_hour FROM miniapp_workout_reminders WHERE user_id = $1 AND pack_chat_id = $2`
	var s WorkoutReminderSettings
	err := d.db.QueryRow(q, userID, packChatID).Scan(&s.Enabled, &s.RemindHour)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("get workout reminder settings: %w", err)
	}
	return s, nil
}

// SaveWorkoutReminderSettings — upsert настроек. Смена настроек сбрасывает last_sent_msk_date,
// чтобы напоминание могло прийти сегодня же в новый час.
func (d *Database) SaveWorkoutReminderSettings(userID, packChatID int64, s WorkoutReminderSettings) error {
	if d == nil || userID == 0 || packChatID == 0 {
		return nil
	}
	if s.RemindHour < 0 || s.RemindHour > 23 {
		return fmt.Errorf("remind_hour out of range: %d", s.RemindHour)
	}
	const q = `
		INSERT INTO miniapp_workout_reminders (user_id, pack_chat_id, enabled, remind_hour, last_sent_msk_date, updated_at)
		VALUES ($1, $2, $3, $4, NULL, NOW())
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			enabled            = EXCLUDED.enabled,
			remind_hour        = EXCLUDED.remind_hour,
			last_sent_msk_date = NULL,
			updated_at         = NOW()`
	if _, err := d.db.Exec(q, userID, packChatID, s.Enabled, s.RemindHour); err != nil {
		return fmt.Errorf("save workout reminder settings: %w", err)
	}
	return nil
}

// ListDueWorkoutReminderCandidates — все включённые напоминания пачки вместе с данными,
// нужными планировщику для расчёта локального часа и проверки «тренировался сегодня».
// Фильтрацию по часу/дате делает вызывающий (локальный TZ считается в Go).
func (d *Database) ListDueWorkoutReminderCandidates(packChatID int64) ([]WorkoutReminderDue, error) {
	if d == nil || packChatID == 0 {
		return nil, nil
	}
	const q = `
		SELECT r.user_id,
		       r.pack_chat_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       r.remind_hour,
		       COALESCE(ts.timezone_offset_from_moscow, 0),
		       ts.last_training_date,
		       to_char(r.last_sent_msk_date, 'YYYY-MM-DD')
		FROM miniapp_workout_reminders r
		JOIN training_state ts
			ON ts.user_id = r.user_id AND ts.chat_id = r.pack_chat_id
		WHERE r.pack_chat_id = $1
		  AND r.enabled = TRUE
		  AND ts.is_deleted = FALSE
		  AND ts.is_exempt_from_deletion = FALSE
		  AND NOT (ts.has_sick_leave = TRUE AND ts.has_healthy = FALSE)`
	rows, err := d.db.Query(q, packChatID)
	if err != nil {
		return nil, fmt.Errorf("list due workout reminders: %w", err)
	}
	defer rows.Close()
	var out []WorkoutReminderDue
	for rows.Next() {
		var c WorkoutReminderDue
		if err := rows.Scan(&c.UserID, &c.PackChatID, &c.Username, &c.RemindHour,
			&c.TimezoneOffsetFromMoscow, &c.LastTrainingDate, &c.LastSentMskDate); err != nil {
			return nil, fmt.Errorf("scan workout reminder candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkWorkoutReminderSent — фиксируем дату МСК последней отправки (идемпотентность одной отправки в день).
func (d *Database) MarkWorkoutReminderSent(userID, packChatID int64, mskDate string) error {
	if d == nil || userID == 0 || packChatID == 0 || mskDate == "" {
		return nil
	}
	const q = `UPDATE miniapp_workout_reminders SET last_sent_msk_date = $3 WHERE user_id = $1 AND pack_chat_id = $2`
	if _, err := d.db.Exec(q, userID, packChatID, mskDate); err != nil {
		return fmt.Errorf("mark workout reminder sent: %w", err)
	}
	return nil
}

// PackMemberRow — участник стаи для экрана «Друзья».
type PackMemberRow struct {
	UserID         int64
	Username       string
	DisplayName    string
	StreakDays     int
	Following      bool // viewer подписан на этого участника
	NotifyWorkouts bool // уведомлять viewer о тренировках target (только при Following)
}

// ListFollowingPackMembers — только леопарды, за которыми viewer следит (экран «Слежу за»).
func (d *Database) ListFollowingPackMembers(packChatID, viewerUserID int64) ([]PackMemberRow, error) {
	if d == nil || packChatID == 0 || viewerUserID == 0 {
		return nil, nil
	}
	const q = `
		SELECT ts.user_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), ''),
		       COALESCE(ts.streak_days, 0),
		       TRUE,
		       s.notify_workouts
		FROM miniapp_friend_subscriptions s
		JOIN training_state ts
			ON ts.user_id = s.target_id AND ts.chat_id = s.pack_chat_id
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = ts.user_id AND p.pack_chat_id = ts.chat_id
		WHERE s.pack_chat_id = $1
		  AND s.subscriber_id = $2
		  AND ts.is_deleted = FALSE
		ORDER BY ts.streak_days DESC, ts.user_id ASC`
	rows, err := d.db.Query(q, packChatID, viewerUserID)
	if err != nil {
		return nil, fmt.Errorf("list following pack members: %w", err)
	}
	defer rows.Close()
	var out []PackMemberRow
	for rows.Next() {
		var m PackMemberRow
		if err := rows.Scan(&m.UserID, &m.Username, &m.DisplayName, &m.StreakDays, &m.Following, &m.NotifyWorkouts); err != nil {
			return nil, fmt.Errorf("scan following pack member: %w", err)
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// AddFriendSubscription — viewer подписывается на target. Идемпотентно.
func (d *Database) AddFriendSubscription(subscriberID, targetID, packChatID int64) error {
	if d == nil || subscriberID == 0 || targetID == 0 || packChatID == 0 {
		return nil
	}
	if subscriberID == targetID {
		return fmt.Errorf("cannot subscribe to self")
	}
	// notify_workouts наследует текущее «глобальное» состояние тумблера
	// «Тренировки друзей»: если viewer выключил уведомления, новая подписка
	// не должна снова включить DM (bool_or по существующим подпискам, TRUE для первой).
	const q = `
		INSERT INTO miniapp_friend_subscriptions (subscriber_id, target_id, pack_chat_id, notify_workouts)
		VALUES ($1, $2, $3, COALESCE((
			SELECT bool_or(notify_workouts)
			FROM miniapp_friend_subscriptions
			WHERE subscriber_id = $1 AND pack_chat_id = $3
		), TRUE))
		ON CONFLICT (subscriber_id, target_id, pack_chat_id) DO NOTHING`
	if _, err := d.db.Exec(q, subscriberID, targetID, packChatID); err != nil {
		return fmt.Errorf("add friend subscription: %w", err)
	}
	return nil
}

// SetAllFriendsWorkoutNotify — глобальный тумблер «Тренировки друзей» в уведомлениях:
// вкл/выкл DM о тренировках сразу по всем подпискам viewer'а. Идемпотентно.
func (d *Database) SetAllFriendsWorkoutNotify(subscriberID, packChatID int64, enabled bool) error {
	if d == nil || subscriberID == 0 || packChatID == 0 {
		return nil
	}
	const q = `
		UPDATE miniapp_friend_subscriptions
		SET notify_workouts = $3
		WHERE subscriber_id = $1 AND pack_chat_id = $2`
	if _, err := d.db.Exec(q, subscriberID, packChatID, enabled); err != nil {
		return fmt.Errorf("set all friends workout notify: %w", err)
	}
	return nil
}

// RemoveFriendSubscription — viewer отписывается от target. Идемпотентно.
func (d *Database) RemoveFriendSubscription(subscriberID, targetID, packChatID int64) error {
	if d == nil || subscriberID == 0 || targetID == 0 || packChatID == 0 {
		return nil
	}
	const q = `DELETE FROM miniapp_friend_subscriptions WHERE subscriber_id = $1 AND target_id = $2 AND pack_chat_id = $3`
	if _, err := d.db.Exec(q, subscriberID, targetID, packChatID); err != nil {
		return fmt.Errorf("remove friend subscription: %w", err)
	}
	return nil
}

// SetFriendWorkoutNotify — вкл/выкл уведомления о тренировках target для subscriber.
func (d *Database) SetFriendWorkoutNotify(subscriberID, targetID, packChatID int64, enabled bool) error {
	if d == nil || subscriberID == 0 || targetID == 0 || packChatID == 0 {
		return nil
	}
	const q = `
		UPDATE miniapp_friend_subscriptions
		SET notify_workouts = $4
		WHERE subscriber_id = $1 AND target_id = $2 AND pack_chat_id = $3`
	res, err := d.db.Exec(q, subscriberID, targetID, packChatID, enabled)
	if err != nil {
		return fmt.Errorf("set friend workout notify: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("friend subscription not found")
	}
	return nil
}

// ListFriendWorkoutNotifySubscribers — кто хочет получать уведомления о тренировках target.
func (d *Database) ListFriendWorkoutNotifySubscribers(targetID, packChatID int64) ([]int64, error) {
	if d == nil || targetID == 0 || packChatID == 0 {
		return nil, nil
	}
	const q = `
		SELECT subscriber_id
		FROM miniapp_friend_subscriptions
		WHERE target_id = $1 AND pack_chat_id = $2 AND notify_workouts = TRUE`
	rows, err := d.db.Query(q, targetID, packChatID)
	if err != nil {
		return nil, fmt.Errorf("list friend workout notify subscribers: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan friend workout notify subscriber: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// ListFriendTargetIDs — на кого подписан subscriber в этой стае (для фильтра «Друзья» в ленте).
func (d *Database) ListFriendTargetIDs(subscriberID, packChatID int64) ([]int64, error) {
	if d == nil || subscriberID == 0 || packChatID == 0 {
		return nil, nil
	}
	const q = `SELECT target_id FROM miniapp_friend_subscriptions WHERE subscriber_id = $1 AND pack_chat_id = $2`
	rows, err := d.db.Query(q, subscriberID, packChatID)
	if err != nil {
		return nil, fmt.Errorf("list friend target ids: %w", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan friend target: %w", err)
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
