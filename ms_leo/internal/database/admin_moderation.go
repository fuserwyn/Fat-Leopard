package database

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/utils"
)

// PackUserAdminFilter — фильтр списка участников в админке.
type PackUserAdminFilter string

const (
	PackUserAdminFilterAll    PackUserAdminFilter = "all"
	PackUserAdminFilterActive PackUserAdminFilter = "active"
	PackUserAdminFilterKicked PackUserAdminFilter = "kicked"
)

func NormalizePackUserAdminFilter(raw string) PackUserAdminFilter {
	switch strings.TrimSpace(strings.ToLower(raw)) {
	case "active":
		return PackUserAdminFilterActive
	case "kicked":
		return PackUserAdminFilterKicked
	default:
		return PackUserAdminFilterAll
	}
}

func packUserAdminFilterClause(filter PackUserAdminFilter) string {
	switch filter {
	case PackUserAdminFilterActive:
		return " AND ts.is_deleted = FALSE"
	case PackUserAdminFilterKicked:
		return " AND ts.is_deleted = TRUE"
	default:
		return ""
	}
}

// AdminPackUserCounts — счётчики участников стаи для админки.
type AdminPackUserCounts struct {
	Total  int
	Active int
	Kicked int
}

// AdminPackUserSearchHit — результат поиска пользователя в админке.
type AdminPackUserSearchHit struct {
	UserID      int64
	Username    string
	DisplayName string
	IsDeleted   bool
}

// SearchPackUsersForAdmin ищет пользователей стаи по ID, @нику или display_name из мини-аппа.
func (d *Database) SearchPackUsersForAdmin(packChatID int64, rawQuery string, limit int, filter PackUserAdminFilter) ([]AdminPackUserSearchHit, error) {
	if d == nil || packChatID == 0 {
		return nil, nil
	}
	q := strings.TrimSpace(rawQuery)
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 20 {
		limit = 20
	}

	pattern := "%" + strings.TrimPrefix(q, "@") + "%"
	var exactID int64
	if id, err := strconv.ParseInt(q, 10, 64); err == nil && id > 0 {
		exactID = id
	}

	statusClause := packUserAdminFilterClause(filter)
	query := fmt.Sprintf(`
		SELECT ts.user_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), ''),
		       ts.is_deleted
		FROM training_state ts
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = ts.user_id AND p.pack_chat_id = ts.chat_id
		WHERE ts.chat_id = $1%s
		  AND (
		        ($2 > 0 AND ts.user_id = $2)
		     OR ts.username ILIKE $3
		     OR COALESCE(p.display_name, '') ILIKE $3
		  )
		ORDER BY ts.is_deleted ASC, ts.cups_earned DESC, ts.user_id ASC
		LIMIT $4
	`, statusClause)
	rows, err := d.db.Query(query, packChatID, exactID, pattern, limit)
	if err != nil {
		return nil, fmt.Errorf("search pack users: %w", err)
	}
	defer rows.Close()

	var out []AdminPackUserSearchHit
	for rows.Next() {
		var hit AdminPackUserSearchHit
		if err := rows.Scan(&hit.UserID, &hit.Username, &hit.DisplayName, &hit.IsDeleted); err != nil {
			return nil, err
		}
		out = append(out, hit)
	}
	return out, rows.Err()
}

// AdminPackUserListRow — строка списка пользователей в админке.
type AdminPackUserListRow struct {
	UserID           int64
	Username         string
	DisplayName      string
	Cups             int
	StreakDays       int
	IsDeleted        bool
	HasActivePaywall bool
}

// AdminPaywallPaymentRow — строка оплаты для админки.
type AdminPaywallPaymentRow struct {
	ID            int64
	UserID        int64
	Username      string
	DisplayName   string
	Status        string
	CreatedAt     time.Time
	CompletedAt   sql.NullTime
	AmountMinor   sql.NullInt64
	Currency      sql.NullString
	AccessActive  bool
}

func (d *Database) CountPackUsersForAdmin(packChatID int64) (int, error) {
	counts, err := d.CountPackUsersBreakdownForAdmin(packChatID)
	if err != nil {
		return 0, err
	}
	return counts.Total, nil
}

