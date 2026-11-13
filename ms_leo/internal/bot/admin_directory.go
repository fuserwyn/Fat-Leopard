package bot

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	adminUsersListPageSize    = 10
	adminPaymentsListPageSize = 8
)

func (b *Bot) showAdminUsersListPage(chatID int64, offset int) {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MonetizedChatID not configured."))
		return
	}
	if offset < 0 {
		offset = 0
	}
	counts, err := b.db.CountPackUsersBreakdownForAdmin(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load list."))
		return
	}
	total := counts.Total
	users, err := b.db.ListPackUsersForAdmin(packChatID, offset, adminUsersListPageSize, database.PackUserAdminFilterAll)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ List error: "+err.Error()))
		return
	}

	subtitle := "Nobody yet."
	tableText := ""
	if total > 0 {
		from := offset + 1
		to := offset + len(users)
		subtitle = fmt.Sprintf("Active %d · kicked %d · rows %d–%d of %d · tap # below",
			counts.Active, counts.Kicked, from, to, total)

		tbl := newAdminTable(
			[]string{"No.", "ID", "Username", "Cups", "Str"},
			[]int{2, 10, 12, 4, 3},
		)
		for i, u := range users {
			tbl.addRow(
				strconv.Itoa(offset+i+1),
				strconv.FormatInt(u.UserID, 10),
				adminPaywallPersonLabel(u.Username, u.DisplayName, u.UserID),
				strconv.Itoa(u.Cups),
				strconv.Itoa(u.StreakDays),
			)
		}
		tableText = tbl.render()
	}

	keyboardRows := make([][]tgbotapi.InlineKeyboardButton, 0, 4)
	if len(users) > 0 {
		keyboardRows = appendAdminPickButtonRows(keyboardRows, users, offset)
	}
	nav := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if offset > 0 {
		prev := offset - adminUsersListPageSize
		if prev < 0 {
			prev = 0
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀️ Back", "admin_users_list_"+strconv.Itoa(prev)))
	}
	if offset+len(users) < total {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("▶️ Next", "admin_users_list_"+strconv.Itoa(offset+adminUsersListPageSize)))
	}
	if len(nav) > 0 {
		keyboardRows = append(keyboardRows, nav)
	}
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("🔍 Find", "admin_users"),
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
	))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboardRows}
	b.sendAdminHTMLPreTable(chatID, "📋 Pack members", subtitle, tableText, kb)
}

func (b *Bot) showAdminPaymentsPage(chatID int64, offset int) {
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ MonetizedChatID not configured."))
		return
	}
	if offset < 0 {
		offset = 0
	}
	sums, err := b.db.AdminSumCompletedMoney(packChatID, time.Time{}, false)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load payment summary."))
		return
	}
	statsTbl := adminBuildMoneyStatsTable(sums)

	total, err := b.db.CountMoneyPaymentsForAdmin(packChatID)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load payments."))
		return
	}
	payments, err := b.db.ListMoneyPaymentsForAdmin(packChatID, offset, adminPaymentsListPageSize)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Error: "+err.Error()))
		return
	}

	subtitle := "No requests yet."
	tableText := ""
	if total > 0 {
		from := offset + 1
		to := offset + len(payments)
		subtitle = fmt.Sprintf("Rows %d–%d of %d · tap # below", from, to, total)

		tbl := newAdminTable(
			[]string{"No.", "Type", "Username", "Status", "Amount", "Date"},
			[]int{2, 7, 13, 6, 7, 8},
		)
		for i, p := range payments {
			cur := ""
			if p.Currency.Valid {
				cur = p.Currency.String
			}
			when := p.CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04")
			tbl.addRow(
				strconv.Itoa(offset+i+1),
				adminMoneyKindCurrencyLabel(p.Kind, cur),
				adminPaywallPersonLabel(p.Username, p.DisplayName, p.UserID),
				adminPaymentStatusForKind(p.Kind, p.Status, p.AccessActive),
				adminFormatPaymentAmount(p.AmountMinor, p.Currency),
				when,
			)
		}
		tableText = tbl.render()
	}

	statsTable := newAdminTable(statsTbl.Columns, []int{12, 5, 7})
	for _, row := range statsTbl.Rows {
		statsTable.addRow(row...)
	}
	combinedTable := statsTable.render()
	if tableText != "" {
		combinedTable += "\n\n" + tableText
	}

	keyboardRows := make([][]tgbotapi.InlineKeyboardButton, 0, 4)
	if len(payments) > 0 {
		pickUsers := make([]database.AdminPackUserListRow, len(payments))
		for i, p := range payments {
			pickUsers[i] = database.AdminPackUserListRow{UserID: p.UserID}
		}
		keyboardRows = appendAdminPickButtonRows(keyboardRows, pickUsers, offset)
	}
	nav := make([]tgbotapi.InlineKeyboardButton, 0, 2)
	if offset > 0 {
		prev := offset - adminPaymentsListPageSize
		if prev < 0 {
			prev = 0
		}
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("◀️ Back", "admin_payments_"+strconv.Itoa(prev)))
	}
	if offset+len(payments) < total {
		nav = append(nav, tgbotapi.NewInlineKeyboardButtonData("▶️ Next", "admin_payments_"+strconv.Itoa(offset+adminPaymentsListPageSize)))
	}
	if len(nav) > 0 {
		keyboardRows = append(keyboardRows, nav)
	}
	keyboardRows = append(keyboardRows, tgbotapi.NewInlineKeyboardRow(
		tgbotapi.NewInlineKeyboardButtonData("📋 Users", "admin_users_list_0"),
		tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin", "admin_open"),
	))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: keyboardRows}
	pageSubtitle := statsTbl.Subtitle
	if subtitle != "" {
		pageSubtitle += "\n" + subtitle
	}
	b.sendAdminHTMLPreTable(chatID, "💳 Payments", pageSubtitle, combinedTable, kb)
}

