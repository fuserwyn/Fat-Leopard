package miniappapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// handleServerPaymentWebhook — уведомление ЮKassa об оплате счёта за сервер.
func (s *Server) handleServerPaymentWebhook(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	w.WriteHeader(serverPaymentWebhookStatus(body, s.bot.ConfirmServerPaymentCard))
}

// serverPaymentWebhookStatus — HTTP-статус ответа ЮKassa. Телу не верим: confirm сам запрашивает статус платежа
// в ЮKassa и знает только платежи наших счетов. 5xx — ЮKassa повторит уведомление.
func serverPaymentWebhookStatus(body []byte, confirm func(paymentID string) error) int {
	var n struct {
		Event  string `json:"event"`
		Object struct {
			ID string `json:"id"`
		} `json:"object"`
	}
	if err := json.Unmarshal(body, &n); err != nil {
		return http.StatusBadRequest
	}
	id := strings.TrimSpace(n.Object.ID)
	if n.Event != "payment.succeeded" || id == "" {
		return http.StatusOK
	}
	if err := confirm(id); err != nil {
		return http.StatusInternalServerError
	}
	return http.StatusOK
}
