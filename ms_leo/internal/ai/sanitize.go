package ai

import (
	"regexp"
	"strings"
)

// Строка целиком в виде *реплика* / *Рычит* (и опц. эмодзи) — сценарная ремарка, убираем.
var reStarOnlyLine = regexp.MustCompile(`(?m)^[ \t]*\*[^*]{1,200}\*(?:[ \t]*[🐆🦁🐯])?[ \t]*\r?$`)

// *Рычит* / *р-р* / *мр-я* (внутри фразы)
var reStarInlineGimmicks = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(?:^|[\s,.!?:;—–-]|\(|\)|«|»|])\*рыч(ит|а|у|н(ул|ёшь|ит|ито)?|ь|о)\*(\s*[🐆🦁🐯]?\s*)?`),
	regexp.MustCompile(`(?i)(?:^|[\s,.!?:;—–-]|\(|\)|«|»|])\*р+[-\s/]?р+[^*]{0,8}\*(\s*[🐆🦁🐯]?\s*)?`),
	regexp.MustCompile(`(?i)(?:^|[\s,.!?:;—–-]|\(|\)|«|»|])\*мр+я+[^*]{0,4}\*(\s*[🐆🦁🐯]?\s*)?`),
}

func stripAsteriskStageRemarks(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = reStarOnlyLine.ReplaceAllString(s, "\n")
	for _, p := range reStarInlineGimmicks {
		s = p.ReplaceAllString(s, " ")
	}
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(strings.Join(strings.Fields(line), " "))
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

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

	clean = stripAsteriskStageRemarks(clean)
	if strings.TrimSpace(clean) == "" {
		return ""
	}

	return clean
}
