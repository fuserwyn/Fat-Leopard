package bot

import (
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const paywallPayloadPrefix = "pw_"

const paywallCallbackResendInvoice = "paywall_resend_invoice" // совместимость со старыми сообщениями
const paywallCallbackPayStars = "paywall_pay_stars"
const paywallCallbackPayYookassa = "paywall_pay_yookassa"
const paywallCallbackPayProvider = "paywall_pay_provider"
const paywallCallbackReturnToPack = "paywall_return_to_pack"
const paywallCallbackBackToMethods = "paywall_back_to_methods"

// Несколько попыток: вебхук ЮKassa может закрыть заявку в БД чуть позже события успешной оплаты.
const paywallAccessRecheckAttempts = 5
const paywallAccessRecheckDelay = 800 * time.Millisecond

func (b *Bot) userHasActivePaywallAccessResilient(userID, chatID int64) (bool, error) {
	var lastErr error
	for i := 0; i < paywallAccessRecheckAttempts; i++ {
		if i > 0 {
			time.Sleep(paywallAccessRecheckDelay)
		}
		ok, err := b.db.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			lastErr = err
			continue
		}
		lastErr = nil
		if ok {
			return true, nil
		}
	}
	if lastErr != nil {
		return false, lastErr
	}
	return false, nil
}

// paywallInvoiceErrLog — строка для логов (полная диагностика).
func paywallInvoiceErrLog(err error) string {
	if err == nil {
		return ""
	}
	var tgErr *tgbotapi.Error
	if errors.As(err, &tgErr) {
		return fmt.Sprintf("telegram error_code=%d: %s", tgErr.Code, tgErr.Message)
	}
	return err.Error()
}

// paywallInvoiceShortHintForUser — коротко, без переменных окружения (детали только в логах).
func paywallInvoiceShortHintForUser(err error) string {
	if err == nil {
		return ""
	}
	var tgErr *tgbotapi.Error
	if !errors.As(err, &tgErr) {
		return "Попробуй ещё раз чуть позже или другой способ оплаты."
	}
	m := strings.ToLower(tgErr.Message)
	switch {
	case strings.Contains(m, "payment_provider_invalid"):
		return "Счёт в Telegram сейчас недоступен. Попробуй другой способ кнопкой ниже."
	case strings.Contains(m, "currency_invalid"), strings.Contains(m, "currency_total_amount_invalid"):
		return "Платёж не прошёл проверку. Нажми /start и запроси счёт снова."
	case strings.Contains(m, "invoice_payload_invalid"), strings.Contains(m, "invoice_invalid"):
		return "Счёт отклонён Telegram. Обнови приложение или выбери оплату картой другой кнопкой."
	case tgErr.Code == 403 || strings.Contains(m, "blocked"):
		return "Разблокируй бота: ⋮ в чате → Разблокировать."
	case strings.Contains(m, "chat not found") || strings.Contains(m, "user is deactivated"):
		return "Напиши боту любое сообщение в личке и снова нажми кнопку."
	default:
		return "Не вышло отправить счёт. Попробуй оплату картой другой кнопкой или /start позже."
	}
}

// paywallYookassaShortHintForUser — понятная подсказка пользователю по типовым сбоям создания ссылки ЮKassa.
func paywallYookassaShortHintForUser(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "credentials empty"):
		return "Оплата ЮKassa временно недоступна (не заданы ключи). Напиши администратору."
	case strings.Contains(msg, "amount must be positive"):
		return "Оплата ЮKassa временно недоступна (некорректная сумма). Напиши администратору."
	case strings.Contains(msg, "return_url must be http"):
		return "Оплата ЮKassa временно недоступна (некорректный URL возврата). Напиши администратору."
	case strings.Contains(msg, "http 401"), strings.Contains(msg, "http 403"):
		return "ЮKassa отклонила запрос (проверь ключи магазина). Попробуй позже."
	case strings.Contains(msg, "http 400"):
		return "ЮKassa вернула ошибку параметров платежа. Попробуй позже."
	case strings.Contains(msg, "timeout"), strings.Contains(msg, "deadline exceeded"):
		return "ЮKassa долго отвечает. Попробуй ещё раз через минуту."
	default:
		return "Ссылка на оплату не создалась. Попробуй позже."
	}
}

// paywallActive — платёжная инфраструктура включена и задана целевая стая.
// Включает и бесплатный режим входа: оплата остаётся нужна для возврата после кика
// (см. freeEntryActive) и для донатов из профиля мини-аппа.
func (b *Bot) paywallActive() bool {
	return b.config.PaywallEnabled && b.config.MonetizedChatID != 0
}

// freeEntryActive — вход для новичков бесплатный (PAYWALL_ENTRY_FREE, по умолчанию true).
// Платной остаётся единственная точка: возврат после кика за 8 дней неактивности.
func (b *Bot) freeEntryActive() bool {
	return b.paywallActive() && b.config.PaywallEntryFree
}

// paywallEntryRequiresPayment — нужен ли платёж за сам вход в стаю (для UserInPackOrPaid).
func (b *Bot) paywallEntryRequiresPayment() bool {
	return b.paywallActive() && !b.config.PaywallEntryFree
}

// paywallPriceYookassaShort — короткая «210 ₽» / «210,50 ₽» / «100 USD» для кнопки/UI.
func (b *Bot) paywallPriceYookassaShort() string {
	if !b.paywallYookassaReady() {
		return ""
	}
	yk := b.paywallYookassaAmountMinor()
	yc := strings.TrimSpace(b.config.YookassaCurrency)
	if yk <= 0 {
		return ""
	}
	return formatPaywallAmountShort(yk, yc)
}

// paywallPriceProviderShort — короткая цена для Telegram Provider Invoice (карта в TG).
func (b *Bot) paywallPriceProviderShort() string {
	if !b.config.PaywallUsesTelegramProviderInvoice() {
		return ""
	}
	am := b.paywallProviderAmountMinor()
	cur := strings.TrimSpace(b.config.PaymentCurrency)
	if am <= 0 || cur == "" || cur == "XTR" {
		return ""
	}
	return formatPaywallAmountShort(am, cur)
}

// paywallPriceStarsShort — «350 ⭐» для кнопки.
func (b *Bot) paywallPriceStarsShort() string {
	n := b.config.PaywallStarsInvoiceAmount()
	if n <= 0 {
		return ""
	}
	return fmt.Sprintf("%d ⭐", n)
}

