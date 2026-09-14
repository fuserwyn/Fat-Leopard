package database

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/lib/pq"
)

// PackGroupReactionAgg — агрегат реакций на сообщение общего чата.
type PackGroupReactionAgg struct {
	Emoji  string
	Count  int
	Voters []Voter
}

// ListPackGroupReactionAggs — подсчёт реакций по списку id сообщений; viewerID для флага «моя».
func (d *Database) ListPackGroupReactionAggs(packChatID int64, messageIDs []int64, viewerUserID int64) (map[int64][]PackGroupReactionAgg, map[int64]string, error) {
	out := make(map[int64][]PackGroupReactionAgg)
	me := make(map[int64]string)
	if len(messageIDs) == 0 {
		return out, me, nil
	}
	q := `
		SELECT pack_message_id, emoji, COUNT(*)::int AS cnt
		FROM miniapp_pack_group_reactions
		WHERE pack_chat_id = $1 AND pack_message_id = ANY($2)
		GROUP BY pack_message_id, emoji
		ORDER BY pack_message_id, emoji
	`
	rows, err := d.db.Query(q, packChatID, pq.Array(messageIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("pack group reaction aggs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var emoji string
		var cnt int
		if err := rows.Scan(&mid, &emoji, &cnt); err != nil {
			return nil, nil, err
		}
		out[mid] = append(out[mid], PackGroupReactionAgg{Emoji: emoji, Count: cnt})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	qVoters := `
		SELECT r.pack_message_id, r.emoji, r.user_id,
		       COALESCE(NULLIF(TRIM(r.username), ''), CONCAT('Участник ', r.user_id::text)) AS voter,
		       COALESCE(p.telegram_photo_url, '') AS photo
		FROM miniapp_pack_group_reactions r
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = r.user_id AND p.pack_chat_id = r.pack_chat_id
		WHERE r.pack_chat_id = $1 AND r.pack_message_id = ANY($2)
		ORDER BY r.pack_message_id, r.emoji, r.created_at ASC, r.id ASC
	`
	rv, err := d.db.Query(qVoters, packChatID, pq.Array(messageIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("pack group reaction voters: %w", err)
	}
	defer rv.Close()
	for rv.Next() {
		var mid, uid int64
		var emoji, voter, photo string
		if err := rv.Scan(&mid, &emoji, &uid, &voter, &photo); err != nil {
			return nil, nil, err
		}
		aggs := out[mid]
		for i := range aggs {
			if aggs[i].Emoji == emoji {
				aggs[i].Voters = append(aggs[i].Voters, Voter{UserID: uid, Name: voter, PhotoURL: photo})
				break
			}
		}
		out[mid] = aggs
	}
	if err := rv.Err(); err != nil {
		return nil, nil, err
	}
	qMe := `
		SELECT pack_message_id, emoji FROM miniapp_pack_group_reactions
		WHERE pack_chat_id = $1 AND pack_message_id = ANY($2) AND user_id = $3
	`
	r2, err := d.db.Query(qMe, packChatID, pq.Array(messageIDs), viewerUserID)
	if err != nil {
		return nil, nil, fmt.Errorf("pack group reaction me: %w", err)
	}
	defer r2.Close()
	for r2.Next() {
		var mid int64
		var emoji string
		if err := r2.Scan(&mid, &emoji); err != nil {
			return nil, nil, err
		}
		me[mid] = emoji
	}
	return out, me, r2.Err()
}

// SetPackGroupReaction — поставить реакцию; та же эмодзи повторно снимает.
func (d *Database) SetPackGroupReaction(packChatID, messageID, userID int64, username, emoji string) error {
	emoji = strings.TrimSpace(emoji)
	var cur sql.NullString
	err := d.db.QueryRow(
		`SELECT emoji FROM miniapp_pack_group_reactions WHERE pack_message_id = $1 AND user_id = $2`,
		messageID, userID,
	).Scan(&cur)
	if err != nil && err != sql.ErrNoRows {
		return err
	}
	if err == nil && cur.Valid && cur.String == emoji {
		_, err := d.db.Exec(
			`DELETE FROM miniapp_pack_group_reactions WHERE pack_message_id = $1 AND user_id = $2`,
			messageID, userID,
		)
		return err
	}
	const upsert = `
		INSERT INTO miniapp_pack_group_reactions (pack_chat_id, pack_message_id, user_id, username, emoji)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (pack_message_id, user_id) DO UPDATE SET
			emoji = EXCLUDED.emoji,
			username = EXCLUDED.username,
			updated_at = NOW()
	`
	_, err = d.db.Exec(upsert, packChatID, messageID, userID, strings.TrimSpace(username), emoji)
	return err
}
