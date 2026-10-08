package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/database"
)

// welcomeEventIdempotencyKey — ключ дедупликации welcome_message_sent по заявке paywall.
// reqID<=0 (нет заявки) => пустой ключ (без дедупликации).
func welcomeEventIdempotencyKey(reqID int64) string {
	if reqID <= 0 {
		return ""
	}
	return fmt.Sprintf("welcome_message_sent:%d", reqID)
}

// trackPaymentCompleted фиксирует успешную оплату (воронка 1, §3) с дедупликацией по заявке.
// provider/amount/payment_id берём из записи paywall-заявки.
func (b *Bot) trackPaymentCompleted(userID int64, rec *database.PaywallAccessRequest) {
	if b == nil || b.db == nil || rec == nil {
		return
	}
	payload := map[string]any{}
	var idemSuffix string
	switch {
	case rec.YookassaPaymentID.Valid && rec.YookassaPaymentID.String != "":
		payload["provider"] = "yukassa"
		payload["payment_id"] = rec.YookassaPaymentID.String
		idemSuffix = rec.YookassaPaymentID.String
	case rec.TelegramPaymentChargeID.Valid && rec.TelegramPaymentChargeID.String != "":
		payload["provider"] = "stars"
		payload["payment_id"] = rec.TelegramPaymentChargeID.String
		idemSuffix = rec.TelegramPaymentChargeID.String
	}
	if rec.TotalAmountMinor.Valid {
		payload["amount_minor"] = rec.TotalAmountMinor.Int64
	}
	if rec.Currency.Valid && rec.Currency.String != "" {
		payload["currency"] = rec.Currency.String
	}
	if idemSuffix == "" {
		idemSuffix = fmt.Sprintf("req:%d", rec.ID)
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:           database.EventPaymentCompleted,
		UserID:         userID,
		TelegramID:     userID,
		Payload:        payload,
		IdempotencyKey: "payment_completed:" + idemSuffix,
	})
}

// parseStartSource достаёт канал атрибуции из аргумента deep-link `/start`.

// trackModerationBlocked фиксирует срабатывание PRE-фильтра модерации (analytics_BT_v1 §6).
// surface: "workout_note" | "feed_comment" | "leo_chat"; reason — код причины (APICode).
func (b *Bot) trackModerationBlocked(surface, reason string, telegramID int64) {
	if b == nil || b.db == nil {
		return
	}
	if reason == "" {
		reason = "profanity"
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventModerationBlocked,
		TelegramID: telegramID,
		Payload:    map[string]any{"surface": surface, "reason": reason},
	})
}

// trackFeedReactionAdded — реакция в ленте стаи (analytics_BT_v1 §6).
func (b *Bot) trackFeedReactionAdded(telegramID int64, targetType, reaction string) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventFeedReactionAdded,
		UserID:     telegramID,
		TelegramID: telegramID,
		Payload:    map[string]any{"target_type": targetType, "reaction_type": reaction},
	})
}

// trackFeedCommentPosted — комментарий в треде ленты (analytics_BT_v1 §6).
func (b *Bot) trackFeedCommentPosted(telegramID int64, length int) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventFeedCommentPosted,
		UserID:     telegramID,
		TelegramID: telegramID,
		Payload:    map[string]any{"length": length},
	})
}

// trackComplaintFiled — жалоба через кнопку (analytics_BT_v1 §6).
func (b *Bot) trackComplaintFiled(telegramID int64, targetType string, targetID int64) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventComplaintFiled,
		UserID:     telegramID,
		TelegramID: telegramID,
		Payload:    map[string]any{"target_type": targetType, "target_id": targetID},
	})
}

// trackReportResolved — админ закрыл жалобу (analytics_BT_v1 §6).
// action: "delete" | "hide" | "mute" | "no_action". Идемпотентность по reportID:
// одна жалоба закрывается один раз.
func (b *Bot) trackReportResolved(reportID, adminTelegramID, targetUserID int64, action string) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:           database.EventReportResolved,
		TelegramID:     adminTelegramID,
		Payload:        map[string]any{"action": action, "report_id": reportID, "target_user_id": targetUserID},
		IdempotencyKey: fmt.Sprintf("report_resolved:%d", reportID),
	})
}

// trackAccountDeletedInactivity — авто-удаление за неактивность (analytics_BT_v1 §5).
// Идемпотентно по (user, chat): повторный проход watchdog не плодит дубль.
func (b *Bot) trackAccountDeletedInactivity(userID, packChatID int64) {
	if b == nil || b.db == nil {
		return
	}
	payload := map[string]any{}
	if ml, err := b.db.GetMessageLog(userID, packChatID); err == nil && ml != nil {
		payload["last_streak"] = ml.StreakDays
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:           database.EventAccountDeletedInactivity,
		UserID:         userID,
		TelegramID:     userID,
		Payload:        payload,
		IdempotencyKey: fmt.Sprintf("account_deleted_inactivity:%d:%d", userID, packChatID),
	})
}

// trackAccountReactivated — вернулся после удаления (новый платёж, §5).
func (b *Bot) trackAccountReactivated(userID int64) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventAccountReactivated, UserID: userID, TelegramID: userID,
	})
}

// trackStreakAttemptUsed — использована попытка спасения стрика (§5).
func (b *Bot) trackStreakAttemptUsed(userID int64, attemptsLeft int) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventStreakAttemptUsed, UserID: userID, TelegramID: userID,
		Payload: map[string]any{"attempts_left": attemptsLeft},
	})
}

// trackSickLeaveStarted / trackSickLeaveEnded — больничный (§5). via: manual|training|auto_14d.
func (b *Bot) trackSickLeaveStarted(userID int64) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventSickLeaveStarted, UserID: userID, TelegramID: userID,
	})
}

func (b *Bot) trackSickLeaveEnded(userID int64, via string) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventSickLeaveEnded, UserID: userID, TelegramID: userID,
		Payload: map[string]any{"via": via},
	})
}

// trackLeoChatLimitReached — хит дневного лимита сообщений Лео (analytics_BT_v1 §6).
func (b *Bot) trackLeoChatLimitReached(telegramID int64) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventLeoChatLimitReached,
		UserID:     telegramID,
		TelegramID: telegramID,
	})
}

// parseStartSource достаёт канал атрибуции из аргумента deep-link `/start`.
// Формат UTM (analytics_BT_v1 §8): `?start=src-tg_channel_main` → "tg_channel_main".
// Ссылка на челлендж `?start=ch-<код>` даёт источник "ch-<код>" как есть.
// Пустой аргумент => "organic". Значение усечено до 32 символов (колонка source).
func parseStartSource(arg string) string {
	arg = strings.TrimSpace(arg)
	if arg == "" {
		return "organic"
	}
	arg = strings.TrimPrefix(arg, "src-")
	if len(arg) > 32 {
		arg = arg[:32]
	}
	return arg
}

// TrackEvent — фасад над database.TrackEvent для слоёв, у которых есть только *Bot
// (например HTTP-сервер мини-аппа). Запись non-blocking, ошибки не пробрасываются.
func (b *Bot) TrackEvent(ev database.AnalyticsEvent) {
	if b == nil || b.db == nil {
		return
	}
	b.db.TrackEvent(ev)
}
