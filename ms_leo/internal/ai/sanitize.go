package ai

import "strings"

// SanitizeTextForUser удаляет служебные артефакты/утечки промптов из AI-текста перед отправкой пользователю.
func SanitizeTextForUser(text string) string {
	clean := strings.TrimSpace(strings.ReplaceAll(text, "**", ""))
	if clean == "" {
		return ""
	}

	lower := strings.ToLower(clean)
	blockedStarts := []string{
		"сделай ",
		"напиши ",
		"дай ",
		"отвечай ",
		"вопрос пользователя:",
	}
	for _, s := range blockedStarts {
		if strings.HasPrefix(lower, s) {
			return ""
		}
	}

	blockedContains := []string{
		"без markdown",
		"1–2 предложения",
		"1-2 предложения",
		"приписку к предупреждению",
		"критическому предупреждению",
		"не повторяй цифры и факты",
		"если не отправит #training_done",
	}
	for _, s := range blockedContains {
		if strings.Contains(lower, s) {
			return ""
		}
	}

	return clean
}
