package utils

import "time"

// WeekStartMondayMSK — YYYY-MM-DD ближайшего понедельника (включая сегодня) по Москве.
func WeekStartMondayMSK(now time.Time) string {
	m := now.In(moscowLocation)
	daysSinceMonday := (int(m.Weekday()) + 6) % 7 // Monday = 0, Sunday = 6
	start := m.AddDate(0, 0, -daysSinceMonday)
	return start.Format("2006-01-02")
}

// WeekEndSundayMSK — YYYY-MM-DD воскресенья текущей недели (понедельник–воскресенье) по Москве.
func WeekEndSundayMSK(now time.Time) string {
	m := now.In(moscowLocation)
	daysUntilSunday := int(time.Sunday - m.Weekday())
	if daysUntilSunday < 0 {
		daysUntilSunday += 7
	}
	end := m.AddDate(0, 0, daysUntilSunday)
	return end.Format("2006-01-02")
}
