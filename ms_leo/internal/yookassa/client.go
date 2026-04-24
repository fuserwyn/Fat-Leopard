package yookassa

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const createPaymentURL = "https://api.yookassa.ru/v3/payments"
const createRefundURL = "https://api.yookassa.ru/v3/refunds"

type createPaymentReq struct {
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Confirmation struct {
		Type      string `json:"type"`
		ReturnURL string `json:"return_url"`
	} `json:"confirmation"`
	// См. https://yookassa.ru/developers/api#create_payment — для платежа можно задать URL уведомлений явно.
	NotificationURL string            `json:"notification_url,omitempty"`
	Capture         bool              `json:"capture"`
	Description     string            `json:"description"`
	Metadata        map[string]string `json:"metadata"`
}

type createPaymentResp struct {
	ID           string `json:"id"`
	Status       string `json:"status"`
	Confirmation struct {
		Type            string `json:"type"`
		ConfirmationURL string `json:"confirmation_url"`
	} `json:"confirmation"`
}

type apiError struct {
	Type        string            `json:"type"`
	ID          string            `json:"id"`
	Code        string            `json:"code"`
	Description string            `json:"description"`
	Parameter   string            `json:"parameter"`
	RetryAfter  int               `json:"retry_after,omitempty"`
	Meta        map[string]string `json:"metadata,omitempty"`
}

type createRefundReq struct {
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	PaymentID string `json:"payment_id"`
}

func newIDempotenceKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// CreatePayment создаёт платёж с подтверждением redirect; в metadata должны быть строки для вебхука (user_telegram_id, invoice_payload).
// notificationURL — если не пустой, ЮKassa шлёт HTTP-уведомления для этого платежа на этот адрес (HTTPS), независимо от URL в ЛК.
func CreatePayment(shopID, secretKey string, amountMinor int, currency, description, returnURL, notificationURL string, metadata map[string]string) (paymentID, confirmationURL string, err error) {
	if shopID == "" || secretKey == "" {
		return "", "", fmt.Errorf("yookassa shop_id or secret_key empty")
	}
	if amountMinor <= 0 {
		return "", "", fmt.Errorf("amount must be positive")
	}
	if currency == "" {
		currency = "RUB"
	}
	if len(returnURL) < 8 || (returnURL[:8] != "https://" && returnURL[:7] != "http://") {
		return "", "", fmt.Errorf("return_url must be http(s) URL")
	}

	idem, err := newIDempotenceKey()
	if err != nil {
		return "", "", fmt.Errorf("idempotence key: %w", err)
	}

	var body createPaymentReq
	body.Amount.Value = fmt.Sprintf("%.2f", float64(amountMinor)/100.0)
	body.Amount.Currency = currency
	body.Confirmation.Type = "redirect"
	body.Confirmation.ReturnURL = returnURL
	if u := strings.TrimSpace(notificationURL); u != "" {
		body.NotificationURL = u
	}
	body.Capture = true
	body.Description = description
	body.Metadata = metadata

	raw, err := json.Marshal(body)
	if err != nil {
		return "", "", err
	}

	req, err := http.NewRequest(http.MethodPost, createPaymentURL, bytes.NewReader(raw))
	if err != nil {
		return "", "", err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(shopID + ":" + secretKey))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Idempotence-Key", idem)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return "", "", fmt.Errorf("yookassa request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		_ = json.Unmarshal(respBody, &ae)
		if ae.Description != "" {
			return "", "", fmt.Errorf("yookassa HTTP %d: %s", resp.StatusCode, ae.Description)
		}
		return "", "", fmt.Errorf("yookassa HTTP %d: %s", resp.StatusCode, string(respBody))
	}

	var out createPaymentResp
	if err := json.Unmarshal(respBody, &out); err != nil {
		return "", "", fmt.Errorf("decode payment: %w", err)
	}
	if out.ID == "" || out.Confirmation.ConfirmationURL == "" {
		return "", "", fmt.Errorf("yookassa: empty id or confirmation_url in response")
	}
	return out.ID, out.Confirmation.ConfirmationURL, nil
}

