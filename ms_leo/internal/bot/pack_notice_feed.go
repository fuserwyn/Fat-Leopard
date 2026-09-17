package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"

	"leo-bot/internal/prompts"
)

const userMessageTypeInactiveNotice = "inactive_notice"

// PackRemovedFeedNotice — текст для общей ленты (видна стае; выбывшему уже может быть недоступно).
func PackRemovedFeedNotice(p prompts.Bundle, displayName string) string {
	d := strings.TrimSpace(displayName)
	if d == "" {
		d = "Участник"
	}
	raw := ""
	if p.PackFeedParticipantRemoved != "" {
		raw = strings.TrimSpace(p.PackFeedParticipantRemoved)
	}
	if raw != "" && strings.Contains(raw, "%s") && strings.Count(raw, "%") == 1 {
		return strings.TrimSpace(fmt.Sprintf(raw, d))
	}
	if raw != "" {
		return strings.ReplaceAll(raw, "{{name}}", d)
	}
	return fmt.Sprintf("%s, не двигался 7 дней подряд и я его съел..", d)
}

// saveInactiveNoticePackFeed — короткая отметка в общей ленте (дубликат темы предупреждения Лео в ЛС).
func (b *Bot) saveInactiveNoticePackFeed(targetUserID int64, username, body string) {
	if b == nil || b.db == nil || targetUserID == 0 {
		return
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return
	}
	um := &domain.UserMessage{
		UserID:      targetUserID,
		ChatID:      chatID,
		Username:    strings.TrimSpace(username),
		MessageText: body,
		MessageType: userMessageTypeInactiveNotice,
	}
	if err := b.db.SaveUserMessage(um); err != nil {
		b.logger.Warnf("inactive_notice pack feed user=%d: %v", targetUserID, err)
	}
}
