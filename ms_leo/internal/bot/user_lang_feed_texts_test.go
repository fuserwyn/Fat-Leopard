package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/i18n"
)

func TestTrainingDoneStatsTextLang(t *testing.T) {
	ru := trainingDoneStatsText(i18n.RU, 2, 5, 21)
	for _, want := range []string{"Отчёт принят", "Стрик: 2 дня", "+5 кубков", "всего: 21"} {
		if !strings.Contains(ru, want) {
			t.Fatalf("ru %q missing %q", ru, want)
		}
	}
	en := trainingDoneStatsText(i18n.EN, 1, 1, 1)
	for _, want := range []string{"Report accepted", "Streak: 1 day", "+1 cup", "total: 1"} {
		if !strings.Contains(en, want) {
			t.Fatalf("en %q missing %q", en, want)
		}
	}
	if !strings.Contains(trainingDoneInactiveText(i18n.RU), "Неактивность") {
		t.Fatal("ru inactive block")
	}
	if !strings.Contains(trainingDoneInactiveText(i18n.EN), "Inactivity") {
		t.Fatal("en inactive block")
	}
	if ruCupsWord(1) != "кубок" || ruCupsWord(3) != "кубка" || ruCupsWord(11) != "кубков" {
		t.Fatal("ruCupsWord")
	}
	if ruDaysWord(1) != "день" || ruDaysWord(4) != "дня" || ruDaysWord(5) != "дней" {
		t.Fatal("ruDaysWord")
	}
}

func TestPackChatReplyDMTextLang(t *testing.T) {
	if got := leoPackChatReplyDMText(i18n.RU, "ок"); !strings.HasPrefix(got, "🦁 Лео ответил в чате стаи.") || !strings.Contains(got, "«ок»") {
		t.Fatalf("ru leo: %q", got)
	}
	if got := leoPackChatReplyDMText(i18n.EN, "ok"); !strings.HasPrefix(got, "🦁 Leo replied in the pack chat.") {
		t.Fatalf("en leo: %q", got)
	}
	if got := packChatReplyDMText(i18n.RU, "Аня", "f", "п"); !strings.HasPrefix(got, "↩️ Аня ответила на твоё сообщение") {
		t.Fatalf("ru f: %q", got)
	}
	if got := packChatReplyDMText(i18n.RU, "Ваня", "m", "п"); !strings.HasPrefix(got, "↩️ Ваня ответил на твоё сообщение") {
		t.Fatalf("ru m: %q", got)
	}
	if got := packChatReplyDMText(i18n.RU, "Кто-то", "", "п"); !strings.HasPrefix(got, "↩️ Ответ от Кто-то") {
		t.Fatalf("ru neutral: %q", got)
	}
	if got := packChatReplyDMText(i18n.EN, "Ann", "f", "p"); !strings.HasPrefix(got, "↩️ Ann replied to your message") {
		t.Fatalf("en: %q", got)
	}
}

func TestPackJoinTextsLang(t *testing.T) {
	if got := packJoinMiniappFeedPublicText("Аня"); got != "В стае новый участник: Аня." {
		t.Fatalf("feed: %q", got)
	}
	if got := packJoinMiniappFeedPublicText(" "); got != "В стае новый участник." {
		t.Fatalf("feed empty: %q", got)
	}
	if !strings.HasPrefix(packRejoinMiniappFeedPublicText(), "Участник вернулся") {
		t.Fatal("rejoin feed")
	}
	if got := packJoinMiniappWelcomeText(i18n.RU, ""); !strings.HasPrefix(got, "Привет, участник! Я Лео") {
		t.Fatalf("ru welcome: %q", got)
	}
	if got := packJoinMiniappWelcomeText(i18n.EN, "Ann"); !strings.HasPrefix(got, "Hi, Ann! I'm Leo") {
		t.Fatalf("en welcome: %q", got)
	}
	if got := packRejoinMiniappWelcomeText(i18n.RU, "Аня"); !strings.HasPrefix(got, "Аня, с возвращением!") {
		t.Fatalf("ru rejoin: %q", got)
	}
	if got := packRejoinMiniappWelcomeText(i18n.EN, ""); !strings.HasPrefix(got, "pack member, welcome back!") {
		t.Fatalf("en rejoin: %q", got)
	}
}
