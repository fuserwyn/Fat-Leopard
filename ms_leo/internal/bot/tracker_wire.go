package bot

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type trackerWireStatus struct {
	OK      bool            `json:"ok"`
	Service string          `json:"service"`
	Wire    json.RawMessage `json:"wire"`
}

// pingTrackerWire — как связка бот↔воркер в myvibelab: перед постановкой
// джоба проверяем, что ms_tracker жив и умеет писать обратно в доску.
func (b *Bot) pingTrackerWire() error {
	if b == nil || b.config == nil {
		return ErrTrackerNotConfigured
	}
	base := strings.TrimRight(strings.TrimSpace(b.config.BoardURL), "/")
	if base == "" || strings.TrimSpace(b.config.BoardSecret) == "" {
		return ErrTrackerNotConfigured
	}
	req, err := http.NewRequest(http.MethodGet, base+"/health", nil)
	if err != nil {
		return err
	}
	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("трекер недоступен: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
	var parsed trackerWireStatus
	if json.Unmarshal(raw, &parsed) != nil || !parsed.OK {
		return fmt.Errorf("трекер не ответил на /health")
	}
	// Старый /health без wire ещё живой. Новый без notify — доска глухая.
	if len(parsed.Wire) > 0 {
		var wire struct {
			Notify bool `json:"notify"`
		}
		if json.Unmarshal(parsed.Wire, &wire) == nil && !wire.Notify {
			return fmt.Errorf("на трекере нет LEO_NOTIFY_URL — доска не узнает о сдаче")
		}
	}
	return nil
}
