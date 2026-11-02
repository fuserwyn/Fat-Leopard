package database

import "testing"

func TestServerPaymentReminderClaim(t *testing.T) {
	var nilDB *Database
	if ok, err := nilDB.ClaimServerPaymentReminder("2026-11", 1); ok || err != nil {
		t.Fatalf("nil db: %v %v", ok, err)
	}
	if err := nilDB.ReleaseServerPaymentReminder("2026-11"); err != nil {
		t.Fatal(err)
	}

	d := openMigratedTestDB(t)
	if ok, err := d.ClaimServerPaymentReminder("", 1); ok || err != nil {
		t.Fatalf("пустой месяц: %v %v", ok, err)
	}
	ok, err := d.ClaimServerPaymentReminder("2026-11", 42)
	if err != nil || !ok {
		t.Fatalf("первая отметка: %v %v", ok, err)
	}
	ok, err = d.ClaimServerPaymentReminder("2026-11", 42)
	if err != nil || ok {
		t.Fatalf("повторная отметка должна вернуть false: %v %v", ok, err)
	}
	if err := d.ReleaseServerPaymentReminder("2026-11"); err != nil {
		t.Fatal(err)
	}
	ok, err = d.ClaimServerPaymentReminder("2026-11", 42)
	if err != nil || !ok {
		t.Fatalf("после снятия отметки: %v %v", ok, err)
	}
}
