package main

import (
	"testing"

	"go-machine/adapters/aster"
)

func TestResolveOrderRef(t *testing.T) {
	t.Run("numeric id", func(t *testing.T) {
		ref, err := resolveOrderRef("12345", "")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if !ref.HasNumericID || ref.OrderID != 12345 {
			t.Fatalf("unexpected ref: %+v", ref)
		}
	})

	t.Run("client id", func(t *testing.T) {
		ref, err := resolveOrderRef("", "abc123")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if ref.HasNumericID || ref.ClientOrderID != "abc123" {
			t.Fatalf("unexpected ref: %+v", ref)
		}
	})

	t.Run("invalid numeric id", func(t *testing.T) {
		_, err := resolveOrderRef("not-a-number", "")
		if err == nil {
			t.Fatal("expected parse error")
		}
	})
}

func TestBumpQtyToMinNotionalFromZero(t *testing.T) {
	q := bumpQtyToMinNotional(0, 1970, 20, 0.01, 2)
	if q <= 0 {
		t.Fatalf("expected bumped qty > 0, got %f", q)
	}
	if q != 0.02 {
		t.Fatalf("expected 0.02, got %f", q)
	}
}

func TestFormatFloatTrimsTrailingZeros(t *testing.T) {
	if got := formatFloat(1929.473, 5); got != "1929.473" {
		t.Fatalf("expected trimmed float, got %q", got)
	}
	if got := formatFloat(2.00000, 5); got != "2" {
		t.Fatalf("expected whole number trimming, got %q", got)
	}
}

func TestFilterBalancesByAssets(t *testing.T) {
	in := []aster.Balance{
		{Asset: "USDT", Balance: 1},
		{Asset: "BTC", Balance: 2},
		{Asset: "ETH", Balance: 3},
	}
	out := filterBalancesByAssets(in, "USDT, BTC")
	if len(out) != 2 {
		t.Fatalf("expected 2 balances, got %d", len(out))
	}
}
