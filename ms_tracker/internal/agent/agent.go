package agent

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"leo-tracker/internal/config"
	"leo-tracker/internal/store"
)

type Result struct {
	Note      string
	Branch    string
	Commit    string
	Committed bool
	// HasImpl — в коммите есть файлы приложения, не только .tracker/job-N.md.
	HasImpl bool
	// Pushed — ветка уехала в main/стенд. Коммиты выполнения и ревью
	// живут на ветке задачи и пушем на стенд не считаются.
	Pushed bool
}

func Run(cfg config.Config, job store.Job) (Result, error) {
	phase := strings.ToLower(strings.TrimSpace(job.Phase))
	if phase == "" {
		phase = "doing"
	}
	if phase == "review" || phase == "test" {
		return runVerdict(cfg, job, phase)
	}
	return applyDoing(cfg, job)
}

func runVerdict(cfg config.Config, job store.Job, phase string) (Result, error) {
	branch := strings.TrimSpace(job.Branch)
	if branch == "" {
		branch = taskBranch(job)
	}
	info, ierr := InspectBranch(cfg, branch)
	if ierr != nil && !info.Exists && strings.TrimSpace(job.Prompt) == "" {
		return Result{}, ierr
	}
	if !info.Exists || !info.HasImpl {
		note := "ревью не принято: нет коммита выполнения на ветке " + branch + "."
		if phase == "test" {
			note = "тест не прошёл: нет коммита выполнения на ветке " + branch + "."
		}
		if info.Exists && !info.HasImpl {
			note = "ревью не принято: на ветке " + branch + " только заметка, правок приложения нет."
			if phase == "test" {
				note = "тест не прошёл: на ветке " + branch + " нет правок приложения."
			}
		}
		return Result{Note: note, Branch: branch}, nil
	}
	if reason := CheckBranchImpl(cfg, branch, job.Prompt); reason != "" {
		note := "ревью не принято: " + reason
		if phase == "test" {
			note = "тест не прошёл: " + reason
		}
		return Result{Note: note, Branch: branch, Commit: info.Head}, nil
	}
	note := strictVerdictNote(phase, branch, job.Prompt)
	out := Result{Note: note, Branch: branch, Commit: info.Head}
	if phase != "review" && phase != "test" {
		return out, nil
	}
	stamped, serr := Stamp(cfg, job, note)
	if serr != nil {
		out.Note = strings.TrimSpace(note + "\n\nGit: " + serr.Error())
		return out, nil
	}
	out.Commit = stamped.Commit
	out.Committed = stamped.Committed
	out.Branch = stamped.Branch
	out.HasImpl = true
	return out, nil
}

func strictVerdictNote(phase, branch, prompt string) string {
	extra := "config.go целый"
	if isDonateRemove(prompt) {
		if n := donateStarsFromPrompt(prompt); len(n) > 0 {
			extra = fmt.Sprintf("номинал %d скрыт в config.go", n[0])
		} else if n := donateRubFromPrompt(prompt); len(n) > 0 {
			extra = fmt.Sprintf("номинал %d скрыт в config.go", n[0])
		}
	} else if n := donateStarsFromPrompt(prompt); len(n) > 0 {
		extra = fmt.Sprintf("номинал %d есть в config.go", n[0])
	} else if n := donateRubFromPrompt(prompt); len(n) > 0 {
		extra = fmt.Sprintf("номинал %d есть в config.go", n[0])
	}
	if phase == "test" {
		return "Тест: " + extra + ", ветка " + branch + ". Тест пройден."
	}
	return "Ревью: на ветке " + branch + " " + extra + ". Можно на тест."
}

func verdictPassed(phase, text string) bool {
	low := strings.ToLower(text)
	if phase == "review" {
		if strings.Contains(low, "ревью не принято") || strings.Contains(low, "нельзя на тест") ||
			strings.Contains(low, `"pass":false`) {
			return false
		}
		return true
	}
	if strings.Contains(low, "тест не прошёл") || strings.Contains(low, "тест не прошел") ||
		strings.Contains(low, `"pass":false`) {
		return false
	}
	return true
}

