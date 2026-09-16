package database

import (
	"fmt"

	"leo-bot/internal/trainingfeed"
)

// BackfillTrainingFeedReactionEmojis — заменяет недопустимые реакции на отчётах training_done
// (в т.ч. реакции Лео) на одобряющие и соответствующие виду спорта.
func (d *Database) BackfillTrainingFeedReactionEmojis() (updated int, err error) {
	rows, err := d.db.Query(`
		SELECT r.id, r.user_message_id, r.emoji, um.text
		FROM miniapp_training_feed_reactions r
		INNER JOIN user_messages um ON um.id = r.user_message_id
		WHERE um.type = 'training_done'
	`)
	if err != nil {
		return 0, fmt.Errorf("backfill training feed reactions select: %w", err)
	}
	defer rows.Close()

	type row struct {
		id            int64
		userMessageID int64
		emoji         string
		reportText    string
	}
	var batch []row
	for rows.Next() {
		var r row
		if err := rows.Scan(&r.id, &r.userMessageID, &r.emoji, &r.reportText); err != nil {
			return updated, fmt.Errorf("backfill training feed reactions scan: %w", err)
		}
		batch = append(batch, r)
	}
	if err := rows.Err(); err != nil {
		return updated, err
	}

	for _, r := range batch {
		next := trainingfeed.RemapReactionEmoji(r.emoji, r.userMessageID, r.reportText)
		if next == r.emoji {
			continue
		}
		if _, err := d.db.Exec(`UPDATE miniapp_training_feed_reactions SET emoji = $1, updated_at = NOW() WHERE id = $2`, next, r.id); err != nil {
			return updated, fmt.Errorf("backfill training feed reactions update id=%d: %w", r.id, err)
		}
		updated++
	}
	return updated, nil
}
