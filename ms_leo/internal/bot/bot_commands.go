package bot

import (
	"fmt"
	"strconv"
	"strings"

	"leo-bot/internal/database"
	"leo-bot/internal/domain"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Команды бота в чате: старт, помощь, топ, кубки, исключения, рассылки.

// leopardOnboardingBody — длинный онбординг (legacy): группы / paywall выключен.
// При активном paywall оплативший в личке получает на /start короткий текст как после оплаты (paywallPostPaymentUserText).
func leopardOnboardingBody() string {
	return leopardOnboardingBodyText
}

func welcomeStartText() string {
	return leopardOnboardingBody()
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() && b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.ensurePaywallInvoiceSent(msg.From.ID)
		if !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			return
		}
		if err := b.sendPaywallUnpaidPrivateScreen(msg.Chat.ID); err != nil {
			b.logger.Errorf("Failed to send paywall-only help: %v", err)
		}
		return
	}

	helpText := `🦁 Fat Leopard — Help

🐆 Getting in:
• Tap /start — mini app access is immediate and free
• Log any movement daily: 8 days without activity and you drop from the pack
• After dropping out, return is paid only (Russia card or Telegram Stars)

❤️ Support the project:
• «Donate» in the mini app profile
• Telegram Stars — worldwide; card — Russia
• Donations are optional: they don't affect access or removal rules

💬 Support:
• Tap «💬 Support» at the bottom
• Or message us — it goes straight to the team
• We reply in your DM with the bot

Stay active! 🦁`

	reply := tgbotapi.NewMessage(msg.Chat.ID, helpText)

	b.logger.Infof("Sending help message to chat %d", msg.Chat.ID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send help message: %v", errSend)
	} else {
		b.logger.Infof("Successfully sent help message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	// Вход в приложение на компьютере: ссылка t.me/<bot>?start=auth_<nonce>.
	// Показываем только подтверждение — витрина тут не к месту.
	if msg != nil && msg.Chat != nil && msg.Chat.IsPrivate() {
		if nonce := ParseDesktopStartPayload(msg.CommandArguments()); nonce != "" {
			b.handleDesktopLoginStart(msg, nonce)
			return
		}
	}
	// Ссылка на челлендж t.me/<бот>?start=ch-<код>: запоминаем его за пользователем,
	// мини-апп предложит принять (сам челлендж не стартует).
	var challengeInvite *database.Challenge
	if msg.From != nil && msg.Chat != nil && msg.Chat.IsPrivate() && b.db != nil {
		challengeInvite = b.rememberChallengeInviteFromStart(msg.From.ID, msg.CommandArguments())
	}
	// Личная ссылка друга t.me/<бот>?start=ref-<id>: засчитываем новичка за пригласившим
	// до записи визита и профиля — иначе он уже не новичок.
	if msg.From != nil && msg.Chat != nil && msg.Chat.IsPrivate() && b.db != nil {
		b.rememberReferralFromStart(msg.From.ID, msg.CommandArguments())
	}
	// Фиксируем визит в личке
	if msg.From != nil && msg.Chat.IsPrivate() && b.db != nil {
		b.SeedMiniappUILangIfEmpty(msg.From.ID, b.MonetizedChatID(), msg.From.LanguageCode)
		username := msg.From.UserName
		firstName := msg.From.FirstName
		lastName := msg.From.LastName
		go func() {
			if err := b.db.RecordBotVisit(msg.From.ID, username, firstName, lastName); err != nil {
				b.logger.Warnf("RecordBotVisit: %v", err)
			}
		}()
		// Воронка 1: bot_started с channel attribution из deep-link (?start=src-...).
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventBotStarted,
			TelegramID: msg.From.ID,
			Source:     parseStartSource(msg.CommandArguments()),
		})
	}
	// После оплаты ЮKassa вебхук может опоздать — подтягиваем succeeded и выдаём доступ до проверки paywall.
	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() {
		if b.config.PaywallYookassaReady() {
			b.paywallTrySyncYookassaPayment(msg.From.ID)
		}
		b.paywallTryFinishPaidAccessDelivery(msg.From.ID)
	}
	// Донат картой мог быть оплачен в браузере без возврата в мини-апп — догоняем «спасибо».
	// В горутине: это HTTP к ЮKassa, а /start ждать не должен.
	if msg.From != nil && msg.Chat.IsPrivate() {
		userID := msg.From.ID
		go b.DonateSyncPendingForUser(userID)
	}
	// Меню-кнопка LeopardMiniApp в ЛС: только paid+не кикнутым (после sync выше).
	if msg.From != nil && msg.Chat.IsPrivate() {
		b.applyMiniappMenuButtonForUser(msg.From.ID)
	}

	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() && b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.ensurePaywallInvoiceSent(msg.From.ID)
		if b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			b.logger.Infof("Sending paywall-only /start to chat %d", msg.Chat.ID)
			if err := b.sendPaywallUnpaidPrivateScreen(msg.Chat.ID); err != nil {
				b.logger.Errorf("Failed to send paywall /start: %v", err)
			}
			return
		}
		// Оплата могла подтянуться (sync) — показываем полный /start оплатившему, без второго сообщения только со ссылкой.
	}

	// Бесплатный вход: доступ есть, значит с этого /start заводим профиль стаи и таймер.
	if msg.From != nil && msg.Chat.IsPrivate() {
		b.EnsureFreeEntryFromStart(msg.From.ID, displayNameFromTelegramUser(msg.From))
	}

	welcomeText := welcomeStartText()
	// Короткий текст «ты в стае» — всем, у кого доступ есть: оплатившим и вошедшим бесплатно.
	if msg.Chat.IsPrivate() && b.paywallActive() && msg.From != nil && !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.logger.Infof("/start access granted user=%d free_entry=%t snapshot=%s",
			msg.From.ID, b.freeEntryActive(), b.db.PaywallAccessDebugSnapshot(msg.From.ID, b.config.MonetizedChatID))
		welcomeText = b.paywallPostPaymentUserText()
	}

	if challengeInvite != nil {
		welcomeText += fmt.Sprintf("\n\n🔥 You're invited to challenge «%s» — %d %s in a row with a workout. Open the mini app to accept.",
			challengeInvite.Title, challengeInvite.LengthDays, daysWordForm(challengeInvite.LengthDays))
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcomeText)
	if msg.Chat.IsPrivate() && msg.From != nil {
		if b.isAdminTelegramUser(msg.From.ID) {
			welcomeText += "\n\n⚙️ Admin panel — tap «" + botAdminReplyButtonText + "» at the bottom (or /admin)."
			reply.Text = welcomeText
		}
		if kb := b.privateBottomReplyKeyboard(msg.From.ID); kb != nil {
			reply.ReplyMarkup = kb
		}
		// Оплатившему добавляем сообщение с inline-кнопкой «Открыть» мини-аппу —
		// отдельным сообщением, чтобы не конфликтовать с reply-клавиатурой выше.
		if b.paywallActive() && !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			if ikb := b.miniappOpenInlineKeyboard(msg.From.ID); ikb != nil {
				open := tgbotapi.NewMessage(msg.Chat.ID, "Open workouts — tap the button below 👇")
				open.ReplyMarkup = *ikb
				if _, err := b.api.Send(open); err != nil {
					b.logger.Warnf("send miniapp open button user=%d: %v", msg.From.ID, err)
				}
			}
		}
	}

	b.logger.Infof("Sending start message to chat %d", msg.Chat.ID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send start message: %v", errSend)
	} else {
		b.logger.Infof("Successfully sent start message to chat %d", msg.Chat.ID)
	}
}

