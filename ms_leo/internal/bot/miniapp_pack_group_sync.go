package bot

import (
	"fmt"
	"strings"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// maybeSyncPackGroupChatFromTelegram — копирует сообщения из основной TG-стаи в таблицу чата мини-аппа.
// Реплики с префиксом «💬 Мини-апп» не трогаем: они уже записаны из HTTP с тем же telegram_message_id.
func (b *Bot) maybeSyncPackGroupChatFromTelegram(msg *tgbotapi.Message, text string) {
	if b == nil || b.db == nil || b.config.MonetizedChatID == 0 || msg == nil || msg.Chat == nil {
		return
	}
	if msg.Chat.ID != b.config.MonetizedChatID {
		return
	}
	if msg.Chat.Type != "supergroup" && msg.Chat.Type != "group" {
		return
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	if msg.IsCommand() {
		return
	}
	if msg.From == nil {
		return
	}
	if strings.HasPrefix(t, "💬 Мини-апп") {
		return
	}
	tgID := int64(msg.MessageID)
	if tgID == 0 {
		return
	}

	if msg.From.IsBot {
		if b.api == nil || msg.From.ID != b.api.Self.ID {
			return
		}
		leoName := "Лео"
		if b.api.Self.UserName != "" {
			leoName = "@" + b.api.Self.UserName
		}
		inserted, err := b.db.InsertMiniappPackGroupFromTelegramDedup(msg.Chat.ID, 0, leoName, true, t, tgID)
		if err != nil {
			b.logger.Warnf("pack group sync (bot): %v", err)
			return
		}
		if inserted {
			b.logger.Infof("pack group sync: inserted Leo TG msg_id=%d", tgID)
		}
		return
	}

	username := displayNameFromTGUser(msg.From)
	inserted, err := b.db.InsertMiniappPackGroupFromTelegramDedup(msg.Chat.ID, msg.From.ID, username, false, t, tgID)
	if err != nil {
		b.logger.Warnf("pack group sync (user): %v", err)
		return
	}
	if inserted {
		b.logger.Infof("pack group sync: inserted user %d TG msg_id=%d", msg.From.ID, tgID)
	}
}

func displayNameFromTGUser(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	if u.UserName != "" {
		return "@" + u.UserName
	}
	s := u.FirstName
	if u.LastName != "" {
		s += " " + u.LastName
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return fmt.Sprintf("user%d", u.ID)
	}
	return s
}

// relayPackMiniAppMessageToTelegram — дублирует сообщение из мини-аппа в основную группу (видят все в TG).
func (b *Bot) relayPackMiniAppMessageToTelegram(chatID int64, username, inner string) (int64, error) {
	if b == nil || b.api == nil {
		return 0, fmt.Errorf("bot api nil")
	}
	u := strings.TrimSpace(username)
	if u == "" {
		u = "участник"
	}
	body := "💬 Мини-апп · " + u + "\n\n" + strings.TrimSpace(inner)
	if len(body) > 4096 {
		body = body[:4089] + "…"
	}
	m := tgbotapi.NewMessage(chatID, body)
	m.DisableWebPagePreview = true
	sent, err := b.api.Send(m)
	if err != nil {
		return 0, err
	}
	return int64(sent.MessageID), nil
}
