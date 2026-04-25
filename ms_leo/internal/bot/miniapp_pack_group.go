package bot

import (
	"fmt"
	"regexp"
	"strings"
	"time"

	"leo-bot/internal/domain"

	initdata "github.com/telegram-mini-apps/init-data-golang"
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// @leo или @<username_бота> (как в группе).
var reMentionLeo = regexp.MustCompile(`(?i)@leo\b`)

func textMentionsLeoForPackGroup(text, botUsername string) bool {
	t := strings.TrimSpace(text)
	if t == "" {
		return false
	}
	if reMentionLeo.MatchString(t) {
		return true
	}
	bu := strings.TrimSpace(strings.ToLower(botUsername))
	if bu == "" {
		return false
	}
	lt := strings.ToLower(t)
	return strings.Contains(lt, "@"+bu)
}

func displayNameFromInitData(d initdata.InitData) string {
	u := d.User
	if u.Username != "" {
		return "@" + u.Username
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

// PackGroupChatForViewer — история общего чата (мини-апп), те же права, что и лента.
func (b *Bot) PackGroupChatForViewer(viewerUserID int64, initD initdata.InitData) ([]*domain.PackGroupChatMessage, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []*domain.PackGroupChatMessage{}, nil
	}
	if b.config.OwnerID != 0 && viewerUserID == b.config.OwnerID {
		// ok
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.config.PaywallEnabled)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrPackFeedForbidden
		}
	}
	return b.db.ListMiniappPackGroupChat(chatID, 100)
}

// ProcessMiniAppPackGroupMessage — сохраняет реплику; при @leo / @бот вызывает ИИ, без отправки в Telegram.
func (b *Bot) ProcessMiniAppPackGroupMessage(d initdata.InitData, text string) (MiniAppTextProcessResult, error) {
	out := MiniAppTextProcessResult{}
	if b == nil || strings.TrimSpace(text) == "" {
		return out, nil
	}
	if d.User.ID == 0 {
		return out, nil
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return out, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return out, nil
	}
	if b.config.OwnerID != 0 && d.User.ID == b.config.OwnerID {
		// владелец
	} else {
		ok, err := b.db.UserInPackOrPaid(d.User.ID, chatID, b.config.PaywallEnabled)
		if err != nil {
			return out, err
		}
		if !ok {
			return out, ErrPackFeedForbidden
		}
	}
	uname := displayNameFromInitData(d)

	var tgUser *int64
	if mid, err := b.relayPackMiniAppMessageToTelegram(chatID, uname, text); err != nil {
		b.logger.Warnf("pack miniapp relay to TG: %v", err)
	} else if mid != 0 {
		tgUser = &mid
	}
	if _, err := b.db.InsertMiniappPackGroupMessage(chatID, d.User.ID, uname, false, text, tgUser); err != nil {
		b.logger.Warnf("pack miniapp insert user row: %v", err)
	}

	botName := ""
	if b.api != nil && b.api.Self.ID != 0 {
		botName = b.api.Self.UserName
	}
	if !textMentionsLeoForPackGroup(text, botName) {
		return out, nil
	}
	tgU := tgbotapiUserFromInitData(d.User)
	if tgU == nil {
		return out, nil
	}
	msg := &tgbotapi.Message{
		MessageID: 0,
		From:      tgU,
		Chat: &tgbotapi.Chat{
			ID:    chatID,
			Type:  "supergroup",
			Title: "Staya",
		},
		Text: text,
		Date: int(time.Now().Unix()),
	}
	ch := make(chan string, 2)
	b.handleAIQuestion(msg, text, ch, true, true)
	var reply string
	select {
	case reply = <-ch:
	case <-time.After(3 * time.Minute):
		return out, nil
	}
	if strings.TrimSpace(reply) == "" {
		return out, nil
	}
	leoName := "Лео"
	if b.api != nil && b.api.Self.ID != 0 && b.api.Self.UserName != "" {
		leoName = "@" + b.api.Self.UserName
	}
	var leoTg *int64
	if b.api != nil {
		m := tgbotapi.NewMessage(chatID, reply)
		m.DisableWebPagePreview = true
		if sent, err := b.api.Send(m); err != nil {
			b.logger.Warnf("pack miniapp Leo reply to TG: %v", err)
		} else {
			v := int64(sent.MessageID)
			leoTg = &v
		}
	}
	if _, err := b.db.InsertMiniappPackGroupMessage(chatID, 0, leoName, true, reply, leoTg); err != nil {
		b.logger.Warnf("pack miniapp insert Leo row: %v", err)
	}
	out.ReplyText = reply
	return out, nil
}
