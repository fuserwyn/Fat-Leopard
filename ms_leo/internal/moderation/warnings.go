package moderation

import "fmt"

// UserWarnings — системные тексты при PRE-блоке (не LLM). UX Lead может заменить.
var UserWarnings = map[Reason]string{
	ReasonProfanity:   "Не опубликовал — в тексте есть недопустимые слова. Перепиши без оскорблений и мата.",
	ReasonLink:        "Не опубликовал — ссылки в Стае пока запрещены.",
	ReasonCriticalRU:  "Не опубликовал — такой текст нельзя публиковать.",
	ReasonRateLimited: "Подожди немного перед следующим сообщением.",
	ReasonMuted:       "Публикация в Стае временно ограничена. Если это ошибка — напиши в поддержку.",
	ReasonLeoDaily:    "Достигнут лимит — 20 сообщений Лео в день. Завтра можно продолжить.",
	ReasonSpam:        "Не опубликовал — слишком много повторяющихся слов. Перепиши понятнее.",
}

func warningFor(reason Reason, maxRunes int) string {
	if reason == ReasonTooLong {
		return fmt.Sprintf("Слишком длинно — сократи до %d символов.", maxRunes)
	}
	if msg, ok := UserWarnings[reason]; ok {
		return msg
	}
	return "Не опубликовал — текст не прошёл проверку."
}

func apiCodeFor(reason Reason) string {
	switch reason {
	case ReasonProfanity:
		return "moderation_profanity"
	case ReasonLink:
		return "moderation_link"
	case ReasonCriticalRU:
		return "moderation_critical"
	case ReasonTooLong:
		return "moderation_too_long"
	case ReasonRateLimited:
		return "moderation_rate_limited"
	case ReasonLeoDaily:
		return "leo_daily_limited"
	case ReasonMuted:
		return "user_muted"
	case ReasonSpam:
		return "moderation_spam_repetition"
	default:
		return "moderation_blocked"
	}
}