// paywallPrivateUnpaidUserText — только оплата и шаги (без полной справки бота).
// Цены вынесены прямо в кнопки выбора способа (см. paywallUnpaidInlineKeyboard) — здесь не дублируем.
//
// Модель доступа: разовая оплата. После успешной оплаты доступ к мини-аппу не истекает по сроку;
// единственное, что его закрывает — кик за 8 дней неактивности (после кика нужна повторная покупка).
// Поэтому никаких «на 30 дней» в текстах не пишем (см. отдельную просьбу пользователя).
func (b *Bot) paywallPrivateUnpaidUserText() string {
	if !b.paywallPaymentReady() {
		return `💳 Платный доступ к Fat Leopard MiniApp

⚠️ Оплата у бота ещё не настроена. Напиши администратору.

Когда заработает — здесь появятся кнопки выбора способа оплаты.`
	}

	// Бесплатный вход: этот экран видят только выбывшие за 8 дней неактивности —
	// новичкам платить не за что, и обещать им «что получаешь» уже не нужно.
	if b.freeEntryActive() {
		return `
Рык! Ты выбыл из стаи — 8 дней без единого движения.

Вход для новичков бесплатный, но возвращение — нет: цена невысокая, зато она позволит моим создателям развивать проект.

После оплаты твой профиль снова откроется: чат со Стаей, комментарии Лео к тренировкам и отслеживание прогресса. Выбери способ — картой для РФ или звёздами телеграма для любой страны.`
	}

	return `
Рык! Платный вход. Цена невысокая, но она позволит моим создателям развивать проект для тебя.

Что получаешь:
🐆 чат со Стаей
💬 комментарии Лео к тренировкам
📈 отслеживание своего прогресса

Выбери, как оплатить — картой для РФ или звёздами телеграма для любой страны. После оплаты ты получишь доступ в Стаю. Пока я тебя не съем....`
}

// paywallUnpaidInlineKeyboard — отдельные кнопки под каждый способ оплаты.
// Цена пишется прямо в подписи кнопки, чтобы юзер не получал отдельных сообщений
// с дублированием цены (см. требование пользователя «и в карте и в звёздах сразу
// в способах оплаты писать стоимость»).
func (b *Bot) paywallUnpaidInlineKeyboard() *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if b.paywallYookassaReady() {
		label := "💳 Банковской картой — для РФ"
		if p := b.paywallPriceYookassaShort(); p != "" {
			label = "💳 Банковской картой — для РФ — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayYookassa),
		))
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		label := "💳 Банковская карта (в Telegram)"
		if p := b.paywallPriceProviderShort(); p != "" {
			label = "💳 Банковская карта (в Telegram) — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayProvider),
		))
	}
	if b.config.PaywallUsesStars() {
		label := "⭐ Звёздами Telegram"
		if p := b.paywallPriceStarsShort(); p != "" {
			label = "⭐ Звёздами Telegram — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayStars),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// sendPaywallUnpaidPrivateScreen — paywall + «💬 Поддержка» внизу; кнопки оплаты отдельным сообщением (API не совмещает reply и inline).
func (b *Bot) sendPaywallUnpaidPrivateScreen(chatID int64) error {
	m := tgbotapi.NewMessage(chatID, b.paywallPrivateUnpaidUserText())
	if kb := b.privateBottomReplyKeyboard(chatID); kb != nil {
		m.ReplyMarkup = kb
	}
	if _, err := b.api.Send(m); err != nil {
		return err
	}
	if ik := b.paywallUnpaidInlineKeyboard(); ik != nil {
		methods := tgbotapi.NewMessage(chatID, "Способы оплаты:")
		methods.ReplyMarkup = ik
		if _, err := b.api.Send(methods); err != nil {
			return err
		}
	}
	// Воронка 1: юзер увидел экран выбора способа оплаты.
	b.db.TrackEvent(database.AnalyticsEvent{Name: database.EventPaywallViewed, TelegramID: chatID})
	return nil
}

func (b *Bot) paywallReturnInlineKeyboard() *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	if b.paywallYookassaReady() {
		label := "💳 Банковской картой — для РФ"
		if p := b.paywallPriceYookassaShort(); p != "" {
			label = "💳 Банковской картой — для РФ — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayYookassa),
		))
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		label := "💳 Оплатить картой (Telegram)"
		if p := b.paywallPriceProviderShort(); p != "" {
			label = "💳 Оплатить картой (Telegram) — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayProvider),
		))
	}
	if b.config.PaywallUsesStars() {
		label := "⭐ Звёздами Telegram"
		if p := b.paywallPriceStarsShort(); p != "" {
			label = "⭐ Звёздами Telegram — " + p
		}
		rows = append(rows, tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData(label, paywallCallbackPayStars),
		))
	}
	if len(rows) == 0 {
		return nil
	}
	return &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

// paywallReturnPromptText — текст экрана покупки доступа (после кика за неактивность или из inline-кнопки «Вернуться»).
// Группы у нас больше нет, и доступ — разовая покупка без срока (см. paywallPrivateUnpaidUserText), поэтому ни
// «возвращения», ни «вступления», ни «N дней» в тексте. Цена прямо на кнопках (см. paywallReturnInlineKeyboard).
// Параметр оставлен в сигнатуре для совместимости со старыми вызовами/тестами, но не подставляется.
func paywallReturnPromptText(_ string) string {
	return "🚪 Вход в Fat Leopard MiniApp открывается после оплаты.\n\nВыбери способ оплаты ниже — цена указана прямо на кнопке."
}

func starsWordRU(n int) string {
	n = n % 100
	if n >= 11 && n <= 14 {
		return "звёзд"
	}
	switch n % 10 {
	case 1:
		return "звезда"
	case 2, 3, 4:
		return "звезды"
	default:
		return "звёзд"
	}
}

func starsChargeVerbRU(n int) string {
	n = n % 100
	if n == 1 || (n >= 21 && n%10 == 1) {
		return "спишется"
	}
	return "спишутся"
}

func (b *Bot) paywallStarsMethodText() string {
	stars := b.config.PaywallStarsInvoiceAmount()
	if stars <= 0 {
		stars = 1
	}
	return fmt.Sprintf(
		"Оплата звёздами Telegram.\n\n%d %s %s с твоего баланса. После успешной оплаты ты сразу получишь кнопку доступа в мини-апп приложение Леопарда в этом чате. Если с оплатой будет проблема, напиши запрос в нашу поддержку.\n\nНажимая кнопку «Оплатить», ты принимаешь оферту, правила Стаи и политику модерации UGC. Тренировки и комментарии видны участникам Стаи. Персональные данные обрабатываются для работы сервиса (152-ФЗ).",
		stars, starsWordRU(stars), starsChargeVerbRU(stars),
	)
}

func (b *Bot) paywallCardMethodText() string {
	price := b.paywallPriceYookassaShort()
	if strings.TrimSpace(price) == "" {
		price = "сумма по тарифу"
	}
	return fmt.Sprintf(
		"Оплата банковской картой.\n\nДоступна картами РФ. %s спишется, и после успешной оплаты ты сразу получишь кнопку доступа в мини-апп приложение Леопарда в этом чате. Если с оплатой будет проблема, напиши запрос в нашу поддержку.\n\nНажимая кнопку «Оплатить», ты принимаешь оферту, правила Стаи и политику модерации UGC. Тренировки и комментарии видны участникам Стаи. Персональные данные обрабатываются для работы сервиса (152-ФЗ).",
		price,
	)
}

// paywallStarsInvoiceReplyMarkup — Pay (свой текст) + «Назад»; первая кнопка обязана быть Pay (Telegram API).
func (b *Bot) paywallStarsInvoiceReplyMarkup() *tgbotapi.InlineKeyboardMarkup {
	stars := b.config.PaywallStarsInvoiceAmount()
	if stars <= 0 {
		stars = 1
	}
	return &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			{{
				Text: fmt.Sprintf("Оплатить ⭐%d", stars),
				Pay:  true,
			}},
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", paywallCallbackBackToMethods),
			),
		},
	}
}

