package agent

import (
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
		{"default cursor", both, store.Job{}, engineCursor},
		{"job claude", both, store.Job{Model: "claude-sonnet-5-5"}, engineClaude},
		{"job claude alias", both, store.Job{Model: "Claude"}, engineClaude},
		{"job composer beats env", config.Config{TrackerAgent: "claude"}, store.Job{Model: "composer-2.5"}, engineCursor},
		{"env claude", config.Config{TrackerAgent: "claude", CursorAPIKey: "c"}, store.Job{Model: "cursor-composer"}, engineClaude},
		{"only anthropic key", config.Config{AnthropicAPIKey: "a"}, store.Job{}, engineClaude},
		{"cursor without key falls to claude", config.Config{TrackerAgent: "cursor", ClaudeOAuthToken: "o"}, store.Job{}, engineClaude},
		{"composer without key falls to claude", config.Config{ClaudeOAuthToken: "o"}, store.Job{Model: "composer-2.5"}, engineClaude},
		{"env cursor", config.Config{TrackerAgent: "cursor", CursorAPIKey: "c", ClaudeOAuthToken: "o"}, store.Job{}, engineCursor},
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
