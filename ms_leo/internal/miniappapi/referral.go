package miniappapi

import (
	"encoding/json"
	"net/http"
)

// POST /api/miniapp/referral/state — личная ссылка «Позвать в стаю» и счётчики:
// сколько пришло, сколько записали тренировку, сколько попыток спасти стрик выдано.
func (s *Server) handlePostReferralState(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	v, err := s.bot.GetReferralForAPI(parsed.User.ID)
	if err != nil {
		s.logger.Warnf("referral state user=%d: %v", parsed.User.ID, err)
		s.jsonErr(w, http.StatusInternalServerError, "referral_failed")
		return
	}
	s.writeJSONOK(w, map[string]any{"referral": v})
}
