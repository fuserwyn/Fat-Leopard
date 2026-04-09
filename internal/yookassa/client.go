package yookassa

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const createPaymentURL = "https://api.yookassa.ru/v3/payments"

type createPaymentReq struct {
	Amount struct {
		Value    string `json:"value"`
		Currency string `json:"currency"`
	} `json:"amount"`
	Confirmation struct {
		Type      string `json:"type"`
		ReturnURL string `json:"return_url"`
	} `json:"confirmation"`
	Capture     bool              `json:"capture"`
	Description string            `json:"description"`
	Metadata    map[string]string `json:"metadata"`
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

func newIDempotenceKey() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

// CreatePayment создаёт платёж с подтверждением redirect; в metadata должны быть строки для вебхука (user_telegram_id, invoice_payload).
func CreatePayment(shopID, secretKey string, amountMinor int, currency, description, returnURL string, metadata map[string]string) (paymentID, confirmationURL string, err error) {
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
