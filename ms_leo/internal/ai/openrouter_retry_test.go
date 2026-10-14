package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"leo-bot/internal/prompts"
)

// fakeOpenRouter отвечает кодами из очереди codes; 200 — с текстом «ок».
func fakeOpenRouter(t *testing.T, codes ...int) (*OpenRouterClient, *[]ChatRequest) {
	t.Helper()
	var mu sync.Mutex
	var seen []ChatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var req ChatRequest
		_ = json.Unmarshal(raw, &req)
		mu.Lock()
		seen = append(seen, req)
		code := http.StatusOK
		if len(codes) > 0 {
			code, codes = codes[0], codes[1:]
		}
		mu.Unlock()
		if code != http.StatusOK {
			w.WriteHeader(code)
			_, _ = w.Write([]byte(`{"error":{"message":"deepseek/deepseek-chat is temporarily rate-limited upstream"}}`))
			return
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ок"}}]}`))
	}))
	t.Cleanup(srv.Close)
	prev := chatRetryDelays
	chatRetryDelays = []time.Duration{time.Millisecond, time.Millisecond}
	t.Cleanup(func() { chatRetryDelays = prev })
	c := NewOpenRouterClient("key", "deepseek/deepseek-chat", prompts.Bundle{}, nil, 5*time.Second)
	c.baseURL = srv.URL
	return c, &seen
}

// Перегрузка модели — повтор с паузой, ответ не теряется.
func TestChatRetriesWhenModelIsOverloaded(t *testing.T) {
	c, seen := fakeOpenRouter(t, 429, 503)
	got, err := c.Chat([]ChatMessage{{Role: "user", Content: "привет"}}, "")
	if err != nil || got != "ок" {
		t.Fatalf("ответ после двух перегрузок: %q %v", got, err)
	}
	if len(*seen) != 3 {
		t.Errorf("запросов: %d, ждали 3", len(*seen))
	}
}

// Перегрузка не прошла за все попытки — ошибка с кодом, а не зависание.
func TestChatGivesUpAfterRetries(t *testing.T) {
	c, seen := fakeOpenRouter(t, 429, 429, 429, 429)
	if _, err := c.Chat([]ChatMessage{{Role: "user", Content: "x"}}, ""); err == nil || !strings.Contains(err.Error(), "429") {
		t.Fatalf("ошибка после всех попыток: %v", err)
	}
	if len(*seen) != 3 {
		t.Errorf("запросов: %d, ждали 3 (первый и два повтора)", len(*seen))
	}
}

// Ошибка запроса (не перегрузка) не повторяется — повтор её не исправит.
func TestChatDoesNotRetryClientErrors(t *testing.T) {
	c, seen := fakeOpenRouter(t, 400)
	if _, err := c.Chat([]ChatMessage{{Role: "user", Content: "x"}}, ""); err == nil {
		t.Fatal("ждали ошибку")
	}
	if len(*seen) != 1 {
		t.Errorf("запросов: %d, ждали 1", len(*seen))
	}
}

// Запасные модели уходят в OpenRouter списком: он сам переключается, если
// основная перегружена. Без запасных список не передаётся вовсе.
func TestChatSendsFallbackModels(t *testing.T) {
	c, seen := fakeOpenRouter(t)
	if _, err := c.Chat([]ChatMessage{{Role: "user", Content: "x"}}, ""); err != nil {
		t.Fatal(err)
	}
	if len((*seen)[0].Models) != 0 || (*seen)[0].Model != "deepseek/deepseek-chat" {
		t.Errorf("без запасных: %+v", (*seen)[0])
	}

	c.SetFallbackModels([]string{" deepseek/deepseek-v3.2 ", "", "deepseek/deepseek-chat", "openai/gpt-4o-mini"})
	if _, err := c.Chat([]ChatMessage{{Role: "user", Content: "x"}}, ""); err != nil {
		t.Fatal(err)
	}
	want := []string{"deepseek/deepseek-chat", "deepseek/deepseek-v3.2", "openai/gpt-4o-mini"}
	if got := (*seen)[1].Models; strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("список моделей: %v, ждали %v", got, want)
	}
	// Явно заданная модель (как у планировщика задач) стоит первой.
	if _, err := c.Chat([]ChatMessage{{Role: "user", Content: "x"}}, "anthropic/claude-haiku-4.5"); err != nil {
		t.Fatal(err)
	}
	if got := (*seen)[2]; got.Model != "anthropic/claude-haiku-4.5" || got.Models[0] != "anthropic/claude-haiku-4.5" {
		t.Errorf("своя модель первой: %+v", got)
	}
}