func appendAdminPickButtonRows(rows [][]tgbotapi.InlineKeyboardButton, users []database.AdminPackUserListRow, offset int) [][]tgbotapi.InlineKeyboardButton {
	const perRow = 5
	btnRow := make([]tgbotapi.InlineKeyboardButton, 0, perRow)
	for i, u := range users {
		btnRow = append(btnRow, tgbotapi.NewInlineKeyboardButtonData(
			strconv.Itoa(offset+i+1),
			"admin_user_open_"+strconv.FormatInt(u.UserID, 10),
		))
		if len(btnRow) >= perRow {
			rows = append(rows, btnRow)
			btnRow = make([]tgbotapi.InlineKeyboardButton, 0, perRow)
		}
	}
	if len(btnRow) > 0 {
		rows = append(rows, btnRow)
	}
	return rows
}

// adminPaywallPersonLabel — только Telegram-ник (@username) для таблиц adminки.
func adminPaywallPersonLabel(username, displayName string, userID int64) string {
	_ = displayName
	u := strings.TrimSpace(username)
	if u == "" {
		return strconv.FormatInt(userID, 10)
	}
	if strings.HasPrefix(u, "@") {
		return u
	}
	if strings.HasPrefix(strings.ToLower(u), "user") {
		return strconv.FormatInt(userID, 10)
	}
	if !strings.Contains(u, " ") && adminLooksLikeTelegramHandle(u) {
		return "@" + u
	}
	return strconv.FormatInt(userID, 10)
}

func adminLooksLikeTelegramHandle(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			continue
		}
		return false
	}
	return true
}

// adminPaymentStatusShort — сокращённый статус для узкой колонки.
func adminPaymentStatusShort(status string, accessActive bool) string {
	switch status {
	case "completed":
		if accessActive {
			return "ok"
		}
		return "ok-"
	case "pending":
		return "pending"
	case "cancelled":
		return "cancel"
	case "refunded":
		return "return"
	default:
		return status
	}
}

func adminFormatPaymentAmount(amountMinor sql.NullInt64, currency sql.NullString) string {
	if !amountMinor.Valid || amountMinor.Int64 <= 0 {
		return "—"
	}
	cur := "RUB"
	if currency.Valid && strings.TrimSpace(currency.String) != "" {
		cur = strings.ToUpper(strings.TrimSpace(currency.String))
	}
	major := float64(amountMinor.Int64) / 100.0
	if cur == "XTR" || strings.EqualFold(cur, "STARS") {
		return fmt.Sprintf("%d⭐", amountMinor.Int64)
	}
	return fmt.Sprintf("%.0f%s", major, cur)
}

