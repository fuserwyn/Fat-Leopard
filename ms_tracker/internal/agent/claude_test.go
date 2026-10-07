package agent

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

func TestAgentEngine(t *testing.T) {
	both := config.Config{CursorAPIKey: "c", AnthropicAPIKey: "a"}
	cases := []struct {
		name string
		cfg  config.Config
		job  store.Job
		want string
	}{
		{"default claude", both, store.Job{}, engineClaude},
		{"board default model", both, store.Job{Model: "cursor-composer"}, engineClaude},
		{"job claude", both, store.Job{Model: "claude-sonnet-5-5"}, engineClaude},
		{"job claude alias", both, store.Job{Model: "Claude"}, engineClaude},
		{"job composer beats default", both, store.Job{Model: "composer-2.5"}, engineCursor},
		{"env cursor", config.Config{TrackerAgent: "cursor", CursorAPIKey: "c", ClaudeOAuthToken: "o"}, store.Job{}, engineCursor},
		{"env cursor, board default model", config.Config{TrackerAgent: "cursor", CursorAPIKey: "c", ClaudeOAuthToken: "o"}, store.Job{Model: "cursor-composer"}, engineCursor},
		{"only cursor key", config.Config{CursorAPIKey: "c"}, store.Job{}, engineCursor},
		{"only anthropic key", config.Config{AnthropicAPIKey: "a"}, store.Job{}, engineClaude},
		{"cursor without key falls to claude", config.Config{TrackerAgent: "cursor", ClaudeOAuthToken: "o"}, store.Job{}, engineClaude},
		{"composer without key falls to claude", config.Config{ClaudeOAuthToken: "o"}, store.Job{Model: "composer-2.5"}, engineClaude},
	}
	for _, c := range cases {
		if got := agentEngine(c.cfg, c.job); got != c.want {
			t.Errorf("%s: got %s want %s", c.name, got, c.want)
		}
	}
}

func TestClaudeModelID(t *testing.T) {
	if got := claudeModelID(config.Config{}, store.Job{}); got != claudeDefaultModel {
		t.Fatalf("default: %s", got)
	}
	if got := claudeModelID(config.Config{}, store.Job{Model: "claude"}); got != claudeDefaultModel {
		t.Fatalf("alias: %s", got)
	}
	if got := claudeModelID(config.Config{ClaudeModel: "claude-sonnet-5-5"}, store.Job{Model: "cursor-composer"}); got != "claude-sonnet-5-5" {
		t.Fatalf("cfg: %s", got)
	}
	if got := claudeModelID(config.Config{ClaudeModel: "claude-sonnet-5-5"}, store.Job{Model: "claude:claude-haiku-4-5"}); got != "claude-haiku-4-5" {
		t.Fatalf("job wins: %s", got)
	}
}

func TestApplyDoingRequiresClaudeToken(t *testing.T) {
	_, err := applyDoing(config.Config{GithubToken: "t", Repo: "o/r", TrackerAgent: "claude"}, store.Job{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("%v", err)
	}
}

func TestClaudeDoingPrompt(t *testing.T) {
	p := claudeDoingPrompt(store.Job{Prompt: "Почини кнопку"}, "tracker/25-74")
	if !strings.Contains(p, "Почини кнопку") || !strings.Contains(p, "tracker/25-74") {
		t.Fatal(p)
	}
}

func TestClaudeEnvPrefersSubscription(t *testing.T) {
	t.Setenv("ANTHROPIC_API_KEY", "from-env")
	t.Setenv("ANTHROPIC_BASE_URL", "https://openrouter.ai/api")
	t.Setenv("ANTHROPIC_AUTH_TOKEN", "sk-or-x")
	creds := claudeCreds(config.Config{ClaudeOAuthToken: "sk-ant-oat01-tok", AnthropicAPIKey: "sk-ant-api03-key"})
	if len(creds) != 2 || !creds[0].oauth || creds[1].oauth {
		t.Fatalf("сначала подписка, потом ключ: %+v", creds)
	}
	env := strings.Join(claudeEnv(creds[0]), "\n")
	if !strings.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-oat01-tok") || strings.Contains(env, "ANTHROPIC_API_KEY=") {
		t.Fatal("подписка должна вытеснять API-ключ")
	}
	if strings.Contains(env, "openrouter") || strings.Contains(env, "ANTHROPIC_AUTH_TOKEN=") {
		t.Fatal("переменные чужого провайдера не должны доходить до CLI")
	}
	env = strings.Join(claudeEnv(creds[1]), "\n")
	if !strings.Contains(env, "ANTHROPIC_API_KEY=sk-ant-api03-key") || strings.Contains(env, "CLAUDE_CODE_OAUTH_TOKEN=") {
		t.Fatal("ключ API — без токена подписки")
	}
}

func TestClaudeCredsSortByShape(t *testing.T) {
	// Токен подписки в ANTHROPIC_API_KEY, порванный переносом строки.
	creds := claudeCreds(config.Config{AnthropicAPIKey: "\"sk-ant-oat01-ab\n cd\""})
	if len(creds) != 1 || !creds[0].oauth || creds[0].value != "sk-ant-oat01-abcd" || creds[0].label() != "подписка" {
		t.Fatalf("%+v", creds)
	}
	creds = claudeCreds(config.Config{ClaudeOAuthToken: "CLAUDE_CODE_OAUTH_TOKEN=sk-ant-api03-k"})
	if len(creds) != 1 || creds[0].oauth || creds[0].value != "sk-ant-api03-k" {
		t.Fatalf("%+v", creds)
	}
	odd := claudeCreds(config.Config{ClaudeOAuthToken: "mvl_abc"})
	if len(odd) != 1 || !strings.Contains(odd[0].label(), "не похож") {
		t.Fatalf("%+v", odd)
	}
	if !isClaudeAuthError("Failed to authenticate. API Error: 401 API key is invalid.") || isClaudeAuthError("claude sdk timeout") {
		t.Fatal("auth error detection")
	}
}

// AskClaude: вопрос уходит в claude_run.py в режиме ask, без репозитория.
func TestAskClaudePassesAskPayload(t *testing.T) {
	dir := t.TempDir()
	stub := filepath.Join(dir, "claude_stub.sh")
	seen := filepath.Join(dir, "payload.json")
	script := "#!/bin/sh\ncat > " + seen + "\necho '{\"ok\": true, \"status\": \"success\", \"result\": \"  {\\\"tasks\\\": []}  \"}'\n"
	if err := os.WriteFile(stub, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CLAUDE_RUN", stub)

	cfg := config.Config{ClaudeOAuthToken: "sk-ant-oat01-x", ClaudeModel: "claude-sonnet-5-5"}
	got, err := AskClaude(cfg, "Ты Лео", "Придумай спринт", "")
	if err != nil || got != `{"tasks": []}` {
		t.Fatalf("ответ: %q %v", got, err)
	}
	raw, err := os.ReadFile(seen)
	if err != nil {
		t.Fatal(err)
	}
	var payload map[string]string
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if payload["mode"] != "ask" || payload["system"] != "Ты Лео" || payload["prompt"] != "Придумай спринт" || payload["model"] != "claude-sonnet-5-5" {
		t.Fatalf("payload: %v", payload)
	}

	if _, err := AskClaude(cfg, "", "  ", ""); err == nil {
		t.Fatal("пустой вопрос должен отклоняться")
	}
	if _, err := AskClaude(config.Config{}, "", "вопрос", ""); err == nil || !strings.Contains(err.Error(), "CLAUDE_CODE_OAUTH_TOKEN") {
		t.Fatalf("без доступа: %v", err)
	}
}
