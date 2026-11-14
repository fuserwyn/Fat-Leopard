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

func TestParsePackWeeklyWorkoutGoal(t *testing.T) {
	for _, bad := range []int{-5, 0, maxPackWeeklyWorkoutGoal + 1} {
		if _, err := parsePackWeeklyWorkoutGoal(bad); err == nil {
			t.Errorf("goal %d должен отклоняться", bad)
		}
	}
	if got, err := parsePackWeeklyWorkoutGoal(120); err != nil || got != 120 {
		t.Fatalf("120: %d %v", got, err)
	}
}

func TestPackWeeklyWorkoutGoalWithoutOverride(t *testing.T) {
	var b *Bot
	if got := b.packWeeklyWorkoutGoal(1, "2026-10-05"); got != PackWeeklyWorkoutGoal {
		t.Fatalf("без базы — цель по умолчанию, got %d", got)
	}
}
