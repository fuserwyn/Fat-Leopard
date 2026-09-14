package bot

import (
	"strings"
	"testing"

	"leo-bot/internal/domain"
)

func TestPackFeedLeoAvatarURL(t *testing.T) {
	got := packFeedLeoAvatarURL("https://app.example")
	if got != "https://app.example/leo-avatar.png" {
		t.Fatalf("got %q", got)
	}
	if packFeedLeoAvatarURL("") != "/leo-avatar.png" {
		t.Fatal("empty base should return static path")
	}
}

func TestLeoTrainingFeedReactionEmojiVariesByPost(t *testing.T) {
	a := leoTrainingFeedReactionEmoji(101)
	b := leoTrainingFeedReactionEmoji(202)
	if a == "" || b == "" {
		t.Fatal("expected non-empty emoji")
	}
	if a == b {
		t.Fatalf("expected different emojis for different posts, both %q", a)
	}
	if leoTrainingFeedReactionEmoji(101) != a {
		t.Fatal("emoji must be stable for the same post id")
	}
}

func TestPackFeedResolveAuthorPhotoPrefersProxy(t *testing.T) {
	proxy := packFeedResolveAuthorPhoto("https://t.me/i/userpic/320/abc.jpg", "https://app.example", 42, "query_id=1")
	if proxy == "" {
		t.Fatal("expected proxy url")
	}
	if !strings.HasPrefix(proxy, "https://app.example/api/miniapp/user-avatar?") {
		t.Fatalf("got %q", proxy)
	}
	if !strings.Contains(proxy, "user_id=42") {
		t.Fatalf("proxy missing user_id: %s", proxy)
	}
}

func TestPackFeedResolveAuthorPhotoFallsBackToSafePublic(t *testing.T) {
	got := packFeedResolveAuthorPhoto("https://t.me/i/userpic/320/abc.jpg", "", 0, "")
	if got != "https://t.me/i/userpic/320/abc.jpg" {
		t.Fatalf("got %q", got)
	}
}

func TestPackFeedTelegramPhotoURLIsSafePublic(t *testing.T) {
	if packFeedTelegramPhotoURLIsSafePublic("https://api.telegram.org/file/botTOKEN/x") {
		t.Fatal("bot file link must not be treated as public")
	}
	if !packFeedTelegramPhotoURLIsSafePublic("https://t.me/i/userpic/320/abc.jpg") {
		t.Fatal("t.me userpic should be safe public")
	}
}

func TestApplyMessageThreadQuoteSkipsRootParent(t *testing.T) {
	m := &domain.PackGroupChatMessage{
		ReplyToID:       50,
		ReplyToUsername: "Лео",
		ReplyToText:     "привет",
		ReplyToIsLeo:    true,
	}
	var pr PackFeedThreadReply
	applyMessageThreadQuote(&pr, m, 50)
	if pr.ReplyToID != 0 || pr.ReplyToUsername != "" {
		t.Fatalf("quote on root parent must be empty, got %+v", pr)
	}
}

func TestApplyMessageThreadQuoteKeepsNestedReply(t *testing.T) {
	m := &domain.PackGroupChatMessage{
		ReplyToID:       77,
		ReplyToUsername: "Лео",
		ReplyToText:     "держись",
		ReplyToIsLeo:    true,
	}
	var pr PackFeedThreadReply
	applyMessageThreadQuote(&pr, m, 50)
	if pr.ReplyToID != 77 || pr.ReplyToUsername != "Лео" || pr.ReplyToText != "держись" || !pr.ReplyToIsLeo {
		t.Fatalf("nested quote: %+v", pr)
	}
}
