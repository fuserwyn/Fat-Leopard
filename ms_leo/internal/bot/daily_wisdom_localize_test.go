package bot

import (
	"testing"
	"time"

	"leo-bot/internal/i18n"
)

func TestDailyWisdomDMTextLang(t *testing.T) {
	en := dailyWisdomDMText(i18n.EN, "alice", "Stay steady.")
	if en != "🐆 @alice, daily wisdom from Leo:\n\nStay steady." {
		t.Fatalf("en: %q", en)
	}
	ru := dailyWisdomDMText(i18n.RU, "alice", "Держи ритм.")
	if ru != "🐆 @alice, мудрость дня от Лео:\n\nДержи ритм." {
		t.Fatalf("ru: %q", ru)
	}
}

func TestLocalizeDailyWisdomNilSafe(t *testing.T) {
	var b *Bot
	got := b.localizeDailyWisdomForViewer(1, time.Now(), "hello")
	if got != "hello" {
		t.Fatalf("got %q", got)
	}
}
