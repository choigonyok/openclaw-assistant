package app

import "testing"

func TestNormalizeKISAccountSplitsFullAccount(t *testing.T) {
	accountNo, product := normalizeKISAccount("12345678-01", "")

	if accountNo != "12345678" {
		t.Fatalf("accountNo = %q, want %q", accountNo, "12345678")
	}
	if product != "01" {
		t.Fatalf("product = %q, want %q", product, "01")
	}
}

func TestNormalizeKISAccountKeepsExplicitProduct(t *testing.T) {
	accountNo, product := normalizeKISAccount("1234567801", "22")

	if accountNo != "12345678" {
		t.Fatalf("accountNo = %q, want %q", accountNo, "12345678")
	}
	if product != "22" {
		t.Fatalf("product = %q, want %q", product, "22")
	}
}

func TestPickUSDCash(t *testing.T) {
	cash, krw := pickUSDCash([]kisForeignCashRow{
		{Currency: "HKD", ForeignCash: "7.5"},
		{Currency: "USD", ForeignCash: "", ForeignUsable: "123.45", BaseRate: "1350.5"},
	})

	if cash != "123.45" {
		t.Fatalf("cash = %q, want %q", cash, "123.45")
	}
	if krw != "166719" {
		t.Fatalf("krw = %q, want %q", krw, "166719")
	}
}
