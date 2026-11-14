package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/i18n"
	"leo-bot/internal/moderation"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

var ErrModerationBlocked = errors.New("moderation blocked")

// ModerationBlockedError — PRE-блок UGC с кодом для miniapp API.
type ModerationBlockedError struct {
	APICode string
	Message string
}

func (e *ModerationBlockedError) Error() string {
	if e == nil {
		return "moderation blocked"
	}
	if e.Message != "" {
		return e.Message
	}
	return "moderation blocked"
}

func (b *Bot) ugcGate() *moderation.Gate {
	if b == nil {
		return moderation.NewGate(nil)
	}
	if b.ugcModerationGate == nil {
		if b.ugcModerationLimiter == nil {
			b.ugcModerationLimiter = moderation.NewLimiter()
		}
		b.ugcModerationGate = moderation.NewGate(b.ugcModerationLimiter)
	}
	return b.ugcModerationGate
}

func (b *Bot) enforceUGC(text string, surface moderation.Surface, userID int64) (moderation.Result, error) {
	if b != nil && b.db != nil && b.config != nil && b.config.MonetizedChatID != 0 && userID > 0 {
		if muted, err := b.db.IsUserUGCMuted(userID, b.config.MonetizedChatID); err != nil {
			b.logger.Warnf("ugc mute check user=%d: %v", userID, err)
		} else if muted {
			res := moderation.Result{
				Allowed:     false,
				Reason:      moderation.ReasonMuted,
				UserMessage: moderation.UserWarnings[moderation.ReasonMuted],
				APICode:     "user_muted",
			}
			b.deliverModerationWarning(userID, surface, text, res)
			return res, &ModerationBlockedError{APICode: res.APICode, Message: res.UserMessage}
		}
	}
	res := b.ugcGate().Check(text, surface, userID, time.Now())
	if res.Allowed {
		return res, nil
	}
	b.deliverModerationWarning(userID, surface, text, res)
	if res.AlertAdmin {
		b.notifyAdminsModerationAlert(userID, surface, text, res)
	}
	if res.Reason == moderation.ReasonCriticalRU && b.db != nil && b.config != nil && b.config.MonetizedChatID != 0 {
		b.recordUGCViolation(userID, b.config.MonetizedChatID, false)
	}
	return res, &ModerationBlockedError{APICode: res.APICode, Message: res.UserMessage}
}

// enforceAdminBroadcast — PRE для админ-постов/опросов (без rate-limit и mute).
func (b *Bot) enforceAdminBroadcast(text string, surface moderation.Surface) error {
	res := b.ugcGate().CheckContent(text, surface, time.Now())
	if res.Allowed {
		return nil
	}
	if res.AlertAdmin && b.config != nil {
		b.notifyAdminsModerationAlert(0, surface, text, res)
	}
	return fmt.Errorf("%s", res.UserMessage)
}

func (b *Bot) userTimezoneOffset(userID int64) int {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return 0
	}
	log, err := b.db.GetMessageLog(userID, b.config.MonetizedChatID)
	if err != nil || log == nil {
		return 0
	}
	return log.TimezoneOffsetFromMoscow
}

