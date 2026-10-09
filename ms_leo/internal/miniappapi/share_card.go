package miniappapi

import (
	"net/http"
	"strings"
)

// POST /api/miniapp/share/card — картинка-карточка для сторис и чатов (multipart:
// init_data, photo). Мини-апп рисует карточку сам (ачивка, новый уровень, тренировка),
// а сюда кладёт готовый JPEG/PNG: Telegram WebApp.shareToStory и превью ссылки в чате
// принимают только публичный https-адрес картинки. В ответ — этот адрес и личная
// ссылка на бота, которую карточка ведёт в подпись.
func (s *Server) handlePostShareCard(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	if err := r.ParseMultipartForm(maxWorkoutPhotoBytes + 65536); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_multipart")
		return
	}
	parsed, ok := s.authMiniapp(w, strings.TrimSpace(r.FormValue("init_data")))
	if !ok {
		return
	}
	fs := r.MultipartForm.File["photo"]
	if len(fs) == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_photo")
		return
	}
	file, err := fs[0].Open()
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "photo_open_error")
		return
	}
	defer file.Close()
	publicURL, ok := s.storeUploadedPhoto(w, r, file)
	if !ok {
		return
	}
	s.writeJSONOK(w, map[string]any{"url": publicURL, "link": s.bot.ShareLinkForUser(parsed.User.ID)})
}
