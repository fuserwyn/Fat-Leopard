package utils

import (
	"testing"
	"time"
)

func TestGetMoscowTime(t *testing.T) {
	moscowTime := GetMoscowTime()

	// Проверяем, что время не пустое
	if moscowTime.IsZero() {
		t.Error("Moscow time should not be zero")
	}

	// Проверяем, что время в московском часовом поясе
	expectedLocation := "Europe/Moscow"
	if moscowTime.Location().String() != expectedLocation {
		t.Errorf("Expected location %s, got %s", expectedLocation, moscowTime.Location().String())
	}
}

func TestFormatMoscowTime(t *testing.T) {
	now := time.Now()
	formatted := FormatMoscowTime(now)

	// Проверяем, что отформатированное время не пустое
	if formatted == "" {
		t.Error("Formatted time should not be empty")
	}

	// Проверяем, что можно распарсить обратно
	parsed, err := ParseMoscowTime(formatted)
	if err != nil {
		t.Errorf("Failed to parse formatted time: %v", err)
	}

	// Проверяем, что время в московском часовом поясе
	if parsed.Location().String() != "Europe/Moscow" {
		t.Errorf("Parsed time should be in Moscow timezone, got %s", parsed.Location().String())
	}
}

func TestGetMoscowDate(t *testing.T) {
	date := GetMoscowDate()

	// Проверяем формат даты (YYYY-MM-DD)
	if len(date) != 10 {
		t.Errorf("Date should be 10 characters long, got %d: %s", len(date), date)
	}

	// Проверяем, что можно распарсить как дату
	_, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Errorf("Failed to parse date: %v", err)
	}
}

func TestGetMoscowDateFromTime(t *testing.T) {
	now := time.Now()
	date := GetMoscowDateFromTime(now)

	// Проверяем формат даты
	if len(date) != 10 {
		t.Errorf("Date should be 10 characters long, got %d: %s", len(date), date)
	}

	// Проверяем, что можно распарсить как дату
	_, err := time.Parse("2006-01-02", date)
	if err != nil {
		t.Errorf("Failed to parse date: %v", err)
	}
}

