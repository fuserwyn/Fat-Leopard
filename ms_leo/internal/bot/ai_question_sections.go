package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/prompts"
)

// aiQuestionSections — блоки structured knowledge для промпта Лео.
type aiQuestionSections struct {
	users  strings.Builder
	rules  strings.Builder
	facts  strings.Builder
	thread strings.Builder
}

func (s *aiQuestionSections) initRules() {
	if s.rules.Len() > 0 {
		return
	}
	s.rules.WriteString(prompts.GameRulesBlock())
}

func formatUserEntityLine(u *domain.MessageLog, cups int, remaining string) string {
	if u == nil {
		return ""
	}
	name := strings.TrimSpace(u.Username)
	if name == "" {
		name = fmt.Sprintf("id=%d", u.UserID)
	}
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}
	status := "✅ active"
	switch {
	case u.HasSickLeave:
		status = "🏥 sick leave"
	case u.HasHealthy:
		status = "💚 after #healthy"
	}
	line := fmt.Sprintf("👤 %s (id=%d) | 🏆 cups: %d | 💪 streak: %d %s | %s",
		name, u.UserID, cups, u.StreakDays, daysWordForm(u.StreakDays), status)
	if u.LastTrainingDate != nil && strings.TrimSpace(*u.LastTrainingDate) != "" {
		line += fmt.Sprintf(" | 📅 last workout: %s", strings.TrimSpace(*u.LastTrainingDate))
	}
	if remaining != "" {
		line += fmt.Sprintf(" | ⏳ %s", remaining)
	}
	g := strings.TrimSpace(strings.ToLower(u.Gender))
	if g == "m" {
		line += " | gender: m"
	} else if g == "f" {
		line += " | gender: f"
	}
	return line
}

func formatTrainingFactLine(createdAt, username string, userID int64, messageType, text string) string {
	name := strings.TrimSpace(username)
	if name == "" {
		name = fmt.Sprintf("id=%d", userID)
	}
	if !strings.HasPrefix(name, "@") {
		name = "@" + name
	}
	tag := strings.TrimSpace(messageType)
	switch tag {
	case "training_done":
		tag = "workout"
	case "sick_leave":
		tag = "sick leave"
	case "healthy":
		tag = "recovery"
	case "ai_reply":
		tag = "Leo reply"
	default:
		if tag == "" || tag == "general" {
			tag = "text"
		}
	}
	t := strings.TrimSpace(text)
	if len(t) > 400 {
		t = t[:400] + "…"
	}
	return fmt.Sprintf("[%s] %s (id=%d) [%s]: %s", createdAt, name, userID, tag, t)
}
