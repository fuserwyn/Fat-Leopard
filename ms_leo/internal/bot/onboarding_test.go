package bot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestLeopardOnboardingBody_KeySections(t *testing.T) {
	s := leopardOnboardingBody()
	if s != leopardOnboardingBodyText {
		t.Fatal("leopardOnboardingBody() must return leopardOnboardingBodyText")
	}
	if strings.TrimSpace(s) != s {
		t.Fatal("onboarding body must not have leading/trailing whitespace")
	}
	if utf8.RuneCountInString(s) < 200 {
		t.Fatalf("onboarding body unexpectedly short (%d runes)", utf8.RuneCountInString(s))
	}

	// Фразы из актуального онбординга (при правке текста в bot.go обнови список).
	checks := []string{
		"Добро пожаловать в стаю, Fat Leopard",
		"⚡️ КАК ОТМЕТИТЬ ТРЕНИРОВКУ",
		"#training_done",
		"🔥 КАК РАБОТАЕТ XP",
		"Стартовый баланс — 42 XP",
		"+6 XP",
		"−6 XP",
		"⏰ ЧТО БУДЕТ, ЕСЛИ ПРОПУСКАТЬ",
		"День 5 без #training_done",
		"День 6",
		"День 7 — XP обнуляется",
		"День 8 — удаление из стаи",
		"/start в личке",
		"🏆 АЧИВКИ",
		"28 дней",
		"Заморозку",
		"❄️ ПЛАТНАЯ ЗАМОРОЗКА",
		"42 ₽ за 7 дней",
		"🔄 ВЕРНУТЬСЯ В СТАЮ",
		"210 ₽",
		"🎯 Начни прямо сейчас — отправь #training_done",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Fatalf("onboarding body missing %q\n---\n%s\n---", c, s)
		}
	}
}

func TestWelcomeStartTextUsesLeopardOnboarding(t *testing.T) {
	if welcomeStartText() != leopardOnboardingBody() {
		t.Fatal("welcomeStartText must equal leopardOnboardingBody()")
	}
}

func TestSendWelcomeMessage_OnboardingFormat(t *testing.T) {
	const username = "testuser"
	got := username + "\n\n" + leopardOnboardingBody()
	wantPrefix := username + "\n\n"
	if !strings.HasPrefix(got, wantPrefix) {
		preview := got
		if len(preview) > 120 {
			preview = preview[:120] + "..."
		}
		t.Fatalf("expected prefix %q, got %q", wantPrefix, preview)
	}
	if !strings.Contains(got, "Fat Leopard") {
		t.Fatal("welcome DM must include onboarding body")
	}
}
