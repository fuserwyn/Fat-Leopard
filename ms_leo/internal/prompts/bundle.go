package prompts

import (
	_ "embed"
	"os"
	"strings"
)

// Bundle — промпты персонажа Fat Leopard для OpenRouter и бота.
type Bundle struct {
	DailySummary            string
	MonthlySummary          string
	AnswerUserQuestion      string
	DailyWisdomWriting      string
	DailyWisdomTraining     string
	DailyWisdomLangRule     string
	DailyWisdomUserTemplate string
	WritingChatSuffix       string
	TrainingChatSuffix      string
	CriticalTimerQuestion   string
	WarningTimerQuestion    string // предупреждение за 6 дней без отчёта
}

//go:embed data/daily_summary.txt
var embeddedDailySummary string

//go:embed data/monthly_summary.txt
var embeddedMonthlySummary string

//go:embed data/answer_user_question.txt
var embeddedAnswerUserQuestion string

//go:embed data/daily_wisdom_writing.txt
var embeddedDailyWisdomWriting string

//go:embed data/daily_wisdom_training.txt
var embeddedDailyWisdomTraining string

//go:embed data/daily_wisdom_lang_rule.txt
var embeddedDailyWisdomLangRule string

//go:embed data/daily_wisdom_user_template.txt
var embeddedDailyWisdomUserTemplate string

//go:embed data/writing_chat_suffix.txt
var embeddedWritingChatSuffix string

//go:embed data/training_chat_suffix.txt
var embeddedTrainingChatSuffix string

//go:embed data/critical_timer_question.txt
var embeddedCriticalTimerQuestion string

//go:embed data/warning_timer_question.txt
var embeddedWarningTimerQuestion string

// DefaultBundle возвращает встроенные тексты из каталога data/.
func DefaultBundle() Bundle {
	return Bundle{
		DailySummary:            embeddedDailySummary,
		MonthlySummary:          embeddedMonthlySummary,
		AnswerUserQuestion:      embeddedAnswerUserQuestion,
		DailyWisdomWriting:      embeddedDailyWisdomWriting,
		DailyWisdomTraining:     embeddedDailyWisdomTraining,
		DailyWisdomLangRule:     embeddedDailyWisdomLangRule,
		DailyWisdomUserTemplate: embeddedDailyWisdomUserTemplate,
		WritingChatSuffix:       embeddedWritingChatSuffix,
		TrainingChatSuffix:      embeddedTrainingChatSuffix,
		CriticalTimerQuestion:   embeddedCriticalTimerQuestion,
		WarningTimerQuestion:    embeddedWarningTimerQuestion,
	}
}

// BundleFromEnv строит Bundle: сначала встроенные значения, затем непустые переменные окружения.
func BundleFromEnv() Bundle {
	b := DefaultBundle()
	if v := envPrompt("PROMPT_DAILY_SUMMARY"); v != "" {
		b.DailySummary = v
	}
	if v := envPrompt("PROMPT_MONTHLY_SUMMARY"); v != "" {
		b.MonthlySummary = v
	}
	if v := envPrompt("PROMPT_ANSWER_USER_QUESTION"); v != "" {
		b.AnswerUserQuestion = v
	}
	if v := envPrompt("PROMPT_DAILY_WISDOM_WRITING"); v != "" {
		b.DailyWisdomWriting = v
	}
	if v := envPrompt("PROMPT_DAILY_WISDOM_TRAINING"); v != "" {
		b.DailyWisdomTraining = v
	}
	if v := envPrompt("PROMPT_DAILY_WISDOM_LANG_RULE"); v != "" {
		b.DailyWisdomLangRule = v
	}
	if v := envPrompt("PROMPT_DAILY_WISDOM_USER_TEMPLATE"); v != "" {
		b.DailyWisdomUserTemplate = v
	}
	if v := envPrompt("PROMPT_WRITING_CHAT_SUFFIX"); v != "" {
		b.WritingChatSuffix = v
	}
	if v := envPrompt("PROMPT_TRAINING_CHAT_SUFFIX"); v != "" {
		b.TrainingChatSuffix = v
	}
	if v := envPrompt("PROMPT_CRITICAL_TIMER_QUESTION"); v != "" {
		b.CriticalTimerQuestion = v
	}
	if v := envPrompt("PROMPT_WARNING_TIMER_QUESTION"); v != "" {
		b.WarningTimerQuestion = v
	}
	return b
}

func envPrompt(key string) string {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return ""
	}
	return unescapeEnvEscapes(raw)
}

func unescapeEnvEscapes(s string) string {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\t`, "\t")
	s = strings.ReplaceAll(s, `\r`, "")
	return s
}
