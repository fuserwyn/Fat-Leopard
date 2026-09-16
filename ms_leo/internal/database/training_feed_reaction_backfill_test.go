package database

import (
	"os"
	"strings"
	"testing"

	"leo-bot/internal/trainingfeed"
)

func TestBackfillMissingLeoTrainingFeedReactions(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set — backfill test skipped")
	}

	d, err := New(dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer d.Close()
	if err := d.CreateTables(); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	const (
		author = int64(-888001)
		pack   = int64(-100888)
	)
	t.Cleanup(func() {
		_, _ = d.AdminPurgeUserEverywhere(author)
	})

	mustExec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.db.Exec(q, args...); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
	}
	mustID := func(q string, args ...any) int64 {
		t.Helper()
		var id int64
		if err := d.db.QueryRow(q, args...).Scan(&id); err != nil {
			t.Fatalf("seed %q: %v", q, err)
		}
		return id
	}

	mustExec(`INSERT INTO training_state (user_id, chat_id, username, last_message) VALUES ($1,$2,'seed','')`, author, pack)

	runReport := "бег, 15 мин, инт. 3/5"
	yogaReport := "йога, 20 мин, инт. 2/5"
	runPost := mustID(`INSERT INTO user_messages (user_id, chat_id, message_text, message_type)
		VALUES ($1,$2,$3,'training_done') RETURNING id`, author, pack, runReport)
	yogaPost := mustID(`INSERT INTO user_messages (user_id, chat_id, message_text, message_type)
		VALUES ($1,$2,$3,'training_done') RETURNING id`, author, pack, yogaReport)
	swimPost := mustID(`INSERT INTO user_messages (user_id, chat_id, message_text, message_type)
		VALUES ($1,$2,'плавание, 30 мин, инт. 4/5','training_done') RETURNING id`, author, pack)
	mustExec(`INSERT INTO miniapp_training_feed_reactions (pack_chat_id, user_message_id, user_id, username, emoji)
		VALUES ($1,$2,0,'Лео','🔥')`, pack, swimPost)

	inserted, err := d.BackfillMissingLeoTrainingFeedReactions()
	if err != nil {
		t.Fatalf("backfill: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 inserted, got %d", inserted)
	}

	var runEmoji, yogaEmoji, swimEmoji string
	if err := d.db.QueryRow(
		`SELECT emoji FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = 0`,
		runPost,
	).Scan(&runEmoji); err != nil {
		t.Fatalf("run leo reaction: %v", err)
	}
	if err := d.db.QueryRow(
		`SELECT emoji FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = 0`,
		yogaPost,
	).Scan(&yogaEmoji); err != nil {
		t.Fatalf("yoga leo reaction: %v", err)
	}
	if err := d.db.QueryRow(
		`SELECT emoji FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = 0`,
		swimPost,
	).Scan(&swimEmoji); err != nil {
		t.Fatalf("swim leo reaction: %v", err)
	}
	if !trainingfeed.IsEmojiAllowedForReport(runEmoji, runReport) {
		t.Fatalf("run emoji %q not allowed for run report", runEmoji)
	}
	if !trainingfeed.IsEmojiAllowedForReport(yogaEmoji, yogaReport) {
		t.Fatalf("yoga emoji %q not allowed for yoga report", yogaEmoji)
	}
	if swimEmoji != "🔥" {
		t.Fatalf("existing leo reaction must stay, got %q", swimEmoji)
	}
	if runEmoji == yogaEmoji {
		t.Fatalf("expected different leo emojis for different posts, both %q", runEmoji)
	}

	inserted2, err := d.BackfillMissingLeoTrainingFeedReactions()
	if err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if inserted2 != 0 {
		t.Fatalf("second backfill must be idempotent, got %d", inserted2)
	}
}