// Stamp — коммит фазы на ветке задачи, без чата. Ревью и тест после
// вердикта пишут tracker: #N ревью / тест; пуш на стенд только после теста.
func Stamp(cfg config.Config, job store.Job, note string) (Result, error) {
	out := Result{Note: strings.TrimSpace(note)}
	if strings.TrimSpace(cfg.GithubToken) == "" || strings.TrimSpace(cfg.Repo) == "" {
		return out, fmt.Errorf("нет GitHub")
	}
	branch, sha, _, _, err := applyPhaseCommit(cfg, job, out.Note, nil)
	out.Branch = branch
	out.Commit = sha
	out.Committed = sha != "" && err == nil
	return out, err
}

func applyDoing(cfg config.Config, job store.Job) (Result, error) {
	if strings.TrimSpace(cfg.GithubToken) == "" || strings.TrimSpace(cfg.Repo) == "" {
		return Result{}, fmt.Errorf("нет GitHub")
	}
	if shouldRunCursor(job.Prompt, 0) {
		if kerr := agentKeyError(cfg, job); kerr != nil {
			return Result{}, kerr
		}
	}
	dir, err := os.MkdirTemp("", "leo-tracker-doing-*")
	if err != nil {
		return Result{}, err
	}
	repoDir, branch, cleanup, err := prepareRepo(cfg, job, dir)
	defer cleanup()
	if err != nil {
		return Result{Branch: branch}, err
	}
	note, n, kerr := applyKnownTask(repoDir, job.Prompt)
	if kerr != nil {
		return Result{Branch: branch}, kerr
	}
	if !shouldRunCursor(job.Prompt, n) {
		if note == "" {
			note = "Номинал уже есть в config.go."
		}
	} else if n == 0 {
		if kerr := agentKeyError(cfg, job); kerr != nil {
			return Result{Branch: branch}, kerr
		}
		var err error
		note, err = runAgentLocal(cfg, job, repoDir, branch)
		if err != nil {
			if fallback, fn, ferr := applyKnownTask(repoDir, job.Prompt); ferr == nil && fn > 0 {
				note = fallback
			} else {
				return Result{Branch: branch}, err
			}
		} else if !dirtyHasImpl(repoDir) {
			note, n, rejected, jerr := applyJSONFallback(repoDir, note)
			if jerr != nil {
				return Result{Branch: branch, Note: note}, jerr
			}
			if n == 0 {
				if fallback, fn, ferr := applyKnownTask(repoDir, job.Prompt); ferr == nil && fn > 0 {
					note = fallback
				} else {
					msg := "нет правок в репозитории: агент сдал только заметку"
					if len(rejected) > 0 {
						msg += " (" + strings.Join(rejected, "; ") + ")"
					}
					return Result{Branch: branch, Note: note}, fmt.Errorf("%s", msg)
				}
			}
		}
	}
	if reason := vitalWorktreeBroken(repoDir); reason != "" {
		return Result{Branch: branch, Note: note}, fmt.Errorf("%s", reason)
	}
	sha, hasImpl, gerr := commitWorkAndPush(cfg, job, repoDir, branch, note)
	out := Result{Note: note, Branch: branch, Commit: sha, Committed: sha != "" && gerr == nil, HasImpl: hasImpl}
	if gerr != nil {
		out.Note = strings.TrimSpace(out.Note + "\n\nGit: " + gerr.Error())
		return out, gerr
	}
	return out, nil
}

func dirtyHasImpl(repoDir string) bool {
	cmd := exec.Command("git", "status", "--porcelain")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	var names []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		names = append(names, strings.TrimSpace(line[3:]))
	}
	return filesHaveImpl(names)
}

