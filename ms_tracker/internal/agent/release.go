package agent

import (
	"context"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// releaseEnv включает уборку: задан только в образе (см. Dockerfile), чтобы
// локальный запуск и тесты не стёрли кэши Go на машине разработчика.
const releaseEnv = "LEO_TRACKER_RELEASE"

// ReleaseAfterJobs освобождает то, что задача оставила в контейнере: Railway
// считает память контейнера, а между задачами трекер простаивает. Временный
// Postgres живёт своим демоном, кэши сборки растут от задачи к задаче. Следующая
// задача соберётся чуть дольше, но память дороже CPU на порядок.
// Звать, только когда ни одна задача не выполняется.
func ReleaseAfterJobs() {
	if os.Getenv(releaseEnv) != "1" {
		return
	}
	before := containerMemory()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if _, err := exec.LookPath("leo-test-pg"); err == nil {
		if err := exec.CommandContext(ctx, "leo-test-pg", "stop").Run(); err != nil {
			log.Printf("трекер: не остановить временный Postgres: %v", err)
		}
	}
	if _, err := exec.LookPath("go"); err == nil {
		if err := exec.CommandContext(ctx, "go", "clean", "-cache", "-testcache", "-modcache").Run(); err != nil {
			log.Printf("трекер: не очистить кэш Go: %v", err)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		_ = os.RemoveAll(filepath.Join(home, ".npm"))
	}
	for _, dir := range releaseLeftovers(os.TempDir()) {
		_ = os.RemoveAll(dir)
	}
	log.Printf("трекер: память контейнера до уборки: %s; после: %s", before, containerMemory())
}

// releaseLeftovers — что осталось от оборванных сборок. Каталоги leo-tracker-ask-*
// не трогаем: вопросы к агенту идут параллельно с очередью.
func releaseLeftovers(tmp string) []string {
	var out []string
	for _, pattern := range []string{"go-build*", "leo-tracker-doing-*", "leo-tracker-test-*"} {
		found, err := filepath.Glob(filepath.Join(tmp, pattern))
		if err == nil {
			out = append(out, found...)
		}
	}
	return out
}

// containerMemory — память cgroup: anon (процессы) и file (кэш файлов).
func containerMemory() string {
	raw, err := os.ReadFile("/sys/fs/cgroup/memory.stat")
	if err != nil {
		return "нет данных"
	}
	return memoryStatSummary(string(raw))
}

func memoryStatSummary(stat string) string {
	var parts []string
	for _, line := range strings.Split(stat, "\n") {
		key, value, ok := strings.Cut(strings.TrimSpace(line), " ")
		if ok && (key == "anon" || key == "file") {
			parts = append(parts, key+"="+value)
		}
	}
	if len(parts) == 0 {
		return "нет данных"
	}
	return strings.Join(parts, " ")
}
