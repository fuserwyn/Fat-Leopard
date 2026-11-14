package bot

import (
	"strings"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// PackMember — леопард из «Слежу за» в мини-аппе.
type PackMember struct {
	UserID         int64  `json:"user_id"`
	Name           string `json:"name"`
	StreakDays     int    `json:"streak_days"`
	Following      bool   `json:"following"`
	NotifyWorkouts bool   `json:"notify_workouts"`
}

func packMemberDisplayName(displayName, username string) string {
	name := strings.TrimSpace(displayName)
	if name == "" {
		name = normalizeUserDisplayName(username)
	}
	if strings.TrimSpace(name) == "" {
		name = "Pack member"
	}
	return name
}

// ListPackMembersForViewer — леопарды, за которыми viewer следит (экран «Слежу за»).
func (b *Bot) ListPackMembersForViewer(viewerUserID int64, initD initdata.InitData) ([]PackMember, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return nil, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	rows, err := b.db.ListFollowingPackMembers(chatID, viewerUserID)
	if err != nil {
		return nil, err
	}
	out := make([]PackMember, 0, len(rows))
	for _, r := range rows {
		out = append(out, PackMember{
			UserID:         r.UserID,
			Name:           packMemberDisplayName(r.DisplayName, r.Username),
			StreakDays:     r.StreakDays,
			Following:      true,
			NotifyWorkouts: r.NotifyWorkouts,
		})
	}
	return out, nil
}

// FollowFriend / UnfollowFriend — подписка viewer на участника стаи.
func (b *Bot) FollowFriend(viewerUserID int64, initD initdata.InitData, targetUserID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	if targetUserID == 0 || targetUserID == viewerUserID {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	// Подписываться можно только на реального участника стаи.
	ok, err := b.db.UserInPackOrPaid(targetUserID, chatID, b.paywallEntryRequiresPayment())
	if err != nil {
		return err
	}
	if !ok {
		return ErrTrainingFeedParentNotFound
	}
	return b.db.AddFriendSubscription(viewerUserID, targetUserID, chatID)
}

// SetFriendWorkoutNotify — вкл/выкл уведомления о тренировках леопарда из «Слежу за».
func (b *Bot) SetFriendWorkoutNotify(viewerUserID int64, initD initdata.InitData, targetUserID int64, enabled bool) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	if targetUserID == 0 || targetUserID == viewerUserID {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	if err := b.db.SetFriendWorkoutNotify(viewerUserID, targetUserID, chatID, enabled); err != nil {
		if strings.Contains(err.Error(), "not found") {
			return ErrTrainingFeedParentNotFound
		}
		return err
	}
	return nil
}

func (b *Bot) UnfollowFriend(viewerUserID int64, initD initdata.InitData, targetUserID int64) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	if targetUserID == 0 {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	return b.db.RemoveFriendSubscription(viewerUserID, targetUserID, chatID)
}

// SetAllFriendsWorkoutNotify — глобальный тумблер «Тренировки друзей» (секция
// «Уведомления» в профиле): вкл/выкл DM о тренировках по всем подпискам viewer'а.
func (b *Bot) SetAllFriendsWorkoutNotify(viewerUserID int64, initD initdata.InitData, enabled bool) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	return b.db.SetAllFriendsWorkoutNotify(viewerUserID, b.config.MonetizedChatID, enabled)
}

// enrichPackFeedFriends — проставляет IsFriend на карточках авторов, на которых подписан viewer.
// Один запрос за все подписки viewer'а; стоит дёшево даже на большой ленте.
func (b *Bot) enrichPackFeedFriends(items []PackFeedItem, viewerUserID, packChatID int64) []PackFeedItem {
	if b == nil || b.db == nil || viewerUserID == 0 || packChatID == 0 || len(items) == 0 {
		return items
	}
	targetIDs, err := b.db.ListFriendTargetIDs(viewerUserID, packChatID)
	if err != nil {
		b.logger.Warnf("pack feed friends enrich: %v", err)
		return items
	}
	if len(targetIDs) == 0 {
		return items
	}
	friends := make(map[int64]struct{}, len(targetIDs))
	for _, id := range targetIDs {
		friends[id] = struct{}{}
	}
	for i := range items {
		if _, ok := friends[items[i].UserID]; ok {
			items[i].IsFriend = true
		}
	}
	return items
}

// notifyFriendWorkoutWatchers — DM подписчикам с включёнными уведомлениями о тренировках леопарда.
func (b *Bot) notifyFriendWorkoutWatchers(authorUserID, packChatID int64, authorName string, streak int, authorGender string) {
	if b == nil || b.db == nil || authorUserID == 0 || packChatID == 0 {
		return
	}
	subscriberIDs, err := b.db.ListFriendWorkoutNotifySubscribers(authorUserID, packChatID)
	if err != nil {
		b.logger.Warnf("friend workout notify subscribers: %v", err)
		return
	}
	if len(subscriberIDs) == 0 {
		return
	}
	for _, subID := range subscriberIDs {
		if subID == authorUserID {
			continue
		}
		lang := b.EffectiveUILang(subID)
		body := friendWorkoutText(lang, socialNameOr(authorName, lang), authorGender, streak)
		b.sendTrainingThreadCommentDM(subID, body)
	}
}
