package bot

import (
	"testing"

	"leo-bot/internal/i18n"
)

func TestPickLang(t *testing.T) {
	if got := pickLang(i18n.RU, "привет", "hi"); got != "привет" {
		t.Fatalf("ru: got %q", got)
	}
	if got := pickLang(i18n.EN, "привет", "hi"); got != "hi" {
		t.Fatalf("en: got %q", got)
	}
	if got := pickLang("", "привет", "hi"); got != "hi" {
		t.Fatalf("empty: got %q", got)
	}
	var b *Bot
	if got := b.userText(1, "привет", "hi"); got != "hi" {
		t.Fatalf("nil bot: got %q", got)
	}
}
