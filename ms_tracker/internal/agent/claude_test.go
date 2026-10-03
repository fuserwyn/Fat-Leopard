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
		{"env cursor", config.Config{TrackerAgent: "cursor", AnthropicAPIKey: "a"}, store.Job{}, engineCursor},
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

func TestApplyDoingRequiresAnthropicKey(t *testing.T) {
	_, err := applyDoing(config.Config{GithubToken: "t", Repo: "o/r", TrackerAgent: "claude"}, store.Job{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "ANTHROPIC_API_KEY") {
		t.Fatalf("%v", err)
	}
}

func TestClaudeDoingPrompt(t *testing.T) {
	p := claudeDoingPrompt(store.Job{Prompt: "Почини кнопку"}, "tracker/25-74")
	if !strings.Contains(p, "Почини кнопку") || !strings.Contains(p, "tracker/25-74") {
		t.Fatal(p)
	}
}
