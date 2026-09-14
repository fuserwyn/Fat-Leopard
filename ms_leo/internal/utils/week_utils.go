package utils

import "time"

// WeekStartSundayMSK — YYYY-MM-DD ближайшего воскресенья (включая сегодня) по Москве.
func WeekStartSundayMSK(now time.Time) string {
	m := now.In(moscowLocation)
	daysSinceSunday := int(m.Weekday()) // Sunday = 0
	start := m.AddDate(0, 0, -daysSinceSunday)
	return start.Format("2006-01-02")
}

// WeekEndSaturdayMSK — YYYY-MM-DD субботы текущей недели (воскресенье–суббота) по Москве.
func WeekEndSaturdayMSK(now time.Time) string {
	m := now.In(moscowLocation)
	daysUntilSaturday := int(time.Saturday - m.Weekday())
	if daysUntilSaturday < 0 {
		daysUntilSaturday += 7
	}
	end := m.AddDate(0, 0, daysUntilSaturday)
	return end.Format("2006-01-02")
}
