package bot

import (
	"strings"
	"testing"
	"unicode/utf8"

	"leo-bot/internal/i18n"
)

func TestWorkoutReminderText_ShortSingleLine(t *testing.T) {
	const wantBody = "don't forget to log today's workout in the mini app"

	got := workoutReminderText(i18n.EN, "")
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
	got := workoutReminderText(i18n.EN, "mike")
	want := "🐆 @mike, don't forget to log today's workout in the mini app"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWorkoutReminderText_FitsTelegramPreview(t *testing.T) {
	longUser := strings.Repeat("a", 30)
	got := workoutReminderText(i18n.EN, longUser)
	if n := utf8.RuneCountInString(got); n > 120 {
		t.Fatalf("reminder too long for a short DM: %d runes in %q", n, got)
	}
	gotRU := workoutReminderText(i18n.RU, longUser)
	if n := utf8.RuneCountInString(gotRU); n > 120 {
		t.Fatalf("ru reminder too long for a short DM: %d runes in %q", n, gotRU)
	}
}

func TestWorkoutReminderText_Russian(t *testing.T) {
	got := workoutReminderText(i18n.RU, "i_ann_ann")
	want := "🐆 @i_ann_ann, не забудь записать сегодняшнюю тренировку в мини-аппе"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
	if strings.Contains(got, "don't forget") || strings.Contains(got, "\n") || strings.HasSuffix(got, "!") {
		t.Fatalf("unexpected shape: %q", got)
	}
}

func TestWorkoutReminderText_UnknownLangStaysEnglish(t *testing.T) {
	got := workoutReminderText("", "mike")
	want := "🐆 @mike, don't forget to log today's workout in the mini app"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestWorkoutReminderText_FitsTelegramPreviewRULength(t *testing.T) {
	longUser := strings.Repeat("я", 30)
	got := workoutReminderText(i18n.RU, longUser)
	if n := utf8.RuneCountInString(got); n > 120 {
		t.Fatalf("reminder too long for a short DM: %d runes in %q", n, got)
	}
}