func (b *Bot) paywallCardMethodInlineKeyboard(confirmURL string) *tgbotapi.InlineKeyboardMarkup {
	var rows [][]tgbotapi.InlineKeyboardButton
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonURL("💳 Оплатить (ЮKassa)", confirmURL),
	))
	rows = append(rows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", paywallCallbackBackToMethods),
	))
	return &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
}

func (b *Bot) paywallNotifyUser(userID int64, text string) {
	if userID == 0 {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, text)); err != nil {
		b.logger.Errorf("paywall user notify: %v", err)
	}
}

// ensurePaywallInvoiceSent создаёт pending-заявку и подтягивает оплату ЮKassa при /start; счета не шлёт — пользователь жмёт кнопки.
func (b *Bot) ensurePaywallInvoiceSent(userID int64) {
	if !b.paywallActive() || userID == 0 {
		return
	}
	if !b.paywallPaymentReady() {
		return
	}
	if ok, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID); err != nil {
		b.logger.Errorf("paywall ensure invoice access check: %v", err)
	} else if ok {
		b.paywallTryFinishPaidAccessDelivery(userID)
		return
	}
	// Оплата по ссылке ЮKassa: вебхук может не дойти — подтягиваем succeeded по payment id в pending-заявке.
	if b.paywallYookassaReady() && b.paywallTrySyncYookassaPayment(userID) {
		return
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall ensure invoice get pending: %v", err)
		b.paywallNotifyUser(userID, "⚠️ Временная ошибка. Попробуй /start чуть позже.")
		return
	}
	if pending != nil {
		return
	}
	if _, err := b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID); err != nil {
		b.logger.Errorf("paywall ensure invoice insert: %v", err)
		b.paywallNotifyUser(userID, "⚠️ Не удалось начать оплату. Попробуй /start снова.")
	}
}

// paywallGetOrCreatePendingReqID — для callback: последняя pending-заявка или новая.
func (b *Bot) paywallGetOrCreatePendingReqID(userID int64) (int64, error) {
	rec, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		return 0, err
	}
	if rec != nil {
		return rec.ID, nil
	}
	return b.db.InsertPaywallAccessRequest(userID, b.config.MonetizedChatID)
}

// paywallSendPaymentOffers — всё сразу (старые кнопки «выслать снова»); ошибки пользователю короткие, детали в логах.
func (b *Bot) paywallSendPaymentOffers(userID, reqID int64) {
	if b.paywallYookassaReady() {
		if err := b.SendYookassaPaymentLink(userID, reqID); err != nil {
			b.logger.Errorf("paywall yookassa link: %v", err)
			b.paywallNotifyUser(userID, "⚠️ "+paywallYookassaShortHintForUser(err))
		}
	}
	if b.config.PaywallUsesTelegramProviderInvoice() {
		if err := b.SendPaywallProviderInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall provider invoice: %s", paywallInvoiceErrLog(err))
			b.paywallNotifyUser(userID, "⚠️ "+paywallInvoiceShortHintForUser(err))
		}
	}
	if b.config.PaywallUsesStars() {
		if err := b.SendPaywallStarsInvoice(userID, reqID); err != nil {
			b.logger.Errorf("paywall stars invoice: %s", paywallInvoiceErrLog(err))
			b.paywallNotifyUser(userID, "⚠️ "+paywallInvoiceShortHintForUser(err))
		}
	}
}

// SendPaywallStarsInvoice — XTR, provider_token пустой; payload pw_<reqID>.
func paywallInvoiceClipTitle(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Доступ"
	}
	r := []rune(s)
	if len(r) > 32 {
		return string(r[:32])
	}
	return s
}

func paywallInvoiceClipDescription(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "Оплата доступа"
	}
	r := []rune(s)
	if len(r) > 255 {
		return string(r[:255])
	}
	return s
}

// paywallStarsInvoiceTitleAndDesc — полный текст в счёте (title ≤32, description ≤255).
func paywallStarsInvoiceTitleAndDesc(methodText string) (title, desc string) {
	methodText = strings.TrimSpace(methodText)
	if methodText == "" {
		return paywallInvoiceClipTitle("Оплата звёздами"), paywallInvoiceClipDescription("Оплата доступа")
	}
	head, body, _ := strings.Cut(methodText, "\n\n")
	head = strings.TrimSpace(head)
	body = strings.TrimSpace(body)
	if head == "" {
		return paywallInvoiceClipTitle("Оплата звёздами"), paywallInvoiceClipDescription(body)
	}
	if body == "" {
		return paywallInvoiceClipTitle(head), paywallInvoiceClipDescription("Оплата доступа")
	}
	return paywallInvoiceClipTitle(head), paywallInvoiceClipDescription(body)
}

func (b *Bot) SendPaywallStarsInvoice(userID, reqID int64) error {
	if !b.config.PaywallUsesStars() {
		return fmt.Errorf("stars payment not configured")
	}
	amt := b.config.PaywallStarsInvoiceAmount()
	if amt <= 0 {
		return fmt.Errorf("stars amount invalid")
	}
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	prices := []tgbotapi.LabeledPrice{{Label: "Доступ в Стаю", Amount: amt}}
	title, desc := paywallStarsInvoiceTitleAndDesc(b.paywallStarsMethodText())
	inv := tgbotapi.NewInvoice(
		userID,
		title,
		desc,
		payload,
		"",
		"",
		"XTR",
		prices,
	)
	// Workaround for Telegram API validation: library may encode nil as null.
	// Telegram expects an array for suggested_tip_amounts when field is present.
	inv.SuggestedTipAmounts = []int{}
	inv.ReplyMarkup = b.paywallStarsInvoiceReplyMarkup()
	_, err := b.api.Send(inv)
	return err
}

// SendPaywallProviderInvoice — RUB/др. через PAYMENT_PROVIDER_TOKEN; payload pw_<reqID>.
func (b *Bot) SendPaywallProviderInvoice(userID, reqID int64) error {
	if !b.config.PaywallUsesTelegramProviderInvoice() {
		return fmt.Errorf("telegram provider invoice not configured")
	}
	tok := strings.TrimSpace(b.config.PaymentProviderToken)
	payload := fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID)
	prices := []tgbotapi.LabeledPrice{{Label: "Доступ", Amount: b.paywallProviderAmountMinor()}}
	inv := tgbotapi.NewInvoice(
		userID,
		paywallInvoiceClipTitle(b.config.PaymentInvoiceTitle),
		paywallInvoiceClipDescription(b.config.PaymentInvoiceDesc),
		payload,
		tok,
		"",
		b.config.PaymentCurrency,
		prices,
	)
	inv.SuggestedTipAmounts = []int{}
	_, err := b.api.Send(inv)
	return err
}