// handleRejoin — после миграции на мини-апп TG-группа выпилена; /rejoin turned intoи в напоминание открыть мини-апп.
func (b *Bot) handleRejoin(msg *tgbotapi.Message) {
	if !msg.Chat.IsPrivate() {
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "ℹ️ /rejoin works in DM with the bot."))
		return
	}
	if !b.paywallActive() || msg.From == nil {
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "ℹ️ Paid entry isn't used right now."))
		return
	}
	if b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Pay for access first. Tap /start to get an invoice.")
		if kb := b.privateBottomReplyKeyboard(msg.From.ID); kb != nil {
			reply.ReplyMarkup = kb
		}
		if _, err := b.api.Send(reply); err == nil {
			if ik := b.paywallUnpaidInlineKeyboard(msg.From.ID); ik != nil {
				methods := tgbotapi.NewMessage(msg.Chat.ID, b.Msg(msg.From.ID, "paywall_methods"))
				methods.ReplyMarkup = ik
				_, _ = b.api.Send(methods)
			}
		}
		return
	}
	_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Access is active. Open the bot mini app — at the bottom of this chat (or via the ⋮ menu)."))
}

func (b *Bot) handleDB(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Only administrators or the owner can use this command!")
		b.api.Send(reply)
		return
	}

	// Получаем статистику
	stats, err := b.db.GetDatabaseStats()
	if err != nil {
		b.logger.Errorf("Failed to get database stats: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load data")
		b.api.Send(reply)
		return
	}

	// Формируем отчет
	report := fmt.Sprintf("📊 DB stats:\n\n👥 Total users: %v\n✅ With training_done: %v\n🏥 On sick leave: %v\n💪 Recovered: %v",
		stats["total_users"], stats["training_done"], stats["sick_leave"], stats["healthy"])

	reply := tgbotapi.NewMessage(msg.Chat.ID, report)

	b.logger.Infof("Sending DB stats message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send DB stats message: %v", err)
	} else {
		b.logger.Infof("Successfully sent DB stats message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleTop(msg *tgbotapi.Message) {
	rowChat := b.packTrainingStateChatID(msg)
	// Получаем топ пользователей
	topUsers, err := b.db.GetTopUsers(rowChat, 10)
	if err != nil {
		b.logger.Errorf("Failed to get top users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load data")
		b.api.Send(reply)
		return
	}

	if len(topUsers) == 0 {
		emptyText := "🏆 **Leaderboard:**\n\n📊 No workout data yet"
		reply := tgbotapi.NewMessage(msg.Chat.ID, emptyText)
		reply.ParseMode = "Markdown"
		b.api.Send(reply)
		return
	}

	topText := "🏆 Leaderboard by cups:\n\n"
	for i, user := range topUsers {
		emoji := "🥇"
		if i == 1 {
			emoji = "🥈"
		} else if i == 2 {
			emoji = "🥉"
		} else {
			emoji = fmt.Sprintf("%d️⃣", i+1)
		}
		topText += fmt.Sprintf("%s %s — %d %s\n", emoji, user.Username, user.CupsEarned, cupsWordForm(user.CupsEarned))
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, topText)

	b.logger.Infof("Sending top users message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send top users message: %v", err)
	} else {
		b.logger.Infof("Successfully sent top users message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleCups(msg *tgbotapi.Message) {
	rowChat := b.packTrainingStateChatID(msg)
	// Получаем кубки пользователя
	cups, err := b.db.GetUserCups(msg.From.ID, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user cups: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load data")
		b.api.Send(reply)
		return
	}

	// Получаем пол пользователя для гендерной адаптации
	messageLog, err := b.db.GetMessageLog(msg.From.ID, rowChat)
	userGender := ""
	if err == nil {
		userGender = strings.TrimSpace(strings.ToLower(messageLog.Gender))
		if userGender == "" {
			userGender = b.detectGenderFromName(msg.From.FirstName)
		}
	}
	forms := b.getGenderForms(userGender)

	// Получаем никнейм пользователя
	username := ""
	if msg.From.UserName != "" {
		username = "@" + msg.From.UserName
	} else if msg.From.FirstName != "" {
		username = msg.From.FirstName
		if msg.From.LastName != "" {
			username += " " + msg.From.LastName
		}
	} else {
		username = fmt.Sprintf("User%d", msg.From.ID)
	}

	// Формируем сообщение в зависимости от количества cups
	var cupsText string
	if cups > 420 {
		cupsText = fmt.Sprintf("🌟⚡ SUPER LEVEL! ⚡🌟\n\n👤 %s\n🎯 Total cups earned: %d\n\n🎊 BEYOND ALL EXPECTATIONS! 🎊\n\n🦁 Fat Leopard is thrilled!\n💪 You're not just a champion — you're a SUPER-CHAMPION!\n🔥 Your power knows no bounds!\n⭐️ You inspire the whole pack!\n👑 Motivation can't believe it!\n🌟 You shine brightest!\n\n🎯 Keep it up, super leopard!", username, cups)
	} else if cups >= 420 {
		cupsText = fmt.Sprintf("🎊 CONGRATS! 🎊\n\n👤 %s\n🎯 Total cups earned: %d\n\n🏆 YOU %s THE RAFFLE GOAL!\n🎁 You're in the Fat Leopard tee raffle!\n💪 You're a true %s!\n🔥 Keep logging workouts!", username, cups, strings.ToUpper(forms.Reached), forms.Champion)
	} else {
		cupsText = fmt.Sprintf("🏆 Your cups:\n\n👤 %s\n🎯 Total cups earned: %d\n\n💡 Log workouts in the mini app to earn cups!\n\n🎊 Fat Leopard tee raffle at 420 cups!", username, cups)
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsText)

	b.logger.Infof("Sending cups message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send cups message: %v", err)
	} else {
		b.logger.Infof("Successfully sent cups message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleSetExempt(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Only administrators or the owner can use this command!")
		b.api.Send(reply)
		return
	}

	rowChat := b.packTrainingStateChatID(msg)

	// Парсим аргументы команды
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Usage: /set_exempt @username")
		b.api.Send(reply)
		return
	}

	// Извлекаем username из аргумента
	searchUsername := args[1]

	// Логируем поиск для отладки
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, rowChat)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ User %s not found in the database", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Устанавливаем исключение
	messageLog, err := b.db.GetMessageLog(userID, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load user data")
		b.api.Send(reply)
		return
	}

	messageLog.IsExemptFromDeletion = true
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to save message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to save data")
		b.api.Send(reply)
		return
	}

	// Отменяем таймер если он активен
	b.cancelTimer(userID)

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ User %s is exempt from inactivity removal", messageLog.Username))
	b.api.Send(reply)
}

