package miniappapi

import (
	"errors"
	"net/http"
	"testing"
)

func TestServerPaymentWebhookStatus(t *testing.T) {
	cases := []struct {
		name, body string
		confirmErr error
		wantStatus int
		wantID     string
	}{
		{name: "оплата", body: `{"event":"payment.succeeded","object":{"id":"yk-1"}}`, wantStatus: http.StatusOK, wantID: "yk-1"},
		{name: "сбой — ЮKassa повторит", body: `{"event":"payment.succeeded","object":{"id":"yk-1"}}`, confirmErr: errors.New("db"), wantStatus: http.StatusInternalServerError, wantID: "yk-1"},
		{name: "другое событие", body: `{"event":"payment.canceled","object":{"id":"yk-1"}}`, wantStatus: http.StatusOK},
		{name: "без id", body: `{"event":"payment.succeeded","object":{}}`, wantStatus: http.StatusOK},
		{name: "не JSON", body: `nope`, wantStatus: http.StatusBadRequest},
	}
	for _, c := range cases {
		gotID := ""
		got := serverPaymentWebhookStatus([]byte(c.body), func(id string) error {
			gotID = id
			return c.confirmErr
		})
		if got != c.wantStatus || gotID != c.wantID {
			t.Errorf("%s: status=%d id=%q", c.name, got, gotID)
		}
	}
}