// paywallCreateYookassaPayment создаёт платёж в ЮKassa и возвращает confirmation URL.
// Сообщение пользователю НЕ отправляет — это нужно для нового флоу с editMessage в callback'е,
// где URL-кнопка ставится прямо в исходное сообщение «Выбери способ оплаты», без второго экрана
// «вот тебе ещё одна кнопка Оплатить» (см. требование пользователя).
//
// Если у заявки уже есть yookassa_payment_id и он succeeded — новый платёж не создаём
// (иначе затрём id успешной оплаты и sync/вебхук потеряют след: кейс redraych 2026-09-22).
func (b *Bot) paywallCreateYookassaPayment(userID, reqID int64) (string, error) {
	if b.config.YookassaShopID == "" || b.config.YookassaSecretKey == "" {
		return "", fmt.Errorf("yookassa credentials empty")
	}
	if existing, err := b.db.GetPaywallAccessRequestByID(reqID); err == nil && existing != nil {
		if existing.YookassaPaymentID.Valid {
			oldID := strings.TrimSpace(existing.YookassaPaymentID.String)
			if oldID != "" {
				info, gerr := yookassa.GetPayment(b.config.YookassaShopID, b.config.YookassaSecretKey, oldID)
				if gerr == nil && info != nil {
					st := strings.ToLower(strings.TrimSpace(info.Status))
					if st == "succeeded" && info.Paid {
						return "", fmt.Errorf("yookassa payment already succeeded (id=%s); sync instead of new link", oldID)
					}
					// pending / waiting_for_capture — не плодим второй счёт, отдаём ошибку
					// вызывающему: пусть сначала дожмёт sync или покажет «оплата ещё в обработке».
					if st == "pending" || st == "waiting_for_capture" {
						return "", fmt.Errorf("yookassa payment still %s (id=%s); wait or /start sync", st, oldID)
					}
				}
			}
		}
	}
	returnURL := strings.TrimSpace(b.config.YookassaReturnURL)
	if returnURL == "" {
		returnURL = "https://t.me"
	}
	meta := map[string]string{
		"user_telegram_id": fmt.Sprintf("%d", userID),
		"invoice_payload":  fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID),
	}
	paymentID, confirmURL, err := yookassa.CreatePayment(
		b.config.YookassaShopID,
		b.config.YookassaSecretKey,
		b.paywallYookassaAmountMinor(),
		b.config.YookassaCurrency,
		b.config.PaymentInvoiceDesc,
		returnURL,
		b.config.YookassaNotificationURL,
		meta,
	)
	if err != nil {
		return "", err
	}
	if err := b.db.SetPaywallYookassaPaymentID(reqID, paymentID); err != nil {
		b.logger.Warnf("paywall SetPaywallYookassaPaymentID: %v", err)
	}
	return confirmURL, nil
}

// SendYookassaPaymentLink создаёт платёж в ЮKassa и шлёт пользователю отдельное сообщение
// с кнопкой-ссылкой. Используется legacy-флоу paywallSendPaymentOffers (resend invoice),
// где мы шлём ВСЕ доступные способы сразу. В обновлённом per-method-callback'е используем
// paywallCreateYookassaPayment + edit исходного сообщения (ниже в handlePaywallPayYookassaCallback).
func (b *Bot) SendYookassaPaymentLink(userID, reqID int64) error {
	confirmURL, err := b.paywallCreateYookassaPayment(userID, reqID)
	if err != nil {
		return err
	}
	text := `💳 Оплата доступа картой (ЮKassa).`
	msg := tgbotapi.NewMessage(userID, text)
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(tgbotapi.NewInlineKeyboardButtonURL("💳 Оплатить (ЮKassa)", confirmURL)),
		},
	}
	_, err = b.api.Send(msg)
	return err
}

func (b *Bot) handlePaywallResendInvoiceCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	if !b.paywallActive() || !b.paywallPaymentReady() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата сейчас недоступна."))
		return
	}
	if b.paywallYookassaReady() {
		if b.paywallTrySyncYookassaPayment(callback.From.ID) {
			_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
			return
		}
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(callback.From.ID)
	if err != nil {
		b.logger.Errorf("paywall resend pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	b.paywallSendPaymentOffers(callback.From.ID, reqID)
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Проверь новые сообщения в чате."))
}

func (b *Bot) handlePaywallPayStarsCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.paywallPaymentReady() || !b.config.PaywallUsesStars() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Счёт на звёзды сейчас недоступен."))
		return
	}
	// Воронка 1: выбран способ оплаты «Звёзды».
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventPaymentMethodSelected,
		TelegramID: uid,
		Payload:    map[string]any{"provider": "stars"},
	})
	if b.paywallYookassaReady() && b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall stars cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	// Один счёт: полный текст + «Оплатить ⭐N» + «Назад» (Telegram Stars).
	if err := b.SendPaywallStarsInvoice(uid, reqID); err != nil {
		b.logger.Errorf("paywall stars invoice: %s", paywallInvoiceErrLog(err))
		h := paywallInvoiceShortHintForUser(err)
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}
	// Воронка 1: счёт показан → переход к оплате.
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventPaymentInitiated,
		TelegramID: uid,
		Payload:    map[string]any{"provider": "stars"},
	})
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Нажми «Оплатить» в счёте выше."))
}

func (b *Bot) handlePaywallPayYookassaCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.paywallPaymentReady() || !b.paywallYookassaReady() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата картой сейчас недоступна."))
		return
	}
	// Воронка 1: выбран способ оплаты «Карта РФ» (ЮKassa).
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventPaymentMethodSelected,
		TelegramID: uid,
		Payload:    map[string]any{"provider": "yukassa"},
	})
	if b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall yk cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}

	confirmURL, err := b.paywallCreateYookassaPayment(uid, reqID)
	if err != nil {
		// Уже оплаченный / ещё обрабатываемый платёж на этой заявке — не создаём второй счёт.
		if b.paywallTrySyncYookassaPayment(uid) {
			_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
			return
		}
		b.logger.Errorf("paywall yookassa create payment: %v", err)
		h := paywallYookassaShortHintForUser(err)
		if strings.Contains(err.Error(), "already succeeded") {
			h = "Оплата уже прошла. Нажми /start — доступ должен открыться."
		} else if strings.Contains(err.Error(), "still pending") || strings.Contains(err.Error(), "waiting_for_capture") {
			h = "Оплата ещё обрабатывается. Подожди минуту и нажми /start."
		}
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}

	// Не плодим новое сообщение «вот тебе ещё одна кнопка Оплатить» (см. требование пользователя):
	// заменяем кнопки выбора способа в исходном сообщении на одну URL-кнопку с готовой ссылкой ЮKassa.
	// Юзер делает один тап и сразу попадает на страницу оплаты — без промежуточного экрана.
	payButton := b.paywallCardMethodInlineKeyboard(confirmURL)
	step := tgbotapi.NewMessage(uid, b.paywallCardMethodText())
	step.ReplyMarkup = payButton
	if _, err := b.api.Send(step); err != nil {
		b.logger.Errorf("paywall yk callback send step message: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось открыть оплату. Попробуй /start."))
		return
	}
	// Воронка 1: ссылка ЮKassa создана → переход к форме оплаты.
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:       database.EventPaymentInitiated,
		TelegramID: uid,
		Payload:    map[string]any{"provider": "yukassa"},
	})
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
}

func (b *Bot) handlePaywallBackToMethodsCallback(callback *tgbotapi.CallbackQuery) {
	if callback == nil || callback.From == nil {
		return
	}
	if !b.paywallActive() || !b.paywallPaymentReady() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Оплата сейчас недоступна."))
		return
	}
	if err := b.sendPaywallUnpaidPrivateScreen(callback.From.ID); err != nil {
		b.logger.Errorf("paywall back to methods send: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось вернуть экран оплаты. Напиши /start."))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
}