func (b *Bot) handleRemoveExempt(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Only administrators or the owner can use this command!")
		b.api.Send(reply)
		return
	}

	rowChat := b.packTrainingStateChatID(msg)

	// Парсим аргументы команды
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Usage: /remove_exempt @username")
		b.api.Send(reply)
		return
	}

	// Извлекаем username из аргумента
	searchUsername := args[1]

	// Логируем поиск для отладки
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, rowChat)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ User %s not found in the database", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Убираем исключение
	messageLog, err := b.db.GetMessageLog(userID, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load user data")
		b.api.Send(reply)
		return
	}

	messageLog.IsExemptFromDeletion = false
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to save message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to save data")
		b.api.Send(reply)
		return
	}

	// Запускаем таймер для пользователя
	b.startTimer(userID, rowChat, messageLog.Username)

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ User %s is no longer exempt from inactivity removal. Timer started.", messageLog.Username))
	b.api.Send(reply)
}

func (b *Bot) handleListUsers(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Only administrators or the owner can use this command!")
		b.api.Send(reply)
		return
	}

	rowChat := b.packTrainingStateChatID(msg)

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load user list")
		b.api.Send(reply)
		return
	}

	if len(users) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "📝 No users in the database for this chat")
		b.api.Send(reply)
		return
	}

	// Формируем список пользователей
	var userList strings.Builder
	userList.WriteString("📋 Users in this chat:\n\n")

	for i, user := range users {
		exemptStatus := "❌"
		if user.IsExemptFromDeletion {
			exemptStatus = "✅"
		}

		userList.WriteString(fmt.Sprintf("%d. %s (ID: %d) %s\n",
			i+1, user.Username, user.UserID, exemptStatus))
	}

	userList.WriteString("\n✅ = exempt from removal\n❌ = subject to inactivity removal")

	reply := tgbotapi.NewMessage(msg.Chat.ID, userList.String())
	b.api.Send(reply)
}

