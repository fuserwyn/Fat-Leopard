package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// MiniappSupportChatHistory — отдельный чат пользователя с поддержкой.
func (b *Bot) MiniappSupportChatHistory(userID int64, sinceID int64) ([]*domain.MiniappSupportChatMessage, error) {
	if b == nil || b.db == nil || userID == 0 {
		return []*domain.MiniappSupportChatMessage{}, nil
	}
	if b.config == nil || b.config.MonetizedChatID == 0 {
		return []*domain.MiniappSupportChatMessage{}, nil
	}
	items, err := b.db.ListMiniappSupportChat(userID, b.config.MonetizedChatID, 200, sinceID)
	if err != nil {
		return nil, err
	}
	for _, m := range items {
		if m != nil && m.PhotoURL != "" {
			m.PhotoURL = b.canonicalMiniappTrainingPhotoURL(m.PhotoURL)
		}
	}
	return items, nil
}

// MiniappSupportSendFromUser — пользователь пишет в поддержку, не в Лео.
func (b *Bot) MiniappSupportSendFromUser(userID int64, text, photoURL string) error {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || userID == 0 {
		return nil
	}
	t := strings.TrimSpace(text)
	photoURL = strings.TrimSpace(photoURL)
	if t == "" && photoURL == "" {
		return nil
	}
	var err error
	if photoURL != "" {
		_, err = b.db.InsertMiniappSupportChatMessageWithPhoto(userID, b.config.MonetizedChatID, "user", t, photoURL)
	} else {
		_, err = b.db.InsertMiniappSupportChatMessage(userID, b.config.MonetizedChatID, "user", t)
	}
	if err != nil {
		return err
	}
	b.notifyAdminsAboutSupportMessage(userID, t, photoURL)
	return nil
}

// AdminSupportChatHistory — история отдельного support-чата юзера для админа.
func (b *Bot) AdminSupportChatHistory(userID int64) ([]*domain.MiniappSupportChatMessage, error) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || userID == 0 {
		return []*domain.MiniappSupportChatMessage{}, nil
	}
	items, err := b.db.ListMiniappSupportChat(userID, b.config.MonetizedChatID, 30, 0)
	if err != nil {
		return nil, err
	}
	for _, m := range items {
		if m != nil && m.PhotoURL != "" {
			m.PhotoURL = b.canonicalMiniappTrainingPhotoURL(m.PhotoURL)
		}
	}
	return items, nil
}

// AdminSupportReply — ответ админа пользователю в отдельный support-чат.
func (b *Bot) AdminSupportReply(userID int64, text, photoURL string) error {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || userID == 0 {
		return nil
	}
	t := strings.TrimSpace(text)
	photoURL = strings.TrimSpace(photoURL)
	if t == "" && photoURL == "" {
		return nil
	}
	var err error
	if photoURL != "" {
		_, err = b.db.InsertMiniappSupportChatMessageWithPhoto(userID, b.config.MonetizedChatID, "support", t, photoURL)
	} else {
		_, err = b.db.InsertMiniappSupportChatMessage(userID, b.config.MonetizedChatID, "support", t)
	}
	if err != nil {
		return err
	}
	b.sendSupportUserDM(userID, t, photoURL)
	return nil
}

func (b *Bot) sendSupportUserDM(userID int64, text, photoURL string) {
	if b == nil || b.api == nil {
		return
	}
	photoURL = strings.TrimSpace(photoURL)
	if photoURL != "" {
		photoURL = b.canonicalMiniappTrainingPhotoURL(photoURL)
		msg := tgbotapi.NewPhoto(userID, tgbotapi.FileURL(photoURL))
		if t := strings.TrimSpace(text); t != "" {
			msg.Caption = t
		}
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Warnf("admin support dm photo user=%d: %v", userID, err)
		}
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, text)); err != nil {
		b.logger.Warnf("admin support dm user=%d: %v", userID, err)
	}
}

func (b *Bot) notifyAdminsAboutSupportMessage(userID int64, text, photoURL string) {
	if b == nil || b.api == nil || b.config == nil {
		return
	}
	adminIDs := make([]int64, 0, len(b.config.AdminIDs)+1)
	seen := map[int64]struct{}{}
	if b.config.OwnerID != 0 {
		seen[b.config.OwnerID] = struct{}{}
		adminIDs = append(adminIDs, b.config.OwnerID)
	}
	for _, id := range b.config.AdminIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		adminIDs = append(adminIDs, id)
	}
	if len(adminIDs) == 0 {
		return
	}

	title := fmt.Sprintf("Новый запрос в поддержку от %s", b.supportDisplayName(userID))
	body := clipAdminSupportText(text, 500)
	photoURL = strings.TrimSpace(photoURL)
	if photoURL != "" {
		photoURL = b.canonicalMiniappTrainingPhotoURL(photoURL)
	}
	replyMarkup := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("✍️ Ответить", fmt.Sprintf("admin_support_reply_%d", userID)),
		),
	)
	for _, adminID := range adminIDs {
		if photoURL != "" {
			msg := tgbotapi.NewPhoto(adminID, tgbotapi.FileURL(photoURL))
			caption := title
			if body != "" {
				caption += "\n\n" + body
			}
			caption += "\n\nНажми «⚙️ Админ-панель» внизу → Поддержка."
			msg.Caption = clipAdminSupportText(caption, 1024)
			msg.ReplyMarkup = replyMarkup
			if _, err := b.api.Send(msg); err != nil {
				b.logger.Warnf("support notify admin=%d user=%d: %v", adminID, userID, err)
			}
			continue
		}
		msg := tgbotapi.NewMessage(adminID, title+"\n\n"+body+"\n\nНажми «⚙️ Админ-панель» внизу → Поддержка.")
		msg.ReplyMarkup = replyMarkup
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Warnf("support notify admin=%d user=%d: %v", adminID, userID, err)
		}
	}
}

func (b *Bot) supportDisplayName(userID int64) string {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 || userID == 0 {
		return fmt.Sprintf("user%d · %d", userID, userID)
	}
	if p, err := b.db.GetMiniappUserProfile(userID, b.config.MonetizedChatID); err == nil && p != nil {
		if dn := strings.TrimSpace(p.DisplayName); dn != "" {
			return fmt.Sprintf("%s · %d", dn, userID)
		}
	}
	if ml, err := b.db.GetMessageLogAnyState(userID, b.config.MonetizedChatID); err == nil && ml != nil {
		if un := strings.TrimSpace(ml.Username); un != "" {
			return fmt.Sprintf("%s · %d", un, userID)
		}
	}
	return fmt.Sprintf("user%d · %d", userID, userID)
}