func (b *Bot) handlePaywallPayProviderCallback(callback *tgbotapi.CallbackQuery) {
	if callback.From == nil {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, ""))
		return
	}
	uid := callback.From.ID
	if !b.paywallActive() || !b.paywallPaymentReady() || !b.config.PaywallUsesTelegramProviderInvoice() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Счёт провайдера сейчас недоступен."))
		return
	}
	if b.paywallYookassaReady() && b.paywallTrySyncYookassaPayment(uid) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Оплата уже учтена. Нажми /start."))
		return
	}
	reqID, err := b.paywallGetOrCreatePendingReqID(uid)
	if err != nil {
		b.logger.Errorf("paywall provider cb pending: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Ошибка. Попробуй /start."))
		return
	}
	if err := b.SendPaywallProviderInvoice(uid, reqID); err != nil {
		b.logger.Errorf("paywall provider invoice: %s", paywallInvoiceErrLog(err))
		h := paywallInvoiceShortHintForUser(err)
		if len(h) > 180 {
			h = h[:177] + "…"
		}
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, h))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Счёт отправлен — открой его выше и нажми «Оплатить»."))
}

func (b *Bot) handlePaywallReturnToPackCallback(callback *tgbotapi.CallbackQuery) {
	if callback == nil || callback.From == nil {
		return
	}
	if !b.paywallActive() {
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Возврат сейчас недоступен."))
		return
	}

	if !b.paywallPrivateNeedsPayFirst(callback.From.ID) {
		_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Доступ уже активен — открой мини-приложение."))
		return
	}

	msg := tgbotapi.NewMessage(callback.From.ID, paywallReturnPromptText(""))
	msg.ReplyMarkup = b.paywallReturnInlineKeyboard()
	if msg.ReplyMarkup == nil {
		msg.Text = "⚠️ Оплата временно недоступна в твоём регионе. Попробуй позже."
	}
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Errorf("paywall return callback send pay options: %v", err)
		_, _ = b.api.Request(tgbotapi.NewCallbackWithAlert(callback.ID, "Не удалось отправить экран оплаты. Напиши /start."))
		return
	}
	_, _ = b.api.Request(tgbotapi.NewCallback(callback.ID, "Открой сообщение ниже — там выбор оплаты."))
}

// paywallDecideNeedsPayment — вся таблица решений «кто платит» в одном месте, без обращений к БД.
//
//	админ                                  → никогда не платит;
//	есть активная оплата                   → не платит (даже если ещё is_deleted: кик
//	                                          обнуляет access_expires_at, поэтому active access
//	                                          после кика бывает только в окне «оплата прошла,
//	                                          ReactivateReturnedUser ещё не доехал» — тогда
//	                                          paywallTryFinishPaidAccessDelivery дожимает доступ);
//	выбыл за неактивность (kicked)          → платит, даже при бесплатном входе:
//	                                          это единственная платная точка продукта;
//	бесплатный вход (entryFree)             → новичок не платит;
//	платный вход (PAYWALL_ENTRY_FREE=false) → нужна активная (не истёкшая) оплата.
func paywallDecideNeedsPayment(entryFree, isAdmin, kicked, hasActiveAccess bool) bool {
	if isAdmin {
		return false
	}
	if hasActiveAccess {
		return false
	}
	if kicked {
		return true
	}
	if entryFree {
		return false
	}
	return true
}

// paywallPrivateNeedsPayFirst — нужно ли платить перед доступом к мини-аппу.
//
// При бесплатном входе (PAYWALL_ENTRY_FREE) платит только выбывший за неактивность:
// новичок получает доступ сразу, а поддержать проект может донатом из профиля.
// При PAYWALL_ENTRY_FREE=false работает прежняя логика: без активной оплаты входа нет.
func (b *Bot) paywallPrivateNeedsPayFirst(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	if b.config.IsAdminTelegramUser(userID) {
		return false
	}
	kicked := false
	if ml, err := b.db.GetMessageLogAnyState(userID, b.config.MonetizedChatID); err == nil && ml != nil {
		kicked = ml.IsDeleted
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		b.logger.Errorf("paywall deletion check: %v", err)
		return true
	}
	// Активную оплату спрашиваем всегда, когда kick/платный вход могут зависеть от неё:
	// после успешной оплаты ЮKassa заявка уже completed, а is_deleted ещё true, пока outbox
	// не вызвал ReactivateReturnedUser — без этой проверки юзер снова видит экран оплаты.
	hasActiveAccess := false
	if !b.freeEntryActive() || kicked {
		ok, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
		if err != nil {
			b.logger.Errorf("paywall access check: %v", err)
			return true
		}
		hasActiveAccess = ok
	}
	return paywallDecideNeedsPayment(b.config.PaywallEntryFree, false, kicked, hasActiveAccess)
}

