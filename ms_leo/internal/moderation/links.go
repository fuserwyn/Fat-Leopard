package moderation

import "regexp"

// reURL — ссылки (http/www/t.me/tg://) в Стае запрещены.
//
// @-упоминания НЕ блокируем: через @ник или @Имя отмечают напарника на
// совместной тренировке и участников стаи в комментариях и чате — это фича
// (notifyJointTrainingMentions, notifyPackMemberMentions). Раньше любое
// «@слово» из 4+ символов резалось как «упоминание канала», и отчёт о
// совместной тренировке нельзя было опубликовать.
var reURL = regexp.MustCompile(`(?i)(?:https?://|www\.|t\.me/|telegram\.me/|tg://)`)

func containsBlockedLink(text string) bool {
	if text == "" {
		return false
	}
	return reURL.MatchString(text)
}
