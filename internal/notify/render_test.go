package notify

import (
	"strings"
	"testing"
)

func TestRenderEventFormatsModeAwareStartupCard(t *testing.T) {
	msg := RenderEvent(Event{
		Key:     "LIVE_STARTED",
		Title:   "PAPER STARTED",
		Class:   ClassLifecycle,
		Message: "paper process started",
		Metadata: map[string]string{
			"mode":              "PAPER",
			"scan_watch":        "10s/1s",
			"starter_add_max":   "5.00/0.00/5.00",
			"min_avail_reentry": "5.00/0.00",
		},
	})
	if !strings.Contains(msg, "🚦 <b>PAPER STARTED</b>") {
		t.Fatalf("expected structured startup header, got %q", msg)
	}
	if strings.Contains(msg, "[TRADE]") {
		t.Fatalf("expected structured card, got raw tagged output %q", msg)
	}
	if !strings.Contains(msg, "<b>Mode:</b> PAPER") {
		t.Fatalf("expected explicit mode line, got %q", msg)
	}
}
