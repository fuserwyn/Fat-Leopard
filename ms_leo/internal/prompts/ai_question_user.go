package prompts

import (
	"fmt"
	"strings"

	"leo-bot/internal/moderation"
)

// AIQuestionUserPayload — structured user message for AnswerUserQuestion.
type AIQuestionUserPayload struct {
	InterlocutorName string
	InterlocutorID   int64
	UsersBlock       string
	RulesBlock       string
	FactsBlock       string
	ChatThread       string
	RouterHint       string
	Question         string
}

const emptyBlock = "— none —"

// GameRulesBlock — short rules for the Rules / policies section.
func GameRulesBlock() string {
	return strings.TrimSpace(`Fat Leopard / pack:
• Log workouts only via the mini app (“+” form), not hashtags in chat
• #sick_leave — pauses inactivity timer; #healthy — resume with the same deadline left
• Inactivity timer ~7 days; warnings before removal from chat
• Cups for reports per formula; streak — consecutive days with a workout; achievements at streak thresholds`)
}

// FormatAIQuestionUserMessage — user-role prompt with structured knowledge sections.
func FormatAIQuestionUserMessage(p AIQuestionUserPayload) string {
	name := strings.TrimSpace(p.InterlocutorName)
	if name == "" {
		name = "user"
	}
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}

	users := strings.TrimSpace(p.UsersBlock)
	if users == "" {
		users = emptyBlock
	}
	rules := strings.TrimSpace(p.RulesBlock)
	if rules == "" {
		rules = emptyBlock
	}
	facts := strings.TrimSpace(p.FactsBlock)
	if facts == "" {
		facts = emptyBlock
	}
	thread := strings.TrimSpace(p.ChatThread)
	if thread == "" {
		thread = "— no recent messages —"
	}

	var b strings.Builder
	b.WriteString("Context for this answer — ONLY the sections below. Do not invent facts outside them.\n\n")
	fmt.Fprintf(&b, "Current interlocutor: %s (id=%d)\n\n", name, p.InterlocutorID)
	b.WriteString("## Structured knowledge\n\n")
	b.WriteString("### Users / entities\n")
	b.WriteString(users)
	b.WriteString("\n\n### Rules / policies\n")
	b.WriteString(rules)
	b.WriteString("\n\n### Facts / documents\n")
	b.WriteString(facts)
	b.WriteString("\n\n## Recent chat (chronological, oldest → newest)\n")
	b.WriteString(thread)
	if h := strings.TrimSpace(p.RouterHint); h != "" {
		b.WriteString("\n\n## Router hint (optional)\n")
		b.WriteString(h)
	}
	b.WriteString("\n\n")
	fmt.Fprintf(&b, "%s (id=%d) asks:\n", name, p.InterlocutorID)
	b.WriteString(WrapUserQuestion(p.Question))
	return b.String()
}

// WrapUserQuestion — anti-injection wrapper for the user's question.
func WrapUserQuestion(question string) string {
	wrapped := moderation.WrapUserContent("user_question", question)
	if wrapped == "" {
		return emptyBlock
	}
	return wrapped
}

// FormatLegacyUserPrompt — wrapper for older calls with a flat context string.
func FormatLegacyUserPrompt(question, flatContext string) string {
	facts := strings.TrimSpace(flatContext)
	if facts == "" {
		facts = emptyBlock
	}
	return FormatAIQuestionUserMessage(AIQuestionUserPayload{
		InterlocutorName: "user",
		UsersBlock:       emptyBlock,
		RulesBlock:       emptyBlock,
		FactsBlock:       facts,
		ChatThread:       "— no recent messages —",
		Question:         question,
	})
}

// RouterHintForQuestion — lightweight model hint by question type (RU keywords kept for bilingual users).
func RouterHintForQuestion(question string) string {
	q := strings.ToLower(strings.TrimSpace(question))
	if q == "" {
		return ""
	}
	switch {
	case strings.Contains(q, "кубк"), strings.Contains(q, "стрик"), strings.Contains(q, "streak"),
		strings.Contains(q, "статистик"), strings.Contains(q, "сколько"), strings.Contains(q, "таймер"),
		strings.Contains(q, "больнич"), strings.Contains(q, "sick_leave"), strings.Contains(q, "healthy"):
		return "Factual question — answer from structured knowledge only; be brief and precise."
	case strings.Contains(q, "привет"), strings.Contains(q, "здаров"), q == "hi", q == "hello":
		return "Short greeting — 1–2 sentences, no stats or lectures about workouts."
	case strings.Contains(q, "придумай"), strings.Contains(q, "выдумай"), strings.Contains(q, "фантаз"):
		return "Creative mode: you may invent; a light fictional hint is fine if you want."
	default:
		if len([]rune(q)) < 36 && !strings.Contains(q, "?") {
			return "Short remark — answer on point, without extra DB context."
		}
	}
	return ""
}