func parsePaywallPayload(payload string) (requestID int64, ok bool) {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(payload, paywallPayloadPrefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(payload[len(paywallPayloadPrefix):], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// paywallTrySyncYookassaPayment — если вебхук не дошёл, подтягиваем успешную оплату через GET /v3/payments/{id}.
func (b *Bot) paywallTrySyncYookassaPayment(userID int64) bool {
	if !b.paywallActive() || userID == 0 {
		return false
	}
	if !b.paywallYookassaReady() {
		return false
	}
	pending, err := b.db.GetLatestPendingPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil {
		b.logger.Errorf("paywall yookassa sync get pending: %v", err)
		return false
	}
	if pending == nil {
		return false
	}
	if !pending.YookassaPaymentID.Valid || strings.TrimSpace(pending.YookassaPaymentID.String) == "" {
		b.logger.Warnf(
			"paywall yookassa sync: заявка id=%d user=%d в pending, но yookassa_payment_id пуст — опрос API ЮKassa невозможен. "+
				"Нужны миграция 16, деплой бота с сохранением payment id и новая ссылка на оплату (или рабочий вебхук ms_payments).",
			pending.ID, userID,
		)
		return false
	}
	info, err := yookassa.GetPayment(b.config.YookassaShopID, b.config.YookassaSecretKey, pending.YookassaPaymentID.String)
	if err != nil {
		b.logger.Warnf("paywall yookassa sync GetPayment: %v", err)
		return false
	}
	st := strings.ToLower(strings.TrimSpace(info.Status))
	if st != "succeeded" || !info.Paid {
		// canceled — терминальный отказ ЮKassa. Воронка 1: payment_failed.
		// Idempotency по payment_id: опрос при /start повторяется, дубли не плодим (§9.2).
		if st == "canceled" {
			b.db.TrackEvent(database.AnalyticsEvent{
				Name:           database.EventPaymentFailed,
				UserID:         userID,
				TelegramID:     userID,
				Payload:        map[string]any{"provider": "yukassa", "reason": "canceled", "payment_id": pending.YookassaPaymentID.String},
				IdempotencyKey: "payment_failed:" + pending.YookassaPaymentID.String,
			})
		}
		return false
	}
	meta := info.Metadata
	userStr := strings.TrimSpace(meta["user_telegram_id"])
	if userStr == "" {
		userStr = strings.TrimSpace(meta["user_telegramId"])
	}
	payloadStr := strings.TrimSpace(meta["invoice_payload"])
	if payloadStr == "" {
		payloadStr = strings.TrimSpace(meta["invoicePayload"])
	}
	if userStr != fmt.Sprintf("%d", userID) {
		b.logger.Warnf("paywall yookassa sync user mismatch meta=%q db_user=%d", userStr, userID)
		return false
	}
	reqFromMeta, ok := parsePaywallPayload(payloadStr)
	if !ok || reqFromMeta != pending.ID {
		b.logger.Warnf("paywall yookassa sync payload mismatch meta=%q want id=%d", payloadStr, pending.ID)
		return false
	}
	amountMinor := info.AmountMinor
	cur := info.Currency
	if amountMinor <= 0 || cur == "" {
		amountMinor = b.paywallYookassaAmountMinor()
		cur = b.config.YookassaCurrency
	}
	if cur == "" {
		cur = "RUB"
	}
	okDb, err := b.db.CompletePaywallAccessRequestAndEnqueueRestore(pending.ID, userID, b.config.MonetizedChatID, info.ID, amountMinor, cur)
	if err != nil {
		b.logger.Errorf("paywall yookassa sync complete: %v", err)
		return false
	}
	if !okDb {
		paid, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
		if err == nil && paid {
			b.logger.Infof("paywall yookassa sync: заявка %d уже completed (вебхук)", pending.ID)
			b.paywallTryFinishPaidAccessDelivery(userID)
		}
		return false
	}
	b.logger.Infof("paywall yookassa sync: заявка %d закрыта по API ЮKassa", pending.ID)
	if derr := b.paywallDeliverAccessAfterPayment(userID, pending.ID, nil); derr != nil {
		b.logger.Errorf("paywall yookassa sync deliver user=%d req=%d: %v", userID, pending.ID, derr)
	}
	b.paywallAfterPaywallAccessGranted(userID, pending.ID)
	return true
}

// paywallTryFinishPaidAccessDelivery — доступ в БД есть, но welcome/таймер/кнопка мини-аппа не доехали (сбой outbox или вебхука).
func (b *Bot) paywallTryFinishPaidAccessDelivery(userID int64) {
	if !b.paywallActive() || userID == 0 || b.config.IsAdminTelegramUser(userID) {
		return
	}
	ok, err := b.db.UserHasActivePaywallAccess(userID, b.config.MonetizedChatID)
	if err != nil || !ok {
		return
	}
	rec, err := b.db.GetLatestCompletedPaywallAccessRequest(userID, b.config.MonetizedChatID)
	if err != nil || rec == nil {
		return
	}
	needDeliver := false
	welcomeSent, err := b.db.PaywallPostPaymentWelcomeSent(rec.ID)
	if err != nil {
		b.logger.Warnf("paywall finish deliver welcome check req=%d: %v", rec.ID, err)
	}
	if !welcomeSent {
		needDeliver = true
	}
	// GetMessageLog — только активные; для кикнутых берём AnyState: иначе после оплаты
	// при is_deleted=true «нет таймера» не видим и не дожимаем ReactivateReturnedUser.
	ml, mlErr := b.db.GetMessageLogAnyState(userID, b.config.MonetizedChatID)
	if mlErr != nil || ml == nil || ml.IsDeleted || ml.TimerStartTime == nil || strings.TrimSpace(*ml.TimerStartTime) == "" {
		needDeliver = true
	}
	if !needDeliver {
		invalidateMiniappMenuButtonCache(userID)
		b.applyMiniappMenuButtonForUser(userID)
		return
	}
	if derr := b.paywallDeliverAccessAfterPayment(userID, rec.ID, nil); derr != nil {
		b.logger.Errorf("paywall finish deliver user=%d req=%d: %v", userID, rec.ID, derr)
		if enqErr := b.db.EnqueuePaywallAccessRestoreEvent(rec.ID, userID, b.config.MonetizedChatID); enqErr != nil {
			b.logger.Errorf("paywall finish enqueue restore req=%d: %v", rec.ID, enqErr)
		}
	}
	b.paywallAfterPaywallAccessGranted(userID, rec.ID)
}

func (b *Bot) paywallAfterPaywallAccessGranted(userID, requestID int64) {
	invalidateMiniappMenuButtonCache(userID)
	b.applyMiniappMenuButtonForUser(userID)
	if requestID > 0 {
		b.logger.Infof("paywall access granted user=%d req=%d", userID, requestID)
	}
}

func (b *Bot) handlePaywallPreCheckout(q *tgbotapi.PreCheckoutQuery) {
	// reject — единая точка отказа в pre_checkout: фиксирует payment_failed (воронка 1, §3) и
	// отвечает Telegram OK:false. provider определяем по валюте счёта (XTR = звёзды, иначе карта).
	reject := func(reason, userMsg string) {
		provider := "stars"
		if q.Currency != "XTR" {
			provider = "card"
		}
		var tgID int64
		if q.From != nil {
			tgID = q.From.ID
		}
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventPaymentFailed,
			UserID:     tgID,
			TelegramID: tgID,
			Payload:    map[string]any{"provider": provider, "reason": reason, "stage": "pre_checkout"},
		})
		_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: false, ErrorMessage: userMsg})
	}
	if q.From == nil {
		reject("no_sender", "Оплата недоступна.")
		return
	}
	telegramInvoice := b.config.PaywallUsesTelegramInvoice()
	if !b.paywallActive() || !telegramInvoice {
		reject("unavailable", "Оплата недоступна.")
		return
	}
	switch q.Currency {
	case "XTR":
		if !b.config.PaywallUsesStars() || q.TotalAmount != b.config.PaywallStarsInvoiceAmount() {
			b.logger.Warnf("paywall pre_checkout stars mismatch: got %s %d want XTR %d", q.Currency, q.TotalAmount, b.config.PaywallStarsInvoiceAmount())
			reject("amount_mismatch", "Неверная сумма (звёзды). Обнови заявку /start.")
			return
		}
	default:
		if !b.config.PaywallUsesTelegramProviderInvoice() {
			reject("unavailable", "Оплата недоступна.")
			return
		}
		if strings.TrimSpace(b.config.PaymentProviderToken) == "" {
			reject("unavailable", "Оплата недоступна.")
			return
		}
		if q.Currency != b.config.PaymentCurrency || q.TotalAmount != b.paywallProviderAmountMinor() {
			b.logger.Warnf("paywall pre_checkout amount mismatch: got %s %d want %s %d", q.Currency, q.TotalAmount, b.config.PaymentCurrency, b.paywallProviderAmountMinor())
			reject("amount_mismatch", "Неверная сумма. Обнови заявку и попробуй снова.")
			return
		}
	}
	reqID, ok := parsePaywallPayload(q.InvoicePayload)
	if !ok {
		reject("invalid_payload", "Некорректный платёж.")
		return
	}
	rec, err := b.db.GetPaywallAccessRequestByID(reqID)
	if err != nil || rec == nil {
		b.logger.Errorf("paywall pre_checkout load request: %v", err)
		reject("request_not_found", "Заявка не найдена. Нажми /start снова.")
		return
	}
	if rec.Status != "pending" {
		reject("stale_invoice", "Этот счёт уже неактуален.")
		return
	}
	if rec.UserID != q.From.ID || rec.MonetizedChatID != b.config.MonetizedChatID {
		reject("account_mismatch", "Платёж не для этого аккаунта.")
		return
	}
	_, _ = b.api.Request(tgbotapi.PreCheckoutConfig{PreCheckoutQueryID: q.ID, OK: true})
}