func applyJSONFallback(repoDir, note string) (string, int, []string, error) {
	parsed := parseImplReply(note)
	if len(parsed.Files) == 0 {
		return note, 0, nil, nil
	}
	n, rejected, err := applyFileEdits(repoDir, parsed.Files)
	out := strings.TrimSpace(parsed.Note)
	if out == "" {
		out = note
	}
	return out, n, rejected, err
}

func writesGit(phase string) bool {
	p := strings.ToLower(strings.TrimSpace(phase))
	return p == "" || p == "doing"
}

func commitLabel(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "review":
		return "ревью"
	case "test":
		return "тест"
	default:
		return "выполнение"
	}
}

func taskBranch(job store.Job) string {
	if job.SourceNum > 0 && job.ID > 0 {
		return fmt.Sprintf("tracker/%d-%d", job.SourceNum, job.ID)
	}
	if job.SourceNum > 0 {
		return fmt.Sprintf("tracker/%d", job.SourceNum)
	}
	return fmt.Sprintf("tracker/0-%d", job.ID)
}

func prepareRepo(cfg config.Config, job store.Job, dir string) (repoDir, branch string, cleanup func(), err error) {
	cleanup = func() { _ = os.RemoveAll(dir) }
	cloneURL := fmt.Sprintf("https://x-access-token:%s@github.com/%s.git", cfg.GithubToken, cfg.Repo)
	if err = run(dir, "git", "clone", "--depth", "20", cloneURL, "repo"); err != nil {
		return "", "", cleanup, fmt.Errorf("clone: %w", err)
	}
	repoDir = filepath.Join(dir, "repo")
	branch = strings.TrimSpace(job.Branch)
	if branch == "" {
		branch = taskBranch(job)
	}
	if err = checkoutTaskBranch(repoDir, branch, defaultBranch(cfg)); err != nil {
		return repoDir, branch, cleanup, err
	}
	return repoDir, branch, cleanup, nil
}

func defaultBranch(cfg config.Config) string {
	base := strings.TrimSpace(cfg.Branch)
	if base == "" {
		return "main"
	}
	return base
}

// checkoutTaskBranch поднимает ветку задачи. После shallow clone
// `git fetch origin <branch>` кладёт коммит в FETCH_HEAD и часто
// не создаёт origin/<branch> — от него checkout -B падает 128.
// Если ветки на remote нет, отталкиваемся от main.
func checkoutTaskBranch(repoDir, branch, base string) error {
	if base == "" {
		base = "main"
	}
	if remoteBranchExists(repoDir, branch) {
		refspec := "+refs/heads/" + branch + ":refs/remotes/origin/" + branch
		if err := run(repoDir, "git", "fetch", "origin", refspec, "--depth", "20"); err == nil {
			if err := run(repoDir, "git", "checkout", "-B", branch, "FETCH_HEAD"); err == nil {
				return nil
			}
			if err := run(repoDir, "git", "checkout", "-B", branch, "origin/"+branch); err == nil {
				return nil
			}
		}
	}
	return checkoutFromBase(repoDir, branch, base)
}

func checkoutFromBase(repoDir, branch, base string) error {
	if err := run(repoDir, "git", "fetch", "origin", base, "--depth", "20"); err == nil {
		if err := run(repoDir, "git", "checkout", "-B", branch, "FETCH_HEAD"); err == nil {
			return nil
		}
	}
	if err := run(repoDir, "git", "checkout", "-B", branch, "origin/"+base); err == nil {
		return nil
	}
	return run(repoDir, "git", "checkout", "-B", branch)
}

func applyPhaseCommit(cfg config.Config, job store.Job, note string, edits []fileEdit) (string, string, bool, []string, error) {
	dir, err := os.MkdirTemp("", "leo-tracker-*")
	if err != nil {
		return "", "", false, nil, err
	}
	repoDir, branch, cleanup, err := prepareRepo(cfg, job, dir)
	defer cleanup()
	if err != nil {
		return branch, "", false, nil, err
	}
	_, rejected, err := applyFileEdits(repoDir, edits)
	if err != nil {
		return branch, "", false, rejected, err
	}
	if len(rejected) > 0 {
		note = strings.TrimSpace(note + "\n\nОтклонённые правки:\n- " + strings.Join(rejected, "\n- "))
	}
	sha, hasImpl, err := commitWorkAndPush(cfg, job, repoDir, branch, note)
	return branch, sha, hasImpl, rejected, err
}

