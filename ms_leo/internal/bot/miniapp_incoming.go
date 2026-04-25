package bot

import (
	"time"

	initdata "github.com/telegram-mini-apps/init-data-golang"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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

// ProcessMiniAppPrivateText — валидация initData, та же ветка, что и getUpdates (личка).
// Вызывать с уже валидированной init-строкой; для проверки подписи смотри initdata.Validate.
func (b *Bot) ProcessMiniAppPrivateText(d initdata.InitData, text string) {
	if text == "" || b == nil {
		return
	}
	if d.User.ID == 0 {
		return
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return
	}
	msg := PrivateTextMessageFromInitUser(d, text)
	if b.enforcePaywallForMonetizedChatMessage(msg) {
		return
	}
	b.dispatchTextMessageFromUser(msg)
}