func (d *Database) CountPackUsersBreakdownForAdmin(packChatID int64) (AdminPackUserCounts, error) {
	var out AdminPackUserCounts
	if packChatID == 0 {
		return out, nil
	}
	err := d.db.QueryRow(`
		SELECT COUNT(*),
		       COUNT(*) FILTER (WHERE is_deleted = FALSE),
		       COUNT(*) FILTER (WHERE is_deleted = TRUE)
		FROM training_state
		WHERE chat_id = $1
	`, packChatID).Scan(&out.Total, &out.Active, &out.Kicked)
	return out, err
}

func (d *Database) CountPackUsersForAdminFiltered(packChatID int64, filter PackUserAdminFilter) (int, error) {
	if packChatID == 0 {
		return 0, nil
	}
	filter = NormalizePackUserAdminFilter(string(filter))
	switch filter {
	case PackUserAdminFilterActive:
		var n int
		err := d.db.QueryRow(`SELECT COUNT(*) FROM training_state WHERE chat_id = $1 AND is_deleted = FALSE`, packChatID).Scan(&n)
		return n, err
	case PackUserAdminFilterKicked:
		var n int
		err := d.db.QueryRow(`SELECT COUNT(*) FROM training_state WHERE chat_id = $1 AND is_deleted = TRUE`, packChatID).Scan(&n)
		return n, err
	default:
		return d.CountPackUsersForAdmin(packChatID)
	}
}

func (d *Database) ListPackUsersForAdmin(packChatID int64, offset, limit int, filter PackUserAdminFilter) ([]AdminPackUserListRow, error) {
	if packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 8
	}
	if offset < 0 {
		offset = 0
	}
	statusClause := packUserAdminFilterClause(filter)
	query := fmt.Sprintf(`
		SELECT ts.user_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), ''),
		       COALESCE(ts.cups_earned, 0),
		       COALESCE(ts.streak_days, 0),
		       ts.is_deleted,
		       EXISTS (
		           SELECT 1 FROM paywall_access_requests par
		           WHERE par.user_id = ts.user_id
		             AND par.monetized_chat_id = ts.chat_id
		             AND par.status = 'completed'
		             AND par.access_expires_at IS NOT NULL
		             AND par.access_expires_at > NOW()
		       )
		FROM training_state ts
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = ts.user_id AND p.pack_chat_id = ts.chat_id
		WHERE ts.chat_id = $1%s
		ORDER BY ts.is_deleted ASC, ts.cups_earned DESC, ts.user_id ASC
		OFFSET $2 LIMIT $3
	`, statusClause)
	rows, err := d.db.Query(query, packChatID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminPackUserListRow
	for rows.Next() {
		var r AdminPackUserListRow
		if err := rows.Scan(&r.UserID, &r.Username, &r.DisplayName, &r.Cups, &r.StreakDays, &r.IsDeleted, &r.HasActivePaywall); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Database) CountPaywallPaymentsForAdmin(packChatID int64) (int, error) {
	if packChatID == 0 {
		return 0, nil
	}
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM paywall_access_requests WHERE monetized_chat_id = $1`, packChatID).Scan(&n)
	return n, err
}

func (d *Database) ListPaywallPaymentsForAdmin(packChatID int64, offset, limit int) ([]AdminPaywallPaymentRow, error) {
	if packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	if offset < 0 {
		offset = 0
	}
	const query = `
		SELECT par.id, par.user_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), ''),
		       par.status, par.created_at, par.completed_at,
		       par.total_amount_minor, par.currency,
		       (par.status = 'completed' AND par.access_expires_at IS NOT NULL AND par.access_expires_at > NOW())
		FROM paywall_access_requests par
		LEFT JOIN training_state ts
			ON ts.user_id = par.user_id AND ts.chat_id = par.monetized_chat_id
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = par.user_id AND p.pack_chat_id = par.monetized_chat_id
		WHERE par.monetized_chat_id = $1
		ORDER BY par.id DESC
		OFFSET $2 LIMIT $3
	`
	rows, err := d.db.Query(query, packChatID, offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminPaywallPaymentRow
	for rows.Next() {
		var r AdminPaywallPaymentRow
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Username, &r.DisplayName, &r.Status, &r.CreatedAt, &r.CompletedAt,
			&r.AmountMinor, &r.Currency, &r.AccessActive,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (d *Database) ListPaywallPaymentsForUserAdmin(userID, packChatID int64, limit int) ([]AdminPaywallPaymentRow, error) {
	if userID == 0 || packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 5
	}
	const query = `
		SELECT par.id, par.user_id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), ''),
		       par.status, par.created_at, par.completed_at,
		       par.total_amount_minor, par.currency,
		       (par.status = 'completed' AND par.access_expires_at IS NOT NULL AND par.access_expires_at > NOW())
		FROM paywall_access_requests par
		LEFT JOIN training_state ts
			ON ts.user_id = par.user_id AND ts.chat_id = par.monetized_chat_id
		LEFT JOIN miniapp_user_profile p
			ON p.user_id = par.user_id AND p.pack_chat_id = par.monetized_chat_id
		WHERE par.user_id = $1 AND par.monetized_chat_id = $2
		ORDER BY par.id DESC
		LIMIT $3
	`
	rows, err := d.db.Query(query, userID, packChatID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AdminPaywallPaymentRow
	for rows.Next() {
		var r AdminPaywallPaymentRow
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.Username, &r.DisplayName, &r.Status, &r.CreatedAt, &r.CompletedAt,
			&r.AmountMinor, &r.Currency, &r.AccessActive,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SyncPrivateRowCupsFromPack выравнивает cups_earned на private-row (chat_id = user_id) по pack-row.
func (d *Database) SyncPrivateRowCupsFromPack(userID, packChatID int64) error {
	if userID == 0 || packChatID == 0 || userID == packChatID {
		return nil
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state AS priv
		SET cups_earned = COALESCE(pack.cups_earned, 0),
		    updated_at = $3
		FROM training_state AS pack
		WHERE priv.user_id = $1 AND priv.chat_id = $1
		  AND pack.user_id = $1 AND pack.chat_id = $2
		  AND COALESCE(priv.cups_earned, 0) <> COALESCE(pack.cups_earned, 0)
	`, userID, packChatID, moscowTime)
	if err != nil {
		return fmt.Errorf("sync private row cups from pack: %w", err)
	}
	return nil
}

