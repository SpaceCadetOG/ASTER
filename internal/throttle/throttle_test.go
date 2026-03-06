package throttle

import (
	"testing"
	"time"
)

func TestCooldown(t *testing.T) {
	c := NewCooldown(5 * time.Minute)
	now := time.Now()
	if !c.Allow("BTCUSDT", now) {
		t.Fatal("first should pass")
	}
	if c.Allow("BTCUSDT", now.Add(2*time.Minute)) {
		t.Fatal("second inside window should block")
	}
	if !c.Allow("BTCUSDT", now.Add(6*time.Minute)) {
		t.Fatal("after window should pass")
	}
}

func TestDedupe(t *testing.T) {
	d := NewDedupe(120 * time.Second)
	now := time.Now()
	if !d.Allow("ETHUSDT", "BUY", now) {
		t.Fatal("first should pass")
	}
	if d.Allow("ETHUSDT", "BUY", now.Add(20*time.Second)) {
		t.Fatal("duplicate should block")
	}
	if !d.Allow("ETHUSDT", "SELL", now.Add(20*time.Second)) {
		t.Fatal("different side should pass")
	}
}
