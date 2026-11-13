package bot

import (
	"strings"

	"leo-bot/internal/database"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// LikeNotificationView — настройка «уведомлять о лайках в ленте» для отдачи в мини-апп.
type LikeNotificationView struct {
	Enabled bool `json:"enabled"`
}

// GetLikeNotificationForViewer — настройка уведомлений о лайках текущего пользователя (дефолт — ВЫКЛ).
func (b *Bot) GetLikeNotificationForViewer(viewerUserID int64, initD initdata.InitData) (LikeNotificationView, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return LikeNotificationView{}, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return LikeNotificationView{}, err
	}
	s, err := b.db.GetLikeNotificationSettings(viewerUserID, b.config.MonetizedChatID)
	if err != nil {
		return LikeNotificationView{}, err
	}
	return LikeNotificationView{Enabled: s.Enabled}, nil
}

// SaveLikeNotificationForViewer — сохранить настройку уведомлений о лайках текущего пользователя.
func (b *Bot) SaveLikeNotificationForViewer(viewerUserID int64, initD initdata.InitData, enabled bool) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	return b.db.SaveLikeNotificationSettings(viewerUserID, b.config.MonetizedChatID, database.LikeNotificationSettings{
		Enabled: enabled,
	})
}

// likeVerbForGender — «liked/liked/liked» в зависимости от пола лайкнувшего.
func likeVerbForGender(gender string) string {
	switch strings.TrimSpace(strings.ToLower(gender)) {
	case "m":
		return "liked"
	case "f":
		return "liked"
	default:
		return "liked"
	}
}

// notifyFeedPostLiked — DM автору отчёта, когда его карточку в ленте лайкнули (поставили реакцию).
// Идемпотентно по паре (автор, лайкнувший, пост): переставленная реакция не спамит личку.
func (b *Bot) notifyFeedPostLiked(packChatID, likerUserID int64, likerName string, userMessageID int64, emoji string) {
	if b == nil || b.db == nil || packChatID == 0 || likerUserID == 0 || userMessageID == 0 {
		return
	}
	authorID, ok, err := b.db.GetUserMessageAuthorUserID(packChatID, userMessageID)
	if err != nil {
		b.logger.Warnf("feed like notify: author lookup: %v", err)
		return
	}
	if !ok || authorID == 0 || authorID == likerUserID {
		return
	}
	b.deliverFeedLikeDM(packChatID, authorID, likerUserID, likerName, "post", userMessageID, emoji, "your workout report")
}

// notifyFeedCommentLiked — DM автору комментария, когда его реплику в треде лайкнули.
func (b *Bot) notifyFeedCommentLiked(packChatID, likerUserID int64, likerName string, threadReplyID, authorID int64) {
	if b == nil || b.db == nil || packChatID == 0 || likerUserID == 0 || threadReplyID == 0 {
		return
	}
	// authorID == 0 — комментарий Лео (системный), уведомлять некого.
	if authorID == 0 || authorID == likerUserID {
		return
	}
	b.deliverFeedLikeDM(packChatID, authorID, likerUserID, likerName, "comment", threadReplyID, "❤️", "your comment")
}

// deliverFeedLikeDM — общая доставка: проверяет подписку получателя, дедуп и шлёт личку.
func (b *Bot) deliverFeedLikeDM(packChatID, recipientID, likerUserID int64, likerName, targetKind string, targetID int64, emoji, what string) {
	enabled, err := b.db.IsLikeNotificationEnabled(recipientID, packChatID)
	if err != nil {
		b.logger.Warnf("feed like notify: settings: %v", err)
		return
	}
	if !enabled {
		return
	}
	firstTime, err := b.db.MarkFeedLikeNotified(recipientID, packChatID, likerUserID, targetKind, targetID)
	if err != nil {
		b.logger.Warnf("feed like notify: dedupe: %v", err)
		return
	}
	if !firstTime {
		return
	}
	name := strings.TrimSpace(likerName)
	if name == "" {
		name = "Pack member"
	}
	gender, _, _ := b.GetMiniappUserProfileJSONForAPI(likerUserID, packChatID)
	verb := likeVerbForGender(gender)
	badge := strings.TrimSpace(emoji)
	if badge == "" {
		badge = "❤️"
	}
	body := badge + " " + name + " " + verb + " " + what + " in the pack.\n\nOpen the mini app → Pack tab."
	// Пуш в inbox мини-аппа (доедет на новые устройства) + Telegram-личка, как «мудрость дня».
	b.miniappPersonalPush(recipientID, body)
	b.sendTrainingThreadCommentDM(recipientID, body)
}