// ApplyStreakSaveForUserScope — спасение стрика: сдвигает last_training_date и при необходимости
// восстанавливает streak_days на pack-row и private-row (chat_id = user_id).
func (d *Database) ApplyStreakSaveForUserScope(userID, packChatID int64, newLastTrainingDate string, restoreStreakDays int) error {
	if userID == 0 || packChatID == 0 {
		return nil
	}
	newLastTrainingDate = strings.TrimSpace(newLastTrainingDate)
	if newLastTrainingDate == "" {
		return fmt.Errorf("empty last training date")
	}
	if restoreStreakDays < 0 {
		restoreStreakDays = 0
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state
		SET last_training_date = $3,
		    streak_days = CASE
		        WHEN COALESCE(streak_days, 0) > 0 THEN streak_days
		        ELSE $4
		    END,
		    updated_at = $5
		WHERE user_id = $1 AND (chat_id = $2 OR chat_id = $1)
	`, userID, packChatID, newLastTrainingDate, restoreStreakDays, moscowTime)
	if err != nil {
		return fmt.Errorf("apply streak save for user scope: %w", err)
	}
	return nil
}

// SetCupsForUserScope выставляет cups_earned на pack-row и private-row (chat_id = user_id).
func (d *Database) SetCupsForUserScope(userID, packChatID int64, cups int) error {
	if userID == 0 || packChatID == 0 {
		return nil
	}
	if cups < 0 {
		cups = 0
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state
		SET cups_earned = $3,
		    updated_at = $4
		WHERE user_id = $1 AND (chat_id = $2 OR chat_id = $1)
	`, userID, packChatID, cups, moscowTime)
	if err != nil {
		return fmt.Errorf("set cups for user scope: %w", err)
	}
	return nil
}

