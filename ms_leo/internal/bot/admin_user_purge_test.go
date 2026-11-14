package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/database"
)

// Регрессия на порядок веток в switch: у полного удаления три шага с вложенными префиксами,
// и если общий admin_user_purge_ окажется выше, подтверждение будет вечно открывать превью.
func TestAdminUserPurgeCallbackPrefixSpecificity(t *testing.T) {
	const yes = "admin_user_purge_yes_4242"
	const goStep = "admin_user_purge_go_4242"
	const prompt = "admin_user_purge_4242"

	// Оба специфичных шага матчатся и общим префиксом — значит в switch они обязаны идти раньше.
	for _, data := range []string{yes, goStep} {
		if !strings.HasPrefix(data, "admin_user_purge_") {
			t.Fatalf("sanity: %q must share the generic prefix", data)
		}
	}
	if strings.HasPrefix(prompt, "admin_user_purge_yes_") || strings.HasPrefix(prompt, "admin_user_purge_go_") {
		t.Fatal("preview data must not match the confirm prefixes")
	}
	if strings.HasPrefix(goStep, "admin_user_purge_yes_") {
		t.Fatal("confirm step must not match the execute prefix")
	}
	// Панель роутит в handleAdminUserMgmtCallback по admin_user_ — иначе кнопка мертва.
	if !strings.HasPrefix(prompt, "admin_user_") {
		t.Fatal("purge callbacks must be routed by the admin_user_ prefix")
	}
}

// Роутинг ветки «🛡 Админы»: снятие прав тоже двухшаговое, порядок префиксов важен так же.
func TestAdminRosterCallbackPrefixSpecificity(t *testing.T) {
	const yes = "admin_admin_del_yes_4242"
	const confirm = "admin_admin_del_4242"

	if !strings.HasPrefix(yes, "admin_admin_del_") {
		t.Fatal("sanity: execute data must share the confirm prefix")
	}
	if strings.HasPrefix(confirm, "admin_admin_del_yes_") {
		t.Fatal("confirm data must not match the execute prefix")
	}
	// Ветка админов не должна перехватываться диспетчером пользователей.
	for _, data := range []string{"admin_admins", "admin_admin_add", yes, confirm} {
		if strings.HasPrefix(data, "admin_user_") {
			t.Fatalf("%q must not collide with the user-management branch", data)
		}
	}
}

// Отчёт админу показывает только непустые таблицы — иначе список из 30 нулей.
func TestAdminUserPurgeSummarySkipsEmptyTables(t *testing.T) {
	report := database.AdminUserPurgeReport{
		UserID: 4242,
		Rows: []database.AdminUserPurgeRow{
			{Label: "посты в ленте и сообщения", Table: "user_messages", Rows: 3},
			{Label: "донаты", Table: "donations", Rows: 0},
			{Label: "тренировки", Table: "training_sessions", Rows: 7},
		},
		Total: 10,
	}

	summary := adminUserPurgeSummary(report)
	if !strings.Contains(summary, "посты в ленте и сообщения: 3") {
		t.Fatalf("non-empty table must be listed: %q", summary)
	}
	if strings.Contains(summary, "донаты") {
		t.Fatalf("empty table must be skipped: %q", summary)
	}
	if !strings.Contains(summary, "Всего строк: 10") {
		t.Fatalf("total must be reported: %q", summary)
	}

	empty := adminUserPurgeSummary(database.AdminUserPurgeReport{UserID: 4242})
	if !strings.Contains(empty, "нечего") {
		t.Fatalf("empty report must say so plainly: %q", empty)
	}
}
