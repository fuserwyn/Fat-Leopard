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

// Claude Agent SDK — основной исполнитель задач; Cursor SDK — запасной.
// Запускается так же: python-скрипт в клоне репо, JSON через stdin/stdout.

const (
	// С запасным Cursor (20 мин) укладываемся в час, после которого задачу
	// снимают как зависшую (worker.staleAfter).
	claudeWait         = 35 * time.Minute
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

// claudeReady — есть доступ к Claude Agent SDK. Основной путь — токен
// подписки Claude Code (CLAUDE_CODE_OAUTH_TOKEN, `claude setup-token`);
// ANTHROPIC_API_KEY — запасной, если токена нет.
func claudeReady(cfg config.Config) bool {
	return len(claudeCreds(cfg)) > 0
}

func cursorReady(cfg config.Config) bool {
	return strings.TrimSpace(cfg.CursorAPIKey) != ""
}

// agentEngine — с кого начинать задачу:
//  1. модель карточки: claude-* → Claude, composer-*/cursor-* → Cursor;
//  2. TRACKER_AGENT=claude|cursor;
//  3. по умолчанию Claude, а если у Claude нет доступа — Cursor.
//
// Если первый исполнитель недоступен (нет токенов, лимит, ошибка, таймаут) —
// runAgentLocal сам передаёт задачу второму.
func agentEngine(cfg config.Config, job store.Job) string {
	pick := engineClaude
	switch {
	case isClaudeModel(job.Model):
		pick = engineClaude
	case isCursorModel(job.Model) && !strings.EqualFold(strings.TrimSpace(job.Model), "cursor-composer"):
		pick = engineCursor
	case strings.EqualFold(strings.TrimSpace(cfg.TrackerAgent), engineCursor):
		pick = engineCursor
	}
	if pick == engineCursor && !cursorReady(cfg) && claudeReady(cfg) {
		return engineClaude
	}
	if pick == engineClaude && !claudeReady(cfg) && cursorReady(cfg) {
		return engineCursor
	}
	return pick
}

// agentKeyError — ни у одного подходящего исполнителя нет доступа.
func agentKeyError(cfg config.Config, job store.Job) error {
	if agentEngine(cfg, job) == engineClaude {
		if !claudeReady(cfg) {
			return fmt.Errorf("нет CLAUDE_CODE_OAUTH_TOKEN для Claude Agent SDK")
		}
		return nil
	}
	if !cursorReady(cfg) {
		return fmt.Errorf("нет CURSOR_API_KEY")
	}
	return nil
}

// runAgentLocal запускает основного исполнителя, а при его сбое — запасного.
// Кто именно сделал задачу, в заметку не пишем: на доске это шум.
func runAgentLocal(cfg config.Config, job store.Job, repoDir, branch string) (string, error) {
	if agentEngine(cfg, job) == engineClaude {
		note, err := runClaudeLocal(cfg, job, repoDir, branch)
		if err == nil || !cursorReady(cfg) {
			return note, err
		}
		resetWorktree(repoDir)
		cnote, cerr := runCursorLocal(cfg, job, repoDir, branch)
		if cerr != nil {
			return "", fmt.Errorf("%v; запасной Cursor: %v", err, cerr)
		}
		return cnote, nil
	}
	note, err := runCursorLocal(cfg, job, repoDir, branch)
	if !claudeReady(cfg) {
		return note, err
	}
	if err == nil {
		// Cursor «сдал», но в репо нет правок и нет JSON с файлами —
		// считаем, что он не справился, и отдаём задачу Claude.
		if dirtyHasImpl(repoDir) || len(parseImplReply(note).Files) > 0 {
			return note, nil
		}
		err = fmt.Errorf("cursor sdk сдал задачу без правок")
	}
	resetWorktree(repoDir)
	cnote, cerr := runClaudeLocal(cfg, job, repoDir, branch)
	if cerr != nil {
		return "", fmt.Errorf("%v; запасной Claude: %v", err, cerr)
	}
	return cnote, nil
}

// resetWorktree откатывает недоделки первого исполнителя, чтобы запасной
// начал с чистой ветки.
func resetWorktree(repoDir string) {
	_ = run(repoDir, "git", "reset", "--hard", "HEAD")
	_ = run(repoDir, "git", "clean", "-fd")
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
	b.WriteString("В окружении есть Go, Node и Postgres. Перед сдачей проверь то, что менял:\n")
	b.WriteString("- Go-сервис (ms_leo, ms_tracker): в его каталоге `go build ./... && go test ./...`;\n")
	b.WriteString("- miniapp: `npm ci`, затем `npm run build` и `npx vitest run`;\n")
	b.WriteString("- SQL-запросы и миграции ms_leo: `LEO_TEST_PG_DSN=$(leo-test-pg) go test ./internal/database/` — ")
	b.WriteString("поднимет временную базу со всеми миграциями; на новый запрос добавь проверку в migrations_integration_test.go.\n")
	b.WriteString("Свои ошибки исправь. Если тест падал и до твоих правок — не чини его, а назови в отчёте.\n")
	b.WriteString("В конце кратко напиши по-русски, что сделал, какие файлы тронул и какие проверки прошли. Без эмодзи.\n\n")
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
	creds := claudeCreds(cfg)
	if len(creds) == 0 {
		return "", fmt.Errorf("нет CLAUDE_CODE_OAUTH_TOKEN для Claude Agent SDK")
	}
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("нет каталога репозитория")
	}
	var errs []string
	for _, cred := range creds {
		note, err := runClaudeOnce(cfg, job, repoDir, branch, cred)
		if err == nil {
			return note, nil
		}
		errs = append(errs, err.Error())
		// Дальше пробуем второй доступ только если этот не пустили:
		// протухший токен подписки не должен ронять задачу при живом ключе API.
		if !isClaudeAuthError(err.Error()) {
			break
		}
	}
	return "", fmt.Errorf("%s", strings.Join(errs, "; "))
}

