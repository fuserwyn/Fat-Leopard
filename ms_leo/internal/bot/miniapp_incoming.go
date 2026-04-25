package bot

import (
	"strings"
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

// MiniAppTextProcessResult — ответ бота в личку: либо сразу reply_text (редко), либо pending + poll очереди.
type MiniAppTextProcessResult struct {
	ReplyText string `json:"reply_text,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
}

// ProcessMiniAppPrivateText — валидация initData; обработка в фоне, HTTP не ждёт ИИ.
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
	b.miniappPersonalClear(d.User.ID)
	go b.mirrorMiniAppToPrivateChat(d.User.ID, text)
	go b.runMiniAppPrivateTextWorker(d, text)
	out.Pending = true
	return out
}

func (b *Bot) runMiniAppPrivateTextWorker(d initdata.InitData, text string) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("miniapp private worker panic: %v", r)
		}
	}()
	msg := PrivateTextMessageFromInitUser(d, text)
	ch := make(chan string, 32)
	b.dispatchTextMessageFromUser(msg, ch)
	for {
		select {
		case t := <-ch:
			tr := strings.TrimSpace(t)
			if tr == "" {
				continue
			}
			b.miniappPersonalPush(d.User.ID, tr)
		default:
			return
		}
	}
}
