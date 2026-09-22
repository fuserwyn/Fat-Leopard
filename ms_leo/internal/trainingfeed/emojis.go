package trainingfeed

import (
	"strings"

	"leo-bot/internal/game/leopardmoney"
)

// trainingCategoryDisplayOrder — порядок видов спорта в UI (как в мини-аппе workoutCategories).
var trainingCategoryDisplayOrder = []string{
	"run", "walk", "bike", "swim", "yoga", "rowing", "workout", "crossfit", "stretch", "dance",
	"hiit", "cardio", "kettlebell", "strength", "jump_rope", "pole", "rollerblade",
	"basketball", "football", "volleyball", "tennis", "padel", "gymnastics", "morning_exercise", "other",
}

// TrainingFeedApprovingEmojis — одобряющие реакции без привязки к виду спорта.
// Без злости/гнева, без «нейтральных» (👍, 👀, ⭐) и без шока/усталости.
var TrainingFeedApprovingEmojis = []string{
	"🔥", "💪", "👏", "❤️", "🎉", "🙌", "✨", "🤝", "🏆", "🥳", "🤩", "🤗", "👌", "🫶", "🧡", "💜", "🤘",
}

// TrainingCategoryReactionEmoji — эмодзи вида спорта (совпадает с мини-аппом workoutCategories).
var TrainingCategoryReactionEmoji = map[string]string{
	"run":         "🏃",
	"walk":        "🚶",
	"bike":        "🚴",
	"swim":        "🏊",
	"yoga":        "🧘",
	"rowing":      "🚣",
	"workout":     "🔥",
	"crossfit":    "🎯",
	"stretch":     "🧎",
	"dance":       "💃",
	"hiit":        "⚡",
	"cardio":      "💓",
	"kettlebell":  "🏋️",
	"strength":    "🏋️",
	"jump_rope":   "🪢",
	"pole":        "🤸",
	"rollerblade": "🛼",
	"basketball":  "🏀",
	"football":    "⚽",
	"volleyball":  "🏐",
	"tennis":      "🎾",
	"padel":       "🏏",
	"gymnastics":       "🙆",
	"morning_exercise": "☀️",
	"other":            "✨",
}

// TrainingFeedAllowedEmojis — полный список для UI: одобряющие + все виды спорта (порядок отображения).
var TrainingFeedAllowedEmojis = buildTrainingFeedAllowedEmojis()

func buildTrainingFeedAllowedEmojis() []string {
	seen := make(map[string]struct{}, len(TrainingFeedApprovingEmojis)+len(TrainingCategoryReactionEmoji))
	out := make([]string, 0, len(seen))
	add := func(e string) {
		if e == "" {
			return
		}
		if _, ok := seen[e]; ok {
			return
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	for _, e := range TrainingFeedApprovingEmojis {
		add(e)
	}
	for _, id := range trainingCategoryDisplayOrder {
		add(TrainingCategoryReactionEmoji[id])
	}
	return out
}

// CategoryIDsFromReport — виды спорта из текста отчёта; при нераспознанном формате — other.
func CategoryIDsFromReport(reportText string) []string {
	_, _, cats, ok := leopardmoney.ParseTrainingDoneReportCategories(reportText)
	if !ok || len(cats) == 0 {
		return []string{"other"}
	}
	return cats
}

// AllowedEmojisForCategories — одобряющие + эмодзи видов из отчёта (без чужих видов спорта).
func AllowedEmojisForCategories(categoryIDs []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(TrainingFeedApprovingEmojis)+len(categoryIDs))
	add := func(e string) {
		if e == "" {
			return
		}
		if _, ok := seen[e]; ok {
			return
		}
		seen[e] = struct{}{}
		out = append(out, e)
	}
	for _, e := range TrainingFeedApprovingEmojis {
		add(e)
	}
	for _, id := range categoryIDs {
		add(TrainingCategoryReactionEmoji[strings.TrimSpace(strings.ToLower(id))])
	}
	return out
}

// AllowedEmojisForReport — допустимые реакции на конкретный отчёт о тренировке.
func AllowedEmojisForReport(reportText string) []string {
	return AllowedEmojisForCategories(CategoryIDsFromReport(reportText))
}

func isEmojiInList(emoji string, list []string) bool {
	emoji = strings.TrimSpace(emoji)
	for _, e := range list {
		if emoji == e {
			return true
		}
	}
	return false
}

// IsEmojiAllowedForCategories — эмодзи из одобряющих или из видов отчёта.
func IsEmojiAllowedForCategories(emoji string, categoryIDs []string) bool {
	return isEmojiInList(emoji, AllowedEmojisForCategories(categoryIDs))
}

// IsEmojiAllowedForReport — проверка реакции на отчёт о тренировке.
func IsEmojiAllowedForReport(emoji, reportText string) bool {
	return IsEmojiAllowedForCategories(emoji, CategoryIDsFromReport(reportText))
}

// LeoReactionEmoji — стабильная одобряющая реакция Лео для поста: из пула отчёта.
func LeoReactionEmoji(userMessageID int64, categoryIDs []string) string {
	list := AllowedEmojisForCategories(categoryIDs)
	n := len(list)
	if n == 0 {
		return "💪"
	}
	if userMessageID < 0 {
		userMessageID = -userMessageID
	}
	idx := int((userMessageID*7919 + 104729) % int64(n))
	return list[idx]
}

// LeoReactionEmojiForReport — реакция Лео по тексту отчёта.
func LeoReactionEmojiForReport(userMessageID int64, reportText string) string {
	return LeoReactionEmoji(userMessageID, CategoryIDsFromReport(reportText))
}

// RemapReactionEmoji — если эмодзи недопустима для отчёта, подбирает допустимую (стабильно по id поста).
func RemapReactionEmoji(emoji string, userMessageID int64, reportText string) string {
	cats := CategoryIDsFromReport(reportText)
	if IsEmojiAllowedForCategories(emoji, cats) {
		return strings.TrimSpace(emoji)
	}
	return LeoReactionEmoji(userMessageID, cats)
}
