package config

import (
	"os"
	"strings"
)

type Config struct {
	Port            string
	DatabaseURL     string
	TrackerSecret   string
	OpenRouterKey   string
	OpenRouterModel string
	CursorAPIKey    string
	CursorAPI       string
	CursorModel     string
	// Claude Agent SDK — второй исполнитель задач (claude_run.py).
	AnthropicAPIKey string
	ClaudeModel     string
	// TrackerAgent — исполнитель по умолчанию: "cursor" или "claude".
	// Пусто — cursor, если есть CURSOR_API_KEY, иначе claude.
	TrackerAgent     string
	GithubToken      string
	GithubAPI        string
	Repo             string
	Branch           string
	LeoNotifyURL     string
	NotifySecret     string
	RailwayToken     string
	RailwayProjectID string
	RailwayEnvID     string
}

func Load() Config {
	port := strings.TrimSpace(os.Getenv("PORT"))
	if port == "" {
		port = "8080"
	}
	model := strings.TrimSpace(os.Getenv("OPENROUTER_MODEL"))
	if model == "" {
		model = "x-ai/grok-4.6"
	}
	repo := strings.TrimSpace(os.Getenv("BOARD_REPO"))
	if repo == "" {
		repo = "fuserwyn/Fat-Leopard"
	}
	return Config{
		Port:            port,
		DatabaseURL:     strings.TrimSpace(os.Getenv("DATABASE_URL")),
		TrackerSecret:   firstEnv("TRACKER_SECRET", "BOARD_SSO_SECRET"),
		OpenRouterKey:   strings.TrimSpace(os.Getenv("OPENROUTER_API_KEY")),
		OpenRouterModel: model,
		CursorAPIKey:    firstEnv("CURSOR_API_KEY"),
		CursorAPI:       strings.TrimSpace(os.Getenv("CURSOR_API")),
		CursorModel:     firstEnv("CURSOR_MODEL", "BOARD_MODEL"),
		AnthropicAPIKey: firstEnv("ANTHROPIC_API_KEY"),
		ClaudeModel:     firstEnv("CLAUDE_MODEL"),
		TrackerAgent:    strings.ToLower(firstEnv("TRACKER_AGENT")),
		// Личный PAT fuserwyn из MyVibeLab — Fat-Leopard его репозиторий.
		// Орговый GITHUB_TOKEN клонирует публичное репо, а push падает.
		GithubToken:      firstEnv("GITHUB_PERSONAL_TOKEN", "GITHUB_TOKEN", "GH_TOKEN"),
		GithubAPI:        strings.TrimSpace(os.Getenv("GITHUB_API")),
		Repo:             repo,
		Branch:           strings.TrimSpace(os.Getenv("BOARD_BRANCH")),
		LeoNotifyURL:     firstEnv("LEO_NOTIFY_URL", "BOARD_NOTIFY_URL"),
		NotifySecret:     firstEnv("NOTIFY_SECRET", "BOARD_SSO_SECRET"),
		RailwayToken:     firstEnv("RAILWAY_API_TOKEN"),
		RailwayProjectID: firstEnv("RAILWAY_PROJECT_ID"),
		RailwayEnvID:     firstEnv("RAILWAY_ENVIRONMENT_ID"),
	}
}

func firstEnv(keys ...string) string {
	for _, k := range keys {
		if v := cleanSecret(os.Getenv(k)); v != "" {
			return v
		}
	}
	return ""
}

func cleanSecret(v string) string {
	v = strings.TrimSpace(v)
	v = strings.Trim(v, `"'`)
	if i := strings.Index(v, "="); i > 0 {
		left := v[:i]
		if left == "GH_TOKEN" || strings.HasPrefix(left, "GITHUB_") {
			v = strings.Trim(strings.TrimSpace(v[i+1:]), `"'`)
		}
	}
	return v
}
