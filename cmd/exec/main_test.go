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

func TestClassifyAuthFailureSignerMismatch(t *testing.T) {
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		User:       "0x1111111111111111111111111111111111111111",
		Signer:     "0x2222222222222222222222222222222222222222",
		PrivateKey: "0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7",
		AuthMode:   "agent",
		ChainID:    1666,
		ChainIDSet: true,
	})
	if got := classifyAuthFailure(rest, "account", rest.ConfigError()); got != "signer_private_key_mismatch" {
		t.Fatalf("unexpected classification: %s", got)
	}
}

func TestClassifyAuthFailureLegacyHMACUnexpected(t *testing.T) {
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		APIKey:    "key",
		APISecret: "secret",
		AuthMode:  "auto",
	})
	if got := classifyAuthFailure(rest, "account", assertErr("boom")); got != "legacy_hmac_path_selected_unexpectedly" {
		t.Fatalf("unexpected classification: %s", got)
	}
}

func TestClassifyAuthFailureCanonicalMismatch(t *testing.T) {
	rest := aster.NewRESTAuthWithConfig(aster.RESTAuthConfig{
		User:       "0x1111111111111111111111111111111111111111",
		Signer:     "0x1111111111111111111111111111111111111111",
		PrivateKey: "0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7",
		AuthMode:   "auto",
		ChainID:    1666,
		ChainIDSet: true,
	})
	got := classifyAuthFailure(rest, "account", assertErr(`http 400 GET /fapi/v3/account: {"code":-1000,"msg":"Signature check failed"}`))
	if got != "canonical_querystring_mismatch" {
		t.Fatalf("unexpected classification: %s", got)
	}
}

type assertErr string

func (e assertErr) Error() string { return string(e) }
