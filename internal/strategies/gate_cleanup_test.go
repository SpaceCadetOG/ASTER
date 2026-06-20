package strategies

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRouterNoLongerContainsRemovedHandshakeOrTargetGates(t *testing.T) {
	body, err := os.ReadFile(filepath.Join("router.go"))
	if err != nil {
		t.Fatalf("read router.go: %v", err)
	}
	src := string(body)
	for _, needle := range []string{
		"flow_handshake_missing",
		"location_missing_entry",
		"location_not_significant",
		"dead_zone_non_aplus_grade",
		"target_too_close",
		"RequireOrderFlowHandshake",
		"RequireLocationHandshake",
		"AllowDeadZoneOnlyAPlus",
		"RejectIfTargetTooClosePct",
		"locationHandshake(",
		"orderFlowHandshake(",
	} {
		if strings.Contains(src, needle) {
			t.Fatalf("expected router cleanup to remove %q", needle)
		}
	}
}
