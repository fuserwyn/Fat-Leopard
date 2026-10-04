package bot

import (
	"strings"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestContactCardDisplayName(t *testing.T) {
	if got := contactCardDisplayName(nil); got != "" {
		t.Fatalf("nil: %q", got)
	}
	if got := contactCardDisplayName(&tgbotapi.Contact{FirstName: " Аня ", LastName: "Смирнова"}); got != "Аня Смирнова" {
		t.Fatalf("full: %q", got)
	}
	if got := contactCardDisplayName(&tgbotapi.Contact{FirstName: "Аня"}); got != "Аня" {
		t.Fatalf("first only: %q", got)
	}
}

func TestSanitizeContactJoinedPackText(t *testing.T) {
	if got := sanitizeContactJoinedPackText("  «Аня теперь в стае»  "); got != "Аня теперь в стае" {
		t.Fatalf("quotes: %q", got)
	}
	long := strings.Repeat("я", contactJoinedPackMaxLen+50)
	got := sanitizeContactJoinedPackText(long)
	if n := len([]rune(got)); n != contactJoinedPackMaxLen+1 {
		t.Fatalf("truncate: %d runes", n)
	}
}

func TestContactJoinedPackFallbackText(t *testing.T) {
	if got := contactJoinedPackFallbackText("Аня"); !strings.HasPrefix(got, "Аня теперь в стае") {
		t.Fatalf("named: %q", got)
	}
	if got := contactJoinedPackFallbackText(" "); !strings.HasPrefix(got, "Твой контакт") {
		t.Fatalf("empty: %q", got)
	}
}

func TestHandleSharedContactIgnoresNonContact(t *testing.T) {
	var b *Bot
	if b.handleSharedContact(&tgbotapi.Message{}) {
		t.Fatal("nil bot must not handle")
	}
	b = &Bot{}
	if b.handleSharedContact(&tgbotapi.Message{Text: "привет"}) {
		t.Fatal("message without contact must not be handled")
	}
}
