package mltrain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadCSVRejectsForbiddenFeature(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "x.csv")
	data := strings.Join([]string{
		"trade_id,entry_ts,realized_r,win",
		"t1,2026-06-27T00:00:00Z,1,1",
	}, "\n")
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := LoadCSV(path, "win", []string{"realized_r"}, nil)
	if err == nil {
		t.Fatalf("expected leakage guard failure")
	}
}