func runClaudeOnce(cfg config.Config, job store.Job, repoDir, branch string, cred claudeCred) (string, error) {
	tag := "claude sdk (" + cred.label() + ")"
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
	cmd.Env = claudeEnv(cred)
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	_ = killCursorGroup(cmd)
	var out cursorLocalOut
	if raw := lastJSONLine(stdout.Bytes()); len(raw) > 0 {
		if jerr := json.Unmarshal(raw, &out); jerr != nil {
			return "", fmt.Errorf("%s: %s", tag, clip(string(raw)+" "+stderr.String(), 240))
		}
	}
	if runErr != nil && !out.OK && strings.TrimSpace(out.Error) == "" {
		if ctx.Err() != nil {
			return "", fmt.Errorf("%s timeout", tag)
		}
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		return "", fmt.Errorf("%s: %s", tag, clip(msg, 240))
	}
	if !out.OK {
		errText := strings.TrimSpace(out.Error)
		if errText == "" {
			errText = "claude sdk не сдал задачу"
		}
		return "", fmt.Errorf("%s: %s", tag, clip(errText, 240))
	}
	note := strings.TrimSpace(out.Result)
	if note == "" {
		note = "Claude сдал задачу."
	}
	return note, nil
}

// claudeCred — один способ авторизации CLI Claude.
type claudeCred struct {
	oauth bool // токен подписки Claude Code, иначе ключ API
	value string
}

// label — что именно пробовали, чтобы по ошибке на доске было видно,
// какой доступ не пустили и похож ли он вообще на нужный.
func (c claudeCred) label() string {
	if c.oauth {
		if !strings.HasPrefix(c.value, claudeOAuthPrefix) {
			return "подписка, токен не похож на " + claudeOAuthPrefix + "…"
		}
		return "подписка"
	}
	if !strings.HasPrefix(c.value, "sk-ant-") {
		return "ключ API, не похож на sk-ant-…"
	}
	return "ключ API"
}

