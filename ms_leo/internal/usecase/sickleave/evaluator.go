package sickleave

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
)

type AIClient interface {
	AnswerUserQuestion(question string, userContext string) (string, error)
}

type Evaluator struct {
	ai     AIClient
	logger logger.Logger
}

var (
	positiveKeywords = []string{
		"болен", "болею", "болит", "заболел", "заболела", "забол", "заболева", "простыл", "простуд", "температур", "кашля", "кашель", "грипп",
		"орви", "ангин", "плохо", "лежу", "честно", "правда", "шанс", "выздоров", "выздоравли", "таблет", "врач", "болезн", "недомог", "жар",
		"сон", "боляч", "мигрен", "лихорад", "fever", "flu", "cold", "ill", "sick",
	}
	supportKeywords = []string{
		"дай шанс", "прошу", "пожалуйста", "исправлюсь", "буду тренироваться", "честно-честно", "умоляю", "пожал", "верь", "поверь", "обещаю",
	}
	negativeKeywords = []string{
		"делами", "работаю", "работа", "работе", "работ", "work", "workout", "воркаут", "лень", "просто не", "не хочу", "другие дела",
		"прогул", "хитр", "обман", "схитрить", "занят", "занята",
	}
)

func NewEvaluator(ai AIClient, log logger.Logger) *Evaluator {
	return &Evaluator{
		ai:     ai,
		logger: log,
	}
}

func (e *Evaluator) Evaluate(text string, messageLog *domain.MessageLog) bool {
	clean := strings.TrimSpace(strings.ToLower(text))
	clean = strings.ReplaceAll(clean, "#sick_leave", "")
	clean = strings.ReplaceAll(clean, "#sickleave", "")
	clean = strings.ReplaceAll(clean, "#healthy", "")
	clean = strings.ReplaceAll(clean, "#здоров", "")

	heuristicsApprove, hasNegative := evaluateHeuristics(clean)
	if heuristicsApprove {
		return true
	}
	if hasNegative {
		return false
	}
	if e.ai == nil || clean == "" {
		return false
	}

	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("Оцени убедительность больничного запроса.\n")
	if messageLog != nil {
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", messageLog.Username))
		ctxBuilder.WriteString(fmt.Sprintf("StreakDays: %d\n", messageLog.StreakDays))
		ctxBuilder.WriteString(fmt.Sprintf("CalorieStreakDays: %d\n", messageLog.CalorieStreakDays))
		ctxBuilder.WriteString(fmt.Sprintf("HasSickLeave: %t\n", messageLog.HasSickLeave))
		ctxBuilder.WriteString(fmt.Sprintf("HasHealthy: %t\n", messageLog.HasHealthy))
	}
	ctxBuilder.WriteString(fmt.Sprintf("Текст запроса: \"%s\"\n", clean))
	ctxBuilder.WriteString("Эвристика не нашла явных признаков ни болезни, ни обмана.\n")

	question := "Если сообщение описывает реальную болезнь, ответь строго словом APPROVE. " +
		"Если это похоже на отговорку (работа, дела, лень и т.п.), ответь строго словом REJECT. " +
		"Никаких других слов или пояснений."

	answer, err := e.ai.AnswerUserQuestion(question, ctxBuilder.String())
	if err != nil {
		e.logger.Errorf("AI sick leave evaluation failed: %v", err)
		return false
	}

	normalized := strings.ToUpper(strings.TrimSpace(answer))
	switch {
	case strings.Contains(normalized, "APPROVE"):
		return true
	case strings.Contains(normalized, "REJECT"):
		return false
	default:
		return false
	}
}

func evaluateHeuristics(text string) (approved bool, hasNegative bool) {
	if text == "" {
		return false, false
	}

	for _, neg := range negativeKeywords {
		if strings.Contains(text, neg) {
			return false, true
		}
	}

	score := 0
	for _, pos := range positiveKeywords {
		if strings.Contains(text, pos) {
			score++
		}
	}
	if strings.Contains(text, "боле") {
		score++
	}
	if strings.Contains(text, "забол") {
		score++
	}
	if strings.Contains(text, "простуд") {
		score++
	}
	if strings.Contains(text, "температ") {
		score++
	}
	if strings.Contains(text, "кашл") {
		score++
	}
	if strings.Contains(text, "плохое самочувствие") {
		score++
	}
	for _, sup := range supportKeywords {
		if strings.Contains(text, sup) {
			score++
		}
	}
	return score >= 1, false
}
