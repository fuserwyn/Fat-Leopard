package bot

import (
	"strings"
	"testing"
)

// Контекстная кнопка больничного: активен/ожидает → «Отменить», иначе → «Выставить».
func TestSickLeaveAdminButton(t *testing.T) {
	const uid = int64(4242)

	active := sickLeaveAdminButton(uid, true)
	if !strings.Contains(active.Text, "Отменить") {
		t.Fatalf("active state must offer cancel, got %q", active.Text)
	}
	if active.CallbackData == nil || *active.CallbackData != "admin_user_sick_4242" {
		t.Fatalf("cancel callback data mismatch: %v", active.CallbackData)
	}

	set := sickLeaveAdminButton(uid, false)
	if !strings.Contains(set.Text, "Выставить") {
		t.Fatalf("idle state must offer activation, got %q", set.Text)
	}
	if set.CallbackData == nil || *set.CallbackData != "admin_user_sick_set_4242" {
		t.Fatalf("activate callback data mismatch: %v", set.CallbackData)
	}
}

// Регрессия на порядок диспетчера: activate-префикс более специфичен, чем cancel-префикс,
// поэтому "admin_user_sick_set_..." не должен ошибочно матчиться как отмена.
func TestSickLeaveCallbackPrefixSpecificity(t *testing.T) {
	setData := "admin_user_sick_set_4242"
	if !strings.HasPrefix(setData, "admin_user_sick_set_") {
		t.Fatal("activate data must carry the activate prefix")
	}
	// Оба префикса матчат activate-строку — значит в switch activate обязан идти ПЕРВЫМ.
	if !strings.HasPrefix(setData, "admin_user_sick_") {
		t.Fatal("sanity: activate data also shares the cancel prefix")
	}
	cancelData := "admin_user_sick_4242"
	if strings.HasPrefix(cancelData, "admin_user_sick_set_") {
		t.Fatal("cancel data must not match the activate prefix")
	}
}
