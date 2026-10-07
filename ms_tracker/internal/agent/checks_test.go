package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestChecksForFiles(t *testing.T) {
	labels := func(files ...string) string {
		var out []string
		for _, c := range checksForFiles(files) {
			out = append(out, c.Label)
		}
		return strings.Join(out, "|")
	}
	if got := labels(".tracker/job-1.md", "README.md"); got != "" {
		t.Errorf("заметка и корень не требуют проверок: %q", got)
	}
	if got := labels("ms_leo/internal/bot/x.go"); got != "ms_leo: сборка|ms_leo: тесты" {
		t.Errorf("ms_leo: %q", got)
	}
	got := labels("miniapp/src/App.tsx", "ms_leo/internal/bot/x.go", "ms_tracker/Dockerfile")
	for _, want := range []string{"ms_leo: тесты", "ms_tracker: сборка", "miniapp: сборка", "miniapp: тесты"} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в %q", want, got)
		}
	}
}

func TestTailLines(t *testing.T) {
	out := "a\n\nb\n  \nc\nd\n"
	if got := tailLines(out, 2, 100); got != "c\nd" {
		t.Errorf("последние строки: %q", got)
	}
	if got := tailLines("абвгдеж", 5, 3); got != "…деж" {
		t.Errorf("обрезка по длине: %q", got)
	}
}

// Проверяем итог мержа: чистая ветка получает main, конфликтная — отказ.
func TestMergeBaseForCheck(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("нет git")
	}
	root := t.TempDir()
	git := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(path, body string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	origin := filepath.Join(root, "origin")
	git(root, "init", "-q", "-b", "main", origin)
	git(origin, "config", "user.email", "t@t")
	git(origin, "config", "user.name", "t")
	write(filepath.Join(origin, "a.txt"), "один\n")
	git(origin, "add", ".")
	git(origin, "commit", "-q", "-m", "init")
	git(origin, "checkout", "-q", "-b", "clean")
	write(filepath.Join(origin, "b.txt"), "ветка\n")
	git(origin, "add", ".")
	git(origin, "commit", "-q", "-m", "clean")
	git(origin, "checkout", "-q", "-b", "clash", "main")
	write(filepath.Join(origin, "a.txt"), "ветка\n")
	git(origin, "commit", "-q", "-am", "clash")
	git(origin, "checkout", "-q", "main")
	write(filepath.Join(origin, "a.txt"), "main ушёл вперёд\n")
	write(filepath.Join(origin, "c.txt"), "новое в main\n")
	git(origin, "add", ".")
	git(origin, "commit", "-q", "-m", "main moves")

	for branch, wantConflict := range map[string]bool{"clean": false, "clash": true} {
		work := filepath.Join(root, "work-"+branch)
		git(root, "clone", "-q", "--branch", branch, "file://"+origin, work)
		reason := mergeBaseForCheck(work, "main")
		if wantConflict != strings.Contains(reason, "конфликт с main") {
			t.Errorf("%s: reason %q", branch, reason)
		}
		if !wantConflict {
			if _, err := os.Stat(filepath.Join(work, "c.txt")); err != nil {
				t.Errorf("%s: main не влит: %v", branch, err)
			}
		}
	}
}

func TestRunCheckReportsFailureOutput(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("нет sh")
	}
	dir := t.TempDir()
	out, err := runCheck(dir, nil, branchCheck{Dir: ".", Label: "x", Name: "sh", Args: []string{"-c", "echo сломано; exit 3"}})
	if err == nil || !strings.Contains(out, "сломано") {
		t.Fatalf("ждали ошибку с выводом: %q %v", out, err)
	}
	if out, err = runCheck(dir, nil, branchCheck{Dir: ".", Label: "x", Name: "sh", Args: []string{"-c", "echo ок"}}); err != nil {
		t.Fatalf("успешная команда: %q %v", out, err)
	}
}
