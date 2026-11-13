package bot

import (
	"strings"
	"time"

	"leo-bot/internal/i18n"
	"leo-bot/internal/utils"
)

// localizeDailyWisdomForViewer — текст мудрости дня под ui_lang зрителя ленты.
// В user_messages лежит один вариант (обычно EN); полный RU/EN — в daily_wisdom_log.
func (b *Bot) localizeDailyWisdomForViewer(viewerUserID int64, createdAt time.Time, stored string) string {
	stored = strings.TrimSpace(stored)
	if b == nil || b.db == nil || stored == "" {
		return stored
	}
	lang := b.EffectiveUILang(viewerUserID)

	var textEN, textRU string
	var ok bool
	if !createdAt.IsZero() {
		mskDate := utils.GetMoscowDateFromTime(createdAt)
		en, ru, found, err := b.db.GetDailyWisdomByMSKDate(mskDate)
		if err != nil {
			b.logger.Warnf("daily wisdom by date: %v", err)
		} else if found {
			textEN, textRU, ok = en, ru, true
		}
	}
	if !ok {
		en, ru, found, err := b.db.GetDailyWisdomMatchingStored(stored)
		if err != nil {
			b.logger.Warnf("daily wisdom match stored: %v", err)
		} else if found {
			textEN, textRU, ok = en, ru, true
		}
	}
	if !ok {
		return stored
	}
	if lang == i18n.RU {
		if strings.TrimSpace(textRU) != "" {
			return textRU
		}
		return stored
	}
	if strings.TrimSpace(textEN) != "" {
		return textEN
	}
	return stored
}
