package main

import (
	"testing"
	"time"
)

func TestCanServeSignedCacheEntryFreshWithoutBackoff(t *testing.T) {
	signedUserDataBackoffState.mu.Lock()
	signedUserDataBackoffState.until = time.Time{}
	signedUserDataBackoffState.mu.Unlock()

	now := time.Now().UTC()
	if !canServeSignedCacheEntry(now.Add(-2*time.Second), now, 3*time.Second, 2*time.Minute) {
		t.Fatal("expected fresh cache entry to be served")
	}
	if canServeSignedCacheEntry(now.Add(-10*time.Second), now, 3*time.Second, 2*time.Minute) {
		t.Fatal("expected stale cache entry to be rejected without backoff")
	}
}

func TestCanServeSignedCacheEntryServesStaleDuringBackoff(t *testing.T) {
	signedUserDataBackoffState.mu.Lock()
	signedUserDataBackoffState.until = time.Time{}
	signedUserDataBackoffState.mu.Unlock()

	now := time.Now().UTC()
	signedUserDataBackoffObserve(now, errString("http 429 GET /fapi/v3/account"))
	if !canServeSignedCacheEntry(now.Add(-30*time.Second), now, 3*time.Second, 2*time.Minute) {
		t.Fatal("expected stale cache entry to be served during backoff")
	}
	if canServeSignedCacheEntry(now.Add(-3*time.Minute), now, 3*time.Second, 2*time.Minute) {
		t.Fatal("expected very old cache entry to stay rejected during backoff")
	}
}

type errString string

func (e errString) Error() string { return string(e) }
