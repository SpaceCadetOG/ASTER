package aster

import (
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestResolveAuthModePrefersAgentInAuto(t *testing.T) {
	mode, source, explicit, err := resolveAuthMode(RESTAuthConfig{
		APIKey:     "legacy-key",
		APISecret:  "legacy-secret",
		User:       "0xuser",
		Signer:     "0xsigner",
		PrivateKey: "0x1234",
		AuthMode:   "auto",
		ChainID:    1666,
		ChainIDSet: true,
	})
	if err != nil {
		t.Fatalf("resolve auth mode: %v", err)
	}
	if mode != "agent" || source != "auto:agent" || explicit {
		t.Fatalf("unexpected auth resolution: mode=%s source=%s explicit=%v", mode, source, explicit)
	}
}

func TestResolveAuthModeRequiresChainIDForAgent(t *testing.T) {
	_, _, _, err := resolveAuthMode(RESTAuthConfig{
		User:       "0xuser",
		Signer:     "0xsigner",
		PrivateKey: "0x1234",
		AuthMode:   "agent",
	})
	if err == nil || !strings.Contains(err.Error(), "ASTER_CHAIN_ID") {
		t.Fatalf("expected missing chain id error, got %v", err)
	}
}

func TestNormalizeSignedValuesRemovesEmptyAndSignature(t *testing.T) {
	vals := url.Values{
		"symbol":    {"BTCUSDT"},
		"signature": {"0xdeadbeef"},
		"empty":     {""},
		"side":      {"BUY"},
	}
	got := normalizeSignedValues(vals)
	if got.Get("signature") != "" || got.Get("empty") != "" {
		t.Fatalf("expected signature/empty to be removed, got=%v", got)
	}
}

func TestEncodeCanonicalQueryDeterministicAcrossInsertionOrder(t *testing.T) {
	a := url.Values{}
	a.Set("symbol", "BTCUSDT")
	a.Set("side", "BUY")
	a.Set("user", "0xabc")
	a.Set("signer", "0xdef")
	a.Set("nonce", "123")

	b := url.Values{}
	b.Set("nonce", "123")
	b.Set("signer", "0xdef")
	b.Set("user", "0xabc")
	b.Set("side", "BUY")
	b.Set("symbol", "BTCUSDT")

	gotA := encodeCanonicalQuery(normalizeSignedValues(a))
	gotB := encodeCanonicalQuery(normalizeSignedValues(b))
	if gotA != gotB {
		t.Fatalf("expected deterministic canonical query:\nA=%s\nB=%s", gotA, gotB)
	}
}

func TestEncodeCanonicalQueryNoDoubleEncoding(t *testing.T) {
	vals := url.Values{}
	vals.Set("newClientOrderId", "alpha beta")
	got := encodeCanonicalQuery(normalizeSignedValues(vals))
	if strings.Contains(got, "%2520") {
		t.Fatalf("expected single encoding, got %s", got)
	}
	if !strings.Contains(got, "alpha+beta") {
		t.Fatalf("expected query escaping, got %s", got)
	}
}

func TestSignAndEncodeAgentSignedStringMatchesSentString(t *testing.T) {
	priv, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7")
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	signer := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	r := &RESTAuth{
		user:       signer,
		signer:     signer,
		privateKey: "0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7",
		authMode:   "agent",
		chainID:    1666,
	}
	vals := url.Values{}
	vals.Set("symbol", "BTCUSDT")
	vals.Set("side", "BUY")
	payload, trace, err := r.signAndEncodeAgent(vals, "GET", "/fapi/v3/order", true)
	if err != nil {
		t.Fatalf("signAndEncodeAgent: %v", err)
	}
	if !strings.HasPrefix(payload, trace.CanonicalMsg+"&signature=") {
		t.Fatalf("expected payload to extend canonical msg:\nmsg=%s\npayload=%s", trace.CanonicalMsg, payload)
	}
	if trace.SentQuery != payload {
		t.Fatalf("expected sent query to match payload:\ntrace=%s\npayload=%s", trace.SentQuery, payload)
	}
	if vals.Get("signature") != "" || vals.Get("user") != "" || vals.Get("nonce") != "" {
		t.Fatalf("expected original params to remain unmutated: %v", vals)
	}
}

func TestDeriveAddressFromPrivateKey(t *testing.T) {
	got, err := deriveAddressFromPrivateKey("0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7")
	if err != nil {
		t.Fatalf("derive address: %v", err)
	}
	if got == "" || !strings.HasPrefix(got, "0x") {
		t.Fatalf("unexpected derived address: %s", got)
	}
}

func TestSignAgentUsesNativeEIP712Signature(t *testing.T) {
	priv, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7")
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	signer := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	r := &RESTAuth{
		user:       signer,
		signer:     signer,
		privateKey: "0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7",
		chainID:    1666,
	}

	msg := "symbol=BTCUSDT&side=BUY&user=" + r.user + "&signer=" + r.signer + "&nonce=1775044326424841"
	sigHex, recovered, err := r.signAgent(msg, true)
	if err != nil {
		t.Fatalf("sign agent: %v", err)
	}
	if !strings.EqualFold(recovered, signer) {
		t.Fatalf("unexpected recovered signer from signer helper: got=%s want=%s", recovered, signer)
	}
	if !strings.HasPrefix(sigHex, "0x") || len(sigHex) != 132 {
		t.Fatalf("unexpected signature format: %q", sigHex)
	}

	hash, err := asterAgentTypedDataHash(msg, r.chainID)
	if err != nil {
		t.Fatalf("typed data hash: %v", err)
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if got := sig[64]; got != 27 && got != 28 {
		t.Fatalf("expected Ethereum recovery id 27/28, got %d", got)
	}

	recoverySig := append([]byte(nil), sig...)
	recoverySig[64] -= 27
	pub, err := crypto.SigToPub(hash, recoverySig)
	if err != nil {
		t.Fatalf("recover signer: %v", err)
	}
	addr := crypto.PubkeyToAddress(*pub)
	want := common.HexToAddress(r.signer)
	if addr != want {
		t.Fatalf("unexpected recovered signer: got=%s want=%s", addr.Hex(), want.Hex())
	}
}

func TestSignAgentSupportsRawRecoveryID(t *testing.T) {
	priv, err := crypto.HexToECDSA("4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7")
	if err != nil {
		t.Fatalf("parse private key: %v", err)
	}
	signer := crypto.PubkeyToAddress(priv.PublicKey).Hex()
	r := &RESTAuth{
		user:       signer,
		signer:     signer,
		privateKey: "0x4c0883a69102937d6231471b5dbb6204fe5129617082790f15c7f8f6b8e6f6d7",
		chainID:    1666,
	}

	msg := "user=" + r.user + "&signer=" + r.signer + "&nonce=1775044326424841"
	sigHex, recovered, err := r.signAgent(msg, false)
	if err != nil {
		t.Fatalf("sign agent raw-v: %v", err)
	}
	if !strings.EqualFold(recovered, signer) {
		t.Fatalf("unexpected recovered signer from signer helper: got=%s want=%s", recovered, signer)
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if got := sig[64]; got != 0 && got != 1 {
		t.Fatalf("expected raw recovery id 0/1, got %d", got)
	}
}
