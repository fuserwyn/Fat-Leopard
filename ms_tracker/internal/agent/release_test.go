package agent

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMemoryStatSummary(t *testing.T) {
	got := memoryStatSummary("anon 100\nfile 200\nkernel 5\nfile_mapped 7\n")
	if got != "anon=100 file=200" {
		t.Errorf("got %q", got)
	}
	if got := memoryStatSummary(""); got != "нет данных" {
		t.Errorf("пусто: %q", got)
	}
}

func TestReleaseLeftoversKeepsAskDirs(t *testing.T) {
	tmp := t.TempDir()
	for _, name := range []string{"go-build1", "leo-tracker-doing-1", "leo-tracker-test-1", "leo-tracker-ask-1", "other"} {
		if err := os.Mkdir(filepath.Join(tmp, name), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := releaseLeftovers(tmp)
	if len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, dir := range got {
		if base := filepath.Base(dir); base == "leo-tracker-ask-1" || base == "other" {
			t.Errorf("лишний каталог: %s", base)
		}
	}
}

func TestReleaseAfterJobsOffWithoutEnv(t *testing.T) {
	t.Setenv(releaseEnv, "")
	home := t.TempDir()
	t.Setenv("HOME", home)
	npm := filepath.Join(home, ".npm")
	if err := os.Mkdir(npm, 0o755); err != nil {
		t.Fatal(err)
	}
	ReleaseAfterJobs()
	if _, err := os.Stat(npm); err != nil {
		t.Errorf("без %s ничего не удаляем: %v", releaseEnv, err)
	}
}