func (b *Bot) handleSendToChat(msg *tgbotapi.Message) {
	// Проверяем права access - только админ из env может отправлять сообщения в другие чаты.
	if !b.config.IsAdminTelegramUser(msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ You don't have permission to use this command")
		b.api.Send(reply)
		return
	}

	// Получаем аргументы команды
	args := msg.CommandArguments()
	if args == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Usage: /send_to_chat <chat_id> <message text>")
		b.api.Send(reply)
		return
	}

	// Разбираем аргументы
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Usage: /send_to_chat <chat_id> <message text>")
		b.api.Send(reply)
		return
	}

	// Парсим chat_id
	idRaw := strings.TrimSpace(parts[0])
	// Нормализация: длинное тире → дефис, убрать неразрывные пробелы
	idRaw = strings.ReplaceAll(idRaw, "–", "-")
	idRaw = strings.ReplaceAll(idRaw, "—", "-")
	idRaw = strings.ReplaceAll(idRaw, "\u00A0", " ")
	// Фильтрация: оставить ведущий '-' и цифры
	var filtered strings.Builder
	for i, r := range idRaw {
		if i == 0 && r == '-' {
			filtered.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			filtered.WriteRune(r)
		}
	}
	idClean := filtered.String()
	chatID, err := strconv.ParseInt(idClean, 10, 64)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Invalid chat_id format")
		b.api.Send(reply)
		return
	}

	// Получаем текст сообщения
	messageText := parts[1]

	// Создаем сообщение для отправки
	chatMessage := tgbotapi.NewMessage(chatID, messageText)

	// Отправляем сообщение в указанный чат
	b.logger.Infof("Sending message to chat %d: %s", chatID, messageText)
	_, err = b.api.Send(chatMessage)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ Failed to send message to chat %d: %v", chatID, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
		b.api.Send(reply)
		b.logger.Errorf("Failed to send message to chat %d: %v", chatID, err)
	} else {
		botUsername := b.api.Self.UserName
		if botUsername == "" {
			botUsername = fmt.Sprintf("bot_%d", b.api.Self.ID)
		}
		if saveErr := b.db.SaveUserMessage(&domain.UserMessage{
			UserID:      b.api.Self.ID,
			ChatID:      chatID,
			Username:    botUsername,
			MessageText: messageText,
			MessageType: "ai_reply",
		}); saveErr != nil {
			b.logger.Warnf("Failed to persist send_to_chat message for chat %d: %v", chatID, saveErr)
		} else {
			b.logger.Infof("Persisted send_to_chat message for chat %d", chatID)
		}

		successMsg := fmt.Sprintf("✅ Message sent to chat %d", chatID)
		reply := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
		b.api.Send(reply)
		b.logger.Infof("Successfully sent message to chat %d", chatID)
	}
}