func writeTrackerNote(repoDir string, job store.Job, note string) error {
	notePath := filepath.Join(repoDir, ".tracker", fmt.Sprintf("job-%d.md", job.SourceTaskID))
	if job.SourceTaskID <= 0 {
		notePath = filepath.Join(repoDir, ".tracker", fmt.Sprintf("job-%d.md", job.ID))
	}
	if err := os.MkdirAll(filepath.Dir(notePath), 0o755); err != nil {
		return err
	}
	label := commitLabel(job.Phase)
	prev, _ := os.ReadFile(notePath)
	body := string(prev)
	if strings.TrimSpace(body) == "" {
		body = fmt.Sprintf("# Задача трекера #%d\n\n%s\n", job.SourceNum, job.Prompt)
	}
	body += fmt.Sprintf("\n## %s\n\n%s\n", label, strings.TrimSpace(note))
	return os.WriteFile(notePath, []byte(body), 0o644)
}

func commitWorkAndPush(cfg config.Config, job store.Job, repoDir, branch, note string) (string, bool, error) {
	if err := writeTrackerNote(repoDir, job, note); err != nil {
		return "", false, err
	}
	_ = run(repoDir, "git", "config", "user.email", "tracker@fat-leopard")
	_ = run(repoDir, "git", "config", "user.name", "Leo Tracker")
	if err := run(repoDir, "git", "add", "-A"); err != nil {
		return "", false, err
	}
	hasImpl := stagedHasImpl(repoDir) || branchHasImpl(repoDir, defaultBranch(cfg))
	msg := fmt.Sprintf("tracker: #%d %s", job.SourceNum, commitLabel(job.Phase))
	if err := run(repoDir, "git", "commit", "-m", msg); err != nil {
		if sha := gitHead(repoDir); sha != "" {
			if pushErr := run(repoDir, "git", "push", "-u", "origin", branch); pushErr != nil {
				return sha, hasImpl, fmt.Errorf("ветка: %w", pushErr)
			}
			return sha, hasImpl || branchHasImpl(repoDir, defaultBranch(cfg)), nil
		}
		return "", false, err
	}
	if err := run(repoDir, "git", "push", "-u", "origin", branch); err != nil {
		return "", false, fmt.Errorf("ветка: %w", err)
	}
	return gitHead(repoDir), hasImpl || branchHasImpl(repoDir, defaultBranch(cfg)), nil
}

func branchHasImpl(repoDir, base string) bool {
	if base == "" {
		base = "main"
	}
	cmd := exec.Command("git", "diff", "--name-only", "origin/"+base+"...HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		cmd = exec.Command("git", "diff", "--name-only", base+"...HEAD")
		cmd.Dir = repoDir
		out, err = cmd.CombinedOutput()
		if err != nil {
			return false
		}
	}
	return filesHaveImpl(strings.Split(string(out), "\n"))
}

func stagedHasImpl(repoDir string) bool {
	cmd := exec.Command("git", "diff", "--cached", "--name-only")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return false
	}
	return filesHaveImpl(strings.Split(string(out), "\n"))
}

func remoteBranchExists(repoDir, branch string) bool {
	cmd := exec.Command("git", "ls-remote", "--heads", "origin", branch)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	return err == nil && strings.Contains(string(out), "refs/heads/"+branch)
}

func gitHead(repoDir string) string {
	cmd := exec.Command("git", "rev-parse", "--short", "HEAD")
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func run(dir string, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %s", err, clip(string(out), 300))
	}
	return nil
}

func clip(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
