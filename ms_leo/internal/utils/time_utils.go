package utils

import (
	"time"
)

var moscowLocation *time.Location

func init() {
	var err error
	moscowLocation, err = time.LoadLocation("Europe/Moscow")
	if err != nil {
		// Fallback на UTC+3 если не удалось загрузить локацию
		moscowLocation = time.FixedZone("MSK", 3*60*60)
	}
}

// GetMoscowTime возвращает текущее время в московском часовом поясе
func GetMoscowTime() time.Time {
	return time.Now().In(moscowLocation)
}

// FormatMoscowTime форматирует время в московском часовом поясе в строку RFC3339
func FormatMoscowTime(t time.Time) string {
	return t.In(moscowLocation).Format(time.RFC3339)
}

// ParseMoscowTime парсит строку времени и возвращает время в московском часовом поясе
func ParseMoscowTime(timeStr string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, timeStr)
	if err != nil {
		return time.Time{}, err
	}
	return t.In(moscowLocation), nil
}

// GetMoscowDate возвращает текущую дату в московском часовом поясе в формате YYYY-MM-DD
func GetMoscowDate() string {
	return GetMoscowTime().Format("2006-01-02")
}

// GetMoscowDateFromTime возвращает дату из времени в московском часовом поясе в формате YYYY-MM-DD
func GetMoscowDateFromTime(t time.Time) string {
	return t.In(moscowLocation).Format("2006-01-02")
}

