package miniappapi

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// shareCardRequest — multipart как у мини-аппа: init_data и картинка карточки.
func shareCardRequest(t *testing.T, initData string, photo []byte) *http.Request {
	t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("init_data", initData)
	if photo != nil {
		part, err := w.CreateFormFile("photo", "card.png")
		if err != nil {
			t.Fatal(err)
		}
		_, _ = part.Write(photo)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/miniapp/share/card", &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return req
}

// Карточка «поделиться» сохраняется как публичная картинка, а в ответе —
// личная ссылка на бота (реферальная), чтобы друг со сторис засчитался приглашением.
func TestShareCardUploadReturnsPublicURLAndBotLink(t *testing.T) {
	j := newJourney(t)
	png := append([]byte("\x89PNG\r\n\x1a\n"), bytes.Repeat([]byte{1}, 64)...)

	code, out := j.do(shareCardRequest(t, signedInit(jAnna, "anna"), png))
	if code != http.StatusOK {
		t.Fatalf("загрузка карточки: код %d, ответ %v", code, out)
	}
	u, _ := out["url"].(string)
	if !strings.HasPrefix(u, "https://example.test/api/miniapp/media/") || !strings.HasSuffix(u, ".png") {
		t.Fatalf("url = %q", u)
	}
	if link, _ := out["link"].(string); link != "https://t.me/leo_test_bot?start=ref-700001" {
		t.Fatalf("link = %q", link)
	}

	if code, _ := j.do(shareCardRequest(t, signedInit(jAnna, "anna"), nil)); code != http.StatusBadRequest {
		t.Fatalf("без картинки: код %d, ждали 400", code)
	}
	if code, _ := j.do(shareCardRequest(t, signedInit(jAnna, "anna"), []byte("not an image"))); code != http.StatusBadRequest {
		t.Fatalf("не картинка: код %d, ждали 400", code)
	}
	if code, _ := j.do(shareCardRequest(t, "user=forged&hash=00", png)); code != http.StatusUnauthorized {
		t.Fatalf("поддельный init_data: код %d, ждали 401", code)
	}
}

func TestShareCardRejectsWithoutBotOrMultipart(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.handlePostShareCard(rec, httptest.NewRequest(http.MethodPost, "/api/miniapp/share/card", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("без бота: код %d", rec.Code)
	}
}
