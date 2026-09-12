package bot

import "testing"

func TestCalendarDaysInclusiveFromJoin(t *testing.T) {
	tests := []struct {
		join, today string
		want        int
	}{
		{"2026-09-12", "2026-09-12", 1},
		{"2026-09-10", "2026-09-12", 3},
		{"2026-09-12", "2026-09-11", 2},
	}
	for _, tc := range tests {
		if got := calendarDaysInclusiveFromJoin(tc.join, tc.today); got != tc.want {
			t.Errorf("calendarDaysInclusiveFromJoin(%q, %q) = %d, want %d", tc.join, tc.today, got, tc.want)
		}
	}
}
