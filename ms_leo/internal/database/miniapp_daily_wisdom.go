package database

import (
	"database/sql"
	"fmt"
)

// SaveDailyWisdomOfDay сохраняет сгенерированную «мудрость дня» за конкретную дату (МСК).
// Upsert: если за день уже есть запись — перезаписываем текстом последней генерации.
func (d *Database) SaveDailyWisdomOfDay(mskDate, text string) error {
	if d == nil || mskDate == "" || text == "" {
		return nil
	}
	const q = `
		INSERT INTO daily_wisdom_log (msk_date, text)
		VALUES ($1, $2)
		ON CONFLICT (msk_date) DO UPDATE SET text = EXCLUDED.text`
	if _, err := d.db.Exec(q, mskDate, text); err != nil {
		return fmt.Errorf("save daily wisdom of day: %w", err)
	}
	return nil
}

// GetLatestDailyWisdom возвращает самую свежую «мудрость дня». ok=false, если лог пуст
// (например, до первой генерации после деплоя).
func (d *Database) GetLatestDailyWisdom() (text string, ok bool, err error) {
	if d == nil {
		return "", false, nil
	}
	const q = `SELECT text FROM daily_wisdom_log ORDER BY msk_date DESC LIMIT 1`
	err = d.db.QueryRow(q).Scan(&text)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("get latest daily wisdom: %w", err)
	}
	return text, true, nil
}

// WisdomSubscriptionSettings — настройки подписки на «мудрость дня» для пары (user, стая).
type WisdomSubscriptionSettings struct {
	Enabled    bool
	RemindHour int // 0–23, локальный час пользователя (TZ из training_state.timezone_offset_from_moscow)
}

// WisdomSubscriptionDue — кандидат на отправку: всё, что нужно планировщику, в одной строке.
type WisdomSubscriptionDue struct {
	UserID                   int64
	PackChatID               int64
	Username                 string
	RemindHour               int
	TimezoneOffsetFromMoscow int
	LastSentMskDate          sql.NullString // YYYY-MM-DD последней отправки в локальном TZ юзера (идемпотентность; имя колонки историческое)
}

// GetWisdomSubscriptionSettings — настройки подписки. Если строки нет — дефолт (ВЫКЛ, 09:00 локально).
// По умолчанию выключено: планировщик шлёт только тем, у кого есть строка с enabled=TRUE,
// поэтому витрина должна честно показывать «выкл», пока пользователь сам не включит галочку.
func (d *Database) GetWisdomSubscriptionSettings(userID, packChatID int64) (WisdomSubscriptionSettings, error) {
	def := WisdomSubscriptionSettings{Enabled: false, RemindHour: 9}
	if d == nil || userID == 0 || packChatID == 0 {
		return def, nil
	}
	const q = `SELECT enabled, remind_hour FROM miniapp_wisdom_subscriptions WHERE user_id = $1 AND pack_chat_id = $2`
	var s WisdomSubscriptionSettings
	err := d.db.QueryRow(q, userID, packChatID).Scan(&s.Enabled, &s.RemindHour)
	if err == sql.ErrNoRows {
		return def, nil
	}
	if err != nil {
		return def, fmt.Errorf("get wisdom subscription settings: %w", err)
	}
	return s, nil
}

// SaveWisdomSubscriptionSettings — upsert настроек. Смена настроек сбрасывает last_sent_msk_date,
// чтобы мудрость могла прийти сегодня же в новый час.
func (d *Database) SaveWisdomSubscriptionSettings(userID, packChatID int64, s WisdomSubscriptionSettings) error {
	if d == nil || userID == 0 || packChatID == 0 {
		return nil
	}
	if s.RemindHour < 0 || s.RemindHour > 23 {
		return fmt.Errorf("remind_hour out of range: %d", s.RemindHour)
	}
	const q = `
		INSERT INTO miniapp_wisdom_subscriptions (user_id, pack_chat_id, enabled, remind_hour, last_sent_msk_date, updated_at)
		VALUES ($1, $2, $3, $4, NULL, NOW())
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			enabled            = EXCLUDED.enabled,
			remind_hour        = EXCLUDED.remind_hour,
			last_sent_msk_date = NULL,
			updated_at         = NOW()`
	if _, err := d.db.Exec(q, userID, packChatID, s.Enabled, s.RemindHour); err != nil {
		return fmt.Errorf("save wisdom subscription settings: %w", err)
	}
	return nil
}

// ListDueWisdomSubscriptionCandidates — все включённые подписки пачки вместе с данными,
// нужными планировщику для расчёта локального часа. Фильтрацию по часу/дате делает
// вызывающий (локальный TZ считается в Go).
func (d *Database) ListDueWisdomSubscriptionCandidates(packChatID int64) ([]WisdomSubscriptionDue, error) {
	if d == nil || packChatID == 0 {
		return nil, nil
	}
	const q = `
		SELECT s.user_id,
		       s.pack_chat_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       s.remind_hour,
		       COALESCE(ts.timezone_offset_from_moscow, 0),
		       to_char(s.last_sent_msk_date, 'YYYY-MM-DD')
		FROM miniapp_wisdom_subscriptions s
		JOIN training_state ts
			ON ts.user_id = s.user_id AND ts.chat_id = s.pack_chat_id
		WHERE s.pack_chat_id = $1
		  AND s.enabled = TRUE
		  AND ts.is_deleted = FALSE`
	rows, err := d.db.Query(q, packChatID)
	if err != nil {
		return nil, fmt.Errorf("list due wisdom subscriptions: %w", err)
	}
	defer rows.Close()
	var out []WisdomSubscriptionDue
	for rows.Next() {
		var c WisdomSubscriptionDue
		if err := rows.Scan(&c.UserID, &c.PackChatID, &c.Username, &c.RemindHour,
			&c.TimezoneOffsetFromMoscow, &c.LastSentMskDate); err != nil {
			return nil, fmt.Errorf("scan wisdom subscription candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// MarkWisdomSubscriptionSent — фиксируем локальную дату юзера (YYYY-MM-DD) последней отправки (идемпотентность одной отправки в день).
func (d *Database) MarkWisdomSubscriptionSent(userID, packChatID int64, mskDate string) error {
	if d == nil || userID == 0 || packChatID == 0 || mskDate == "" {
		return nil
	}
	const q = `UPDATE miniapp_wisdom_subscriptions SET last_sent_msk_date = $3 WHERE user_id = $1 AND pack_chat_id = $2`
	if _, err := d.db.Exec(q, userID, packChatID, mskDate); err != nil {
		return fmt.Errorf("mark wisdom subscription sent: %w", err)
	}
	return nil
}
