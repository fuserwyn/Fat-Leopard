package leopardmoney

import (
	"testing"
	"time"
)

func TestSuggestWorkoutTypeOrder_prefersRecentHistory(t *testing.T) {
	now := time.Date(2026, 3, 10, 18, 0, 0, 0, time.UTC)
	sessions := []WorkoutSessionHint{
		{MessageText: "йога, 30 мин, инт. 2/5", CreatedAt: now.Add(-24 * time.Hour)},
		{MessageText: "йога, 30 мин, инт. 2/5", CreatedAt: now.Add(-48 * time.Hour)},
		{MessageText: "бег, 20 мин, инт. 3/5", CreatedAt: now.Add(-72 * time.Hour)},
	}
	got := SuggestWorkoutTypeOrder(sessions, now, 0, 1)
	if got[0] != "yoga" {
		t.Fatalf("first = %q, want yoga", got[0])
	}
	if indexOf(got, "yoga") > indexOf(got, "run") {
		t.Fatalf("yoga should rank above run: %v", got)
	}
}

func TestSuggestWorkoutTypeOrder_emptyHistoryUsesDefault(t *testing.T) {
	now := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	got := SuggestWorkoutTypeOrder(nil, now, 0, -1)
	if len(got) != len(DefaultWorkoutTypeOrder) {
		t.Fatalf("len = %d", len(got))
	}
	for i, id := range DefaultWorkoutTypeOrder {
		if got[i] != id {
			t.Fatalf("at %d got %q want %q", i, got[i], id)
		}
	}
}

func TestSuggestWorkoutTypeOrder_recoveryAfterBreak(t *testing.T) {
	now := time.Date(2026, 3, 10, 10, 0, 0, 0, time.UTC)
	sessions := []WorkoutSessionHint{
		{MessageText: "кроссфит, 45 мин, инт. 4/5", CreatedAt: now.Add(-240 * time.Hour)},
	}
	got := SuggestWorkoutTypeOrder(sessions, now, 0, 5)
	if indexOf(got, "stretch") >= indexOf(got, "crossfit") {
		t.Fatalf("recovery should boost stretch above crossfit: %v", got)
	}
}

func indexOf(list []string, id string) int {
	for i, v := range list {
		if v == id {
			return i
		}
	}
	return len(list)
}
