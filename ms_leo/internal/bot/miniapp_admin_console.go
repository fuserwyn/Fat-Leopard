package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"leo-bot/internal/database"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Технические разделы админки: база проекта и деньги.
//
// «Данные» — таблицы и SQL-редактор, только чтение (см. database.AdminRunQuery).
// «Ресурсы» — сколько Railway насчитал за текущий месяц и сколько за тот же
// период пришло оплат, всё в долларах, чтобы разницу было видно одним числом.

// requireMiniappDataAdmin — доступ к сырым данным базы (таблицы и SQL-редактор).
// Там видно всё, включая платежи и переписку, поэтому пускаем только владельца
// и админов из настроек сервера; выданные через панель права сюда не дают.
func (b *Bot) requireMiniappDataAdmin(viewerUserID int64, initD initdata.InitData) (int64, error) {
	packID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return 0, err
	}
	if b.config == nil || !b.config.IsAdminTelegramUser(viewerUserID) {
		return 0, ErrPackFeedForbidden
	}
	return packID, nil
}

// MiniappAdminDBTables — список таблиц базы стаи.
func (b *Bot) MiniappAdminDBTables(viewerUserID int64, initD initdata.InitData) ([]database.AdminTableInfo, error) {
	if _, err := b.requireMiniappDataAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	return b.db.AdminListTables()
}

// MiniappAdminDBTable — страница таблицы с сортировкой по колонке.
func (b *Bot) MiniappAdminDBTable(
	viewerUserID int64, initD initdata.InitData, table string, limit, offset int, orderBy string, desc bool,
) (database.AdminQueryResult, error) {
	if _, err := b.requireMiniappDataAdmin(viewerUserID, initD); err != nil {
		return database.AdminQueryResult{}, err
	}
	return b.db.AdminTablePage(table, limit, offset, orderBy, desc)
}

// MiniappAdminDBColumns — структура таблицы для вкладки «Структура».
func (b *Bot) MiniappAdminDBColumns(
	viewerUserID int64, initD initdata.InitData, table string,
) ([]database.AdminColumnInfo, error) {
	if _, err := b.requireMiniappDataAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	return b.db.AdminTableColumns(table)
}

// MiniappAdminDBQuery — произвольный читающий запрос из редактора.
func (b *Bot) MiniappAdminDBQuery(
	viewerUserID int64, initD initdata.InitData, query string,
) (database.AdminQueryResult, error) {
	if _, err := b.requireMiniappDataAdmin(viewerUserID, initD); err != nil {
		return database.AdminQueryResult{}, err
	}
	return b.db.AdminRunQuery(query)
}

// --- Ресурсы и деньги -----------------------------------------------------

// Railway отдаёт накопленные единицы, а не деньги: считаем сами по ставкам
// из тарифов (docs.railway.com/pricing), как это делает MyVibeLab.
const railwayGraphQL = "https://backboard.railway.app/graphql/v2"

const railwayUsageQuery = `query($projectId: String!, $measurements: [MetricMeasurement!]!) {
  estimatedUsage(projectId: $projectId, measurements: $measurements) {
    measurement
    estimatedValue
  }
}`

type MiniappCostPart struct {
	Key   string  `json:"key"`
	Label string  `json:"label"`
	Raw   float64 `json:"raw"`
	USD   float64 `json:"usd"`
}

type MiniappResources struct {
	Month        string            `json:"month"`
	CostParts    []MiniappCostPart `json:"cost_parts"`
	CostUSD      float64           `json:"cost_usd"`
	CostNote     string            `json:"cost_note"`
	Income       []MiniappIncome   `json:"income"`
	IncomeUSD    float64           `json:"income_usd"`
	NetUSD       float64           `json:"net_usd"`
	RatesNote    string            `json:"rates_note"`
	PaymentsNote string            `json:"payments_note"`
}

type MiniappIncome struct {
	Kind     string  `json:"kind"`     // access | donation
	Currency string  `json:"currency"` // XTR | RUB
	Count    int64   `json:"count"`
	Amount   float64 `json:"amount"`
	USD      float64 `json:"usd"`
}

