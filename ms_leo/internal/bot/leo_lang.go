package bot

import "leo-bot/internal/i18n"

// leoAnswer — ответ Лео на языке ui_lang пользователя.
func (b *Bot) leoAnswer(userID int64, question, userContext string) (string, error) {
	if b == nil || b.aiClient == nil {
		return "", nil
	}
	return b.aiClient.AnswerUserQuestionLang(question, userContext, b.EffectiveUILang(userID))
}

func (b *Bot) leoLang(userID int64) i18n.Lang {
	return b.EffectiveUILang(userID)
}
