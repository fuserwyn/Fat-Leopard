package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/i18n"
)

func TestSocialNotifyRU(t *testing.T) {
	if s := feedLikeText(i18n.RU, "✨", "@ann", "f", "post"); !strings.HasPrefix(s, "✨ @ann лайкнула твой отчёт о тренировке в стае.") || !strings.Contains(s, "«Стая»") {
		t.Fatal(s)
	}
	if s := friendWorkoutText(i18n.RU, "@leo", "m", 158); !strings.HasPrefix(s, "🦁 @leo записал тренировку! 🔥 Серия: 158.") {
		t.Fatal(s)
	}
	if s := friendWorkoutText(i18n.EN, "@leo", "m", 158); !strings.HasPrefix(s, "🦁 @leo logged a workout! 🔥 Streak: 158.") {
		t.Fatal(s)
	}
	if s := threadCommentText(i18n.RU, "Вася", "", "", "ок"); !strings.HasPrefix(s, "💬 Вася прокомментировал(а) твою тренировку") {
		t.Fatal(s)
	}
}
