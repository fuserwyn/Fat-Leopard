package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"leo-bot/internal/bot"
)

// Челленджи: N дней подряд с тренировкой (bot/challenges.go).

func (s *Server) writeChallengeErr(w http.ResponseWriter, err error) {
	if s.jsonModerationErr(w, err) {
		return
	}
	switch {
	case errors.Is(err, bot.ErrMiniAppChatMismatch):
		s.jsonErr(w, http.StatusConflict, "chat_mismatch")
	case errors.Is(err, bot.ErrChallengeNotFound):
		s.jsonErr(w, http.StatusNotFound, "challenge_not_found")
	case errors.Is(err, bot.ErrChallengeAlreadyActive):
		s.jsonErr(w, http.StatusConflict, "challenge_already_active")
	case errors.Is(err, bot.ErrChallengeNotActive):
		s.jsonErr(w, http.StatusConflict, "challenge_not_active")
	case errors.Is(err, bot.ErrChallengeCreateForbidden):
		s.jsonErr(w, http.StatusForbidden, "challenge_create_forbidden")
	case errors.Is(err, bot.ErrChallengeBadLength):
		s.jsonErr(w, http.StatusBadRequest, "challenge_bad_length")
	case errors.Is(err, bot.ErrChallengeBadTitle):
		s.jsonErr(w, http.StatusBadRequest, "challenge_bad_title")
	default:
		s.writeSocialErr(w, err)
	}
}

func (s *Server) writeJSONOK(w http.ResponseWriter, out map[string]any) {
	out["ok"] = true
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// POST /api/miniapp/challenges/state — стандартные и свои челленджи, активный,
// история, челлендж из ссылки (предложить принять) и можно ли создавать свои.
func (s *Server) handlePostChallengesState(w http.ResponseWriter, r *http.Request) {
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
	st, err := s.bot.GetChallengesStateForViewer(parsed.User.ID, parsed)
	if err != nil {
		s.writeChallengeErr(w, err)
		return
	}
	s.writeJSONOK(w, map[string]any{
		"standard":   st.Standard,
		"mine":       st.Mine,
		"active":     st.Active,
		"history":    st.History,
		"invite":     st.Invite,
		"can_create": st.CanCreate,
	})
}

// POST /api/miniapp/challenges/accept {code} — принять челлендж.
func (s *Server) handlePostChallengesAccept(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Code     string `json:"code"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	v, err := s.bot.AcceptChallengeForViewer(parsed.User.ID, parsed, body.Code)
	if err != nil {
		s.writeChallengeErr(w, err)
		return
	}
	s.writeJSONOK(w, map[string]any{"active": v})
}

// POST /api/miniapp/challenges/create {title, length_days} — свой челлендж.
func (s *Server) handlePostChallengesCreate(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData   string `json:"init_data"`
		Title      string `json:"title"`
		LengthDays int    `json:"length_days"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	v, err := s.bot.CreateChallengeForViewer(parsed.User.ID, parsed, body.Title, body.LengthDays)
	if err != nil {
		s.writeChallengeErr(w, err)
		return
	}
	s.writeJSONOK(w, map[string]any{"challenge": v})
}

// POST /api/miniapp/challenges/leave — бросить активный челлендж.
func (s *Server) handlePostChallengesLeave(w http.ResponseWriter, r *http.Request) {
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
	if err := s.bot.LeaveChallengeForViewer(parsed.User.ID, parsed); err != nil {
		s.writeChallengeErr(w, err)
		return
	}
	s.writeJSONOK(w, map[string]any{})
}

// POST /api/miniapp/challenges/invite/dismiss — отказаться от челленджа из ссылки.
func (s *Server) handlePostChallengesInviteDismiss(w http.ResponseWriter, r *http.Request) {
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
	if err := s.bot.DismissChallengeInviteForViewer(parsed.User.ID, parsed); err != nil {
		s.writeChallengeErr(w, err)
		return
	}
	s.writeJSONOK(w, map[string]any{})
}
