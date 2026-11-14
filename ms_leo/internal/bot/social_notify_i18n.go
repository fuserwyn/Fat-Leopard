package bot

import (
	"strconv"
	"strings"

	"leo-bot/internal/i18n"
)

// Тексты социальных DM (лайки, комменты, упоминания, тренировки друзей) на языке получателя.

func socialRU(lang i18n.Lang) bool { return i18n.Or(lang, i18n.EN) == i18n.RU }

// ruPastVerb — «лайкнул»/«лайкнула»/«лайкнул(а)» по полу автора действия.
func ruPastVerb(gender, stem string) string {
	switch strings.TrimSpace(strings.ToLower(gender)) {
	case "m":
		return stem
	case "f":
		return stem + "а"
	default:
		return stem + "(а)"
	}
}

func socialPackMember(lang i18n.Lang) string {
	if socialRU(lang) {
		return "Участник стаи"
	}
	return "Pack member"
}

func socialPhotoPreview(lang i18n.Lang) string {
	if socialRU(lang) {
		return "📷 Фото"
	}
	return "📷 Photo"
}

// socialNameOr — имя или «Участник стаи» на языке получателя.
func socialNameOr(name string, lang i18n.Lang) string {
	if n := strings.TrimSpace(name); n != "" && n != "Pack member" {
		return n
	}
	return socialPackMember(lang)
}

func socialPreviewOr(preview string, lang i18n.Lang) string {
	if strings.TrimSpace(preview) == "" || preview == "📷 Photo" {
		return socialPhotoPreview(lang)
	}
	return preview
}

func openPackFooter(lang i18n.Lang) string {
	if socialRU(lang) {
		return "Открой мини-апп → вкладка «Стая»."
	}
	return "Open the mini app → Pack tab."
}

func openPackChatFooter(lang i18n.Lang) string {
	if socialRU(lang) {
		return "Открой мини-апп → «Стая» → «Чат»."
	}
	return "Open the mini app → Pack → Chat."
}

func friendWorkoutText(lang i18n.Lang, name, gender string, streak int) string {
	if socialRU(lang) {
		return "🦁 " + name + " " + ruPastVerb(gender, "записал") + " тренировку! 🔥 Серия: " + strconv.Itoa(streak) + ".\n\n" + openPackFooter(lang)
	}
	return "🦁 " + name + " logged a workout! 🔥 Streak: " + strconv.Itoa(streak) + ".\n\n" + openPackFooter(lang)
}

// likeTargetKind: "post" | "comment".
func feedLikeText(lang i18n.Lang, badge, name, gender, targetKind string) string {
	if socialRU(lang) {
		what := "твой отчёт о тренировке"
		if targetKind == "comment" {
			what = "твой комментарий"
		}
		return badge + " " + name + " " + ruPastVerb(gender, "лайкнул") + " " + what + " в стае.\n\n" + openPackFooter(lang)
	}
	what := "your workout report"
	if targetKind == "comment" {
		what = "your comment"
	}
	return badge + " " + name + " " + likeVerbForGender(gender) + " " + what + " in the pack.\n\n" + openPackFooter(lang)
}

func threadReplyText(lang i18n.Lang, name, gender, preview string) string {
	if socialRU(lang) {
		return "↩️ " + name + " " + ruPastVerb(gender, "ответил") + " на твой комментарий в стае.\n\n«" + preview + "»\n\n" + openPackFooter(lang)
	}
	return "↩️ " + name + " replied to your comment in the pack.\n\n\"" + preview + "\"\n\n" + openPackFooter(lang)
}

func threadCommentText(lang i18n.Lang, name, gender, parentType, preview string) string {
	if socialRU(lang) {
		what := "твою тренировку"
		switch parentType {
		case "sick_leave", "healthy":
			what = "твой больничный"
		case userMessageTypeAdminPost:
			what = "пост админа"
		case userMessageTypeAdminPoll:
			what = "опрос админа"
		case userMessageTypeDailyWisdom:
			what = "мудрость дня"
		case userMessageTypePackRoar:
			what = "рык стаи"
		}
		return "💬 " + name + " " + ruPastVerb(gender, "прокомментировал") + " " + what + " в стае.\n\n«" + preview + "»\n\n" + openPackFooter(lang)
	}
	what := "your workout"
	switch parentType {
	case "sick_leave", "healthy":
		what = "your sick-leave update"
	case userMessageTypeAdminPost:
		what = "the admin post"
	case userMessageTypeAdminPoll:
		what = "the admin poll"
	case userMessageTypeDailyWisdom:
		what = "wisdom of the day"
	case userMessageTypePackRoar:
		what = "the pack roar"
	}
	return "💬 " + name + " commented on " + what + " in the pack.\n\n\"" + preview + "\"\n\n" + openPackFooter(lang)
}

func packMentionText(lang i18n.Lang, name, gender string, surface packMentionSurface, preview string) string {
	footer := openPackFooter(lang)
	if surface == packMentionPackGroupMessage {
		footer = openPackChatFooter(lang)
	}
	if socialRU(lang) {
		place := "в чате"
		switch surface {
		case packMentionFeedPost:
			place = "в посте"
		case packMentionFeedComment:
			place = "в комментарии"
		}
		return "📣 " + name + " " + ruPastVerb(gender, "упомянул") + " тебя " + place + " в стае.\n\n«" + preview + "»\n\n" + footer
	}
	return "📣 " + name + " mentioned you " + packMentionPlace(surface) + " in the pack.\n\n\"" + preview + "\"\n\n" + footer
}

func leoFeedReplyDMText(lang i18n.Lang, preview string) string {
	if socialRU(lang) {
		return "🦁 Лео ответил на твой комментарий в ленте.\n\n«" + preview + "»\n\nОткрой мини-апп → «Стая» → «Лента»."
	}
	return "🦁 Leo replied to your comment on the feed.\n\n«" + preview + "»\n\nOpen the mini app → Pack tab → Feed."
}
