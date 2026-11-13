package prompts

// Slot — one of Leo's training texts. The file in data/ is the built-in original;
// in admin it can be replaced with custom text or an attachment.
type Slot struct {
	Key      string
	File     string
	Title    string
	About    string
	embedded string
}

// Catalog — all production prompts from ms_leo/internal/prompts/data.
func Catalog() []Slot {
	return []Slot{
		{Key: "answer_user_question", File: "answer_user_question.txt", Title: "Leo's character", About: "How Leo replies in chat and in \"Test Leo\"", embedded: embeddedAnswerUserQuestion},
		{Key: "training_evaluation", File: "training_evaluation.txt", Title: "Workout evaluation", About: "Comment after a report", embedded: embeddedTrainingEvaluation},
		{Key: "training_chat_suffix", File: "training_chat_suffix.txt", Title: "Chat suffix", About: "Add-on to chat replies", embedded: embeddedTrainingChatSuffix},
		{Key: "daily_summary", File: "daily_summary.txt", Title: "Daily summary", About: "Summary of workouts for the day", embedded: embeddedDailySummary},
		{Key: "monthly_summary", File: "monthly_summary.txt", Title: "Monthly summary", About: "Summary for the month", embedded: embeddedMonthlySummary},
		{Key: "daily_wisdom_training", File: "daily_wisdom_training.txt", Title: "Wisdom of the day", About: "Wisdom text about training", embedded: embeddedDailyWisdomTraining},
		{Key: "daily_wisdom_lang_rule", File: "daily_wisdom_lang_rule.txt", Title: "Wisdom language", About: "Which language to write wisdom in", embedded: embeddedDailyWisdomLangRule},
		{Key: "daily_wisdom_user_template", File: "daily_wisdom_user_template.txt", Title: "Wisdom template", About: "User message for wisdom of the day", embedded: embeddedDailyWisdomUserTemplate},
		{Key: "daily_wisdom_variation1", File: "daily_wisdom_variation1.txt", Title: "Wisdom variation 1", About: "Alternate wisdom variant", embedded: embeddedDailyWisdomVariation1},
		{Key: "daily_wisdom_variation2", File: "daily_wisdom_variation2.txt", Title: "Wisdom variation 2", About: "Alternate wisdom variant", embedded: embeddedDailyWisdomVariation2},
		{Key: "warning_timer_question", File: "warning_timer_question.txt", Title: "Timer warning", About: "Days 5–7 before kick for inactivity", embedded: embeddedWarningTimerQuestion},
		{Key: "critical_timer_question", File: "critical_timer_question.txt", Title: "Critical timer", About: "Last day before removal", embedded: embeddedCriticalTimerQuestion},
		{Key: "achievement_milestone", File: "achievement_milestone.txt", Title: "Milestone achievement", About: "Text for milestone achievements", embedded: embeddedAchievementMilestone},
		{Key: "release_notes", File: "release_notes.txt", Title: "Release Notes", About: "What's new in the mini-app over two weeks", embedded: embeddedReleaseNotes},
		{Key: "contact_joined_pack", File: "contact_joined_pack.txt", Title: "Contact joined pack", About: "DM: user's contact joined the pack", embedded: embeddedContactJoinedPack},
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
	case "release_notes":
		b.ReleaseNotes = body
	case "contact_joined_pack":
		b.ContactJoinedPack = body
	}
	return b
}
