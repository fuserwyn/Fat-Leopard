package miniappapi

import (
	"encoding/json"
	"net/http"
)

// handlePostFriendsMentionSearch — подсказка участников стаи при наборе «@» в
// описании тренировки: отмеченный напарник получит уведомление о совместной тренировке.
func (s *Server) handlePostFriendsMentionSearch(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Query    string `json:"query"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	members, err := s.bot.SearchJointTrainingMentionsForViewer(parsed.User.ID, parsed, body.Query)
	if err != nil {
		s.writeSocialErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "members": members})
}
