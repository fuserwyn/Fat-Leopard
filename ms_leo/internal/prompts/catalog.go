package prompts

// Slot — один обучающий текст Леопарда. Файл в data/ — встроенный оригинал;
// в админке его можно заменить текстом или вложением.
type Slot struct {
	Key      string
	File     string
	Title    string
	About    string
	embedded string
}

// Catalog — все боевые промпты из ms_leo/internal/prompts/data.
func Catalog() []Slot {
	return []Slot{
		{Key: "answer_user_question", File: "answer_user_question.txt", Title: "Характер Лео", About: "Как отвечает в чате и в «Тест Лео»", embedded: embeddedAnswerUserQuestion},
		{Key: "training_evaluation", File: "training_evaluation.txt", Title: "Оценка тренировки", About: "Комментарий после отчёта", embedded: embeddedTrainingEvaluation},
		{Key: "training_chat_suffix", File: "training_chat_suffix.txt", Title: "Суффикс чата", About: "Добавка к ответу в чате", embedded: embeddedTrainingChatSuffix},
		{Key: "daily_summary", File: "daily_summary.txt", Title: "Ежедневная сводка", About: "Сводка по тренировкам за день", embedded: embeddedDailySummary},
		{Key: "monthly_summary", File: "monthly_summary.txt", Title: "Месячная сводка", About: "Сводка за месяц", embedded: embeddedMonthlySummary},
		{Key: "daily_wisdom_training", File: "daily_wisdom_training.txt", Title: "Мудрость дня", About: "Текст мудрости про тренировки", embedded: embeddedDailyWisdomTraining},
		{Key: "daily_wisdom_lang_rule", File: "daily_wisdom_lang_rule.txt", Title: "Язык мудрости", About: "На каком языке писать мудрость", embedded: embeddedDailyWisdomLangRule},
		{Key: "daily_wisdom_user_template", File: "daily_wisdom_user_template.txt", Title: "Шаблон мудрости", About: "User-сообщение для мудрости дня", embedded: embeddedDailyWisdomUserTemplate},
		{Key: "daily_wisdom_variation1", File: "daily_wisdom_variation1.txt", Title: "Мудрость вариация 1", About: "Альтернативный вариант мудрости", embedded: embeddedDailyWisdomVariation1},
		{Key: "daily_wisdom_variation2", File: "daily_wisdom_variation2.txt", Title: "Мудрость вариация 2", About: "Альтернативный вариант мудрости", embedded: embeddedDailyWisdomVariation2},
		{Key: "warning_timer_question", File: "warning_timer_question.txt", Title: "Предупреждение таймера", About: "День 5–7 до кика за неактивность", embedded: embeddedWarningTimerQuestion},
		{Key: "critical_timer_question", File: "critical_timer_question.txt", Title: "Критический таймер", About: "Последний день перед киком", embedded: embeddedCriticalTimerQuestion},
		{Key: "achievement_milestone", File: "achievement_milestone.txt", Title: "Ачивка-веха", About: "Текст к milestone-ачивке", embedded: embeddedAchievementMilestone},
		{Key: "pack_feed_removed", File: "pack_feed_removed.txt", Title: "Участник ушёл", About: "Карточка в ленте, когда человека уже не видно", embedded: embeddedPackFeedParticipantRemoved},
		{Key: "release_notes", File: "release_notes.txt", Title: "Release Notes", About: "Список пользовательских фич за две недели", embedded: embeddedReleaseNotes},
	}
}

func (s Slot) Embedded() string {
	return s.embedded
}

func SlotByKey(key string) (Slot, bool) {
	for _, s := range Catalog() {
		if s.Key == key {
			return s, true
		}
	}
	return Slot{}, false
}

func EmbeddedByKey(key string) string {
	s, ok := SlotByKey(key)
	if !ok {
		return ""
	}
	return s.embedded
}

func ApplyOverrides(base Bundle, overrides map[string]string) Bundle {
	out := base
	for key, body := range overrides {
		if body == "" {
			continue
		}
		out = setBundleKey(out, key, body)
	}
	return out
}

func setBundleKey(b Bundle, key, body string) Bundle {
	switch key {
	case "daily_summary":
		b.DailySummary = body
	case "monthly_summary":
		b.MonthlySummary = body
	case "answer_user_question":
		b.AnswerUserQuestion = body
	case "daily_wisdom_training":
		b.DailyWisdomTraining = body
	case "daily_wisdom_lang_rule":
		b.DailyWisdomLangRule = body
	case "daily_wisdom_user_template":
		b.DailyWisdomUserTemplate = body
	case "daily_wisdom_variation1":
		b.DailyWisdomVariation1 = body
	case "daily_wisdom_variation2":
		b.DailyWisdomVariation2 = body
	case "training_chat_suffix":
		b.TrainingChatSuffix = body
	case "critical_timer_question":
		b.CriticalTimerQuestion = body
	case "warning_timer_question":
		b.WarningTimerQuestion = body
	case "achievement_milestone":
		b.AchievementMilestone = body
	case "training_evaluation":
		b.TrainingEvaluation = body
	case "pack_feed_removed":
		b.PackFeedParticipantRemoved = body
	case "release_notes":
		b.ReleaseNotes = body
	}
	return b
}
