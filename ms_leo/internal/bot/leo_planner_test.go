package bot

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"leo-bot/internal/ai"
	"leo-bot/internal/config"
)

func TestLeoPlannerPrompt(t *testing.T) {
	system, prompt := leoPlannerPrompt([]ai.ChatMessage{
		{Role: "system", Content: "Ты Лео."},
		{Role: "user", Content: " Придумай спринт "},
	})
	if system != "Ты Лео." || prompt != "Придумай спринт" {
		t.Fatalf("один вопрос: %q / %q", system, prompt)
	}
	_, prompt = leoPlannerPrompt([]ai.ChatMessage{
		{Role: "system", Content: "Ты Лео."},
		{Role: "user", Content: "Нужна задача про ленту"},
		{Role: "assistant", Content: "Какую именно?"},
		{Role: "user", Content: "Про фото"},
	})
	if prompt != "Админ: Нужна задача про ленту\n\nЛео: Какую именно?\n\nАдмин: Про фото" {
		t.Fatalf("переписка: %q", prompt)
	}
}

func TestTrackerAskClaude(t *testing.T) {
	var got map[string]string
	var auth string
	fail := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("X-Tracker-Secret")
		_ = json.NewDecoder(r.Body).Decode(&got)
		if r.URL.Path != "/api/ask" {
			http.NotFound(w, r)
			return
		}
		if fail {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"ok":false,"error":"лимит подписки"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"text":" {\"tasks\":[]} "}`))
	}))
	defer srv.Close()

	b := &Bot{config: &config.Config{BoardURL: srv.URL + "/", BoardSecret: "s3"}}
	text, err := b.trackerAskClaude("Ты Лео.", "Спринт", "claude-sonnet-5-5")
	if err != nil || text != `{"tasks":[]}` {
		t.Fatalf("ответ: %q %v", text, err)
	}
	if auth != "s3" || got["system"] != "Ты Лео." || got["prompt"] != "Спринт" || got["model"] != "claude-sonnet-5-5" {
		t.Fatalf("запрос: %v, секрет %q", got, auth)
	}

	fail = true
	if _, err := b.trackerAskClaude("", "Спринт", ""); err == nil || !strings.Contains(err.Error(), "лимит подписки") {
		t.Fatalf("ошибка трекера: %v", err)
	}
	if _, err := (&Bot{config: &config.Config{}}).trackerAskClaude("", "Спринт", ""); err == nil {
		t.Fatal("без настроек трекера — ошибка")
	}
}
