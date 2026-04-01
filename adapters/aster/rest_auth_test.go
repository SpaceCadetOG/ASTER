package aster

import (
	"encoding/hex"
	"strings"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
)

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
	sigHex, err := r.signAgent(msg, true)
	if err != nil {
		t.Fatalf("sign agent: %v", err)
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
	sigHex, err := r.signAgent(msg, false)
	if err != nil {
		t.Fatalf("sign agent raw-v: %v", err)
	}
	sig, err := hex.DecodeString(strings.TrimPrefix(sigHex, "0x"))
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	if got := sig[64]; got != 0 && got != 1 {
		t.Fatalf("expected raw recovery id 0/1, got %d", got)
	}
}