// SubtractCupsForUserScope уменьшает кубки на pack-row и private-row (chat_id = user_id), не ниже 0.
func (d *Database) SubtractCupsForUserScope(userID, packChatID int64, cups int) error {
	if userID == 0 || packChatID == 0 || cups <= 0 {
		return nil
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state
		SET cups_earned = GREATEST(COALESCE(cups_earned, 0) - $3, 0),
		    updated_at = $4
		WHERE user_id = $1 AND (chat_id = $2 OR chat_id = $1)
	`, userID, packChatID, cups, moscowTime)
	if err != nil {
		return fmt.Errorf("subtract cups for user scope: %w", err)
	}
	return nil
}

// SubtractStreakDaysForUserScope уменьшает streak_days на pack-row и private-row, не ниже 0.
// max_streak_days (рекорд) не трогаем.
func (d *Database) SubtractStreakDaysForUserScope(userID, packChatID int64, days int) error {
	if userID == 0 || packChatID == 0 || days <= 0 {
		return nil
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state
		SET streak_days = GREATEST(COALESCE(streak_days, 0) - $3, 0),
		    updated_at = $4
		WHERE user_id = $1 AND (chat_id = $2 OR chat_id = $1)
	`, userID, packChatID, days, moscowTime)
	if err != nil {
		return fmt.Errorf("subtract streak for user scope: %w", err)
	}
	return nil
}

// SetAchievementsForUserScope выставляет ачивки на pack-row и private-row (chat_id = user_id).
func (d *Database) SetAchievementsForUserScope(userID, packChatID int64, achievementCount, lastAchievementStreakLevel int) error {
	if userID == 0 || packChatID == 0 {
		return nil
	}
	if achievementCount < 0 {
		achievementCount = 0
	}
	if achievementCount > 8 {
		achievementCount = 8
	}
	if lastAchievementStreakLevel < 0 {
		lastAchievementStreakLevel = 0
	}
	moscowTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	_, err := d.db.Exec(`
		UPDATE training_state
		SET achievement_count = $3,
		    last_achievement_streak_level = $4,
		    updated_at = $5
		WHERE user_id = $1 AND (chat_id = $2 OR chat_id = $1)
	`, userID, packChatID, achievementCount, lastAchievementStreakLevel, moscowTime)
	if err != nil {
		return fmt.Errorf("set achievements for user scope: %w", err)
	}
	return nil
}

// DecrementStreakSaveAttemptsUsed уменьшает счётчик использованных попыток (мин. 0) — даёт +1 доступную попытку.
func (d *Database) DecrementStreakSaveAttemptsUsed(userID, chatID int64, count int) (int, error) {
	if count <= 0 {
		count = 1
	}
	query := `
		UPDATE training_state
		SET streak_save_attempts_used = GREATEST(0, COALESCE(streak_save_attempts_used, 0) - $3),
		    updated_at = NOW() AT TIME ZONE 'Europe/Moscow'
		WHERE user_id = $1 AND chat_id = $2
		RETURNING COALESCE(streak_save_attempts_used, 0)
	`
	var used int
	err := d.db.QueryRow(query, userID, chatID, count).Scan(&used)
	if err != nil {
		return 0, err
	}
	return used, nil
}

