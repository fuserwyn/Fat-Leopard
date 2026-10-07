package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"leo-tracker/internal/config"
)

func TestUnwrapScheduledBody(t *testing.T) {
	direct := map[string]any{"when": "сейчас", "prompt": "починить клавиатуру"}
	if got := unwrapScheduledBody(direct); got["prompt"] != "починить клавиатуру" {
		t.Fatalf("direct: %#v", got)
	}

	legacy := map[string]any{
		"session": "x.y",
		"op":      "create",
		"task_id": 7,
		"payload": map[string]any{
			"when":   "сейчас",
			"prompt": "починить клавиатуру",
		},
	}
	got := unwrapScheduledBody(legacy)
	if got["prompt"] != "починить клавиатуру" || got["when"] != "сейчас" {
		t.Fatalf("legacy: %#v", got)
	}
	if _, ok := got["session"]; ok {
		t.Fatal("session must stay in the envelope, not the job")
	}
}

func TestAskReturnsClaudeText(t *testing.T) {
	prev := askClaude
	defer func() { askClaude = prev }()
	s := &Server{}

	askClaude = func(_ config.Config, system, prompt, model string) (string, error) {
		return system + "|" + prompt + "|" + model, nil
	}
	rec := httptest.NewRecorder()
	s.ask(rec, httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"system":"Лео","prompt":"спринт","model":"claude-opus-5-5"}`)))
	var out struct {
		OK   bool   `json:"ok"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || !out.OK || out.Text != "Лео|спринт|claude-opus-5-5" {
		t.Fatalf("ответ: %d %s", rec.Code, rec.Body.String())
	}

	askClaude = func(config.Config, string, string, string) (string, error) {
		return "", fmt.Errorf("лимит подписки")
	}
	rec = httptest.NewRecorder()
	s.ask(rec, httptest.NewRequest(http.MethodPost, "/api/ask", strings.NewReader(`{"prompt":"спринт"}`)))
	if rec.Code != http.StatusBadGateway || !strings.Contains(rec.Body.String(), "лимит подписки") {
		t.Fatalf("ошибка Claude: %d %s", rec.Code, rec.Body.String())
	}
}
