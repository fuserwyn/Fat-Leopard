package bot

// Очередь ответов Лео для poll мини-аппа хранится в памяти процесса (без Redis/БД).
// Один инстанс бота — типичный деплой. Несколько реплик: нужен общий кэш (Redis) или снова БД.

import "strings"

func (b *Bot) miniappPersonalClear(userID int64) {
	if b == nil || userID == 0 {
		return
	}
	b.miniappPersonalMu.Lock()
	defer b.miniappPersonalMu.Unlock()
	if b.miniappPersonalQueue != nil {
		delete(b.miniappPersonalQueue, userID)
	}
}

func (b *Bot) miniappPersonalPush(userID int64, text string) {
	if b == nil || userID == 0 {
		return
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	b.miniappPersonalMu.Lock()
	defer b.miniappPersonalMu.Unlock()
	if b.miniappPersonalQueue == nil {
		b.miniappPersonalQueue = make(map[int64][]string)
	}
	b.miniappPersonalQueue[userID] = append(b.miniappPersonalQueue[userID], t)
}

// PopMiniappPersonalReply — один фрагмент ответа для poll API (FIFO в памяти процесса).
func (b *Bot) PopMiniappPersonalReply(userID int64) (text string, ok bool) {
	if b == nil || userID == 0 {
		return "", false
	}
	b.miniappPersonalMu.Lock()
	defer b.miniappPersonalMu.Unlock()
	q := b.miniappPersonalQueue[userID]
	if len(q) == 0 {
		return "", false
	}
	head := q[0]
	rest := q[1:]
	if len(rest) == 0 {
		delete(b.miniappPersonalQueue, userID)
	} else {
		b.miniappPersonalQueue[userID] = rest
	}
	return head, true
}
