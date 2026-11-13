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

	checks := []string{
		"Welcome to the pack, Fat Leopard",
		"⚡️ HOW TO LOG A WORKOUT",
		"mini app",
		"🏆 CUPS AND STREAK",
		"earns cups by formula",
		"⏰ IF YOU SKIP DAYS",
		"Day 5 without a workout",
		"Day 6",
		"Day 7 — cups reset",
		"Day 8 — removed from the pack",
		"/start in DM",
		"🏆 ACHIEVEMENTS",
		"100 days",
		"Freeze",
		"❄️ PAID FREEZE",
		"42 ₽ for 7 days",
		"🔄 REJOIN THE PACK",
		"210 ₽",
		"🎯 Start now — log a workout in the mini app",
	}
	for _, c := range checks {
		if !strings.Contains(s, c) {
			t.Fatalf("onboarding body missing %q\n---\n%s\n---", c, s)
		}
	}
	if strings.Contains(s, "#training_done") {
		t.Fatal("onboarding must not mention #training_done")
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
