package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

const (
	cursorWait         = 20 * time.Minute
	cursorDefaultModel = "composer-2.5"
)

type cursorLocalOut struct {
	OK        bool   `json:"ok"`
	Status    string `json:"status"`
	Result    string `json:"result"`
	Error     string `json:"error"`
	Retryable bool   `json:"retryable"`
}

func cursorModelID(cfg config.Config, job store.Job) string {
	for _, raw := range []string{job.Model, cfg.CursorModel} {
		m := strings.TrimSpace(raw)
		if m == "" || strings.EqualFold(m, "cursor-composer") {
			continue
		}
		return m
	}
	return cursorDefaultModel
}

func cursorDoingPrompt(job store.Job, branch string) string {
	var b strings.Builder
	b.WriteString("Ты агент трекера Fat Leopard. Репозиторий уже на ветке задачи.\n")
	b.WriteString("Сделай задачу инструментами Cursor (Read/StrReplace/Write): правь файлы точечно.\n")
	b.WriteString("Не возвращай JSON с полным текстом файлов и не пиши правки только в .tracker.\n")
	b.WriteString("Коммить можно локально. Не открывай PR. Не создавай новую ветку cursor/*.\n")
	b.WriteString("Пуш на origin сделает трекер сам, в ветку ")
	b.WriteString(branch)
	b.WriteString(".\n")
	b.WriteString("В конце кратко напиши, что сделал. Без эмодзи.\n\n")
	b.WriteString(strings.TrimSpace(job.Prompt))
	return b.String()
}

func cursorRunPath() string {
	if p := strings.TrimSpace(os.Getenv("CURSOR_RUN")); p != "" {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "cursor_run.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if wd, err := os.Getwd(); err == nil {
		p := filepath.Join(wd, "cursor_run.py")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "cursor_run.py"
}

func pythonBin() string {
	for _, name := range []string{"python3", "python"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return "python3"
}

func cursorCmd(ctx context.Context, script string) *exec.Cmd {
	var cmd *exec.Cmd
	if strings.HasSuffix(strings.ToLower(script), ".py") {
		cmd = exec.CommandContext(ctx, pythonBin(), script)
	} else {
		cmd = exec.CommandContext(ctx, script)
	}
	// Своя группа процессов: python поднимает node-bridge, тот — шеллы агента.
	// По таймауту убиваем всю группу, иначе внуки переживают python и копятся.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killCursorGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second
	return cmd
}

func killCursorGroup(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
}

func runCursorLocal(cfg config.Config, job store.Job, repoDir, branch string) (string, error) {
	if strings.TrimSpace(cfg.CursorAPIKey) == "" {
		return "", fmt.Errorf("нет CURSOR_API_KEY")
	}
	repoDir = strings.TrimSpace(repoDir)
	if repoDir == "" {
		return "", fmt.Errorf("нет каталога репозитория")
	}
	payload, err := json.Marshal(map[string]string{
		"cwd":    repoDir,
		"prompt": cursorDoingPrompt(job, branch),
		"model":  cursorModelID(cfg, job),
	})
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), cursorWait)
	defer cancel()
	cmd := cursorCmd(ctx, cursorRunPath())
	cmd.Dir = repoDir
	cmd.Env = append(os.Environ(), "CURSOR_API_KEY="+strings.TrimSpace(cfg.CursorAPIKey))
	cmd.Stdin = bytes.NewReader(payload)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()
	// python вышел, но node-bridge мог остаться — добиваем группу.
	_ = killCursorGroup(cmd)
	var out cursorLocalOut
	if raw := bytes.TrimSpace(stdout.Bytes()); len(raw) > 0 {
		if jerr := json.Unmarshal(raw, &out); jerr != nil {
			return "", fmt.Errorf("cursor sdk: %s", clip(string(raw)+" "+stderr.String(), 240))
		}
	}
	if runErr != nil && !out.OK && strings.TrimSpace(out.Error) == "" {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = runErr.Error()
		}
		if ctx.Err() != nil {
			return "", fmt.Errorf("cursor sdk timeout")
		}
		return "", fmt.Errorf("cursor sdk: %s", clip(msg, 240))
	}
	if !out.OK {
		errText := strings.TrimSpace(out.Error)
		if errText == "" {
			errText = "cursor sdk не сдал задачу"
		}
		return "", fmt.Errorf("cursor sdk: %s", clip(errText, 240))
	}
	note := strings.TrimSpace(out.Result)
	if note == "" {
		note = "Cursor сдал задачу."
	}
	return note, nil
}