// paywallPostPaymentUserText — сообщение сразу после успешной оплаты (перед первым входом в мини-апп).
func (b *Bot) paywallPostPaymentUserText() string {
	return `Ура, ты в стае! Кнопка внизу - это вход в мини-апп. Там есть подробные правила. Там же я буду ждать твою первую тренировку.

Чтобы оставаться в стае, и чтобы я тебя не съел, просто делай любое движение хотя бы раз в неделю и вноси его в ленту тренировок стаи.

Вход бесплатный, но если захочешь поддержать проект - внутри есть кнопка для донатов. Рык!`
}

// displayNameFromTelegramUser — строка для training_state.username (как при сообщениях из ЛС).
func displayNameFromTelegramUser(u *tgbotapi.User) string {
	if u == nil {
		return ""
	}
	if un := strings.TrimSpace(u.UserName); un != "" {
		return "@" + strings.TrimPrefix(un, "@")
	}
	s := strings.TrimSpace(u.FirstName)
	if ln := strings.TrimSpace(u.LastName); ln != "" {
		if s != "" {
			s += " " + ln
		} else {
			s = ln
		}
	}
	if s != "" {
		return s
	}
	if u.ID != 0 {
		return fmt.Sprintf("user%d", u.ID)
	}
	return ""
}

func displayNameFromTelegramChat(chat *tgbotapi.Chat) string {
	if chat == nil {
		return ""
	}
	if un := strings.TrimSpace(chat.UserName); un != "" {
		return "@" + strings.TrimPrefix(un, "@")
	}
	s := strings.TrimSpace(chat.FirstName)
	if ln := strings.TrimSpace(chat.LastName); ln != "" {
		if s != "" {
			s += " " + ln
		} else {
			s = ln
		}
	}
	return strings.TrimSpace(s)
}

// resolveUsernameForPaywallDeliver — имя до ReactivateReturnedUser: иначе INSERT кладёт NULLIF(”, ”) → NULL в БД.
func (b *Bot) resolveUsernameForPaywallDeliver(userID int64, payer *tgbotapi.User) string {
	if userID == 0 {
		return ""
	}
	if payer != nil {
		if n := displayNameFromTelegramUser(payer); n != "" {
			return n
		}
	}
	if b != nil && b.api != nil {
		ch, err := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: userID}})
		if err != nil {
			b.logger.Warnf("paywall resolve username getChat user=%d: %v", userID, err)
		} else if n := displayNameFromTelegramChat(&ch); n != "" {
			return n
		}
	}
	return fmt.Sprintf("user%d", userID)
}

// paywallDeliverAccessAfterPayment — DM-приветствие после зачёта оплаты (Telegram Payments / ЮKassa / sync API).
//
// По требованию пользователя «при оплате сразу включается таймер timer_start_time»: окно неактивности
// (5/6/7 дни — предупреждения, 8 день — кик) отсчитываем с момента подтверждения оплаты, а не с первого
// открытия мини-аппа. Это убирает класс багов, когда оплативший никогда не зашёл в мини-апп и формально
// «жил вечно» без отчётов; с другой стороны — у новичка есть полные 8 дней, чтобы освоиться.
//
// Здесь же пишем карточку «вступил/вернулся» в ленту мини-аппа — раньше это делал EnsureMiniAppOnboarding
// при первом заходе. Теперь ENtry-stream единый, а EnsureMiniAppOnboarding для оплаченных видит уже
// активный timer_start_time и просто отдаёт InPack=true.
//
// payer — отправитель successful_payment (nil из outbox → имя через getChat).
//
// paywallRequestID > 0 отмечает в БД факт отправки пост-оплатного DM — без второго «Ура, ты в стае…» при ретраях/outbox.
func (b *Bot) paywallDeliverAccessAfterPayment(userID int64, paywallRequestID int64, payer *tgbotapi.User) error {
	chatID := b.config.MonetizedChatID

	if paywallRequestID > 0 {
		rec, err := b.db.GetPaywallAccessRequestByID(paywallRequestID)
		if err != nil {
			return fmt.Errorf("paywall deliver load request %d: %w", paywallRequestID, err)
		}
		if rec == nil {
			return fmt.Errorf("paywall deliver: request %d not found (user deleted or stale outbox?)", paywallRequestID)
		}
		if rec.Status != "completed" {
			return fmt.Errorf("paywall deliver: request %d status=%q, want completed", paywallRequestID, rec.Status)
		}
		if rec.UserID != userID || rec.MonetizedChatID != chatID {
			return fmt.Errorf("paywall deliver: request %d user/chat mismatch", paywallRequestID)
		}
		ok, err := b.db.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			return fmt.Errorf("paywall deliver active access check: %w", err)
		}
		if !ok {
			return fmt.Errorf("paywall deliver: no active paywall access for user=%d chat=%d", userID, chatID)
		}
		// Воронка 1: payment_completed. Idempotency key по заявке — ретраи outbox/Telegram не плодят дубли (§9.2).
		b.trackPaymentCompleted(userID, rec)
	}

	// Сначала текст в ЛС — чтобы юзер не «пропал» после оплаты даже если ниже БД займёт время или ошибётся.
	// По paywallRequestID не шлём второй раз (ретрай Telegram / outbox), чтобы не дублировать «Ура, ты в стае…».
	welcomePending := true
	if paywallRequestID > 0 {
		sent, err := b.db.PaywallPostPaymentWelcomeSent(paywallRequestID)
		if err != nil {
			return fmt.Errorf("paywall welcome_sent check req=%d: %w", paywallRequestID, err)
		}
		welcomePending = !sent
	}
	if welcomePending {
		welcomeMsg := tgbotapi.NewMessage(userID, b.paywallPostPaymentUserText())
		// Сразу после оплаты — inline-кнопка «Открыть» мини-аппу одним тапом.
		if ikb := b.miniappOpenInlineKeyboard(userID); ikb != nil {
			welcomeMsg.ReplyMarkup = *ikb
		}
		if _, err := b.api.Send(welcomeMsg); err != nil {
			b.logger.Errorf("paywall send done msg user=%d: %v", userID, err)
			return err
		}
		if paywallRequestID > 0 {
			if err := b.db.PaywallMarkPostPaymentWelcomeSent(paywallRequestID); err != nil {
				b.logger.Errorf("paywall mark welcome_sent req=%d user=%d: %v", paywallRequestID, userID, err)
			}
		}
		b.logger.Infof("paywall post-payment welcome sent user=%d req=%d", userID, paywallRequestID)
		// Воронка 1: «Ура, ты в Стае» отправлено.
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:           database.EventWelcomeMessageSent,
			TelegramID:     userID,
			IdempotencyKey: welcomeEventIdempotencyKey(paywallRequestID),
		})
	}

	resolvedName := strings.TrimSpace(b.resolveUsernameForPaywallDeliver(userID, payer))
	if resolvedName == "" {
		resolvedName = fmt.Sprintf("user%d", userID)
	}
	reactivated, err := b.db.ReactivateReturnedUser(userID, chatID, resolvedName)
	if err != nil {
		b.logger.Errorf("paywall reactivate returned user=%d: %v", userID, err)
		return err
	}
	if !reactivated {
		b.logger.Errorf("paywall reactivate returned false user=%d chat=%d", userID, chatID)
		return fmt.Errorf("paywall inconsistency: no profile for paid return user=%d chat=%d", userID, chatID)
	}
	b.trackAccountReactivated(userID) // §5: вернулся после удаления

	// Username для приветственной карточки берём из только что обновлённой записи training_state.
	// Если пусто — startTimer / savePackJoinMiniappFeed корректно отработают с пустой строкой.
	username := ""
	if ml, mlErr := b.db.GetMessageLog(userID, chatID); mlErr == nil && ml != nil {
		username = strings.TrimSpace(ml.Username)
	}

	// 1) Стартуем таймер активности немедленно — это пишет timer_start_time = NOW в training_state
	//    и регистрирует горутины для дней 5/6/7/8. ReactivateReturnedUser выше выставил
	//    timer_start_time = NULL, поэтому здесь startTimer делает «чистый» старт без cancel.
	b.startTimer(userID, chatID, username)

	// 2) Карточка ленты мини-аппа: pack_join (первая оплата) либо pack_rejoin (после кика).
	//    Различаем по return_count — он инкрементируется в ReactivateReturnedUser при возврате.
	//    Личное приветствие в ЛС уже отправлено выше (paywallPostPaymentUserText) — второе не шлём.
	isRejoin := false
	if rc, rcErr := b.db.GetUserReturnCount(userID, chatID); rcErr == nil && rc > 1 {
		isRejoin = true
	}
	if isRejoin {
		b.savePackJoinMiniappFeed(
			chatID,
			userID,
			username,
			userMessageTypePackRejoin,
			packRejoinMiniappFeedPublicText(),
			"",
		)
	} else {
		b.savePackJoinMiniappFeed(
			chatID,
			userID,
			username,
			userMessageTypePackJoin,
			packJoinMiniappFeedPublicText(username),
			"",
		)
	}

	// 3) Синяя кнопка LeopardMiniApp в ЛС только для paid + не кикнутых.
	//    После успешной оплаты статус «paid» точно действителен — сбрасываем кеш и применяем.
	invalidateMiniappMenuButtonCache(userID)
	b.applyMiniappMenuButtonForUser(userID)
	return nil
}