func adminMoneyKindShort(kind string) string {
	switch strings.TrimSpace(kind) {
	case "donation":
		return "Donation"
	default:
		return "Access"
	}
}

func adminMoneyCurrencyShort(currency string) string {
	if strings.EqualFold(strings.TrimSpace(currency), "XTR") || strings.EqualFold(strings.TrimSpace(currency), "STARS") {
		return "⭐"
	}
	return "₽"
}

func adminMoneyKindCurrencyLabel(kind, currency string) string {
	return adminMoneyKindShort(kind) + " · " + adminMoneyCurrencyShort(currency)
}

func adminPaymentStatusForKind(kind, status string, accessActive bool) string {
	if kind == "donation" {
		switch status {
		case "completed":
			return "ok"
		case "pending":
			return "pending"
		default:
			return status
		}
	}
	return adminPaymentStatusShort(status, accessActive)
}

func adminBuildMoneyStatsTable(sums []database.AdminMoneyKindSum) MiniappAdminTable {
	return adminBuildMoneyStatsTableForKind("", sums)
}

// adminBuildMoneyStatsTableForKind — сводка только по одному виду оплат ("" — по обоим).
func adminBuildMoneyStatsTableForKind(kind string, sums []database.AdminMoneyKindSum) MiniappAdminTable {
	tbl := MiniappAdminTable{
		Title:    "📊 Summary",
		Subtitle: "Completed payments all time · access & donations · stars & rubles",
		Columns:  []string{"Type", "Payments", "Amount"},
	}
	// Фиксированный порядок: сначала access, потом donations; внутри — звёзды, потом рубли.
	order := [][2]string{
		{"access", "XTR"},
		{"access", "RUB"},
		{"donation", "XTR"},
		{"donation", "RUB"},
	}
	byKey := make(map[string]database.AdminMoneyKindSum, len(sums))
	for _, s := range sums {
		byKey[s.Kind+"|"+strings.ToUpper(s.Currency)] = s
	}
	for _, k := range order {
		if kind != "" && k[0] != kind {
			continue
		}
		key := k[0] + "|" + k[1]
		s, ok := byKey[key]
		if !ok {
			tbl.Rows = append(tbl.Rows, []string{
				adminMoneyKindCurrencyLabel(k[0], k[1]), "0", "—",
			})
			continue
		}
		amt := sql.NullInt64{Int64: s.AmountMinor, Valid: s.AmountMinor > 0}
		cur := sql.NullString{String: s.Currency, Valid: true}
		tbl.Rows = append(tbl.Rows, []string{
			adminMoneyKindCurrencyLabel(s.Kind, s.Currency),
			strconv.FormatInt(s.Count, 10),
			adminFormatPaymentAmount(amt, cur),
		})
	}
	return tbl
}

func (b *Bot) formatAdminUserPaymentsBlock(userID, packChatID int64) string {
	payments, err := b.db.ListPaywallPaymentsForUserAdmin(userID, packChatID, 5)
	if err != nil || len(payments) == 0 {
		return "\n\n💳 Payments: no requests"
	}
	tbl := newAdminTable(
		[]string{"Status", "Amount", "Date"},
		[]int{6, 7, 8},
	)
	for _, p := range payments {
		when := p.CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04")
		tbl.addRow(
			adminPaymentStatusShort(p.Status, p.AccessActive),
			adminFormatPaymentAmount(p.AmountMinor, p.Currency),
			when,
		)
	}
	return "\n\n💳 Payments (latest):\n<pre>" + adminEscapeHTML(tbl.render()) + "</pre>"
}

func (b *Bot) handleAdminDirectoryCallback(callback *tgbotapi.CallbackQuery) bool {
	if callback == nil || callback.Message == nil {
		return false
	}
	data := callback.Data
	chatID := callback.Message.Chat.ID

	if strings.HasPrefix(data, "admin_users_list_") {
		off, err := strconv.Atoi(strings.TrimPrefix(data, "admin_users_list_"))
		if err != nil || off < 0 {
			off = 0
		}
		b.showAdminUsersListPage(chatID, off)
		return true
	}
	if strings.HasPrefix(data, "admin_payments_") {
		off, err := strconv.Atoi(strings.TrimPrefix(data, "admin_payments_"))
		if err != nil || off < 0 {
			off = 0
		}
		b.showAdminPaymentsPage(chatID, off)
		return true
	}
	return false
}
