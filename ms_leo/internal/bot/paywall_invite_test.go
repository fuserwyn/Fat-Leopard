package bot

import (
	"testing"
	"time"
)

// Регрессия: member_limit=1 + прямой вход давали «invite link expired» после одного открытия.
// «Свежая» ссылка: без member_limit, сначала заявка (creates_join_request), затем запасной прямой вход — оба с expire 24h.
func TestPaywallFreshInviteLinkConfigs_NoMemberLimitJoinRequestFirst(t *testing.T) {
	chatID := int64(-1001234567890)
	now := time.Date(2026, 4, 1, 15, 30, 0, 0, time.UTC)
	cfgs := paywallFreshInviteLinkConfigs(chatID, now)
	if len(cfgs) != 2 {
		t.Fatalf("want 2 API attempts, got %d", len(cfgs))
	}
	wantExp := int(now.Add(24 * time.Hour).Unix())
	for i, c := range cfgs {
		if c.ChatID != chatID {
			t.Fatalf("cfg %d: ChatID=%d want %d", i, c.ChatID, chatID)
		}
		if c.MemberLimit != 0 {
			t.Fatalf("cfg %d: MemberLimit=%d must stay 0 (omit in API) — never single-use limit here", i, c.MemberLimit)
		}
		if c.ExpireDate != wantExp {
			t.Fatalf("cfg %d: ExpireDate=%d want %d", i, c.ExpireDate, wantExp)
		}
	}
	if !cfgs[0].CreatesJoinRequest {
		t.Fatal("first config must use CreatesJoinRequest=true")
	}
	if cfgs[1].CreatesJoinRequest {
		t.Fatal("fallback config must use CreatesJoinRequest=false")
	}
}
