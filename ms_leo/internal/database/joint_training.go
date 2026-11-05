package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// Совместная тренировка: в отчёте отмечают напарника через @ник. Участникам стаи
// приходит уведомление, остальным — инвайт в стаю от того, кто отметил.

// normalizeTelegramUsername — ник без «@», в нижнем регистре.
func normalizeTelegramUsername(username string) string {
	return strings.ToLower(strings.TrimLeft(strings.TrimSpace(username), "@"))
}

// FindBotUserOutsidePackByDisplayName — знакомый боту человек вне стаи по display_name
// (или по имени в training_state), если его отметили кириллицей, а не Telegram-ником.
func (d *Database) FindBotUserOutsidePackByDisplayName(packChatID int64, displayName string) (int64, error) {
	name := normalizeTelegramUsername(displayName)
	if d == nil || packChatID == 0 || name == "" {
		return 0, nil
	}
	var userID int64
	err := d.db.QueryRow(`
		SELECT c.user_id FROM (
			SELECT p.user_id, 1 AS pri
			FROM miniapp_user_profile p
			WHERE p.pack_chat_id = $1
			  AND LOWER(BTRIM(COALESCE(p.display_name, ''))) = $2
			UNION ALL
			SELECT ts.user_id, 2 AS pri
			FROM training_state ts
			WHERE ts.chat_id = $1
			  AND LOWER(BTRIM(COALESCE(ts.username, ''))) = $2
		) c
		WHERE c.user_id <> 0
		  AND NOT EXISTS (
			SELECT 1 FROM training_state m
			WHERE m.user_id = c.user_id AND m.chat_id = $1 AND m.is_deleted = FALSE
		  )
		ORDER BY c.pri
		LIMIT 1`, packChatID, name).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find bot user by display name: %w", err)
	}
	return userID, nil
}

// FindBotUserOutsidePackByUsername ищет человека, который уже писал боту (визит или
// бывший профиль), по Telegram-нику — но только если он сейчас не участник стаи.
// 0 — не найден: написать ему первым бот не может.
func (d *Database) FindBotUserOutsidePackByUsername(packChatID int64, username string) (int64, error) {
	u := normalizeTelegramUsername(username)
	if d == nil || u == "" {
		return 0, nil
	}
	var userID int64
	err := d.db.QueryRow(`
		SELECT c.user_id FROM (
			SELECT v.user_id, 1 AS pri, v.visited_at AS seen_at
			FROM bot_visits v
			WHERE LOWER(BTRIM(REGEXP_REPLACE(v.username, '^@+', ''))) = $2
			UNION ALL
			SELECT ts.user_id, 2 AS pri, NULL::timestamptz AS seen_at
			FROM training_state ts
			WHERE LOWER(BTRIM(REGEXP_REPLACE(COALESCE(ts.username, ''), '^@+', ''))) = $2
		) c
		WHERE c.user_id <> 0
		  AND NOT EXISTS (
			SELECT 1 FROM training_state m
			WHERE m.user_id = c.user_id AND m.chat_id = $1 AND m.is_deleted = FALSE
		  )
		ORDER BY c.pri, c.seen_at DESC NULLS LAST
		LIMIT 1`, packChatID, u).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("find bot user by username: %w", err)
	}
	return userID, nil
}

// ClaimJointTrainingInvite резервирует отправку инвайта «inviterID → @username».
// true — можно слать: раньше не слали или прошло больше cooldownDays дней.
func (d *Database) ClaimJointTrainingInvite(inviterID int64, username string, inviteeID int64, cooldownDays int) (bool, error) {
	u := normalizeTelegramUsername(username)
	if d == nil || inviterID == 0 || u == "" {
		return false, nil
	}
	if cooldownDays < 0 {
		cooldownDays = 0
	}
	res, err := d.db.Exec(`
		INSERT INTO joint_training_invites (inviter_user_id, invitee_username, invitee_user_id, sent_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (inviter_user_id, invitee_username) DO UPDATE
			SET sent_at = NOW(), invitee_user_id = EXCLUDED.invitee_user_id
			WHERE joint_training_invites.sent_at < NOW() - make_interval(days => $4)`,
		inviterID, u, inviteeID, cooldownDays)
	if err != nil {
		return false, fmt.Errorf("claim joint training invite: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// JointTrainingMentionCandidate — участник стаи для подсказки при @-отметке.
type JointTrainingMentionCandidate struct {
	UserID      int64
	Username    string
	DisplayName string
}

// SearchPackMembersForMention — активные участники стаи, чей ник или имя в стае
// начинается с query (без «@», регистр не важен). Пустой query — самые активные.
// Сам viewer в выдачу не попадает: себя на совместной тренировке не отмечают.
func (d *Database) SearchPackMembersForMention(packChatID, viewerUserID int64, query string, limit int) ([]JointTrainingMentionCandidate, error) {
	if d == nil || packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 20 {
		limit = 8
	}
	q := normalizeTelegramUsername(query)
	esc := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(q)
	pattern := esc + "%"
	rows, err := d.db.Query(`
		SELECT ts.user_id,
		       LOWER(BTRIM(REGEXP_REPLACE(COALESCE(ts.username, ''), '^@+', ''))),
		       BTRIM(COALESCE(p.display_name, ''))
		FROM training_state ts
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = ts.user_id AND p.pack_chat_id = ts.chat_id
		WHERE ts.chat_id = $1
		  AND ts.is_deleted = FALSE
		  AND ts.user_id <> $2
		  AND (
		        LOWER(BTRIM(REGEXP_REPLACE(COALESCE(ts.username, ''), '^@+', ''))) LIKE $3 ESCAPE '\'
		     OR LOWER(BTRIM(COALESCE(p.display_name, ''))) LIKE $3 ESCAPE '\'
		  )
		ORDER BY COALESCE(ts.streak_days, 0) DESC, ts.user_id ASC
		LIMIT $4`, packChatID, viewerUserID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search pack members for mention: %w", err)
	}
	defer rows.Close()
	out := []JointTrainingMentionCandidate{}
	for rows.Next() {
		var c JointTrainingMentionCandidate
		if err := rows.Scan(&c.UserID, &c.Username, &c.DisplayName); err != nil {
			return nil, fmt.Errorf("scan mention candidate: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
