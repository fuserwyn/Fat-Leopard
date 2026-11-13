package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

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
				"🔇 Pack posts are limited for 24 hours due to rule violations. If this is a mistake — contact support in Profile.",
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
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Failed to mute: "+err.Error()))
		return
	}
	count, _ := b.db.IncrementUGCViolationCount(targetUserID, packChatID)
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf(
		"🔇 User %d: UGC mute for %d h. Violations: %d.",
		targetUserID, hours, count,
	)))
	b.notifyUserTextByID(targetUserID, packChatID,
		fmt.Sprintf("🔇 Pack posts are limited for %d h by a moderator.", hours),
		"", 0)
}

func (b *Bot) adminUnmuteUserUGC(chatID, targetUserID, packChatID int64) {
	if b == nil || b.db == nil || targetUserID == 0 || packChatID == 0 {
		return
	}
	muted, err := b.db.IsUserUGCMuted(targetUserID, packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Failed to check mute status: "+err.Error()))
		return
	}
	if !muted {
		b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("ℹ️ User %d is not UGC-muted.", targetUserID)))
		return
	}
	if err := b.db.UnmuteUserUGC(targetUserID, packChatID); err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Failed to unmute: "+err.Error()))
		return
	}
	b.api.Send(tgbotapi.NewMessage(chatID, fmt.Sprintf("🔊 User %d: UGC mute lifted.", targetUserID)))
	b.notifyUserTextByID(targetUserID, packChatID,
		"🔊 Pack posting limits lifted. You can write in the feed, chat, and notes again.",
		"", 0)
}

func (b *Bot) deliverModerationWarning(userID int64, surface moderation.Surface, rawText string, res moderation.Result) {
	if b == nil || userID == 0 || strings.TrimSpace(res.UserMessage) == "" {
		return
	}
	// Системный текст в личку Telegram (не LLM).
	if b.api != nil {
		text := b.Msg(userID, "mod_warn_header") + "\n\n" + res.UserMessage
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
		surfaceLabel = "workout note"
	case moderation.SurfaceFeedComment:
		surfaceLabel = "feed comment"
	case moderation.SurfacePackGroupChat:
		surfaceLabel = "pack group chat"
	case moderation.SurfaceAdminPost, moderation.SurfaceAdminPollQuestion, moderation.SurfaceAdminPollOption:
		surfaceLabel = "admin post"
	case moderation.SurfaceLeoChat:
		surfaceLabel = "private chat with Leo"
	}
	preview := truncateForDM(rawText, 200)
	body := fmt.Sprintf(
		"🚨 PRE-moderation · %s\nUser: %d\nReason: %s\n\n«%s»",
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
