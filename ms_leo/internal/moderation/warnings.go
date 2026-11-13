package moderation

import "fmt"

// UserWarnings — системные тексты при PRE-блоке (не LLM). UX Lead может заменить.
var UserWarnings = map[Reason]string{
	ReasonProfanity:   "Not published — your text contains words we can't allow. Please rewrite without insults or profanity.",
	ReasonLink:        "Not published — links in the Pack feed aren't allowed yet.",
	ReasonCriticalRU:  "Not published — this kind of text can't be posted.",
	ReasonRateLimited: "Wait a moment before your next message.",
	ReasonMuted:       "Posting in the Pack is temporarily limited. If this is a mistake, contact support.",
	ReasonLeoDaily:    "Daily limit reached — 20 messages to Leo today. You can continue tomorrow.",
	ReasonSpam:        "Not published — too many repeated words. Try writing more clearly.",
}

func warningFor(reason Reason, maxRunes int) string {
	if reason == ReasonTooLong {
		return fmt.Sprintf("Too long — shorten to %d characters.", maxRunes)
	}
	if msg, ok := UserWarnings[reason]; ok {
		return msg
	}
	return "Not published — your text didn't pass moderation."
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
