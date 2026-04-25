package bot

import (
	"encoding/json"
	"testing"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestUnmarshalUpdatesWithWebAppData(t *testing.T) {
	raw := json.RawMessage(` [{
		"update_id": 1,
		"message": {
			"message_id": 7,
			"from": {"id": 42, "is_bot": false, "first_name": "A"},
			"chat": {"id": 42, "type": "private", "first_name": "A"},
			"date": 1,
			"web_app_data": {"data": "hello from web", "button_text": "App"}
		}
	} ] `)
	updates, err := UnmarshalUpdatesWithWebApp(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 {
		t.Fatalf("len %d", len(updates))
	}
	u := updates[0]
	if u.Message == nil {
		t.Fatal("nil message")
	}
	if u.Message.Text != "hello from web" {
		t.Fatalf("text %q", u.Message.Text)
	}
}

func TestUnmarshalUpdatesPlainMessage(t *testing.T) {
	raw, _ := json.Marshal([]tgbotapi.Update{{
		UpdateID: 2,
		Message: &tgbotapi.Message{
			MessageID: 1,
			From:      &tgbotapi.User{ID: 1, FirstName: "A"},
			Chat:      &tgbotapi.Chat{ID: 1, Type: "private", FirstName: "A"},
			Text:      "ok",
		},
	}})
	updates, err := UnmarshalUpdatesWithWebApp(json.RawMessage(raw))
	if err != nil {
		t.Fatal(err)
	}
	if len(updates) != 1 || updates[0].Message.Text != "ok" {
		t.Fatalf("got %#v", updates[0].Message)
	}
}
