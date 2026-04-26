package database

import (
	"fmt"
	"time"

	"leo-bot/internal/domain"
)

// ListPackActivityFeed — последние «отчёты» в чате стаи: #training_done, #sick_leave, #healthy.
// streak берётся из training_state на момент выборки.
func (d *Database) ListPackActivityFeed(chatID int64, limit int) ([]*domain.PackActivityRow, error) {
	if limit <= 0 {
		limit = 50
	}
	if limit > 100 {
		limit = 100
	}
	const q = `
		SELECT
			um.id, um.user_id, um.chat_id, um.username, um.message_text, um.message_type, um.created_at,
			COALESCE(ml.streak_days, 0)::int
		FROM user_messages um
		LEFT JOIN training_state ml
			ON ml.user_id = um.user_id AND ml.chat_id = um.chat_id AND ml.is_deleted = FALSE
		WHERE um.chat_id = $1
		  AND um.message_type IN ('training_done', 'sick_leave', 'healthy')
		ORDER BY um.created_at DESC
		LIMIT $2
	`
	rows, err := d.db.Query(q, chatID, limit)
	if err != nil {
		return nil, fmt.Errorf("pack activity feed: %w", err)
	}
	defer rows.Close()

	var out []*domain.PackActivityRow
	for rows.Next() {
		var r domain.PackActivityRow
		var createdAt time.Time
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.ChatID, &r.Username, &r.MessageText, &r.MessageType, &createdAt, &r.StreakDays,
		); err != nil {
			return nil, err
		}
		r.CreatedAt = createdAt
		out = append(out, &r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []*domain.PackActivityRow{}
	}
	return out, nil
}

// UserInPackOrPaid — участник стаи (training_state) или оплаченный доступ.
func (d *Database) UserInPackOrPaid(userID, chatID int64, paywallEnabled bool) (bool, error) {
	if paywallEnabled {
		ok, err := d.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			return false, err
		}
		if ok {
			return true, nil
		}
	}
	return d.UserHasActiveMessageLogInChat(userID, chatID)
}
