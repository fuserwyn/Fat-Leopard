package bot

import (
	"testing"

	"leo-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Задача #169: админы (из конфига и добавленные в боте) больше не ставят
// автоматические реакции на тренировки в ленте стаи. На новом посте остаётся
// только реакция Лео (user_id = 0).
func TestWorkoutPostGetsOnlyLeoReaction(t *testing.T) {
	const extraAdmin = int64(555901)
	const dynAdmin = int64(555902)
	b, _, db := newIntegrationBot(t, func(c *config.Config) {
		c.AdminIDs = []int64{extraAdmin}
	})
	if err := b.db.AddDynamicAdmin(dynAdmin, "dyn", itAdmin); err != nil {
		t.Fatal(err)
	}
	seedMember(t, db, itUser, "leopard", false)

	// Отчёт из чата стаи: пост ленты уже записан, его id передаётся обработчику.
	const postID = int64(777001)
	b.handleLeopardMoneyTrainingDone(&tgbotapi.Message{
		From: &tgbotapi.User{ID: itUser, UserName: "leopard"},
		Chat: &tgbotapi.Chat{ID: itPack, Type: "supergroup"},
		Text: "#training_done пробежка 5 км",
	}, nil, postID)

	var posts int
	if err := db.QueryRow(`SELECT COUNT(DISTINCT user_message_id) FROM miniapp_training_feed_reactions WHERE pack_chat_id = $1`, itPack).Scan(&posts); err != nil {
		t.Fatal(err)
	}
	if posts != 1 {
		t.Fatalf("ждали реакцию Лео на один пост, постов с реакциями: %d", posts)
	}
	rows, err := db.Query(`SELECT user_id FROM miniapp_training_feed_reactions WHERE pack_chat_id = $1`, itPack)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var users []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		users = append(users, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != 0 {
		t.Errorf("реакции на пост от %v, ждали только Лео (0)", users)
	}
}