// enforceLeoChat — PRE + дневной лимит 20 сообщений в личку с Лео.
func (b *Bot) enforceLeoChat(text string, userID int64) (moderation.Result, error) {
	if b == nil || userID == 0 {
		return moderation.Allowed(), nil
	}
	res := b.ugcGate().Check(text, moderation.SurfaceLeoChat, userID, time.Now())
	if !res.Allowed {
		b.deliverModerationWarning(userID, moderation.SurfaceLeoChat, text, res)
		if res.AlertAdmin {
			b.notifyAdminsModerationAlert(userID, moderation.SurfaceLeoChat, text, res)
		}
		return res, &ModerationBlockedError{APICode: res.APICode, Message: res.UserMessage}
	}
	if b.db != nil && b.config != nil && b.config.MonetizedChatID != 0 {
		localDate := b.getUserLocalDate(b.userTimezoneOffset(userID))
		count, err := b.db.CountMiniappPersonalChatUserMessagesOnDate(userID, b.config.MonetizedChatID, localDate, b.userTimezoneOffset(userID))
		if err != nil {
			b.logger.Warnf("leo daily limit count user=%d: %v", userID, err)
		} else if count >= moderation.MaxLeoChatMessagesPerDay {
			res = moderation.Result{
				Allowed:     false,
				Reason:      moderation.ReasonLeoDaily,
				UserMessage: moderation.UserWarnings[moderation.ReasonLeoDaily],
				APICode:     moderation.APICodeFor(moderation.ReasonLeoDaily),
			}
			b.trackLeoChatLimitReached(userID)
			b.deliverModerationWarning(userID, moderation.SurfaceLeoChat, text, res)
			return res, &ModerationBlockedError{APICode: res.APICode, Message: res.UserMessage}
		}
	}
	return moderation.Allowed(), nil
}

const ugcAutoMuteViolationThreshold = 3
const ugcMuteDuration = 24 * time.Hour

// recordUGCViolation — +1 к счётчику; при autoMute=true или пороге ≥3 — мьют 24ч.
func (b *Bot) recordUGCViolation(userID, packChatID int64, autoMute bool) int {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return 0
	}
	count, err := b.db.IncrementUGCViolationCount(userID, packChatID)
	if err != nil {
		b.logger.Warnf("ugc violation increment user=%d: %v", userID, err)
		return 0
	}
	if autoMute || count >= ugcAutoMuteViolationThreshold {
		until := time.Now().UTC().Add(ugcMuteDuration)
		if err := b.db.MuteUserUGCUntil(userID, packChatID, until); err != nil {
			b.logger.Warnf("ugc auto mute user=%d: %v", userID, err)
		} else {
			b.notifyUserTextByID(userID, packChatID,
				b.userText(userID,
					"🔇 Публикации в Стае временно ограничены на 24 часа из‑за нарушений правил. Если это ошибка — поддержка в профиле.",
					"🔇 Pack posts are limited for 24 hours due to rule violations. If this is a mistake — contact support in Profile."),
				"", 0)
		}
	}
	return count
}

func (b *Bot) adminMuteUserUGC(chatID, targetUserID, packChatID int64, hours int) {
	if hours <= 0 {
		hours = 24
	}
	if err := b.db.MuteUserUGCUntil(targetUserID, packChatID, time.Now().UTC().Add(time.Duration(hours)*time.Hour)); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось замьютить: "+err.Error()))
		return
	}
	count, _ := b.db.IncrementUGCViolationCount(targetUserID, packChatID)
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"🔇 Пользователь %d: UGC-мьют на %d ч. Нарушений: %d.",
		targetUserID, hours, count,
	)))
	b.notifyUserTextByID(targetUserID, packChatID,
		adminUGCMuteNotice(b.EffectiveUILang(targetUserID), hours),
		"", 0)
}

func (b *Bot) adminUnmuteUserUGC(chatID, targetUserID, packChatID int64) {
	if b == nil || b.db == nil || targetUserID == 0 || packChatID == 0 {
		return
	}
	muted, err := b.db.IsUserUGCMuted(targetUserID, packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось проверить мьют: "+err.Error()))
		return
	}
	if !muted {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ℹ️ Пользователь %d не в UGC-мьюте.", targetUserID)))
		return
	}
	if err := b.db.UnmuteUserUGC(targetUserID, packChatID); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось размьютить: "+err.Error()))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔊 Пользователь %d: UGC-мьют снят.", targetUserID)))
	b.notifyUserTextByID(targetUserID, packChatID,
		adminUGCUnmuteNotice(b.EffectiveUILang(targetUserID)),
		"", 0)
}

