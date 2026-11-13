package bot

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestWorkoutReminderText_ShortSingleLine(t *testing.T) {
	const wantBody = "don't forget to log today's workout in the mini app"

	got := workoutReminderText("")
	if got != "🐆 "+wantBody {
		t.Fatalf("without username = %q, want %q", got, "🐆 "+wantBody)
	}
	if strings.Contains(got, "\n") {
		t.Fatalf("expected single line, got %q", got)
	}
	if strings.HasSuffix(got, "!") {
		t.Fatalf("expected no exclamation mark, got %q", got)
	}
	for _, banned := range []string{"streak", "Open the mini app", "💪"} {
		if strings.Contains(got, banned) {
			t.Fatalf("unexpected %q in %q", banned, got)
		}
	}
}

func TestWorkoutReminderText_WithUsername(t *testing.T) {
	got := workoutReminderText("mike")
	want := "🐆 @mike, don't forget to log today's workout in the mini app"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWorkoutReminderText_FitsTelegramPreview(t *testing.T) {
	longUser := strings.Repeat("a", 30)
	got := workoutReminderText(longUser)
	if n := utf8.RuneCountInString(got); n > 120 {
		t.Fatalf("reminder too long for a short DM: %d runes in %q", n, got)
	}
}
