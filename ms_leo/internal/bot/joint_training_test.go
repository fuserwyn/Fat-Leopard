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
	if jointTrainingVerb("m") != "tagged" || jointTrainingVerb("F") != "tagged" || jointTrainingVerb("") != "tagged" {
		t.Fatal("verb")
	}
	if s := jointTrainingMemberText("@vasya", "m", "run"); !strings.HasPrefix(s, "🤝 @vasya tagged you on a joint workout.") || !strings.Contains(s, "«run»") {
		t.Fatalf("member: %q", s)
	}
	if s := jointTrainingInviteText("Anna", "f", "yoga"); !strings.Contains(s, "Anna tagged you on a joint workout in the Fat Leopard pack") {
		t.Fatalf("invite: %q", s)
	}
	if s := jointTrainingForwardText("petya", "https://t.me/x?start=ref-1"); !strings.Contains(s, "@petya isn't in the pack yet") || strings.Contains(s, "@@") || !strings.HasSuffix(s, "https://t.me/x?start=ref-1") {
		t.Fatalf("forward: %q", s)
	}
	if s := jointTrainingForwardText("@masha", "https://t.me/x?start=ref-1"); strings.Contains(s, "@@") || !strings.Contains(s, "@masha isn't in the pack yet") {
		t.Fatalf("forward with @: %q", s)
	}
	if jointTrainingMentionLabel("vasya") != "@vasya" || jointTrainingMentionLabel("@Аня") != "@Аня" {
		t.Fatal("mention label")
	}
	var nilBot *Bot
	nilBot.notifyJointTrainingMentions(1, 1, "x", "@vasya")
}

func TestJointTrainingMentionToken(t *testing.T) {
	t.Parallel()
	for _, c := range []struct{ username, name, want string }{
		{"Vasya", "Вася Пупкин", "Vasya"},
		{"@petya_fit", "", "petya_fit"},
		{"", "Аня", "Аня"},
		{"", "Анна Петрова", ""},
		{"", "Аня!", ""},
		{"", "", ""},
		{"", strings.Repeat("я", 41), ""},
	} {
		if got := jointTrainingMentionToken(c.username, c.name); got != c.want {
			t.Errorf("jointTrainingMentionToken(%q, %q) = %q, want %q", c.username, c.name, got, c.want)
		}
	}
}
