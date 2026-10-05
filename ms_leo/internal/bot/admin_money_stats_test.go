package bot

import (
	"database/sql"
	"testing"

	"leo-bot/internal/database"
)

func TestAdminMoneyKindCurrencyLabel(t *testing.T) {
	if got := adminMoneyKindCurrencyLabel("access", "XTR"); got != "доступ · ⭐" {
		t.Fatalf("access stars: %q", got)
	}
	if got := adminMoneyKindCurrencyLabel("donation", "RUB"); got != "донат · ₽" {
		t.Fatalf("donation rub: %q", got)
	}
}

func TestAdminBuildMoneyStatsTableOrder(t *testing.T) {
	tbl := adminBuildMoneyStatsTable([]database.AdminMoneyKindSum{
		{Kind: "donation", Currency: "RUB", Count: 2, AmountMinor: 100000},
		{Kind: "access", Currency: "XTR", Count: 1, AmountMinor: 50},
	})
	if len(tbl.Rows) != 4 {
		t.Fatalf("rows: %d", len(tbl.Rows))
	}
	if tbl.Rows[0][0] != "доступ · ⭐" {
		t.Fatalf("first row: %v", tbl.Rows[0])
	}
	amt := sql.NullInt64{Int64: 100000, Valid: true}
	cur := sql.NullString{String: "RUB", Valid: true}
	if tbl.Rows[3][2] != adminFormatPaymentAmount(amt, cur) {
		t.Fatalf("donation rub amount: %v", tbl.Rows[3])
	}
}

func TestAdminMoneyPersonLabel(t *testing.T) {
	cases := []struct {
		username, name string
		id             int64
		want           string
	}{
		{"wolf", "Волк", 1, "@wolf · Волк"},
		{"wolf", "", 1, "@wolf"},
		{"", "Волк", 42, "Волк · id42"},
		{"", "", 42, "id42"},
	}
	for _, c := range cases {
		if got := adminMoneyPersonLabel(c.username, c.name, c.id); got != c.want {
			t.Fatalf("label(%q,%q,%d) = %q, want %q", c.username, c.name, c.id, got, c.want)
		}
	}
}

func TestAdminBuildMoneyPayersTable(t *testing.T) {
	payers := []database.AdminMoneyPayerRow{
		{UserID: 7, Username: "wolf", AccessCount: 1, DonationCount: 2, StarsTotal: 50, RubMinorTotal: 30000},
	}
	all := adminBuildMoneyPayersTable("", payers)
	if len(all.Columns) != 6 || len(all.Rows) != 1 {
		t.Fatalf("all: cols=%d rows=%d", len(all.Columns), len(all.Rows))
	}
	row := all.Rows[0]
	if row[0] != "@wolf" || row[1] != "1" || row[2] != "2" || row[3] != "50" || row[4] != "300" {
		t.Fatalf("all row: %v", row)
	}
	don := adminBuildMoneyPayersTable("donation", payers)
	if len(don.Columns) != 5 || don.Rows[0][1] != "2" {
		t.Fatalf("donation table: %v %v", don.Columns, don.Rows)
	}
	empty := adminBuildMoneyPayersTable("access", nil)
	if empty.Rows == nil {
		t.Fatal("rows must be non-nil for JSON")
	}
}