// MiniappAdminResources — расходы Railway и доходы с оплат за текущий месяц.
func (b *Bot) MiniappAdminResources(viewerUserID int64, initD initdata.InitData) (MiniappResources, error) {
	out := MiniappResources{
		CostParts: []MiniappCostPart{},
		Income:    []MiniappIncome{},
	}
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	msk := time.FixedZone("MSK", 3*3600)
	now := time.Now().In(msk)
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, msk)
	out.Month = fmt.Sprintf("%s (по %s)", monthStart.Format("01.2006"), now.Format("02.01"))

	// --- расход
	if strings.TrimSpace(b.config.RailwayToken) == "" || strings.TrimSpace(b.config.RailwayProjectID) == "" {
		out.CostNote = "Не задан RAILWAY_API_TOKEN или RAILWAY_PROJECT_ID — расход Railway не показать."
	} else {
		raw, err := b.railwayUsage()
		if err != nil {
			out.CostNote = "Railway не ответил: " + err.Error()
		} else {
			minutes := b.config.UsageMinutesPerMonth
			if minutes <= 0 {
				minutes = 43800
			}
			parts := []MiniappCostPart{
				{Key: "MEMORY_USAGE_GB", Label: "Память", Raw: raw["MEMORY_USAGE_GB"],
					USD: raw["MEMORY_USAGE_GB"] / minutes * b.config.UsagePriceRAMGBMonth},
				{Key: "CPU_USAGE", Label: "CPU", Raw: raw["CPU_USAGE"],
					USD: raw["CPU_USAGE"] / minutes * b.config.UsagePriceCPUMonth},
				{Key: "DISK_USAGE_GB", Label: "Диск", Raw: raw["DISK_USAGE_GB"],
					USD: raw["DISK_USAGE_GB"] / minutes * b.config.UsagePriceDiskGBMonth},
				// Трафик тарифицируется разово за гигабайт — на время не делим.
				{Key: "NETWORK_TX_GB", Label: "Трафик", Raw: raw["NETWORK_TX_GB"],
					USD: raw["NETWORK_TX_GB"] * b.config.UsagePriceEgressGB},
			}
			for i := range parts {
				parts[i].USD = round2(parts[i].USD)
				out.CostUSD += parts[i].USD
			}
			out.CostUSD = round2(out.CostUSD)
			out.CostParts = parts
			out.CostNote = fmt.Sprintf(
				"Ставки: RAM $%.2f/ГБ·мес · CPU $%.2f/vCPU·мес · диск $%.2f/ГБ·мес · трафик $%.2f/ГБ",
				b.config.UsagePriceRAMGBMonth, b.config.UsagePriceCPUMonth,
				b.config.UsagePriceDiskGBMonth, b.config.UsagePriceEgressGB)
		}
	}

	// --- доход: покупки доступа и донаты, по звёздам и рублям
	packChatID := b.adminPackChatID()
	sums, err := b.db.AdminSumCompletedMoney(packChatID, monthStart.UTC(), true)
	if err != nil {
		return out, err
	}
	for _, s := range sums {
		amount := float64(s.AmountMinor) / 100
		usd := 0.0
		cur := strings.ToUpper(s.Currency)
		switch cur {
		case "XTR": // звёзды Telegram приходят штуками, не копейками
			amount = float64(s.AmountMinor)
			usd = amount * b.config.UsdPerStar
		case "USD":
			usd = amount
		case "RUB":
			if b.config.UsdRubRate > 0 {
				usd = amount / b.config.UsdRubRate
			}
		default:
			if b.config.UsdRubRate > 0 {
				usd = amount / b.config.UsdRubRate
			}
		}
		usd = round2(usd)
		out.IncomeUSD += usd
		out.Income = append(out.Income, MiniappIncome{
			Kind: s.Kind, Currency: cur, Count: s.Count, Amount: amount, USD: usd,
		})
	}
	out.IncomeUSD = round2(out.IncomeUSD)
	out.NetUSD = round2(out.IncomeUSD - out.CostUSD)
	out.RatesNote = fmt.Sprintf("Курс: $1 = %.2f ₽ · звезда = $%.4f (меняется в USD_RUB_RATE и USD_PER_STAR)",
		b.config.UsdRubRate, b.config.UsdPerStar)
	if len(out.Income) == 0 {
		out.PaymentsNote = "В этом месяце оплат и донатов ещё не было."
	} else {
		out.PaymentsNote = "Доступ — покупка входа/возврата; донат — поддержка из профиля. Завершённые за месяц."
	}
	return out, nil
}

func (b *Bot) railwayUsage() (map[string]float64, error) {
	body, _ := json.Marshal(map[string]any{
		"query": railwayUsageQuery,
		"variables": map[string]any{
			"projectId":    strings.TrimSpace(b.config.RailwayProjectID),
			"measurements": []string{"MEMORY_USAGE_GB", "CPU_USAGE", "NETWORK_TX_GB", "DISK_USAGE_GB"},
		},
	})
	req, err := http.NewRequest(http.MethodPost, railwayGraphQL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(b.config.RailwayToken))
	res, err := (&http.Client{Timeout: 20 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("HTTP %d", res.StatusCode)
	}
	var parsed struct {
		Data struct {
			EstimatedUsage []struct {
				Measurement    string  `json:"measurement"`
				EstimatedValue float64 `json:"estimatedValue"`
			} `json:"estimatedUsage"`
		} `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	if len(parsed.Errors) > 0 {
		return nil, fmt.Errorf("%s", parsed.Errors[0].Message)
	}
	out := make(map[string]float64, 4)
	for _, row := range parsed.Data.EstimatedUsage {
		out[row.Measurement] = row.EstimatedValue
	}
	return out, nil
}

func round2(v float64) float64 {
	return float64(int64(v*100+0.5)) / 100
}
