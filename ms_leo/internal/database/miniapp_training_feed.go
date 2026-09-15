package database

import (
	"database/sql"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/lib/pq"
)

// GetUserMessageAuthorUserID — автор отчёта training_done (user_messages.user_id).
func (d *Database) GetUserMessageAuthorUserID(chatID, userMessageID int64) (userID int64, ok bool, err error) {
	err = d.db.QueryRow(
		`SELECT user_id FROM user_messages WHERE id = $1 AND chat_id = $2`,
		userMessageID, chatID,
	).Scan(&userID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userID, true, nil
}

// GetUserMessageTypeByIDForChat — проверка, что строка user_messages принадлежит чату.
func (d *Database) GetUserMessageTypeByIDForChat(id, chatID int64) (messageType string, ok bool, err error) {
	err = d.db.QueryRow(
		`SELECT message_type FROM user_messages WHERE id = $1 AND chat_id = $2`,
		id, chatID,
	).Scan(&messageType)
	if err == sql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return messageType, true, nil
}

// GetUserMessageUsernameByIDForChat — отображаемое имя автора карточки ленты.
func (d *Database) GetUserMessageUsernameByIDForChat(id, chatID int64) (string, error) {
	var u string
	err := d.db.QueryRow(
		`SELECT username FROM user_messages WHERE id = $1 AND chat_id = $2`,
		id, chatID,
	).Scan(&u)
	if err != nil {
		return "", err
	}
	return u, nil
}

// GetUserMessageTextByIDForChat — текст отчёта (user_messages) для контекста ответа Лео в треде.
func (d *Database) GetUserMessageTextByIDForChat(id, chatID int64) (string, error) {
	var t string
	err := d.db.QueryRow(
		`SELECT message_text FROM user_messages WHERE id = $1 AND chat_id = $2`,
		id, chatID,
	).Scan(&t)
	if err != nil {
		return "", err
	}
	return t, nil
}

// Voter — кто поставил реакцию/лайк: id (для прокси-аватара) + имя + аватар (telegram_photo_url из профиля).
type Voter struct {
	UserID   int64
	Name     string
	PhotoURL string
}

// TrainingFeedReactionAgg — агрегат реакций по одному user_message_id.
type TrainingFeedReactionAgg struct {
	Emoji  string
	Count  int
	Voters []Voter
}

// ListTrainingFeedReactionAggs — подсчёт реакций по списку parent id; viewerID для флага «моя».
func (d *Database) ListTrainingFeedReactionAggs(packChatID int64, userMessageIDs []int64, viewerUserID int64) (map[int64][]TrainingFeedReactionAgg, map[int64]string, error) {
	out := make(map[int64][]TrainingFeedReactionAgg)
	me := make(map[int64]string)
	if len(userMessageIDs) == 0 {
		return out, me, nil
	}
	q := `
		SELECT user_message_id, emoji, COUNT(*)::int AS cnt
		FROM miniapp_training_feed_reactions
		WHERE pack_chat_id = $1 AND user_message_id = ANY($2)
		GROUP BY user_message_id, emoji
		ORDER BY user_message_id, emoji
	`
	rows, err := d.db.Query(q, packChatID, pq.Array(userMessageIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("training feed reaction aggs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var mid int64
		var emoji string
		var cnt int
		if err := rows.Scan(&mid, &emoji, &cnt); err != nil {
			return nil, nil, err
		}
		out[mid] = append(out[mid], TrainingFeedReactionAgg{Emoji: emoji, Count: cnt})
	}
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	qVoters := `
		SELECT r.user_message_id, r.emoji, r.user_id,
		       COALESCE(NULLIF(TRIM(r.username), ''), CONCAT('Участник ', r.user_id::text)) AS voter,
		       COALESCE(p.telegram_photo_url, '') AS photo
		FROM miniapp_training_feed_reactions r
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = r.user_id AND p.pack_chat_id = r.pack_chat_id
		WHERE r.pack_chat_id = $1 AND r.user_message_id = ANY($2)
		ORDER BY r.user_message_id, r.emoji, r.created_at ASC, r.id ASC
	`
	rv, err := d.db.Query(qVoters, packChatID, pq.Array(userMessageIDs))
	if err != nil {
		return nil, nil, fmt.Errorf("training feed reaction voters: %w", err)
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
		SELECT user_message_id, emoji FROM miniapp_training_feed_reactions
		WHERE pack_chat_id = $1 AND user_message_id = ANY($2) AND user_id = $3
	`
	r2, err := d.db.Query(qMe, packChatID, pq.Array(userMessageIDs), viewerUserID)
	if err != nil {
		return nil, nil, fmt.Errorf("training feed reaction me: %w", err)
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

// SetTrainingFeedReaction — поставить реакцию; та же эмодзи повторно снимает.
// added=true, если по итогу реакция стоит (новая или сменённая эмодзи); false — если сняли.
func (d *Database) SetTrainingFeedReaction(packChatID, userMessageID, userID int64, username, emoji string) (added bool, err error) {
	emoji = strings.TrimSpace(emoji)
	var cur sql.NullString
	err = d.db.QueryRow(
		`SELECT emoji FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = $2`,
		userMessageID, userID,
	).Scan(&cur)
	if err != nil && err != sql.ErrNoRows {
		return false, err
	}
	if err == nil && cur.Valid && cur.String == emoji {
		_, err := d.db.Exec(
			`DELETE FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = $2`,
			userMessageID, userID,
		)
		return false, err
	}
	const upsert = `
		INSERT INTO miniapp_training_feed_reactions (pack_chat_id, user_message_id, user_id, username, emoji)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (user_message_id, user_id) DO UPDATE SET
			emoji = EXCLUDED.emoji,
			username = EXCLUDED.username,
			updated_at = NOW()
	`
	if _, err = d.db.Exec(upsert, packChatID, userMessageID, userID, strings.TrimSpace(username), emoji); err != nil {
		return false, err
	}
	return true, nil
}

// TrainingFeedThreadRow — одна реплика в треде.
type TrainingFeedThreadRow struct {
	ID            int64
	UserMessageID int64
	FromUserID    int64
	Username      string
	MessageText   string
	PhotoURL      string
	CreatedAt     time.Time
	EditedAt      sql.NullTime
	ReplyToID     sql.NullInt64
	// Голос комментария: 'self' (обычный), 'leo' (админ от имени Лео), 'admin' (админ от имени админов).
	PostedAs string
}

// GetTrainingFeedThreadRow — строка по id (для проверки reply_to).
func (d *Database) GetTrainingFeedThreadRow(id int64) (TrainingFeedThreadRow, bool, error) {
	if id == 0 {
		return TrainingFeedThreadRow{}, false, nil
	}
	const q = `
		SELECT
			t.id,
			t.user_message_id,
			t.from_user_id,
			COALESCE(NULLIF(BTRIM(p.display_name), ''), NULLIF(BTRIM(t.username), ''), ''),
			t.message_text,
			COALESCE(t.photo_url, ''),
			t.created_at,
			t.reply_to_id,
			COALESCE(t.posted_as, 'self')
		FROM miniapp_training_feed_thread t
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = t.from_user_id AND p.pack_chat_id = t.pack_chat_id
		WHERE t.id = $1`
	var r TrainingFeedThreadRow
	var replyTo sql.NullInt64
	err := d.db.QueryRow(q, id).Scan(&r.ID, &r.UserMessageID, &r.FromUserID, &r.Username, &r.MessageText, &r.PhotoURL, &r.CreatedAt, &replyTo, &r.PostedAs)
	if err == sql.ErrNoRows {
		return TrainingFeedThreadRow{}, false, nil
	}
	if err != nil {
		return TrainingFeedThreadRow{}, false, err
	}
	r.ReplyToID = replyTo
	return r, true, nil
}

// InsertTrainingFeedThreadReply — комментарий под отчётом; replyToThreadID=0 без ответа на сообщение треда.
// photoURL — опциональное фото к комментарию (пусто, если без фото).
func (d *Database) InsertTrainingFeedThreadReply(packChatID, userMessageID, fromUserID int64, username, text string, replyToThreadID int64, photoURL, postedAs string) (int64, error) {
	if strings.TrimSpace(postedAs) == "" {
		postedAs = "self"
	}
	const q = `
		INSERT INTO miniapp_training_feed_thread (pack_chat_id, user_message_id, from_user_id, username, message_text, reply_to_id, photo_url, posted_as)
		VALUES ($1, $2, $3, $4, $5, NULLIF($6::bigint, 0), $7, $8)
		RETURNING id
	`
	var id int64
	err := d.db.QueryRow(q, packChatID, userMessageID, fromUserID, strings.TrimSpace(username), text, replyToThreadID, strings.TrimSpace(photoURL), postedAs).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("insert training thread: %w", err)
	}
	return id, nil
}

// GetTrainingFeedThreadRowInPack — строка по id только в нужной стае (для reply_to).
func (d *Database) GetTrainingFeedThreadRowInPack(threadID, packChatID int64) (TrainingFeedThreadRow, bool, error) {
	if threadID == 0 {
		return TrainingFeedThreadRow{}, false, nil
	}
	const q = `
		SELECT
			t.id,
			t.user_message_id,
			t.from_user_id,
			COALESCE(NULLIF(BTRIM(p.display_name), ''), NULLIF(BTRIM(t.username), ''), ''),
			t.message_text,
			COALESCE(t.photo_url, ''),
			t.created_at,
			t.reply_to_id,
			COALESCE(t.posted_as, 'self')
		FROM miniapp_training_feed_thread t
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = t.from_user_id AND p.pack_chat_id = t.pack_chat_id
		WHERE t.id = $1 AND t.pack_chat_id = $2`
	var r TrainingFeedThreadRow
	var replyTo sql.NullInt64
	err := d.db.QueryRow(q, threadID, packChatID).Scan(&r.ID, &r.UserMessageID, &r.FromUserID, &r.Username, &r.MessageText, &r.PhotoURL, &r.CreatedAt, &replyTo, &r.PostedAs)
	if err == sql.ErrNoRows {
		return TrainingFeedThreadRow{}, false, nil
	}
	if err != nil {
		return TrainingFeedThreadRow{}, false, err
	}
	r.ReplyToID = replyTo
	return r, true, nil
}

// ListTrainingFeedThreadRowsByIDs — родительские сообщения для цитат (тот же pack).
func (d *Database) ListTrainingFeedThreadRowsByIDs(packChatID int64, ids []int64) (map[int64]TrainingFeedThreadRow, error) {
	out := make(map[int64]TrainingFeedThreadRow)
	if len(ids) == 0 || packChatID == 0 {
		return out, nil
	}
	q := `
		SELECT
			t.id,
			t.user_message_id,
			t.from_user_id,
			COALESCE(NULLIF(BTRIM(p.display_name), ''), NULLIF(BTRIM(t.username), ''), ''),
			t.message_text,
			COALESCE(t.photo_url, ''),
			t.created_at,
			t.reply_to_id,
			COALESCE(t.posted_as, 'self')
		FROM miniapp_training_feed_thread t
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = t.from_user_id AND p.pack_chat_id = t.pack_chat_id
		WHERE t.pack_chat_id = $1 AND t.id = ANY($2)`
	rows, err := d.db.Query(q, packChatID, pq.Array(ids))
	if err != nil {
		return nil, fmt.Errorf("list training thread by ids: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r TrainingFeedThreadRow
		var replyTo sql.NullInt64
		if err := rows.Scan(&r.ID, &r.UserMessageID, &r.FromUserID, &r.Username, &r.MessageText, &r.PhotoURL, &r.CreatedAt, &replyTo, &r.PostedAs); err != nil {
			return nil, err
		}
		r.ReplyToID = replyTo
		out[r.ID] = r
	}
	return out, rows.Err()
}

// DeleteTrainingFeedThreadReply — удалить реплику; только автор (from_user_id), не ответы Лео (from_user_id = 0).
func (d *Database) DeleteTrainingFeedThreadReply(packChatID, threadReplyID, actorUserID int64) (userMessageID int64, deleted bool, err error) {
	if threadReplyID == 0 || actorUserID == 0 {
		return 0, false, nil
	}
	err = d.db.QueryRow(
		`DELETE FROM miniapp_training_feed_thread
		 WHERE id = $1 AND pack_chat_id = $2 AND from_user_id = $3 AND from_user_id != 0
		 RETURNING user_message_id`,
		threadReplyID, packChatID, actorUserID,
	).Scan(&userMessageID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userMessageID, true, nil
}

// UpdateTrainingFeedThreadReplyByAuthor — правка своего комментария (posted_as=self, не Лео).
func (d *Database) UpdateTrainingFeedThreadReplyByAuthor(packChatID, threadReplyID, actorUserID int64, newText string) (userMessageID int64, updated bool, err error) {
	if threadReplyID == 0 || actorUserID == 0 {
		return 0, false, nil
	}
	err = d.db.QueryRow(
		`UPDATE miniapp_training_feed_thread
		 SET message_text = $4,
		     edited_at = (NOW() AT TIME ZONE 'Europe/Moscow')
		 WHERE id = $1 AND pack_chat_id = $2 AND from_user_id = $3
		   AND from_user_id != 0 AND COALESCE(posted_as, 'self') = 'self'
		 RETURNING user_message_id`,
		threadReplyID, packChatID, actorUserID, newText,
	).Scan(&userMessageID)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, err
	}
	return userMessageID, true, nil
}

// ListTrainingFeedThreadByMessages — все реплики для данных parent id, по времени по возрастанию.
func (d *Database) ListTrainingFeedThreadByMessages(userMessageIDs []int64) (map[int64][]TrainingFeedThreadRow, error) {
	res := make(map[int64][]TrainingFeedThreadRow)
	if len(userMessageIDs) == 0 {
		return res, nil
	}
	const q = `
		SELECT
			t.id,
			t.user_message_id,
			t.from_user_id,
			COALESCE(NULLIF(BTRIM(p.display_name), ''), NULLIF(BTRIM(t.username), ''), ''),
			t.message_text,
			COALESCE(t.photo_url, ''),
			t.created_at,
			t.edited_at,
			t.reply_to_id,
			COALESCE(t.posted_as, 'self')
		FROM miniapp_training_feed_thread t
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = t.from_user_id AND p.pack_chat_id = t.pack_chat_id
		WHERE t.user_message_id = ANY($1)
		  AND COALESCE(t.is_hidden, FALSE) = FALSE
		ORDER BY t.user_message_id, t.created_at ASC
	`
	rows, err := d.db.Query(q, pq.Array(userMessageIDs))
	if err != nil {
		return nil, fmt.Errorf("list training thread: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var r TrainingFeedThreadRow
		var replyTo sql.NullInt64
		if err := rows.Scan(&r.ID, &r.UserMessageID, &r.FromUserID, &r.Username, &r.MessageText, &r.PhotoURL, &r.CreatedAt, &r.EditedAt, &replyTo, &r.PostedAs); err != nil {
			return nil, err
		}
		r.ReplyToID = replyTo
		res[r.UserMessageID] = append(res[r.UserMessageID], r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return res, nil
}

// InsertTrainingThreadUnread — одна запись на комментарий для бейджа «Стая» у автора отчёта.
func (d *Database) InsertTrainingThreadUnread(recipientUserID, packChatID, threadReplyID int64) error {
	if recipientUserID == 0 || threadReplyID == 0 {
		return nil
	}
	_, err := d.db.Exec(
		`INSERT INTO miniapp_training_thread_unread (recipient_user_id, pack_chat_id, thread_reply_id)
		 VALUES ($1, $2, $3) ON CONFLICT (thread_reply_id) DO NOTHING`,
		recipientUserID, packChatID, threadReplyID,
	)
	if err != nil {
		return fmt.Errorf("insert training thread unread: %w", err)
	}
	return nil
}

// DeleteTrainingThreadUnreadByReplyID — при удалении своего комментария убрать из непрочитанных.
func (d *Database) DeleteTrainingThreadUnreadByReplyID(threadReplyID int64) error {
	if threadReplyID == 0 {
		return nil
	}
	_, err := d.db.Exec(`DELETE FROM miniapp_training_thread_unread WHERE thread_reply_id = $1`, threadReplyID)
	if err != nil {
		return fmt.Errorf("delete training thread unread by reply: %w", err)
	}
	return nil
}

// ToggleTrainingFeedThreadLike — лайк на комментарий треда (toggle).
// liked=true, если по итогу лайк поставлен; false — если сняли (или no-op).
func (d *Database) ToggleTrainingFeedThreadLike(packChatID, threadReplyID, userID int64) (liked bool, err error) {
	if packChatID == 0 || threadReplyID == 0 || userID == 0 {
		return false, nil
	}
	var exists bool
	err = d.db.QueryRow(
		`SELECT EXISTS(
			SELECT 1 FROM miniapp_training_feed_thread_likes
			WHERE pack_chat_id = $1 AND thread_reply_id = $2 AND user_id = $3
		)`,
		packChatID, threadReplyID, userID,
	).Scan(&exists)
	if err != nil {
		return false, err
	}
	if exists {
		_, err := d.db.Exec(
			`DELETE FROM miniapp_training_feed_thread_likes
			 WHERE pack_chat_id = $1 AND thread_reply_id = $2 AND user_id = $3`,
			packChatID, threadReplyID, userID,
		)
		return false, err
	}
	if _, err = d.db.Exec(
		`INSERT INTO miniapp_training_feed_thread_likes (pack_chat_id, thread_reply_id, user_id)
		 VALUES ($1, $2, $3)
		 ON CONFLICT (thread_reply_id, user_id) DO NOTHING`,
		packChatID, threadReplyID, userID,
	); err != nil {
		return false, err
	}
	return true, nil
}

// ThreadLikeAgg — лайки по id комментария треда.
type ThreadLikeAgg struct {
	Count  int
	Me     bool
	Voters []Voter // лайкнувшие: имя (display_name → ник, иначе «Участник <id>») + аватар
}

// ListTrainingFeedThreadLikeAggs — агрегаты лайков для списка thread reply id.
func (d *Database) ListTrainingFeedThreadLikeAggs(packChatID int64, threadReplyIDs []int64, viewerUserID int64) (map[int64]ThreadLikeAgg, error) {
	out := make(map[int64]ThreadLikeAgg)
	if packChatID == 0 || len(threadReplyIDs) == 0 {
		return out, nil
	}
	rows, err := d.db.Query(
		`SELECT thread_reply_id, COUNT(*)::int
		 FROM miniapp_training_feed_thread_likes
		 WHERE pack_chat_id = $1 AND thread_reply_id = ANY($2)
		 GROUP BY thread_reply_id`,
		packChatID, pq.Array(threadReplyIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list training thread like aggs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var cnt int
		if err := rows.Scan(&id, &cnt); err != nil {
			return nil, err
		}
		out[id] = ThreadLikeAgg{Count: cnt}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Лайкнувшие: имя профиля (display_name) → ник (username из комментариев/реакций
	// этого человека в стае), иначе «Участник <id>»; + аватар.
	rvoters, err := d.db.Query(
		`SELECT l.thread_reply_id, l.user_id,
		        COALESCE(
		          NULLIF(BTRIM(p.display_name), ''),
		          NULLIF(BTRIM((
		            SELECT u.username FROM (
		              SELECT username, created_at FROM miniapp_training_feed_thread
		               WHERE pack_chat_id = l.pack_chat_id AND from_user_id = l.user_id AND BTRIM(username) <> ''
		              UNION ALL
		              SELECT username, created_at FROM miniapp_training_feed_reactions
		               WHERE pack_chat_id = l.pack_chat_id AND user_id = l.user_id AND BTRIM(username) <> ''
		            ) u ORDER BY u.created_at DESC LIMIT 1
		          )), ''),
		          CONCAT('Участник ', l.user_id::text)
		        ) AS voter,
		        COALESCE(p.telegram_photo_url, '') AS photo
		 FROM miniapp_training_feed_thread_likes l
		 LEFT JOIN miniapp_user_profile p
		   ON p.user_id = l.user_id AND p.pack_chat_id = l.pack_chat_id
		 WHERE l.pack_chat_id = $1 AND l.thread_reply_id = ANY($2)
		 ORDER BY l.thread_reply_id, voter`,
		packChatID, pq.Array(threadReplyIDs),
	)
	if err != nil {
		return nil, fmt.Errorf("list training thread like voters: %w", err)
	}
	defer rvoters.Close()
	for rvoters.Next() {
		var id, uid int64
		var voter, photo string
		if err := rvoters.Scan(&id, &uid, &voter, &photo); err != nil {
			return nil, err
		}
		agg := out[id]
		agg.Voters = append(agg.Voters, Voter{UserID: uid, Name: voter, PhotoURL: photo})
		out[id] = agg
	}
	if err := rvoters.Err(); err != nil {
		return nil, err
	}

	if viewerUserID == 0 {
		return out, nil
	}
	rows2, err := d.db.Query(
		`SELECT thread_reply_id
		 FROM miniapp_training_feed_thread_likes
		 WHERE pack_chat_id = $1 AND thread_reply_id = ANY($2) AND user_id = $3`,
		packChatID, pq.Array(threadReplyIDs), viewerUserID,
	)
	if err != nil {
		return nil, fmt.Errorf("list training thread my likes: %w", err)
	}
	defer rows2.Close()
	for rows2.Next() {
		var id int64
		if err := rows2.Scan(&id); err != nil {
			return nil, err
		}
		agg := out[id]
		agg.Me = true
		out[id] = agg
	}
	return out, rows2.Err()
}

// CountTrainingThreadUnread — число непросмотренных комментариев к своим отчётам.
func (d *Database) CountTrainingThreadUnread(recipientUserID, packChatID int64) (int64, error) {
	if recipientUserID == 0 {
		return 0, nil
	}
	// Ответ могли удалить или скрыть модерацией — такие строки в таблице
	// остаются, а на экране их нет. Счётчик по ним держал бы бейдж вечно,
	// поэтому сначала чистим мусор, потом считаем только видимое.
	if _, err := d.db.Exec(
		`DELETE FROM miniapp_training_thread_unread u
		 WHERE u.recipient_user_id = $1 AND u.pack_chat_id = $2
		   AND NOT EXISTS (
		     SELECT 1 FROM miniapp_training_feed_thread t
		     WHERE t.id = u.thread_reply_id AND COALESCE(t.is_hidden, FALSE) = FALSE
		   )`,
		recipientUserID, packChatID,
	); err != nil {
		return 0, fmt.Errorf("purge stale training thread unread: %w", err)
	}
	var n int64
	err := d.db.QueryRow(
		`SELECT COUNT(*) FROM miniapp_training_thread_unread WHERE recipient_user_id = $1 AND pack_chat_id = $2`,
		recipientUserID, packChatID,
	).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count training thread unread: %w", err)
	}
	return n, nil
}

// ClearTrainingThreadUnread — пользователь открыл ленту (вкладка Стая).
func (d *Database) ClearTrainingThreadUnread(recipientUserID, packChatID int64) error {
	if recipientUserID == 0 {
		return nil
	}
	_, err := d.db.Exec(
		`DELETE FROM miniapp_training_thread_unread WHERE recipient_user_id = $1 AND pack_chat_id = $2`,
		recipientUserID, packChatID,
	)
	if err != nil {
		return fmt.Errorf("clear training thread unread: %w", err)
	}
	return nil
}

// ListTrainingThreadUnreadUserMessageIDs — отчёты в ленте с непрочитанными комментариями/ответами.
func (d *Database) ListTrainingThreadUnreadUserMessageIDs(recipientUserID, packChatID int64) ([]int64, error) {
	if recipientUserID == 0 {
		return nil, nil
	}
	rows, err := d.db.Query(
		`SELECT DISTINCT t.user_message_id
		 FROM miniapp_training_thread_unread u
		 JOIN miniapp_training_feed_thread t ON t.id = u.thread_reply_id
		 WHERE u.recipient_user_id = $1 AND u.pack_chat_id = $2
		 ORDER BY t.user_message_id DESC`,
		recipientUserID, packChatID,
	)
	if err != nil {
		return nil, fmt.Errorf("list training thread unread cards: %w", err)
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		if id > 0 {
			ids = append(ids, id)
		}
	}
	return ids, rows.Err()
}

// ClearTrainingThreadUnreadForUserMessage — просмотрен тред под конкретным отчётом.
func (d *Database) ClearTrainingThreadUnreadForUserMessage(recipientUserID, packChatID, userMessageID int64) error {
	if recipientUserID == 0 || userMessageID == 0 {
		return nil
	}
	_, err := d.db.Exec(
		`DELETE FROM miniapp_training_thread_unread u
		 USING miniapp_training_feed_thread t
		 WHERE u.thread_reply_id = t.id
		   AND u.recipient_user_id = $1 AND u.pack_chat_id = $2
		   AND t.user_message_id = $3`,
		recipientUserID, packChatID, userMessageID,
	)
	if err != nil {
		return fmt.Errorf("clear training thread unread for card: %w", err)
	}
	return nil
}

// SortReactionAggsForDisplay — стабильный порядок эмодзи в UI.
func SortReactionAggsForDisplay(aggs []TrainingFeedReactionAgg, preferredOrder []string) []TrainingFeedReactionAgg {
	idx := make(map[string]int, len(preferredOrder))
	for i, e := range preferredOrder {
		idx[e] = i
	}
	cp := append([]TrainingFeedReactionAgg(nil), aggs...)
	sort.SliceStable(cp, func(i, j int) bool {
		ai, okA := idx[cp[i].Emoji]
		bi, okB := idx[cp[j].Emoji]
		if okA && okB {
			return ai < bi
		}
		if okA {
			return true
		}
		if okB {
			return false
		}
		return cp[i].Emoji < cp[j].Emoji
	})
	return cp
}
