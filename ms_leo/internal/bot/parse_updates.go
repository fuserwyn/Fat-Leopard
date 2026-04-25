package bot

import (
	"encoding/json"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// UnmarshalUpdatesWithWebApp разбирает JSON result от getUpdates и переносит
// message.web_app_data.data в message.text (Telegram.WebApp.sendData).
func UnmarshalUpdatesWithWebApp(result json.RawMessage) ([]tgbotapi.Update, error) {
	if len(result) == 0 || string(result) == "null" {
		return nil, nil
	}
	var raws []json.RawMessage
	if err := json.Unmarshal(result, &raws); err != nil {
		return nil, err
	}
	out := make([]tgbotapi.Update, 0, len(raws))
	for _, raw := range raws {
		var upd tgbotapi.Update
		if err := json.Unmarshal(raw, &upd); err != nil {
			return nil, err
		}
		mergeWebAppDataFromRawUpdateJSON(raw, &upd)
		out = append(out, upd)
	}
	return out, nil
}

func mergeWebAppDataFromRawUpdateJSON(raw []byte, u *tgbotapi.Update) {
	if u == nil {
		return
	}
	var w struct {
		Message *struct {
			WebAppData *struct {
				Data       string `json:"data"`
				ButtonText string `json:"button_text"`
			} `json:"web_app_data,omitempty"`
		} `json:"message,omitempty"`
	}
	if err := json.Unmarshal(raw, &w); err != nil || w.Message == nil || w.Message.WebAppData == nil {
		return
	}
	d := w.Message.WebAppData.Data
	if d == "" {
		return
	}
	if u.Message != nil {
		u.Message.Text = d
	}
}
