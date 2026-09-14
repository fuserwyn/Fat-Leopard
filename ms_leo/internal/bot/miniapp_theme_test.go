package bot

import "testing"

func TestNormalizeMiniappTheme(t *testing.T) {
	got, ok := NormalizeMiniappTheme("leopard", 5)
	if !ok || got != "leopard" {
		t.Fatalf("leo: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappTheme("leopard", 4)
	if !ok || got != "dark" {
		t.Fatalf("lock: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappTheme("light", 1)
	if !ok || got != "light" {
		t.Fatalf("light: %q %v", got, ok)
	}
	if _, ok = NormalizeMiniappTheme("neon", 6); ok {
		t.Fatal("invalid must fail")
	}
}

func TestNormalizeMiniappThemeAccessWild(t *testing.T) {
	got, ok := NormalizeMiniappThemeAccess("wild", MiniappThemeAccess{Level: 1})
	if !ok || got != "dark" {
		t.Fatalf("lock: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappThemeAccess("wild", MiniappThemeAccess{StreakDays: 365})
	if !ok || got != "wild" {
		t.Fatalf("streak: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappThemeAccess("wild", MiniappThemeAccess{MaxStreakDays: 400})
	if !ok || got != "wild" {
		t.Fatalf("record: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappThemeAccess("wild", MiniappThemeAccess{IsAdmin: true})
	if !ok || got != "wild" {
		t.Fatalf("admin: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappThemeAccess("wild", MiniappThemeAccess{StreakDays: 364})
	if !ok || got != "dark" {
		t.Fatalf("almost: %q %v", got, ok)
	}
}

func TestNormalizeMiniappThemeAccessPack(t *testing.T) {
	got, ok := NormalizeMiniappThemeAccess("pack", MiniappThemeAccess{})
	if !ok || got != "dark" {
		t.Fatalf("locked: %q %v", got, ok)
	}
	got, ok = NormalizeMiniappThemeAccess("pack", MiniappThemeAccess{PackBonusThemeActive: true})
	if !ok || got != "pack" {
		t.Fatalf("bonus: %q %v", got, ok)
	}
}
