package bot

import (
	"fmt"
	"strconv"
	"strings"

	"leo-bot/internal/database"
)

// Приглашение друга: личная ссылка t.me/<бот>?start=ref-<id>. Источник визита
// (bot_started.source = "ref-<id>") пишет parseStartSource — в дашборде «Каналы»
// такие визиты сведены в канал «ref». За каждых ReferralRewardEvery друзей,
// которые записали первую тренировку (не просто перешли), — +1 попытка спасти стрик.

const (
	// referralStartPrefix — параметр ссылки t.me/<бот>?start=ref-<id пригласившего>.
	referralStartPrefix = "ref-"
	// ReferralRewardEvery — сколько друзей с первой тренировкой дают одну попытку спасти стрик.
	ReferralRewardEvery = 10
)

// ReferralView — блок «Позвать в стаю» в профиле мини-аппа.
type ReferralView struct {
	Link        string `json:"link"`
	Joined      int    `json:"joined"`       // пришли по ссылке
	Qualified   int    `json:"qualified"`    // из них записали первую тренировку
	RewardEvery int    `json:"reward_every"` // друзей за одну попытку
	Rewards     int    `json:"rewards"`      // уже выдано попыток спасти стрик
	NextIn      int    `json:"next_in"`      // сколько друзей с тренировкой до следующей
}

// parseReferralStartPayload — id пригласившего из аргумента /start; 0 — не ссылка-приглашение.
func parseReferralStartPayload(arg string) int64 {
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(arg, referralStartPrefix) {
		return 0
	}
	id, err := strconv.ParseInt(arg[len(referralStartPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
}

// referralLink — личная ссылка-приглашение.
func referralLink(botName string, userID int64) string {
	if botName == "" || userID <= 0 {
		return ""
	}
	return "https://t.me/" + botName + "?start=" + referralStartPrefix + strconv.FormatInt(userID, 10)
}

// ReferralRewardAttempts — сколько попыток спасти стрик дают qualified друзей с тренировкой.
func ReferralRewardAttempts(qualified int) int {
	if qualified <= 0 {
		return 0
	}
	return qualified / ReferralRewardEvery
}

// referralNextIn — сколько ещё друзей с тренировкой до следующей попытки.
func referralNextIn(qualified int) int {
	if qualified < 0 {
		qualified = 0
	}
	return ReferralRewardEvery - qualified%ReferralRewardEvery
}

// rememberReferralFromStart засчитывает новичка за пригласившим. Вызывать до того,
// как /start заведёт новичку профиль и визит: засчитываются только новые люди.
func (b *Bot) rememberReferralFromStart(userID int64, arg string) bool {
	inviterID := parseReferralStartPayload(arg)
	if inviterID == 0 || b == nil || b.db == nil || userID == 0 {
		return false
	}
	ok, err := b.db.RecordReferral(inviterID, userID)
	if err != nil {
		b.logger.Warnf("referral from start user=%d inviter=%d: %v", userID, inviterID, err)
		return false
	}
	return ok
}

// referralRewardBonus — попытки спасти стрик, заработанные приглашениями.
func (b *Bot) referralRewardBonus(userID int64) int {
	if b == nil || b.db == nil || userID == 0 {
		return 0
	}
	_, qualified, err := b.db.GetReferralCounts(userID)
	if err != nil {
		b.logger.Warnf("referral counts user=%d: %v", userID, err)
		return 0
	}
	return ReferralRewardAttempts(qualified)
}

// referralOnTraining — приглашённый записал тренировку. Первая тренировка засчитывает
// его пригласившему; каждый ReferralRewardEvery-й такой друг приносит попытку спасти стрик.
func (b *Bot) referralOnTraining(userID int64) {
	if b == nil || b.db == nil || userID == 0 {
		return
	}
	inviterID, qualified, err := b.db.MarkReferralFirstWorkout(userID)
	if err != nil {
		b.logger.Warnf("referral first workout user=%d: %v", userID, err)
		return
	}
	if inviterID == 0 || qualified == 0 || qualified%ReferralRewardEvery != 0 {
		return
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventReferralRewardGranted, UserID: inviterID, TelegramID: inviterID,
		Payload:        map[string]any{"qualified": qualified},
		IdempotencyKey: fmt.Sprintf("referral_reward:%d:%d", inviterID, qualified),
	})
	text := fmt.Sprintf("🐆 Уже %d друзей, которых ты позвал, записали первую тренировку. Держи +1 попытку спасти стрик!", qualified)
	b.notifyUserTextByID(inviterID, inviterID, text, "", 0)
}

// GetReferralForAPI — ссылка и счётчики для блока «Позвать в стаю».
func (b *Bot) GetReferralForAPI(userID int64) (ReferralView, error) {
	v := ReferralView{RewardEvery: ReferralRewardEvery, NextIn: ReferralRewardEvery}
	if b == nil || b.db == nil || userID == 0 {
		return v, nil
	}
	joined, qualified, err := b.db.GetReferralCounts(userID)
	if err != nil {
		return v, err
	}
	v.Link = referralLink(b.botUsername(), userID)
	v.Joined = joined
	v.Qualified = qualified
	v.Rewards = ReferralRewardAttempts(qualified)
	v.NextIn = referralNextIn(qualified)
	return v, nil
}

// ShareLinkForUser — личная ссылка на бота для карточек «похвастаться» (ачивка,
// уровень, тренировка): та же реферальная t.me/<бот>?start=ref-<id>, чтобы друг,
// пришедший со сторис, засчитался приглашением. Пусто, если имя бота неизвестно.
func (b *Bot) ShareLinkForUser(userID int64) string {
	if b == nil {
		return ""
	}
	return referralLink(b.botUsername(), userID)
}
