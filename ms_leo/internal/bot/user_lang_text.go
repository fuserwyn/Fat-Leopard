package bot

import "leo-bot/internal/i18n"

// pickLang — ru-вариант для русского языка получателя, иначе en.
func pickLang(lang i18n.Lang, ru, en string) string {
	if socialRU(lang) {
		return ru
	}
	return en
}

// userText — текст на языке ui_lang получателя userID (ru|en).
func (b *Bot) userText(userID int64, ru, en string) string {
	return pickLang(b.EffectiveUILang(userID), ru, en)
}
