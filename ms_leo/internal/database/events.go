package database

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Аналитические события Fat Leopard — спецификация analytics_BT_v1.md.
// Имена событий используются как event_name в таблице events; держим их
// константами, чтобы эмиттеры в боте/мини-аппе не расходились по строкам.
const (
	// Воронка 1 — БОТ (приобретение и оплата)
	EventBotStarted            = "bot_started"
	EventPaywallViewed         = "paywall_viewed"
	EventPaymentMethodSelected = "payment_method_selected"
	EventPaymentInitiated      = "payment_initiated"
	EventPaymentCompleted      = "payment_completed"
	EventPaymentFailed         = "payment_failed"
	EventWelcomeMessageSent    = "welcome_message_sent"
	EventMiniappOpened         = "miniapp_opened"

	// Донаты из профиля мини-аппа — добровольная поддержка, доступ не выдаётся.
	EventDonateInitiated = "donate_initiated"
	EventDonateCompleted = "donate_completed"

	// Воронка 2 — АКТИВАЦИЯ (первая тренировка)
	EventWorkoutLogStarted   = "workout_log_started"
	EventWorkoutLogged       = "workout_logged"
	EventLeoCommentReceived  = "leo_comment_received"
	EventLeoCommentDisplayed = "leo_comment_displayed"

	// Воронка 3 — RETENTION И СЕРИИ
	EventStreakIncremented        = "streak_incremented"
	EventStreakAttemptUsed        = "streak_attempt_used"
	EventStreakBroken             = "streak_broken"
	EventLevelUp                  = "level_up"
	EventMilestoneAchieved        = "milestone_achieved"
	EventBurnWarningSent          = "burn_warning_sent"
	EventBurnRecovered            = "burn_recovered"
	EventSickLeaveStarted         = "sick_leave_started"
	EventSickLeaveEnded           = "sick_leave_ended"
	EventAccountDeletedInactivity = "account_deleted_inactivity"
	EventAccountReactivated       = "account_reactivated"

	// Челленджи: N дней подряд с тренировкой
	EventChallengeJoined    = "challenge_joined"
	EventChallengeCompleted = "challenge_completed"
	EventChallengeFailed    = "challenge_failed"
	EventChallengeCreated   = "challenge_created"

	// UGC, Лео, модерация
	EventFeedReactionAdded    = "feed_reaction_added"
	EventFeedCommentPosted    = "feed_comment_posted"
	EventLeoChatMessageSent   = "leo_chat_message_sent"
	EventLeoChatLimitReached  = "leo_chat_limit_reached"
	EventModerationBlocked    = "moderation_blocked"
	EventComplaintFiled       = "complaint_filed"
	EventReportResolved       = "report_resolved"
	EventSupportButtonClicked = "support_button_clicked"

	// Интерес к нон-спорт функционалу (счётчик запроса фичи «вносить не только спорт»)
	EventNonSportInterest = "non_sport_interest"
)

// AnalyticsEvent — одно событие для записи в таблицу events.
// Нулевые user_id/telegram_id и пустые строковые поля пишутся как NULL.
type AnalyticsEvent struct {
	Name       string
	UserID     int64
	TelegramID int64
	Payload    map[string]any
	Source     string // utm_source / channel attribution
	SessionID  string // UUID; пустая строка => NULL
	AppVersion string
	// IdempotencyKey — ключ дедупликации (напр. payment_id для финансовых событий).
	// При повторной вставке с тем же ключом запись не плодится (§9.2).
	IdempotencyKey string
}

// SetAlphaTesterIDs задаёт множество telegram_id альфа-тестеров (§10). Вызывается
// один раз на старте до приёма трафика — поэтому без мьютекса. Каждое событие
// этих пользователей помечается is_alpha=true для отдельной агрегации.
func (d *Database) SetAlphaTesterIDs(ids []int64) {
	if d == nil {
		return
	}
	m := make(map[int64]bool, len(ids))
	for _, id := range ids {
		if id != 0 {
			m[id] = true
		}
	}
	d.alphaTesterIDs = m
}

func (d *Database) isAlphaTester(telegramID, userID int64) bool {
	if d == nil || len(d.alphaTesterIDs) == 0 {
		return false
	}
	return d.alphaTesterIDs[telegramID] || d.alphaTesterIDs[userID]
}

// eventInsertRetryDelays — задержки повторов записи события (§9.2). Первая попытка
// сразу, далее экспоненциальный backoff. In-memory, без внешней очереди — для F1 хватает.
var eventInsertRetryDelays = []time.Duration{0, 500 * time.Millisecond, 2 * time.Second, 8 * time.Second}

