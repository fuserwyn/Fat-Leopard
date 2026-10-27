package bot

import (
	"strings"
	"testing"
)

func TestIsTelegramUsernameToken(t *testing.T) {
	t.Parallel()
	for tok, want := range map[string]bool{
		"vasya":                 true,
		"petya_fit":             true,
		"a1234":                 true,
		"abc":                   false, // короче 5 символов
		"1vasya":                false,
		"аня":                   false,
		"vasya-pupk":            false,
		strings.Repeat("a", 33): false,
	} {
		if got := isTelegramUsernameToken(tok); got != want {
			t.Errorf("%q: %v, ждали %v", tok, got, want)
		}
	}
}

func TestJointTrainingTexts(t *testing.T) {
	t.Parallel()
	if jointTrainingVerb("m") != "отметил" || jointTrainingVerb("F") != "отметила" || jointTrainingVerb("") != "отметил(а)" {
		t.Fatal("глагол по полу")
	}
	if s := jointTrainingMemberText("@vasya", "m", "бег"); !strings.HasPrefix(s, "🤝 @vasya отметил тебя на совместной тренировке.") || !strings.Contains(s, "«бег»") {
		t.Fatalf("участнику: %q", s)
	}
	if s := jointTrainingInviteText("Аня", "f", "йога"); !strings.Contains(s, "Аня отметила тебя на совместной тренировке в стае") {
		t.Fatalf("инвайт: %q", s)
	}
	if s := jointTrainingForwardText("petya", "https://t.me/x?start=ref-1"); !strings.Contains(s, "@petya") || !strings.HasSuffix(s, "https://t.me/x?start=ref-1") {
		t.Fatalf("пересылка: %q", s)
	}
	var nilBot *Bot
	nilBot.notifyJointTrainingMentions(1, 1, "x", "@vasya")
}