func (b *Bot) handlePaywallSuccessfulPayment(msg *tgbotapi.Message) {
	if msg.From == nil || msg.SuccessfulPayment == nil {
		return
	}
	if !b.paywallActive() {
		b.logger.Warnf("paywall successful_payment ignored: paywall inactive user=%d", msg.From.ID)
		return
	}
	sp := msg.SuccessfulPayment
	cur := strings.ToUpper(strings.TrimSpace(sp.Currency))
	switch cur {
	case "XTR":
		wantStars := b.config.PaywallStarsInvoiceAmount()
		if !b.config.PaywallUsesStars() {
			b.logger.Errorf("paywall successful_payment XTR but stars disabled in config user=%d", msg.From.ID)
			return
		}
		if sp.TotalAmount != wantStars {
			b.logger.Errorf(
				"paywall successful_payment stars mismatch user=%d: got amount=%d currency=%q want_amount=%d — PAYMENT_STARS_AMOUNT / PAYMENT_CURRENCY=XTR / PAYMENT_AMOUNT_MINOR_UNITS",
				msg.From.ID, sp.TotalAmount, cur, wantStars,
			)
			return
		}
	default:
		wantCur := strings.ToUpper(strings.TrimSpace(b.config.PaymentCurrency))
		if !b.config.PaywallUsesTelegramProviderInvoice() || cur != wantCur || sp.TotalAmount != b.paywallProviderAmountMinor() {
			b.logger.Errorf(
				"paywall successful_payment mismatch user=%d: got %s %d, config wants %s %d — провайдер / PAYMENT_AMOUNT_*",
				msg.From.ID, cur, sp.TotalAmount, b.config.PaymentCurrency, b.paywallProviderAmountMinor(),
			)
			return
		}
	}
	reqID, ok := parsePaywallPayload(sp.InvoicePayload)
	if !ok {
		b.logger.Warnf("paywall successful_payment bad invoice_payload user=%d payload=%q", msg.From.ID, sp.InvoicePayload)
		return
	}
	rec, err := b.db.GetPaywallAccessRequestByID(reqID)
	if err != nil || rec == nil {
		b.logger.Errorf("paywall payment load request id=%d user=%d: %v", reqID, msg.From.ID, err)
		return
	}
	if rec.UserID != msg.From.ID || rec.MonetizedChatID != b.config.MonetizedChatID {
		b.logger.Warnf("paywall payment user/chat mismatch req=%d", reqID)
		return
	}
	okDb, err := b.db.CompletePaywallAccessRequest(reqID, msg.From.ID, b.config.MonetizedChatID, sp.TelegramPaymentChargeID, sp.TotalAmount, cur, false)
	if err != nil {
		b.logger.Errorf("paywall complete request: %v", err)
		return
	}
	deliver := func() {
		if derr := b.paywallDeliverAccessAfterPayment(msg.From.ID, reqID, msg.From); derr != nil {
			b.logger.Errorf("paywall deliver after telegram payment user=%d req=%d: %v", msg.From.ID, reqID, derr)
			sent, chkErr := b.db.PaywallPostPaymentWelcomeSent(reqID)
			if chkErr != nil {
				b.logger.Errorf("paywall welcome_sent check after deliver fail req=%d: %v", reqID, chkErr)
				return
			}
			if !sent {
				if enqErr := b.db.EnqueuePaywallAccessRestoreEvent(reqID, msg.From.ID, b.config.MonetizedChatID); enqErr != nil {
					b.logger.Errorf("paywall enqueue restore after deliver fail req=%d: %v", reqID, enqErr)
				} else {
					b.logger.Infof("paywall enqueued restore after deliver fail user=%d req=%d", msg.From.ID, reqID)
				}
			}
		}
	}
	if !okDb {
		// Частый случай: Telegram повторил successful_payment после того как мы уже закрыли заявку в БД,
		// но не успели/не смогли доставить DM (или упали после COMMIT). Раньше здесь был простой return —
		// пользователь оставался без сообщения навсегда.
		recDone, errDone := b.db.GetPaywallAccessRequestByID(reqID)
		if errDone != nil || recDone == nil || recDone.Status != "completed" {
			b.logger.Infof("paywall duplicate successful_payment noop request=%d user=%d", reqID, msg.From.ID)
			return
		}
		if recDone.UserID != msg.From.ID || recDone.MonetizedChatID != b.config.MonetizedChatID {
			b.logger.Warnf("paywall duplicate successful_payment user/chat mismatch req=%d", reqID)
			return
		}
		b.logger.Infof("paywall duplicate successful_payment retry deliver request=%d user=%d", reqID, msg.From.ID)
		deliver()
		return
	}
	// Сразу приветствие и меню мини-аппа — не ждём outbox (иначе сообщение может не прийти при лаге воркера).
	deliver()
}
