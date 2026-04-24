package bot

import (
	"strings"
	"testing"
)

func TestLeopardOnboardingBody_KeySections(t *testing.T) {
	s := leopardOnboardingBody()
	checks := []string{
		"Fat Leopard",
		"#training_done",
		"42 XP",
		"+6 XP",
		"−6 XP",
		"День 8",
		"АЧИВКИ",
		"210 ₽",
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