// PaymentStatusInfo — ответ GET /v3/payments/{id} (для синхронизации без вебхука).
type PaymentStatusInfo struct {
	ID          string
	Status      string
	Paid        bool
	AmountMinor int
	Currency    string
	Metadata    map[string]string
}

// GetPayment запрашивает статус платежа (тот же shop_id:secret_key, что при создании).
func GetPayment(shopID, secretKey, paymentID string) (*PaymentStatusInfo, error) {
	if shopID == "" || secretKey == "" || strings.TrimSpace(paymentID) == "" {
		return nil, fmt.Errorf("yookassa GetPayment: empty shop_id, secret_key or payment_id")
	}
	url := fmt.Sprintf("https://api.yookassa.ru/v3/payments/%s", strings.TrimSpace(paymentID))
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(shopID + ":" + secretKey))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yookassa get payment: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read get payment: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		_ = json.Unmarshal(body, &ae)
		if ae.Description != "" {
			return nil, fmt.Errorf("yookassa get payment HTTP %d: %s", resp.StatusCode, ae.Description)
		}
		return nil, fmt.Errorf("yookassa get payment HTTP %d: %s", resp.StatusCode, string(body))
	}

	var raw struct {
		ID       string                 `json:"id"`
		Status   string                 `json:"status"`
		Paid     bool                   `json:"paid"`
		Amount   map[string]interface{} `json:"amount"`
		Metadata map[string]interface{} `json:"metadata"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode get payment: %w", err)
	}
	out := &PaymentStatusInfo{
		ID:       raw.ID,
		Status:   raw.Status,
		Paid:     raw.Paid,
		Metadata: map[string]string{},
	}
	if raw.Amount != nil {
		if c, ok := raw.Amount["currency"].(string); ok {
			out.Currency = strings.TrimSpace(strings.ToUpper(c))
		}
		if v := raw.Amount["value"]; v != nil {
			var f float64
			switch x := v.(type) {
			case string:
				f, _ = strconv.ParseFloat(strings.ReplaceAll(x, ",", "."), 64)
			case float64:
				f = x
			}
			out.AmountMinor = int(math.Round(f * 100))
		}
	}
	for k, v := range raw.Metadata {
		if v == nil {
			continue
		}
		out.Metadata[k] = strings.TrimSpace(fmt.Sprint(v))
	}
	return out, nil
}

// RefundPayment делает полный/частичный возврат через YooKassa API.
func RefundPayment(shopID, secretKey, paymentID string, amountMinor int, currency string) error {
	if shopID == "" || secretKey == "" || strings.TrimSpace(paymentID) == "" {
		return fmt.Errorf("yookassa RefundPayment: empty shop_id, secret_key or payment_id")
	}
	if amountMinor <= 0 {
		return fmt.Errorf("refund amount must be positive")
	}
	if strings.TrimSpace(currency) == "" {
		currency = "RUB"
	}
	idem, err := newIDempotenceKey()
	if err != nil {
		return fmt.Errorf("idempotence key: %w", err)
	}

	var body createRefundReq
	body.Amount.Value = fmt.Sprintf("%.2f", float64(amountMinor)/100.0)
	body.Amount.Currency = strings.ToUpper(strings.TrimSpace(currency))
	body.PaymentID = strings.TrimSpace(paymentID)

	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest(http.MethodPost, createRefundURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	auth := base64.StdEncoding.EncodeToString([]byte(shopID + ":" + secretKey))
	req.Header.Set("Authorization", "Basic "+auth)
	req.Header.Set("Idempotence-Key", idem)
	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 25 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("yookassa refund request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return fmt.Errorf("read refund response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var ae apiError
		_ = json.Unmarshal(respBody, &ae)
		if ae.Description != "" {
			return fmt.Errorf("yookassa refund HTTP %d: %s", resp.StatusCode, ae.Description)
		}
		return fmt.Errorf("yookassa refund HTTP %d: %s", resp.StatusCode, string(respBody))
	}
	return nil
}