// TrackEvent асинхронно (non-blocking, §9.2) пишет событие в таблицу events.
// При сбое БД повторяет с backoff. Ошибки не возвращаются — аналитика никогда не
// должна мешать основному flow. Каждому событию проставляется idempotency_key
// (свой или авто-UUID), поэтому повтор после «фантомного» успеха не плодит дубли.
func (d *Database) TrackEvent(ev AnalyticsEvent) {
	if d == nil || d.db == nil || strings.TrimSpace(ev.Name) == "" {
		return
	}
	if strings.TrimSpace(ev.IdempotencyKey) == "" {
		ev.IdempotencyKey = ev.Name + ":" + uuid.NewString()
	}
	go d.trackEventWithRetry(ev)
}

func (d *Database) trackEventWithRetry(ev AnalyticsEvent) {
	var lastErr error
	for _, delay := range eventInsertRetryDelays {
		if delay > 0 {
			time.Sleep(delay)
		}
		if err := d.insertEvent(ev); err != nil {
			lastErr = err
			continue
		}
		return // успех (или ON CONFLICT DO NOTHING — дубль не плодим)
	}
	if d.logger != nil {
		d.logger.Warnf("TrackEvent %s: отброшено после %d попыток: %v", ev.Name, len(eventInsertRetryDelays), lastErr)
	} else {
		log.Printf("TrackEvent %s: dropped after %d attempts: %v", ev.Name, len(eventInsertRetryDelays), lastErr)
	}
}

func (d *Database) insertEvent(ev AnalyticsEvent) error {
	// Без payload в колонку должен уйти NULL. Пустой []byte драйвер шлёт как
	// пустую строку, Postgres отвечает «invalid input syntax for type json» —
	// и событие без payload (bot_started, paywall_viewed, …) не записывалось вовсе.
	var payloadJSON any
	if len(ev.Payload) > 0 {
		b, err := json.Marshal(ev.Payload)
		if err != nil {
			return fmt.Errorf("marshal payload: %w", err)
		}
		payloadJSON = string(b)
	}

	// occurred_at заполняется DEFAULT-ом (московский wall-clock, как остальные таблицы).
	// NULLIF(...,'') приводит пустые строки/нули к NULL, чтобы не засорять колонки.
	_, err := d.db.Exec(`
		INSERT INTO events (event_name, user_id, telegram_id, payload, session_id, source, app_version, idempotency_key, is_alpha)
		VALUES ($1, NULLIF($2, 0), NULLIF($3, 0), $4, NULLIF($5, '')::uuid, NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9)
		ON CONFLICT (idempotency_key) WHERE idempotency_key IS NOT NULL DO NOTHING
	`, ev.Name, ev.UserID, ev.TelegramID, payloadJSON, ev.SessionID, ev.Source, ev.AppVersion, ev.IdempotencyKey, d.isAlphaTester(ev.TelegramID, ev.UserID))
	if err != nil {
		return fmt.Errorf("insert event: %w", err)
	}
	return nil
}

// AnonymizeUserAnalytics заменяет telegram_id/user_id во всех событиях пользователя
// на псевдоним — удаление PII при удалении аккаунта (analytics_BT_v1 §9.3, 152-ФЗ).
// salt — секрет деплоя (бот-токен), чтобы псевдоним нельзя было обратить перебором
// telegram_id. Агрегаты по событиям сохраняются (один человек → один псевдоним).
// Идемпотентно: повторный вызов уже не найдёт реального id.
func (d *Database) AnonymizeUserAnalytics(telegramID int64, salt string) error {
	if d == nil || d.db == nil || telegramID == 0 {
		return nil
	}
	pseudo := pseudonymizeID(telegramID, salt)
	_, err := d.db.Exec(`
		UPDATE events
		SET telegram_id = CASE WHEN telegram_id = $2 THEN $1 ELSE telegram_id END,
		    user_id     = CASE WHEN user_id = $2 THEN $1 ELSE user_id END
		WHERE telegram_id = $2 OR user_id = $2
	`, pseudo, telegramID)
	if err != nil {
		return fmt.Errorf("anonymize events: %w", err)
	}
	return nil
}

// pseudonymizeID — детерминированный псевдоним из telegram_id и секретной соли.
// Первые 8 байт sha256 → положительный int64. Огромное значение (до 2^62) не
// пересекается с реальными telegram_id (< ~10^10).
func pseudonymizeID(id int64, salt string) int64 {
	h := sha256.Sum256([]byte(salt + ":pii:" + strconv.FormatInt(id, 10)))
	return int64(binary.BigEndian.Uint64(h[:8]) >> 1)
}
