package bot

import (
	"strings"
	"testing"
)

// Повторный вход: пользователь получает кнопку «Войти в группу» и отдельную «Новая ссылка»
// (callback paywall_refresh_invite), чтобы не зависеть от протухшего invite.
func TestRetryEntry_RejoinInviteKeyboard(t *testing.T) {
	invite := "https://t.me/+testInviteLink"
	kb := freshRejoinInviteKeyboard(invite)
	if kb == nil {
		t.Fatal("keyboard is nil")
	}
	if len(kb.InlineKeyboard) != 2 {
		t.Fatalf("want 2 keyboard rows, got %d", len(kb.InlineKeyboard))
	}

	join := kb.InlineKeyboard[0][0]
	if join.Text != "📩 Войти в группу" {
		t.Fatalf("join button text: got %q", join.Text)
	}
	if join.URL == nil || *join.URL != invite {
		t.Fatalf("join button url: got %+v want %q", join.URL, invite)
	}
	if join.CallbackData != nil {
		t.Fatalf("join button must not use callback_data")
	}

	refresh := kb.InlineKeyboard[1][0]
	if refresh.Text != "🔁 Новая ссылка" {
		t.Fatalf("refresh button text: got %q", refresh.Text)
	}
	if refresh.CallbackData == nil || *refresh.CallbackData != paywallCallbackRefreshInvite {
		t.Fatalf("refresh callback_data: got %+v want %q", refresh.CallbackData, paywallCallbackRefreshInvite)
	}
	if refresh.URL != nil {
		t.Fatalf("refresh button must not use url")
	}
}

func TestRetryEntry_PaidStartHintLine(t *testing.T) {
	s := paidPrivateRetryEntryHintLine()
	if !strings.Contains(s, "кнопки ниже") {
		t.Fatalf("hint should mention buttons below: %q", s)
	}
	if !strings.Contains(s, "/start") {
		t.Fatalf("hint should mention /start for fresh link: %q", s)
	}
}
