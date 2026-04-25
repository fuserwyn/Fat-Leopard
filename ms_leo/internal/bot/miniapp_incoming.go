package bot

import (
	"time"

	initdata "github.com/telegram-mini-apps/init-data-golang"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// mirrorMiniAppToPrivateChat — дублирует в личку с ботом текст из мини-аппа, чтобы переписка в Telegram совпадала по смыслу.
// От имени пользователя в DM из API писать нельзя, только от бота с меткой.
func (b *Bot) mirrorMiniAppToPrivateChat(userID int64, text string) {
	if b == nil || b.api == nil || userID == 0 || text == "" {
		return
	}
	line := "💬 Мини-апп\n\n" + text
	m := tgbotapi.NewMessage(userID, line)
	m.DisableWebPagePreview = true
	if _, err := b.api.Send(m); err != nil {
		b.logger.Warnf("miniapp mirror to private chat: %v", err)
	}
}

// PrivateTextMessageFromInitUser — синтетическое входящее сообщение, как в личке.
func PrivateTextMessageFromInitUser(d initdata.InitData, text string) *tgbotapi.Message {
	u := d.User
	tgU := tgbotapiUserFromInitData(u)
	return &tgbotapi.Message{
		MessageID: 0,
		From:      tgU,
		Date:      int(time.Now().Unix()),
		Chat:      privateChatForUser(tgU),
		Text:      text,
	}
}

func tgbotapiUserFromInitData(u initdata.User) *tgbotapi.User {
	if u.ID == 0 {
		return nil
	}
	return &tgbotapi.User{
		ID:           u.ID,
		IsBot:        u.IsBot,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		UserName:     u.Username,
		LanguageCode: u.LanguageCode,
	}
}

func privateChatForUser(u *tgbotapi.User) *tgbotapi.Chat {
	if u == nil {
		return nil
	}
	return &tgbotapi.Chat{
		ID:        u.ID,
		Type:      "private",
		FirstName: u.FirstName,
		LastName:  u.LastName,
		UserName:  u.UserName,
	}
}

// MiniAppTextProcessResult — копия персонального ответа бота (тот же текст, что в личку), если она сформирована.
type MiniAppTextProcessResult struct {
	ReplyText string `json:"reply_text,omitempty"`
}

// ProcessMiniAppPrivateText — валидация initData, та же ветка, что и getUpdates (личка).
// Вызывать с уже валидированной init-строкой; для проверки подписи смотри initdata.Validate.
func (b *Bot) ProcessMiniAppPrivateText(d initdata.InitData, text string) MiniAppTextProcessResult {
	out := MiniAppTextProcessResult{}
	if text == "" || b == nil {
		return out
	}
	if d.User.ID == 0 {
		return out
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return out
	}
	msg := PrivateTextMessageFromInitUser(d, text)
	if b.enforcePaywallForMonetizedChatMessage(msg) {
		return out
	}
	// Не блокируем HTTP-ответ мини-аппа на Send в Telegram — ИИ и так долгий; зеркало в фоне.
	go b.mirrorMiniAppToPrivateChat(d.User.ID, text)

	ch := make(chan string, 2)
	b.dispatchTextMessageFromUser(msg, ch)

	// dispatch синхронный; ждём ответ для reply_text (таймаут чуть выше OpenRouter HTTP).
	timer := time.NewTimer(4*time.Minute + 15*time.Second)
	defer timer.Stop()
	select {
	case t := <-ch:
		out.ReplyText = t
	case <-timer.C:
		b.logger.Warnf("miniapp private: reply channel timeout user_id=%d", d.User.ID)
	}
	return out
}
