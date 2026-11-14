package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/i18n"
	"leo-bot/internal/prompts"
)

func TestCleanDailyWisdom(t *testing.T) {
	cases := []struct {
		name, in, want string
	}{
		{"plain", "Дай телу проснуться. Сделай первый шаг.", "Дай телу проснуться. Сделай первый шаг."},
		{"paren comparison", "Утро начинается с дыхания (в отличие от вчерашнего послания, сегодня про тело). Шаг за шагом.",
			"Утро начинается с дыхания. Шаг за шагом."},
		{"trailing paren note", "Breathe in, grip the bar.\n\n(This message avoids repeating past days and stays within 4 sentences.)",
			"Breathe in, grip the bar."},
		{"brackets", "Встань [тема: выносливость] и иди.", "Встань и иди."},
		{"nested paren", "Шаг (раз (два)) вперёд.", "Шаг вперёд."},
		{"think block", "<think>The user wants a message about endurance. I should compare with yesterday.</think>\nStand up. Move.", "Stand up. Move."},
		{"think unclosed close", "Let me think about the theme...\n</think>\nStand up.", "Stand up."},
		{"think unopened", "Stand up.\n<think>reasoning that never ends", "Stand up."},
		{"meta lines", "Мудрость дня: Пей воду и иди.\nПримечание: без эмодзи, как просили.\nTheme: endurance", "Пей воду и иди."},
		{"prompt leak", "Сделай сегодня одно дело до конца.\nЯ написал без Markdown и хэштегов, 3–5 предложений.", "Сделай сегодня одно дело до конца."},
		{"quotes and bold", "«**Тихо** начни день.»", "Тихо начни день."},
		{"heading", "### Wisdom\nHold the line.", "Hold the line."},
		{"only reasoning", "<think>only thoughts</think>", ""},
		{"only paren", "(Сравнение с прошлыми днями)", ""},
		{"paragraphs kept", "Первый абзац.\n\n\n\nВторой абзац.", "Первый абзац.\n\nВторой абзац."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := CleanDailyWisdom(c.in); got != c.want {
				t.Errorf("CleanDailyWisdom(%q) = %q, want %q", c.in, got, c.want)
			}
		})
	}
}

func TestDailyWisdomPromptsHaveNoComparisonWording(t *testing.T) {
	b := prompts.DefaultBundle()
	for _, p := range []string{b.DailyWisdomTraining, b.DailyWisdomVariation1, b.DailyWisdomVariation2, b.DailyWisdomUserTemplate} {
		lower := strings.ToLower(p)
		for _, bad := range []string{"past days", "yesterday", "not just wisdom", "repeat"} {
			if strings.Contains(lower, bad) {
				t.Errorf("wisdom prompt contains %q: %s", bad, p)
			}
		}
	}
}

func wisdomServer(t *testing.T, content string) (*OpenRouterClient, *[]ChatRequest) {
	t.Helper()
	var seen []ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req ChatRequest
		_ = json.Unmarshal(raw, &req)
		seen = append(seen, req)
		out, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"message": ChatMessage{Role: "assistant", Content: content}}}})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	c := NewOpenRouterClient("key", "m", prompts.DefaultBundle(), nil, 5*time.Second)
	c.baseURL = srv.URL
	return c, &seen
}

func TestGenerateDailyWisdomLangCleansOutput(t *testing.T) {
	c, seen := wisdomServer(t, "<think>plan</think>Встань и иди (не как вчера).")
	got, err := c.GenerateDailyWisdomLang(i18n.RU)
	if err != nil || got != "Встань и иди." {
		t.Fatalf("got %q, %v", got, err)
	}
	if len(*seen) != 1 || !strings.Contains((*seen)[0].Messages[0].Content, dailyWisdomOutputRule) {
		t.Fatalf("system prompt lacks output rule")
	}
}

func TestGenerateDailyWisdomLangEmptyAfterCleanup(t *testing.T) {
	c, _ := wisdomServer(t, "<think>only reasoning</think>")
	if got, err := c.GenerateDailyWisdom(); err == nil {
		t.Fatalf("expected error, got %q", got)
	}
}
