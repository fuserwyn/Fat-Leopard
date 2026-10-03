package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

// Claude Agent SDK — второй исполнитель задач рядом с Cursor SDK.
// Запускается так же: python-скрипт в клоне репо, JSON через stdin/stdout.

const (
	claudeWait         = 25 * time.Minute
	claudeDefaultModel = "claude-opus-5-5"
)

const (
	engineCursor = "cursor"
	engineClaude = "claude"
)

func isClaudeModel(m string) bool {
	m = strings.ToLower(strings.TrimSpace(m))
	return m == "claude" || strings.HasPrefix(m, "claude-") || strings.HasPrefix(m, "claude:")
}

func isCursorModel(m string) bool {
	m = strings.ToLower(strings.TrimSpace(m))
	return m == "cursor" || strings.HasPrefix(m, "cursor") || strings.HasPrefix(m, "composer")
}

// agentEngine выбирает исполнителя задачи:
//  1. модель карточки: claude-* → Claude, composer-*/cursor-* → Cursor;
//  2. TRACKER_AGENT=claude|cursor;
//  3. есть только один ключ — тот исполнитель; оба — Cursor (как было).
func agentEngine(cfg config.Config, job store.Job) string {
	if isClaudeModel(job.Model) {
		return engineClaude
	}
	explicitCursor := isCursorModel(job.Model) && !strings.EqualFold(strings.TrimSpace(job.Model), "cursor-composer")
	if explicitCursor {
		return engineCursor
	}
	switch strings.ToLower(strings.TrimSpace(cfg.TrackerAgent)) {
	case engineClaude:
		return engineClaude
	case engineCursor:
		return engineCursor
	}
	if strings.TrimSpace(cfg.CursorAPIKey) == "" && strings.TrimSpace(cfg.AnthropicAPIKey) != "" {
		return engineClaude
	}
	return engineCursor
}

// agentKeyError — нет ключа у выбранного исполнителя.
func agentKeyError(cfg config.Config, job store.Job) error {
	if agentEngine(cfg, job) == engineClaude {
		if strings.TrimSpace(cfg.AnthropicAPIKey) == "" {
			return fmt.Errorf("нет ANTHROPIC_API_KEY")
		}
		return nil
	}
	if strings.TrimSpace(cfg.CursorAPIKey) == "" {
		return fmt.Errorf("нет CURSOR_API_KEY")
	}
	return nil
}

func runAgentLocal(cfg config.Config, job store.Job, repoDir, branch string) (string, error) {
	if agentEngine(cfg, job) == engineClaude {
		return runClaudeLocal(cfg, job, repoDir, branch)
	}
	return runCursorLocal(cfg, job, repoDir, branch)
}

func claudeModelID(cfg config.Config, job store.Job) string {
	for _, raw := range []string{job.Model, cfg.ClaudeModel} {
		m := strings.TrimSpace(raw)
		if strings.HasPrefix(strings.ToLower(m), "claude:") {
			m = strings.TrimSpace(m[len("claude:"):])
		}
		if m == "" || strings.EqualFold(m, "claude") || !isClaudeModel(m) {
			continue
		}
		return m
	}
	return claudeDefaultModel
}

func claudeDoingPrompt(job store.Job, branch string) string {
	var b strings.Builder
	b.WriteString("Ты агент трекера Fat Leopard. Репозиторий уже на ветке задачи, текущий каталог — его корень.\n")
	b.WriteString("Сделай задачу инструментами Read/Edit/Write/Glob/Grep/Bash: правь файлы точечно.\n")
	b.WriteString("Не возвращай JSON с полным текстом файлов и не пиши правки только в .tracker.\n")
	b.WriteString("Не делай git push, не открывай PR, не создавай и не переключай ветки, не меняй git config.\n")
	b.WriteString("Пуш на origin сделает трекер сам, в ветку ")
	b.WriteString(branch)
	b.WriteString(".\n")
	b.WriteString("В конце кратко напиши по-русски, что сделал и какие файлы тронул. Без эмодзи.\n\n")
	b.WriteString(strings.TrimSpace(job.Prompt))
	return b.String()
}

func claudeRunPath() string {
	if p := strings.TrimSpace(os.Getenv("CLAUDE_RUN")); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "claude_run.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "claude_run.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "claude_run.py"
}

func runClaudeLocal(cfg config.Config, job store.Job, repoDir, branch string) (string, error) {
	if strings.TrimSpace(cfg.AnthropicAPIKey) == "" {
		return "", fmt.Errorf("нет ANTHROPIC_API_KEY")
	}
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("нет каталога репозитория")
	}
	payload, err := json.Marshal(map[string]string{
		"cwd":    repoDir,
		"prompt": claudeDoingPrompt(job, branch),
		"model":  claudeModelID(cfg, job),
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), claudeWait)
	defer cancel()
	// cursorCmd — общий запуск python в своей группе процессов (CLI Claude
	// тоже плодит шеллы), по таймауту убивается вся группа.
	cmd := cursorCmd(ctx, claudeRunPath())
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(),
		"ANTHROPIC_API_KEY="+strings.TrimSpace(cfg.AnthropicAPIKey),
		"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1",
	)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	_ = killCursorGroup(cmd)
	var out cursorLocalOut
	if raw := lastJSONLine(stdout.Bytes()); len(raw) > 0 {
		if jerr := json.Unmarshal(raw, &out); jerr != nil {
			return "", fmt.Errorf("claude sdk: %s", clip(string(raw)+" "+stderr.String(), 240))
		}
	}
	if runErr != nil && !out.OK && strings.TrimSpace(out.Error) == "" {
		if ctx.Err() != nil {
			return "", fmt.Errorf("claude sdk timeout")
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("claude sdk: %s", clip(msg, 240))
	}
	if !out.OK {
		errText := strings.TrimSpace(out.Error)
		if errText == "" {
			errText = "claude sdk не сдал задачу"
		}
		return "", fmt.Errorf("claude sdk: %s", clip(errText, 240))
	}
	note := strings.TrimSpace(out.Result)
	if note == "" {
		note = "Claude сдал задачу."
	}
	return note, nil
}

// lastJSONLine — последняя непустая строка stdout (вдруг SDK что-то напечатал раньше).
func lastJSONLine(raw []byte) []byte {
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		if l := bytes.TrimSpace(lines[i]); len(l) > 0 {
			return l
		}
	}
	return nil
}