func (b *Bot) deliverModerationWarning(userID int64, surface moderation.Surface, rawText string, res moderation.Result) {
	if b == nil || userID == 0 || strings.TrimSpace(res.UserMessage) == "" {
		return
	}
	// Системный текст в личку Telegram (не LLM).
	if b.api != nil {
		text := b.Msg(userID, "mod_warn_header") + "\n\n" + moderationWarningLang(b.EffectiveUILang(userID), res)
		m := tgbotapi.NewMessage(userID, text)
		if _, err := b.api.Send(m); err != nil {
			b.logger.Warnf("moderation warning DM user=%d: %v", userID, err)
		}
	}
	_ = surface
	_ = rawText
}

func (b *Bot) notifyAdminsModerationAlert(userID int64, surface moderation.Surface, rawText string, res moderation.Result) {
	if b == nil || b.config == nil {
		return
	}
	surfaceLabel := "UGC"
	switch surface {
	case moderation.SurfaceTrainingNote:
		surfaceLabel = "заметка тренировки"
	case moderation.SurfaceFeedComment:
		surfaceLabel = "комментарий в ленте"
	case moderation.SurfacePackGroupChat:
		surfaceLabel = "общий чат стаи"
	case moderation.SurfaceAdminPost, moderation.SurfaceAdminPollQuestion, moderation.SurfaceAdminPollOption:
		surfaceLabel = "админ-публикация"
	case moderation.SurfaceLeoChat:
		surfaceLabel = "личный чат с Лео"
	}
	preview := truncateForDM(rawText, 200)
	body := fmt.Sprintf(
		"🚨 PRE-модерация · %s\nПользователь: %d\nПричина: %s\n\n«%s»",
		surfaceLabel, userID, res.Reason, preview,
	)
	for _, adminID := range b.config.AdminTelegramUserIDs() {
		if adminID == 0 {
			continue
		}
		m := tgbotapi.NewMessage(adminID, body)
		if _, err := b.api.Send(m); err != nil {
			b.logger.Warnf("moderation admin alert admin=%d: %v", adminID, err)
		}
	}
}

// ModerationHTTPStatus — HTTP-код для miniapp API при PRE-блоке.
func ModerationHTTPStatus(code string) int {
	switch code {
	case "moderation_rate_limited", "leo_daily_limited":
		return 429
	case "user_muted":
		return 403
	default:
		return 400
	}
}

// moderationWarningRU — тексты PRE-блока для ui_lang=ru (moderation.UserWarnings — английские).
var moderationWarningRU = map[moderation.Reason]string{
	moderation.ReasonProfanity:   "Не опубликовал — в тексте есть недопустимые слова. Перепиши без оскорблений и мата.",
	moderation.ReasonLink:        "Не опубликовал — ссылки в Стае пока запрещены.",
	moderation.ReasonCriticalRU:  "Не опубликовал — такой текст нельзя публиковать.",
	moderation.ReasonRateLimited: "Подожди немного перед следующим сообщением.",
	moderation.ReasonMuted:       "Публикация в Стае временно ограничена. Если это ошибка — напиши в поддержку.",
	moderation.ReasonLeoDaily:    "Достигнут лимит — 20 сообщений Лео в день. Завтра можно продолжить.",
	moderation.ReasonSpam:        "Не опубликовал — слишком много повторяющихся слов. Перепиши понятнее.",
}

// moderationWarningLang — предупреждение модерации на языке получателя.
func moderationWarningLang(lang i18n.Lang, res moderation.Result) string {
	if !socialRU(lang) {
		return res.UserMessage
	}
	if res.Reason == moderation.ReasonTooLong {
		digits := strings.Map(func(r rune) rune {
			if r >= '0' && r <= '9' {
				return r
			}
			return -1
		}, res.UserMessage)
		if digits != "" {
			return "Слишком длинно — сократи до " + digits + " символов."
		}
		return "Слишком длинно — сократи текст."
	}
	if ru, ok := moderationWarningRU[res.Reason]; ok {
		return ru
	}
	return "Не опубликовал — текст не прошёл проверку."
}
