package agent

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

func TestCursorModelID(t *testing.T) {
	if got := cursorModelID(config.Config{}, store.Job{}); got != "composer-2.5" {
		t.Fatalf("default: %s", got)
	}
	if got := cursorModelID(config.Config{CursorModel: "cursor-composer"}, store.Job{Model: "cursor-composer"}); got != "composer-2.5" {
		t.Fatalf("alias: %s", got)
	}
	if got := cursorModelID(config.Config{CursorModel: "composer-2"}, store.Job{}); got != "composer-2" {
		t.Fatalf("cfg: %s", got)
	}
	if got := cursorModelID(config.Config{CursorModel: "composer-2"}, store.Job{Model: "composer-2.5"}); got != "composer-2.5" {
		t.Fatalf("job wins: %s", got)
	}
}

func TestCursorDoingPrompt(t *testing.T) {
	p := cursorDoingPrompt(store.Job{Prompt: "Сделай донат"}, "tracker/25-74")
	if !strings.Contains(p, "Сделай донат") || !strings.Contains(p, "tracker/25-74") {
		t.Fatal(p)
	}
}

func TestApplyDoingRequiresCursorKey(t *testing.T) {
	_, err := applyDoing(config.Config{GithubToken: "t", Repo: "o/r", TrackerAgent: "cursor"}, store.Job{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "CURSOR_API_KEY") {
		t.Fatalf("%v", err)
	}
}

func TestApplyDoingRequiresGithub(t *testing.T) {
	_, err := applyDoing(config.Config{CursorAPIKey: "k"}, store.Job{Prompt: "x"})
	if err == nil || !strings.Contains(err.Error(), "GitHub") {
		t.Fatalf("%v", err)
	}
}

func TestRunCursorLocal(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "stub.py")
	body := ""
	if runtime.GOOS == "windows" {
		t.Skip("stub uses python3")
	}
	body = "import json,sys\np=json.load(sys.stdin)\nassert p['cwd']\nassert 'Сделай донат' in p['prompt']\nassert p['model']=='composer-2.5'\njson.dump({'ok':True,'status':'finished','result':'добавил донат'}, sys.stdout)\n"
	if err := os.WriteFile(script, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_RUN", script)
	got, err := runCursorLocal(config.Config{CursorAPIKey: "cursor_key"}, store.Job{
		ID: 74, SourceNum: 25, Prompt: "Сделай донат 1 и 5 звезд",
	}, dir, "tracker/25-74")
	if err != nil {
		t.Fatal(err)
	}
	if got != "добавил донат" {
		t.Fatalf("result=%q", got)
	}
}

func TestRunCursorLocalError(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.py")
	if err := os.WriteFile(script, []byte("import json,sys\njson.dump({'ok':False,'error':'нет доступа'}, sys.stdout)\nsys.exit(1)\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CURSOR_RUN", script)
	_, err := runCursorLocal(config.Config{CursorAPIKey: "k"}, store.Job{Prompt: "x"}, dir, "tracker/1")
	if err == nil || !strings.Contains(err.Error(), "нет доступа") {
		t.Fatalf("%v", err)
	}
}
