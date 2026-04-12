// Package leopardmoney — правила Leopard Money Model (XP, ачивки, таймер неактивности).
package leopardmoney

import "time"

const (
	// EntryRub — вход в группу (руб.), для env / документации.
	EntryRub = 420
	// ReturnRub — возврат после удаления (руб.).
	ReturnRub = 210
	// FreezeRub — покупка заморозки XP (руб.).
	FreezeRub = 42

	XPPerActiveDay   = 6
	XPPerInactiveDay = 6

	// MaxAchievements — максимум ачивок за серии по 7 дней (28 дней).
	MaxAchievements = 4

	StarterXP               = 42
	StarterAchievementTitle = "Первый след"

	// InactiveRemovalDays — дней без отчёта до удаления (день 8).
	InactiveRemovalDays = 8

	// FullTimerDuration — окно с момента последнего отчёта до удаления.
	FullTimerDuration = time.Duration(InactiveRemovalDays) * 24 * time.Hour
)

// MilestoneOffset — задержка от момента последнего #training_done до события (дни 5–8).
func MilestoneOffset(day int) time.Duration {
	if day < 5 || day > InactiveRemovalDays {
		return 0
	}
	return time.Duration(day) * 24 * time.Hour
}
