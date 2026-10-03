package bot

import (
	"strings"
	"testing"
)

func TestPackWeeklyWorkoutGoalForWeek(t *testing.T) {
	cases := map[string]int{
		"2026-09-28": 50,
		"2026-10-05": 75,
		"2026-10-12": 75,
		"":           75,
	}
	for weekStart, want := range cases {
		if got := PackWeeklyWorkoutGoalForWeek(weekStart); got != want {
			t.Errorf("PackWeeklyWorkoutGoalForWeek(%q) = %d, want %d", weekStart, got, want)
		}
	}
}

func TestPackWeeklyGoalAchievedFeedMessageUsesGoal(t *testing.T) {
	if msg := packWeeklyGoalAchievedFeedMessage(75); !strings.Contains(msg, "75 тренировок") {
		t.Fatalf("unexpected message: %s", msg)
	}
}
