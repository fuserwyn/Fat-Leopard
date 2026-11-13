package bot

import "testing"

func TestExtractPackMentionTokens(t *testing.T) {
	t.Parallel()
	tokens := extractPackMentionTokens("Привет @vasya и @Anna!", "leobot")
	if len(tokens) != 2 {
		t.Fatalf("expected 2 tokens, got %v", tokens)
	}
	if tokens[0] != "vasya" || tokens[1] != "anna" {
		t.Fatalf("unexpected tokens: %v", tokens)
	}
	// @leo и @бот не считаются упоминаниями участников.
	tokens = extractPackMentionTokens("@leo помоги @LeoBot @masha", "LeoBot")
	if len(tokens) != 1 || tokens[0] != "masha" {
		t.Fatalf("reserved tokens must be skipped: %v", tokens)
	}
	// display_name с кириллицей.
	tokens = extractPackMentionTokens("Спасибо @Аня", "")
	if len(tokens) != 1 || tokens[0] != "аня" {
		t.Fatalf("cyrillic token: %v", tokens)
	}
}

func TestPackMentionPlace(t *testing.T) {
	t.Parallel()
	if packMentionPlace(packMentionFeedPost) != "in a post" {
		t.Fatal("feed post place")
	}
	if packMentionPlace(packMentionFeedComment) != "in a comment" {
		t.Fatal("feed comment place")
	}
	if packMentionPlace(packMentionPackGroupMessage) != "in the chat" {
		t.Fatal("pack group place")
	}
}
