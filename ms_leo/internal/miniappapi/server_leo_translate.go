package miniappapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"unicode/utf8"
)

func (s *Server) handlePostLeoTranslate(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_text")
		return
	}
	if utf8.RuneCountInString(text) > 4000 {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	out, err := s.bot.TranslateLeoTextToEnglish(text)
	if err != nil {
		s.logger.Warnf("leo translate: %v", err)
		s.jsonErr(w, http.StatusBadGateway, "translate_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":          true,
		"text_en":     out,
		"already_en":  out == text,
	})
}
