package bot

import (
	"testing"
	"time"
)

// Регрессия: после кика за неактивность из платной группы ban должен быть бессрочным
// (UntilDate = 0). Раньше ставили временный 40-секундный бан и сразу же делали unban —
// в этот зазор Telegram продолжал считать старую инвайт-ссылку валидной, и человек заходил
// в группу без оплаты до того, как сработают paywall-проверки.
func TestPaywallPermanentKickBanConfig_ForeverNoRevoke(t *testing.T) {
	const chatID = int64(-1009000)
	const userID = int64(7777)

	cfg := paywallPermanentKickBanConfig(chatID, userID)

	if cfg.ChatID != chatID {
		t.Fatalf("ChatID=%d, want %d", cfg.ChatID, chatID)
	}
	if cfg.UserID != userID {
		t.Fatalf("UserID=%d, want %d", cfg.UserID, userID)
	}
	if cfg.UntilDate != 0 {
		t.Fatalf("UntilDate=%d (must be 0 = forever, иначе протухший инвайт снова заработает у Telegram)", cfg.UntilDate)
	}
	if cfg.RevokeMessages {
		t.Fatal("RevokeMessages должен быть false — мы только удаляем из чата, а не чистим историю стаи")
	}
}

// Telegram считает банами «forever» любой until_date, отстоящий от now больше чем на 366 дней
// или меньше чем на 30 секунд. UntilDate=0 явно гарантирует это вне зависимости от now.
// Тест нужен, чтобы будущая правка случайно не вернула короткое значение (например, +40s).
func TestPaywallPermanentKickBanConfig_NotShortLivedBan(t *testing.T) {
	cfg := paywallPermanentKickBanConfig(1, 2)
	if cfg.UntilDate != 0 {
		t.Fatalf("UntilDate=%d не равен 0 — значит снова появилась лазейка после кика", cfg.UntilDate)
	}

	// Контрольная проверка: даже если кто-то поставит «короткий» until_date,
	// он должен укладываться в правило Telegram «<30s или >366d ⇒ forever». Здесь явно фейлим
	// любую попытку сделать средне-длинный (например, 30-дневный) бан в платной группе.
	now := time.Now()
	if cfg.UntilDate != 0 {
		dt := time.Unix(cfg.UntilDate, 0).Sub(now)
		if dt > 30*time.Second && dt < 366*24*time.Hour {
			t.Fatalf("UntilDate выставляет средний срок (%v) — Telegram трактует это как «временный бан», человек снова войдёт по старой ссылке после разблокировки", dt)
		}
	}
}

// 30-дневный бан используется только в обычных (бесплатных) чатах — это исторически сложившееся
// поведение. Зафиксируем точное окно, чтобы поломки тут были заметны.
func TestLegacyKick30dBanConfig_ThirtyDayWindow(t *testing.T) {
	const chatID = int64(-100123)
	const userID = int64(5555)
	now := time.Date(2026, 4, 25, 10, 0, 0, 0, time.UTC)

	cfg := legacyKick30dBanConfig(chatID, userID, now)

	if cfg.ChatID != chatID || cfg.UserID != userID {
		t.Fatalf("Chat/User mismatch: got %d/%d, want %d/%d", cfg.ChatID, cfg.UserID, chatID, userID)
	}
	want := now.Add(30 * 24 * time.Hour).Unix()
	if cfg.UntilDate != want {
		t.Fatalf("UntilDate=%d, want %d (now + 30d)", cfg.UntilDate, want)
	}
}

// Контрактный тест: конфиги бана для платной и бесплатной группы должны отличаться по UntilDate.
// Если кто-то по ошибке унифицирует их — этот тест упадёт и заставит подумать о последствиях.
func TestKickBanConfigs_PaywallVsLegacyDiffer(t *testing.T) {
	now := time.Now()
	pay := paywallPermanentKickBanConfig(1, 2)
	legacy := legacyKick30dBanConfig(1, 2, now)

	if pay.UntilDate == legacy.UntilDate {
		t.Fatal("paywall и legacy кик-бан стали одинаковыми — нарушено разделение поведения")
	}
	if pay.UntilDate != 0 {
		t.Fatalf("paywall ban должен быть бессрочным (0), а сейчас %d", pay.UntilDate)
	}
	if legacy.UntilDate <= now.Unix() {
		t.Fatalf("legacy ban должен быть в будущем, а сейчас %d <= now %d", legacy.UntilDate, now.Unix())
	}
}