// AdminDeleteFeedUserMessage — жёсткое удаление поста ленты (user_messages) и связанных реакций/треда.
func (d *Database) AdminDeleteFeedUserMessage(packChatID, messageID int64) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	if err := tx.QueryRow(
		`SELECT 1 FROM user_messages WHERE id = $1 AND chat_id = $2`,
		messageID, packChatID,
	).Scan(&exists); err == sql.ErrNoRows {
		return false, nil
	} else if err != nil {
		return false, fmt.Errorf("check user_message: %w", err)
	}

	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_thread_unread WHERE thread_reply_id IN (
			SELECT id FROM miniapp_training_feed_thread WHERE user_message_id = $1 AND pack_chat_id = $2
		)`,
		messageID, packChatID,
	); err != nil {
		return false, fmt.Errorf("delete thread unread: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_feed_thread_likes WHERE thread_reply_id IN (
			SELECT id FROM miniapp_training_feed_thread WHERE user_message_id = $1 AND pack_chat_id = $2
		)`,
		messageID, packChatID,
	); err != nil {
		return false, fmt.Errorf("delete thread likes: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_feed_thread WHERE user_message_id = $1 AND pack_chat_id = $2`,
		messageID, packChatID,
	); err != nil {
		return false, fmt.Errorf("delete thread: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND pack_chat_id = $2`,
		messageID, packChatID,
	); err != nil {
		return false, fmt.Errorf("delete reactions: %w", err)
	}
	if _, err := tx.Exec(`DELETE FROM miniapp_feed_poll_votes WHERE user_message_id = $1`, messageID); err != nil {
		return false, fmt.Errorf("delete poll votes: %w", err)
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_feed_reports WHERE user_message_id = $1 AND pack_chat_id = $2`,
		messageID, packChatID,
	); err != nil {
		return false, fmt.Errorf("delete feed reports: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM user_messages WHERE id = $1 AND chat_id = $2`, messageID, packChatID)
	if err != nil {
		return false, fmt.Errorf("delete user_message: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AdminDeleteTrainingFeedThreadReply — удалить комментарий в треде (без проверки автора).
func (d *Database) AdminDeleteTrainingFeedThreadReply(packChatID, threadReplyID int64) (bool, error) {
	if packChatID == 0 || threadReplyID == 0 {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_thread_unread WHERE thread_reply_id = $1`,
		threadReplyID,
	); err != nil {
		return false, err
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_feed_thread_likes WHERE thread_reply_id = $1`,
		threadReplyID,
	); err != nil {
		return false, err
	}
	res, err := tx.Exec(
		`DELETE FROM miniapp_training_feed_thread WHERE id = $1 AND pack_chat_id = $2`,
		threadReplyID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := tx.Exec(
		`DELETE FROM miniapp_feed_reports WHERE thread_reply_id = $1 AND pack_chat_id = $2`,
		threadReplyID, packChatID,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return n > 0, nil
}

// AdminDeletePackGroupMessage — удалить сообщение общего чата стаи (без проверки автора).
func (d *Database) AdminDeletePackGroupMessage(packChatID, messageID int64) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	if _, err := d.db.Exec(
		`DELETE FROM miniapp_pack_group_unread WHERE pack_message_id = $1`,
		messageID,
	); err != nil {
		return false, fmt.Errorf("delete pack group unread: %w", err)
	}
	res, err := d.db.Exec(
		`DELETE FROM miniapp_pack_group_chat WHERE id = $1 AND pack_chat_id = $2`,
		messageID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if _, err := d.db.Exec(
		`DELETE FROM miniapp_feed_reports
		 WHERE pack_chat_id = $1 AND target_type = 'pack_group_message' AND user_message_id = $2`,
		packChatID, messageID,
	); err != nil {
		return false, fmt.Errorf("delete pack group feed reports: %w", err)
	}
	return n > 0, nil
}

// AdminHideFeedUserMessage — soft hide поста ленты (остаётся в БД для аудита).
// reason различает бакеты в админ-инбоксе: "admin_delete" (удалил админ кнопкой)
// или "report" (скрыто по жалобе). Пустой reason — без явной категории (легаси).
func (d *Database) AdminHideFeedUserMessage(packChatID, messageID int64, reason string) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(
		`UPDATE user_messages SET is_hidden = TRUE, hidden_reason = NULLIF($3, '')
		  WHERE id = $1 AND chat_id = $2 AND COALESCE(is_hidden, FALSE) = FALSE`,
		messageID, packChatID, strings.TrimSpace(reason),
	)
	if err != nil {
		return false, fmt.Errorf("hide user_message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AdminHideTrainingFeedThreadReply — soft hide комментария в треде.
func (d *Database) AdminHideTrainingFeedThreadReply(packChatID, threadReplyID int64) (bool, error) {
	if packChatID == 0 || threadReplyID == 0 {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`UPDATE miniapp_training_feed_thread SET is_hidden = TRUE
		  WHERE id = $1 AND pack_chat_id = $2 AND COALESCE(is_hidden, FALSE) = FALSE`,
		threadReplyID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_training_thread_unread WHERE thread_reply_id = $1`,
		threadReplyID,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// AdminHidePackGroupMessage — soft hide сообщения общего чата.
func (d *Database) AdminHidePackGroupMessage(packChatID, messageID int64) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.Exec(
		`UPDATE miniapp_pack_group_chat SET is_hidden = TRUE
		  WHERE id = $1 AND pack_chat_id = $2 AND COALESCE(is_hidden, FALSE) = FALSE`,
		messageID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return false, nil
	}
	if _, err := tx.Exec(
		`DELETE FROM miniapp_pack_group_unread WHERE pack_message_id = $1`,
		messageID,
	); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

// HiddenModerationItem — скрытый админом контент, который можно вернуть.
type HiddenModerationItem struct {
	Kind         string
	ID           int64
	ParentID     int64
	AuthorUserID int64
	AuthorName   string
	Text         string
	CreatedAt    time.Time
	// Reason — причина скрытия поста: "admin_delete" / "report" / "" (легаси/прочее).
	Reason string
}

// CountHiddenModerationItems — сколько постов/комментов/сообщений чата скрыто в стае.
func (d *Database) CountHiddenModerationItems(packChatID int64) (int, error) {
	if packChatID == 0 {
		return 0, nil
	}
	var n int
	err := d.db.QueryRow(
		`SELECT
			(SELECT COUNT(*) FROM user_messages WHERE chat_id = $1 AND is_hidden = TRUE) +
			(SELECT COUNT(*) FROM miniapp_training_feed_thread WHERE pack_chat_id = $1 AND is_hidden = TRUE) +
			(SELECT COUNT(*) FROM miniapp_pack_group_chat WHERE pack_chat_id = $1 AND is_hidden = TRUE)`,
		packChatID,
	).Scan(&n)
	if err != nil {
		return 0, err
	}
	return n, nil
}

// ListHiddenModerationItems — последние скрытые элементы (все типы, по дате создания).
func (d *Database) ListHiddenModerationItems(packChatID int64, limit int) ([]HiddenModerationItem, error) {
	if packChatID == 0 {
		return []HiddenModerationItem{}, nil
	}
	if limit <= 0 {
		limit = 20
	}
	const q = `
		SELECT kind, id, parent_id, author_user_id, author_name, text, created_at, reason FROM (
			SELECT
				'feed_post'::text AS kind,
				um.id,
				0::bigint AS parent_id,
				um.user_id AS author_user_id,
				COALESCE(NULLIF(BTRIM(p.display_name), ''), NULLIF(BTRIM(um.username), ''), '') AS author_name,
				um.message_text AS text,
				um.created_at,
				COALESCE(um.hidden_reason, '') AS reason
			FROM user_messages um
			LEFT JOIN miniapp_user_profile p
				ON p.user_id = um.user_id AND p.pack_chat_id = um.chat_id
			WHERE um.chat_id = $1 AND um.is_hidden = TRUE
			UNION ALL
			SELECT
				'thread_reply'::text,
				t.id,
				t.user_message_id,
				t.from_user_id,
				COALESCE(NULLIF(BTRIM(t.username), ''), ''),
				t.message_text,
				t.created_at,
				''::text
			FROM miniapp_training_feed_thread t
			WHERE t.pack_chat_id = $1 AND t.is_hidden = TRUE
			UNION ALL
			SELECT
				'pack_group_message'::text,
				c.id,
				0::bigint,
				c.from_user_id,
				COALESCE(NULLIF(BTRIM(c.username), ''), ''),
				c.message_text,
				c.created_at,
				''::text
			FROM miniapp_pack_group_chat c
			WHERE c.pack_chat_id = $1 AND c.is_hidden = TRUE
		) hidden
		ORDER BY created_at DESC
		LIMIT $2`
	rows, err := d.db.Query(q, packChatID, limit)
	if err != nil {
		return nil, fmt.Errorf("list hidden moderation items: %w", err)
	}
	defer rows.Close()
	var out []HiddenModerationItem
	for rows.Next() {
		var item HiddenModerationItem
		if err := rows.Scan(
			&item.Kind, &item.ID, &item.ParentID, &item.AuthorUserID, &item.AuthorName, &item.Text, &item.CreatedAt, &item.Reason,
		); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if out == nil {
		out = []HiddenModerationItem{}
	}
	return out, nil
}

// AdminUnhideFeedUserMessage — вернуть скрытый пост в ленту.
func (d *Database) AdminUnhideFeedUserMessage(packChatID, messageID int64) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(
		`UPDATE user_messages SET is_hidden = FALSE, hidden_reason = NULL
		 WHERE id = $1 AND chat_id = $2 AND is_hidden = TRUE`,
		messageID, packChatID,
	)
	if err != nil {
		return false, fmt.Errorf("unhide user_message: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AdminUnhideTrainingFeedThreadReply — вернуть скрытый комментарий в треде.
func (d *Database) AdminUnhideTrainingFeedThreadReply(packChatID, threadReplyID int64) (bool, error) {
	if packChatID == 0 || threadReplyID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(
		`UPDATE miniapp_training_feed_thread SET is_hidden = FALSE
		 WHERE id = $1 AND pack_chat_id = $2 AND is_hidden = TRUE`,
		threadReplyID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AdminUnhidePackGroupMessage — вернуть скрытое сообщение в чате стаи.
func (d *Database) AdminUnhidePackGroupMessage(packChatID, messageID int64) (bool, error) {
	if packChatID == 0 || messageID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(
		`UPDATE miniapp_pack_group_chat SET is_hidden = FALSE
		 WHERE id = $1 AND pack_chat_id = $2 AND is_hidden = TRUE`,
		messageID, packChatID,
	)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AdminDeleteAllFeedMessagesByUser — удаляет все посты пользователя из ленты вместе с тредами,
// реакциями, голосами и жалобами. Возвращает количество удалённых постов.
func (d *Database) AdminDeleteAllFeedMessagesByUser(packChatID, userID int64) (int64, error) {
	if packChatID == 0 || userID == 0 {
		return 0, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		DELETE FROM miniapp_training_thread_unread
		WHERE thread_reply_id IN (
			SELECT t.id FROM miniapp_training_feed_thread t
			JOIN user_messages m ON m.id = t.user_message_id
			WHERE m.user_id = $1 AND m.chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del thread unread: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_training_feed_thread_likes
		WHERE thread_reply_id IN (
			SELECT t.id FROM miniapp_training_feed_thread t
			JOIN user_messages m ON m.id = t.user_message_id
			WHERE m.user_id = $1 AND m.chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del thread likes: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_training_feed_thread
		WHERE pack_chat_id = $2 AND user_message_id IN (
			SELECT id FROM user_messages WHERE user_id = $1 AND chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del threads: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_training_feed_reactions
		WHERE pack_chat_id = $2 AND user_message_id IN (
			SELECT id FROM user_messages WHERE user_id = $1 AND chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del reactions: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_feed_poll_votes
		WHERE user_message_id IN (
			SELECT id FROM user_messages WHERE user_id = $1 AND chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del poll votes: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_feed_reports
		WHERE pack_chat_id = $2 AND user_message_id IN (
			SELECT id FROM user_messages WHERE user_id = $1 AND chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del feed reports: %w", err)
	}
	res, err := tx.Exec(`DELETE FROM user_messages WHERE user_id = $1 AND chat_id = $2`, userID, packChatID)
	if err != nil {
		return 0, fmt.Errorf("del user_messages: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// AdminDeleteAllPackGroupMessagesByUser — удаляет все сообщения пользователя из чата стаи.
// Возвращает количество удалённых сообщений.
func (d *Database) AdminDeleteAllPackGroupMessagesByUser(packChatID, userID int64) (int64, error) {
	if packChatID == 0 || userID == 0 {
		return 0, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.Exec(`
		DELETE FROM miniapp_pack_group_unread
		WHERE pack_message_id IN (
			SELECT id FROM miniapp_pack_group_chat WHERE from_user_id = $1 AND pack_chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del pack group unread: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_pack_group_reactions
		WHERE pack_chat_id = $2 AND pack_message_id IN (
			SELECT id FROM miniapp_pack_group_chat WHERE from_user_id = $1 AND pack_chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del pack group reactions: %w", err)
	}
	if _, err := tx.Exec(`
		DELETE FROM miniapp_feed_reports
		WHERE pack_chat_id = $2 AND target_type = 'pack_group_message' AND user_message_id IN (
			SELECT id FROM miniapp_pack_group_chat WHERE from_user_id = $1 AND pack_chat_id = $2
		)`, userID, packChatID); err != nil {
		return 0, fmt.Errorf("del pack group reports: %w", err)
	}
	res, err := tx.Exec(`
		DELETE FROM miniapp_pack_group_chat WHERE from_user_id = $1 AND pack_chat_id = $2
	`, userID, packChatID)
	if err != nil {
		return 0, fmt.Errorf("del pack group messages: %w", err)
	}
	n, _ := res.RowsAffected()
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return n, nil
}

// AdminPackWipeCounts — сколько записей будет/было удалено при полной очистке ленты и чата стаи.
type AdminPackWipeCounts struct {
	FeedPosts        int64
	FeedThreads      int64
	FeedReports      int64
	PackChatMessages int64
}

func (d *Database) countPackWipeTargets(tx *sql.Tx, packChatID int64) (AdminPackWipeCounts, error) {
	var out AdminPackWipeCounts
	if packChatID == 0 {
		return out, nil
	}
	count := func(query string, dest *int64) error {
		return tx.QueryRow(query, packChatID).Scan(dest)
	}
	if err := count(`SELECT COUNT(*) FROM user_messages WHERE chat_id = $1`, &out.FeedPosts); err != nil {
		return out, err
	}
	if err := count(`SELECT COUNT(*) FROM miniapp_training_feed_thread WHERE pack_chat_id = $1`, &out.FeedThreads); err != nil {
		return out, err
	}
	if err := count(`SELECT COUNT(*) FROM miniapp_feed_reports WHERE pack_chat_id = $1`, &out.FeedReports); err != nil {
		return out, err
	}
	if err := count(`SELECT COUNT(*) FROM miniapp_pack_group_chat WHERE pack_chat_id = $1`, &out.PackChatMessages); err != nil {
		return out, err
	}
	return out, nil
}

// AdminCountPackFeedAndChat — превью перед очисткой ленты и общего чата стаи.
func (d *Database) AdminCountPackFeedAndChat(packChatID int64) (AdminPackWipeCounts, error) {
	if d == nil || packChatID == 0 {
		return AdminPackWipeCounts{}, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return AdminPackWipeCounts{}, err
	}
	defer func() { _ = tx.Rollback() }()
	out, err := d.countPackWipeTargets(tx, packChatID)
	if err != nil {
		return AdminPackWipeCounts{}, err
	}
	return out, nil
}

// AdminClearPackFeedAndChat — удаляет все посты ленты, треды, реакции, жалобы и общий чат стаи.
// training_state / профили / paywall не трогает.
func (d *Database) AdminClearPackFeedAndChat(packChatID int64) (AdminPackWipeCounts, error) {
	if d == nil || packChatID == 0 {
		return AdminPackWipeCounts{}, nil
	}
	tx, err := d.db.Begin()
	if err != nil {
		return AdminPackWipeCounts{}, err
	}
	defer func() { _ = tx.Rollback() }()

	before, err := d.countPackWipeTargets(tx, packChatID)
	if err != nil {
		return AdminPackWipeCounts{}, err
	}

	exec := func(query string, args ...any) error {
		_, err := tx.Exec(query, args...)
		return err
	}

	if err := exec(`
		DELETE FROM miniapp_training_thread_unread
		WHERE thread_reply_id IN (
			SELECT id FROM miniapp_training_feed_thread WHERE pack_chat_id = $1
		)`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del thread unread: %w", err)
	}
	if err := exec(`
		DELETE FROM miniapp_training_feed_thread_likes
		WHERE thread_reply_id IN (
			SELECT id FROM miniapp_training_feed_thread WHERE pack_chat_id = $1
		)`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del thread likes: %w", err)
	}
	if err := exec(`DELETE FROM miniapp_training_feed_thread WHERE pack_chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del threads: %w", err)
	}
	if err := exec(`DELETE FROM miniapp_training_feed_reactions WHERE pack_chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del feed reactions: %w", err)
	}
	if err := exec(`
		DELETE FROM miniapp_feed_poll_votes
		WHERE user_message_id IN (SELECT id FROM user_messages WHERE chat_id = $1)
	`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del poll votes: %w", err)
	}
	if err := exec(`DELETE FROM miniapp_feed_reports WHERE pack_chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del feed reports: %w", err)
	}
	if err := exec(`DELETE FROM user_messages WHERE chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del user_messages: %w", err)
	}
	if err := exec(`
		DELETE FROM miniapp_pack_group_unread
		WHERE pack_message_id IN (SELECT id FROM miniapp_pack_group_chat WHERE pack_chat_id = $1)
	`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del pack unread: %w", err)
	}
	if err := exec(`DELETE FROM miniapp_pack_group_reactions WHERE pack_chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del pack reactions: %w", err)
	}
	if err := exec(`DELETE FROM miniapp_pack_group_chat WHERE pack_chat_id = $1`, packChatID); err != nil {
		return AdminPackWipeCounts{}, fmt.Errorf("del pack chat: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return AdminPackWipeCounts{}, err
	}
	return before, nil
}
