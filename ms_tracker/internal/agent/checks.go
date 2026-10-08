package agent

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

// Настоящая проверка фазы «тест»: собираем и тестируем то, что задача меняла,
// на ветке задачи с влитым main — то есть ровно то, что уедет на прод.
// Раньше «тест» смотрел только, цел ли config.go, и в main попадал код,
// который не собирался.

const checkStepTimeout = 8 * time.Minute

// branchCheck — одна команда проверки в каталоге сервиса.
type branchCheck struct {
	Dir   string // каталог от корня репозитория
	Label string // что проверяем, для текста отказа
	Name  string
	Args  []string
}

// checksForFiles — какие проверки нужны по списку изменённых файлов.
func checksForFiles(files []string) []branchCheck {
	touched := map[string]bool{}
	for _, f := range files {
		f = strings.TrimPrefix(strings.TrimSpace(f), "./")
		if i := strings.Index(f, "/"); i > 0 {
			touched[f[:i]] = true
		}
	}
	var out []branchCheck
	for _, svc := range []string{"ms_leo", "ms_tracker"} {
		if !touched[svc] {
			continue
		}
		out = append(out,
			// go.sum в репозитории нет (он в .gitignore): образ на выкатке делает
			// `go mod tidy` перед сборкой, и здесь нужно то же — иначе свежий клон
			// не собирается с «missing go.sum entry», что бы ни было в задаче.
			branchCheck{Dir: svc, Label: svc + ": зависимости", Name: "go", Args: []string{"mod", "tidy"}},
			branchCheck{Dir: svc, Label: svc + ": сборка", Name: "go", Args: []string{"build", "./..."}},
			// Тот же сценарий, что при сборке образа на выкатке (у ms_leo — с временной
			// базой и порогом покрытия): что прошло здесь, пройдёт и там.
			branchCheck{Dir: svc, Label: svc + ": тесты", Name: "sh", Args: []string{"-c",
				"if [ -f scripts/test-in-build.sh ]; then sh ./scripts/test-in-build.sh; else go test ./...; fi"}},
		)
	}
	if touched["miniapp"] {
		out = append(out,
			branchCheck{Dir: "miniapp", Label: "miniapp: установка зависимостей", Name: "npm", Args: []string{"ci", "--no-audit", "--no-fund"}},
			branchCheck{Dir: "miniapp", Label: "miniapp: сборка", Name: "npm", Args: []string{"run", "build"}},
			branchCheck{Dir: "miniapp", Label: "miniapp: тесты", Name: "npx", Args: []string{"vitest", "run"}},
		)
	}
	return out
}

// runBranchChecks возвращает причину отказа или пустую строку, если всё прошло.
func runBranchChecks(cfg config.Config, job store.Job, files []string) string {
	checks := checksForFiles(files)
	if len(checks) == 0 {
		return ""
	}
	dir, err := os.MkdirTemp("", "leo-tracker-test-*")
	if err != nil {
		return "не создал каталог для проверки: " + err.Error()
	}
	repoDir, _, cleanup, err := prepareRepo(cfg, job, dir)
	defer cleanup()
	if err != nil {
		return "не поднял ветку для проверки: " + err.Error()
	}
	if reason := mergeBaseForCheck(repoDir, defaultBranch(cfg)); reason != "" {
		return reason
	}
	env := checkEnv()
	for _, c := range checks {
		if _, err := exec.LookPath(c.Name); err != nil {
			// Нет инструмента (локальный запуск без Go/Node) — не блокируем задачу.
			continue
		}
		if out, err := runCheck(repoDir, env, c); err != nil {
			return c.Label + " не прошла:\n" + tailLines(out, 25, 900)
		}
	}
	return ""
}

// mergeBaseForCheck вливает main в рабочую копию, чтобы проверять итог мержа.
// Конфликт — отказ; не хватило истории в неглубоком клоне — проверяем ветку как есть.
func mergeBaseForCheck(repoDir, base string) string {
	_ = run(repoDir, "git", "config", "user.email", "tracker@fat-leopard")
	_ = run(repoDir, "git", "config", "user.name", "Leo Tracker")
	if err := run(repoDir, "git", "fetch", "origin", "+refs/heads/"+base+":refs/remotes/origin/"+base, "--depth", "200"); err != nil {
		return ""
	}
	cmd := exec.Command("git", "merge", "--no-edit", "--no-ff", "origin/"+base)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err == nil {
		return ""
	}
	if strings.Contains(string(out), "CONFLICT") {
		return "конфликт с " + base + ": " + tailLines(string(out), 6, 300)
	}
	_ = run(repoDir, "git", "merge", "--abort")
	return ""
}

// checkEnv — окружение проверок: без ключей исполнителей (тестам они не нужны)
// и с временной базой для SQL-тестов ms_leo, если она доступна в образе.
func checkEnv() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if claudeForeignEnv[name] || name == "CURSOR_API_KEY" || name == "LEO_TEST_PG_DSN" {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "CI=1")
	if _, err := exec.LookPath("leo-test-pg"); err == nil {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if out, err := exec.CommandContext(ctx, "leo-test-pg").Output(); err == nil {
			if dsn := strings.TrimSpace(string(out)); strings.HasPrefix(dsn, "postgres://") {
				env = append(env, "LEO_TEST_PG_DSN="+dsn)
			}
		}
	}
	return env
}

func runCheck(repoDir string, env []string, c branchCheck) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), checkStepTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = repoDir + "/" + c.Dir
	cmd.Env = env
	// Своя группа процессов: npm и go плодят потомков, по таймауту убиваем всех.
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return killCursorGroup(cmd) }
	cmd.WaitDelay = 10 * time.Second
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	err := cmd.Run()
	_ = killCursorGroup(cmd)
	if ctx.Err() != nil {
		return buf.String(), fmt.Errorf("таймаут %s", checkStepTimeout)
	}
	return buf.String(), err
}

// tailLines — последние n непустых строк вывода, не длиннее max символов:
// ошибка сборки и упавший тест печатаются в конце.
func tailLines(out string, n, max int) string {
	var keep []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimRight(l, " \t\r"); strings.TrimSpace(l) != "" {
			keep = append(keep, l)
		}
	}
	if len(keep) > n {
		keep = keep[len(keep)-n:]
	}
	s := strings.Join(keep, "\n")
	if r := []rune(s); len(r) > max {
		s = "…" + string(r[len(r)-max:])
	}
	return s
}
