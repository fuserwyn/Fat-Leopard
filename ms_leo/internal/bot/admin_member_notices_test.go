package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/i18n"
)

func TestAdminMemberNoticesFollowMemberLanguage(t *testing.T) {
	if got := adminSickSetNotice(i18n.RU, true); !strings.Contains(got, "больничный") || !strings.Contains(got, "#healthy") {
		t.Fatalf("ru sick set with chat hint: %q", got)
	}
	if got := adminSickSetNotice(i18n.RU, false); strings.Contains(got, "#healthy") {
		t.Fatalf("ru sick set without chat hint: %q", got)
	}
	if got := adminSickSetNotice(i18n.EN, true); !strings.Contains(got, "sick leave") || !strings.Contains(got, "#healthy") {
		t.Fatalf("en sick set with chat hint: %q", got)
	}
	if got := adminSickSetNotice("", false); !strings.Contains(got, "An admin") || strings.Contains(got, "#healthy") {
		t.Fatalf("default sick set: %q", got)
	}
	if got := adminSickCancelNotice(i18n.RU); !strings.Contains(got, "Больничный завершён") {
		t.Fatalf("ru sick cancel: %q", got)
	}
	if got := adminSickCancelNotice(i18n.EN); !strings.Contains(got, "Sick leave ended") {
		t.Fatalf("en sick cancel: %q", got)
	}
	if got := adminUGCMuteNotice(i18n.RU, 24); !strings.Contains(got, "24 ч") {
		t.Fatalf("ru mute: %q", got)
	}
	if got := adminUGCMuteNotice(i18n.EN, 12); !strings.Contains(got, "12 h") {
		t.Fatalf("en mute: %q", got)
	}
	if got := adminUGCUnmuteNotice(i18n.RU); !strings.Contains(got, "сняты") {
		t.Fatalf("ru unmute: %q", got)
	}
	if got := adminUGCUnmuteNotice(i18n.EN); !strings.Contains(got, "lifted") {
		t.Fatalf("en unmute: %q", got)
	}
}