func (b *Bot) handleAnnounceAI(msg *tgbotapi.Message) {
	// Проверяем права access - только админ из env может отправлять объявления.
	if !b.config.IsAdminTelegramUser(msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ You don't have permission to use this command")
		b.api.Send(reply)
		return
	}

	// Получаем все чаты из БД
	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("Failed to get chat IDs: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Failed to load chat list")
		b.api.Send(reply)
		return
	}

	if len(chatIDs) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ No chats found")
		b.api.Send(reply)
		return
	}

	// Формируем объявление о ИИ
	announcement := `🦁 The leopard is alive! 🎉

You can chat with me now — I'm smarter thanks to AI:

💬 What I can do:
• Give workout advice
• Share your stats
• Analyze your progress
• Motivate and cheer you on

🤖 How to talk to me:
• Mention @LeoPoacherBot in a message
• Or reply to any of my messages

Ask me anything: workouts, stats, motivation!

💪 Let's get better together!`

	// Отправляем объявление во все чаты
	successCount := 0
	errorCount := 0

	for _, chatID := range chatIDs {
		chatMessage := tgbotapi.NewMessage(chatID, announcement)
		b.logger.Infof("Sending AI announcement to chat %d", chatID)
		_, err := b.api.Send(chatMessage)
		if err != nil {
			b.logger.Errorf("Failed to send announcement to chat %d: %v", chatID, err)
			errorCount++
		} else {
			b.logger.Infof("Successfully sent announcement to chat %d", chatID)
			successCount++
		}
	}

	// Отправляем отчет владельцу
	resultMsg := fmt.Sprintf("✅ Announcement sent!\n\nSuccess: %d chats\nErrors: %d chats", successCount, errorCount)
	reply := tgbotapi.NewMessage(msg.Chat.ID, resultMsg)
	b.api.Send(reply)
}
