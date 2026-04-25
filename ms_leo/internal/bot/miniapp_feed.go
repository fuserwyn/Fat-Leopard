package bot

import "errors"

// ErrPackFeedForbidden — смотрящему нельзя видеть ленту (нет в стае / не оплачено).
var ErrPackFeedForbidden = errors.New("pack feed forbidden")

// PackFeedItem — JSON для мини-апpa.
type PackFeedItem struct {
	ID         int64  `json:"id"`
	UserID     int64  `json:"user_id"`
	Username   string `json:"username"`
	Type       string `json:"type"`
	Text       string `json:"text"`
	CreatedAt  string `json:"created_at"`
	StreakDays int    `json:"streak_days"`
	IsYou      bool   `json:"is_you"`
}

// PackFeedForViewer — лента «стаи» из user_messages (отчёты) для участника/оплатившего.
func (b *Bot) PackFeedForViewer(viewerUserID int64) ([]PackFeedItem, error) {
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []PackFeedItem{}, nil
	}
	if b.config.OwnerID != 0 && viewerUserID == b.config.OwnerID {
		// владелец видит ленту без лишних проверок
	} else {
		ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.config.PaywallEnabled)
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrPackFeedForbidden
		}
	}
	rows, err := b.db.ListPackActivityFeed(chatID, 50)
	if err != nil {
		return nil, err
	}
	out := make([]PackFeedItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, PackFeedItem{
			ID:         r.ID,
			UserID:     r.UserID,
			Username:   r.Username,
			Type:       r.MessageType,
			Text:       r.MessageText,
			CreatedAt:  r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			StreakDays: r.StreakDays,
			IsYou:      r.UserID == viewerUserID,
		})
	}
	return out, nil
}
