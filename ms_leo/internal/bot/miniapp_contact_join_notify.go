package bot

import (
	"strings"

	"leo-bot/internal/ai"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Уведомление «контакт из твоего списка вступил в стаю».
//
// Bot API не даёт боту читать адресную книгу Telegram, поэтому «список контактов» —
// это карточки, которые пользователь сам присылает боту в личку (скрепка → Контакт).
// Из карточки берём только Telegram user_id и имя, как оно записано у владельца.
// Когда такой человек впервые вступает в стаю (pack_join), Лео пишет владельцу в личку
// текст, сгенерированный по промпту contact_joined_pack в своём tone of voice.
// Отключается в профиле мини-аппа → «Уведомления».

const contactJoinedPackMaxLen = 600

// ContactJoinNotificationView — настройка для экрана профиля.
type ContactJoinNotificationView struct {
	Enabled       bool `json:"enabled"`
	ContactsCount int  `json:"contacts_count"`
}

// GetContactJoinNotificationForViewer — настройка + число сохранённых контактов (дефолт — ВКЛ).
func (b *Bot) GetContactJoinNotificationForViewer(viewerUserID int64, initD initdata.InitData) (ContactJoinNotificationView, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return ContactJoinNotificationView{}, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return ContactJoinNotificationView{}, err
	}
	enabled, err := b.db.IsContactJoinNotificationEnabled(viewerUserID, b.config.MonetizedChatID)
	if err != nil {
		return ContactJoinNotificationView{}, err
	}
	n, err := b.db.CountUserContacts(viewerUserID)
	if err != nil {
		return ContactJoinNotificationView{}, err
	}
	return ContactJoinNotificationView{Enabled: enabled, ContactsCount: n}, nil
}

// SaveContactJoinNotificationForViewer — переключатель из профиля мини-аппа.
func (b *Bot) SaveContactJoinNotificationForViewer(viewerUserID int64, initD initdata.InitData, enabled bool) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	return b.db.SaveContactJoinNotificationEnabled(viewerUserID, b.config.MonetizedChatID, enabled)
}

// contactCardDisplayName — «Имя Фамилия» из карточки контакта.
func contactCardDisplayName(c *tgbotapi.Contact) string {
	if c == nil {
		return ""
	}
	return strings.TrimSpace(strings.TrimSpace(c.FirstName) + " " + strings.TrimSpace(c.LastName))
}

// handleSharedContact — пользователь прислал боту карточку контакта в личку.
// Возвращает true, если сообщение обработано (дальше по пайплайну его не пускаем).
func (b *Bot) handleSharedContact(msg *tgbotapi.Message) bool {
	if b == nil || msg == nil || msg.Contact == nil || msg.From == nil || msg.Chat == nil || !msg.Chat.IsPrivate() {
		return false
	}
	ownerID := msg.From.ID
	c := msg.Contact
	name := contactCardDisplayName(c)
	if name == "" {
		name = "этот контакт"
	}
	var reply string
	switch {
	case c.UserID == 0:
		reply = "Не вижу у " + name + " аккаунта в Telegram — следить не за кем. Пришли контакт, который есть в Telegram."
	case c.UserID == ownerID:
		reply = "Это же ты сам. Пришли контакт друга — скажу, когда он появится в стае."
	default:
		if b.db == nil {
			return true
		}
		if err := b.db.SaveUserContact(ownerID, c.UserID, contactCardDisplayName(c)); err != nil {
			b.logger.Warnf("shared contact save owner=%d: %v", ownerID, err)
			reply = "Не смог запомнить контакт, попробуй ещё раз чуть позже."
			break
		}
		inPack := false
		if chatID := b.config.MonetizedChatID; chatID != 0 {
			if ok, err := b.db.UserInPackOrPaid(c.UserID, chatID, b.paywallEntryRequiresPayment()); err == nil {
				inPack = ok
			}
		}
		if inPack {
			reply = name + " уже в стае. Загляни в мини-апп и поддержи. 🐆"
		} else {
			reply = "Запомнил: " + name + ". Как только вступит в стаю — сообщу. Выключить такие сообщения можно в профиле мини-аппа, в разделе «Уведомления»."
		}
	}
	b.sendPackWelcomeDM(ownerID, reply)
	return true
}

// notifyContactsAboutPackJoin — разослать владельцам контактов, что joinedUserID вступил в стаю.
// Вызывается асинхронно из savePackJoinMiniappFeed только на pack_join (не на возврат).
func (b *Bot) notifyContactsAboutPackJoin(packChatID, joinedUserID int64, joinedDisplayName string) {
	if b == nil || b.db == nil || packChatID == 0 || joinedUserID == 0 {
		return
	}
	owners, err := b.db.ListContactOwners(joinedUserID)
	if err != nil {
		b.logger.Warnf("contact join notify: list owners user=%d: %v", joinedUserID, err)
		return
	}
	fallbackName := strings.TrimSpace(normalizeUserDisplayName(strings.TrimSpace(joinedDisplayName)))
	for _, o := range owners {
		if o.OwnerUserID == 0 || o.OwnerUserID == joinedUserID {
			continue
		}
		enabled, err := b.db.IsContactJoinNotificationEnabled(o.OwnerUserID, packChatID)
		if err != nil {
			b.logger.Warnf("contact join notify: settings owner=%d: %v", o.OwnerUserID, err)
			continue
		}
		if !enabled {
			continue
		}
		firstTime, err := b.db.MarkContactJoinNotified(o.OwnerUserID, joinedUserID, packChatID)
		if err != nil {
			b.logger.Warnf("contact join notify: dedupe owner=%d: %v", o.OwnerUserID, err)
			continue
		}
		if !firstTime {
			continue
		}
		name := strings.TrimSpace(o.ContactName)
		if name == "" {
			name = fallbackName
		}
		if name == "" {
			name = "Твой контакт"
		}
		text := b.contactJoinedPackText(o.OwnerUserID, packChatID, name)
		b.sendTrainingThreadCommentDM(o.OwnerUserID, text)
	}
}

// contactJoinedPackText — текст Лео через LLM; при сбое — короткий фоллбэк.
func (b *Bot) contactJoinedPackText(ownerUserID, packChatID int64, contactName string) string {
	if b.aiClient != nil {
		recipient := ""
		if ml, err := b.db.GetMessageLog(ownerUserID, packChatID); err == nil && ml != nil {
			recipient = strings.TrimSpace(normalizeUserDisplayName(ml.Username))
		}
		raw, err := b.aiClient.GenerateContactJoinedPack(recipient, contactName)
		if err != nil {
			b.logger.Warnf("contact join notify: generate owner=%d: %v", ownerUserID, err)
		} else if t := sanitizeContactJoinedPackText(ai.SanitizeTextForUser(raw)); t != "" {
			return t
		}
	}
	return contactJoinedPackFallbackText(contactName)
}

func sanitizeContactJoinedPackText(s string) string {
	t := strings.TrimSpace(s)
	t = strings.Trim(t, "\"«»")
	t = strings.TrimSpace(t)
	if r := []rune(t); len(r) > contactJoinedPackMaxLen {
		t = strings.TrimSpace(string(r[:contactJoinedPackMaxLen])) + "…"
	}
	return t
}

func contactJoinedPackFallbackText(contactName string) string {
	n := strings.TrimSpace(contactName)
	if n == "" {
		n = "Твой контакт"
	}
	return n + " теперь в стае. Загляни в мини-апп и поприветствуй — вдвоём держать ритм проще. 🐆"
}
