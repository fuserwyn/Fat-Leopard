package bot

import (
	"context"
	"time"
)

// Фоновая сверка оплат ЮKassa.
//
// Штатный путь «оплатил → доступ» — вебхук ms_payments: он закрывает заявку и кладёт
// paywall_access_restore_requested в outbox. Если вебхук не дошёл (сеть, деплой ms_payments,
// неверный notification URL), доступ раньше выдавался только когда пользователь сам возвращался
// в бота и нажимал /start или кнопку оплаты — опрос API висел на действии юзера
// (paywallTrySyncYookassaPayment). Кейс redraych 2026-09-22: оплата картой прошла, доступ
// пришлось выдавать руками через админку.
//
// Здесь тот же GET /v3/payments/{id} крутится сам по pending-заявкам с созданным счётом, так что
// зачёт оплаты больше не зависит ни от вебхука, ни от того, вернётся ли человек в бота.
const (
	paywallReconcileTick         = 3 * time.Minute
	paywallReconcileFirstRunWait = 30 * time.Second
	// Дольше суток счёт ЮKassa всё равно протухает; 72 часа — запас на выходные и долгий инцидент.
	paywallReconcileMaxAge = 72 * time.Hour
	// Лимит на проход: опрос платный по квотам ЮKassa, а очередь разгребётся за несколько тиков.
	paywallReconcileBatch = 25
)

func (b *Bot) startPaywallYookassaReconciler(ctx context.Context) {
	select {
	case <-ctx.Done():
		return
	case <-time.After(paywallReconcileFirstRunWait):
	}
	// Первый проход сразу после старта: деплой бота сам подчищает зависшие оплаты.
	b.reconcilePaywallYookassaPayments()

	ticker := time.NewTicker(paywallReconcileTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.reconcilePaywallYookassaPayments()
		}
	}
}

func (b *Bot) reconcilePaywallYookassaPayments() {
	if !b.paywallActive() || !b.paywallYookassaReady() {
		return
	}
	rows, err := b.db.ListPendingPaywallRequestsWithYookassaPayment(paywallReconcileMaxAge, paywallReconcileBatch)
	if err != nil {
		b.logger.Errorf("paywall reconciler: список pending-заявок: %v", err)
		return
	}
	for i := range rows {
		rec := rows[i]
		if !b.paywallSyncYookassaPendingRequest(&rec) {
			continue
		}
		// Сюда попадаем только если оплата была succeeded, а заявка висела в pending: значит
		// вебхук не доехал. Доступ уже выдан, но владельцу стоит проверить ms_payments.
		b.notifyOps(
			"paywall: оплата ЮKassa зачтена фоновой сверкой, вебхук не дошёл — req=%d user=%d payment=%s. Доступ выдан, проверь ms_payments и YOOKASSA_NOTIFICATION_URL.",
			rec.ID, rec.UserID, rec.YookassaPaymentID.String,
		)
	}
}