const claudeOAuthPrefix = "sk-ant-oat"

// claudeSecret чистит значение из Railway: токен из `claude setup-token`
// длинный и при копировании из терминала рвётся переносом строки, а ещё
// его вставляют вместе с именем переменной и кавычками.
func claudeSecret(v string) string {
	v = strings.Join(strings.Fields(v), "")
	v = strings.Trim(v, "\"'")
	if i := strings.Index(v, "="); i > 0 {
		if left := v[:i]; left == "CLAUDE_CODE_OAUTH_TOKEN" || left == "ANTHROPIC_API_KEY" {
			v = strings.Trim(v[i+1:], "\"'")
		}
	}
	return v
}

// claudeCreds — доступы по порядку: сначала подписка, потом ключ API.
// Значение раскладываем по виду, а не по имени переменной: токен подписки,
// положенный в ANTHROPIC_API_KEY (или наоборот), даёт «401 API key is invalid».
func claudeCreds(cfg config.Config) []claudeCred {
	var oauth, key []claudeCred
	for i, raw := range []string{cfg.ClaudeOAuthToken, cfg.AnthropicAPIKey} {
		v := claudeSecret(raw)
		if v == "" {
			continue
		}
		isOAuth := i == 0
		switch {
		case strings.HasPrefix(v, claudeOAuthPrefix):
			isOAuth = true
		case strings.HasPrefix(v, "sk-ant-api"):
			isOAuth = false
		}
		if isOAuth {
			oauth = append(oauth, claudeCred{oauth: true, value: v})
		} else {
			key = append(key, claudeCred{value: v})
		}
	}
	return append(oauth, key...)
}

func isClaudeAuthError(msg string) bool {
	low := strings.ToLower(msg)
	for _, s := range []string{"401", "403", "authenticate", "api key", "oauth", "unauthorized", "/login"} {
		if strings.Contains(low, s) {
			return true
		}
	}
	return false
}

// claudeForeignEnv — что не должно протечь в CLI Claude из окружения сервиса.
var claudeForeignEnv = map[string]bool{
	"ANTHROPIC_API_KEY":              true,
	"CLAUDE_CODE_OAUTH_TOKEN":        true,
	"ANTHROPIC_AUTH_TOKEN":           true,
	"ANTHROPIC_BASE_URL":             true,
	"ANTHROPIC_MODEL":                true,
	"ANTHROPIC_DEFAULT_OPUS_MODEL":   true,
	"ANTHROPIC_DEFAULT_SONNET_MODEL": true,
	"ANTHROPIC_DEFAULT_HAIKU_MODEL":  true,
	"CLAUDE_CODE_SUBAGENT_MODEL":     true,
}

// claudeEnv — окружение для CLI Claude под один доступ. Второй из окружения
// убираем: CLI предпочитает ANTHROPIC_API_KEY и при токене подписки иначе
// пошёл бы в платный API.
//
// Переменные чужого провайдера (OpenRouter и т.п. — ANTHROPIC_BASE_URL,
// ANTHROPIC_AUTH_TOKEN, подмена моделей), заданные в Railway на проект,
// тоже убираем, как в myvibelab: с ними CLI идёт не в Anthropic и падает
// «401 API key is invalid».
func claudeEnv(cred claudeCred) []string {
	env := make([]string, 0, len(os.Environ())+3)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if claudeForeignEnv[name] {
			continue
		}
		env = append(env, kv)
	}
	if cred.oauth {
		env = append(env, "CLAUDE_CODE_OAUTH_TOKEN="+cred.value)
	} else {
		env = append(env, "ANTHROPIC_API_KEY="+cred.value)
	}
	// Как в myvibelab: адрес API задаём явно, а не полагаемся на умолчание.
	env = append(env, "ANTHROPIC_BASE_URL=https://api.anthropic.com")
	return append(env, "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1")
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
