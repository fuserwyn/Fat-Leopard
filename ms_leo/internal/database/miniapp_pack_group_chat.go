package database

import (
	"fmt"
	"time"

	"leo-bot/internal/domain"
)

// InsertMiniappPackGroupMessage — одно сообщение в общем чате мини-апpa.
func (d *Database) InsertMiniappPackGroupMessage(packChatID, fromUserID int64, username string, isLeo bool, messageText string) (int64, error) {
	const q = `
		INSERT INTO miniapp_pack_group_chat (pack_chat_id, from_user_id, username, is_leo, message_text)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`
	var id int64
	err := d.db.QueryRow(q, packChatID, fromUserID, username, isLeo, messageText).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert miniapp pack group: %w", err)
	}
	return id, nil
}

// ListMiniappPackGroupChat — последние сообщения общего чата.
func (d *Database) ListMiniappPackGroupChat(packChatID int64, limit int) ([]*domain.PackGroupChatMessage, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 200 {
		limit = 200
	}
	const q = `
		SELECT id, from_user_id, COALESCE(username, ''), is_leo, message_text, created_at
		FROM miniapp_pack_group_chat
		WHERE pack_chat_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`
	rows, err := d.db.Query(q, packChatID, limit)
	if err != nil {
		return nil, fmt.Errorf("list miniapp pack group: %w", err)
	}
	defer rows.Close()
	var items []*domain.PackGroupChatMessage
	for rows.Next() {
		var m domain.PackGroupChatMessage
		var t time.Time
		if err := rows.Scan(&m.ID, &m.UserID, &m.Username, &m.IsLeo, &m.Text, &t); err != nil {
			return nil, err
		}
		m.CreatedAt = t.UTC().Format("2006-01-02T15:04:05Z07:00")
		items = append(items, &m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// вернули DESС — в UI удобнее старые сверху, перевернём
	for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
		items[i], items[j] = items[j], items[i]
	}
	if items == nil {
		items = []*domain.PackGroupChatMessage{}
	}
	return items, nil
}
