package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/i18n"
	"leo-bot/internal/moderation"
)

func TestWelcomeStartTextLang(t *testing.T) {
	if got := welcomeStartTextLang(i18n.EN); got != leopardOnboardingBody() {
		t.Fatal("en: onboarding must stay English")
	}
	ru := welcomeStartTextLang(i18n.RU)
	for _, want := range []string{"Добро пожаловать в стаю", "КАК ОТМЕТИТЬ ТРЕНИРОВКУ", "42 ₽ за 7 дней", "отметь тренировку в мини-аппе"} {
		if !strings.Contains(ru, want) {
			t.Fatalf("ru onboarding missing %q", want)
		}
	}
}

func TestCupsCommandTextLang(t *testing.T) {
	var b Bot
	forms := b.getGenderForms("")
	cases := []struct {
		lang         i18n.Lang
		cups         int
		gender, want string
	}{
		{i18n.RU, 10, "", "Ваши кубки"},
		{i18n.RU, 420, "f", "ТЫ ДОСТИГЛА ЦЕЛИ РОЗЫГРЫША"},
		{i18n.RU, 420, "m", "Ты настоящий чемпион"},
		{i18n.RU, 500, "", "СУПЕР-УРОВЕНЬ"},
		{i18n.EN, 10, "", "Your cups"},
		{i18n.EN, 420, "", "THE RAFFLE GOAL"},
		{i18n.EN, 500, "", "SUPER LEVEL"},
	}
	for _, c := range cases {
		got := cupsCommandText(c.lang, "@leo", c.cups, c.gender, forms)
		if !strings.Contains(got, c.want) || !strings.Contains(got, "@leo") {
			t.Errorf("%s/%d: %q", c.lang, c.cups, got)
		}
	}
}

func TestSupportTextsLang(t *testing.T) {
	if !strings.Contains(botSupportPromptText(i18n.RU), "Выйти из поддержки") {
		t.Fatal("ru support prompt")
	}
	if !strings.Contains(botSupportPromptText(i18n.EN), "Exit support") {
		t.Fatal("en support prompt")
	}
	if supportUnavailableText(i18n.RU) == supportUnavailableText(i18n.EN) {
		t.Fatal("support unavailable must differ by lang")
	}
	for _, s := range []string{"💬 Поддержка", "💬 Support", "поддержка", "support"} {
		if !isSupportReplyButtonText(s) {
			t.Errorf("support button %q not recognised", s)
		}
	}
	kb := (&Bot{}).botSupportPromptInlineKeyboard(i18n.RU)
	if kb.InlineKeyboard[0][0].Text != "❎ Выйти из поддержки" {
		t.Fatalf("ru exit button: %q", kb.InlineKeyboard[0][0].Text)
	}
}

func TestModerationWarningLang(t *testing.T) {
	link := moderation.Result{Reason: moderation.ReasonLink, UserMessage: moderation.UserWarnings[moderation.ReasonLink]}
	if got := moderationWarningLang(i18n.EN, link); got != link.UserMessage {
		t.Fatalf("en: %q", got)
	}
	if got := moderationWarningLang(i18n.RU, link); !strings.Contains(got, "ссылки") {
		t.Fatalf("ru link: %q", got)
	}
	long := moderation.Result{Reason: moderation.ReasonTooLong, UserMessage: "Too long — shorten to 500 characters."}
	if got := moderationWarningLang(i18n.RU, long); got != "Слишком длинно — сократи до 500 символов." {
		t.Fatalf("ru too long: %q", got)
	}
	if got := moderationWarningLang(i18n.RU, moderation.Result{Reason: moderation.ReasonTooLong}); got != "Слишком длинно — сократи текст." {
		t.Fatalf("ru too long no digits: %q", got)
	}
	if got := moderationWarningLang(i18n.RU, moderation.Result{Reason: "other", UserMessage: "x"}); !strings.Contains(got, "не прошёл проверку") {
		t.Fatalf("ru fallback: %q", got)
	}
}

func TestDailyWisdomFallbackRU(t *testing.T) {
	if len(dailyWisdomFallbackRU) == 0 {
		t.Fatal("need RU fallbacks")
	}
	for _, w := range dailyWisdomFallbackRU {
		if !strings.ContainsAny(w, "абвгдеёжзийклмнопрстуфхцчшщыьэюя") {
			t.Fatalf("not russian: %q", w)
		}
	}
}
