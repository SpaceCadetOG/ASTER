package notify

import (
	"fmt"
	"sort"
	"strings"
)

func RenderEvent(event Event) string {
	if structured := renderStructuredEvent(event); structured != "" {
		return structured
	}
	switch event.Class {
	case ClassCritical:
		return renderTagged("CRITICAL", event)
	case ClassLifecycle:
		return renderTagged("TRADE", event)
	case ClassState:
		return renderTagged("STATE", event)
	default:
		return renderTagged("INFO", event)
	}
}

func renderStructuredEvent(event Event) string {
	switch strings.TrimSpace(event.Key) {
	case "LIVE_STARTED":
		lines := []string{}
		if mode := strings.TrimSpace(event.Metadata["mode"]); mode != "" {
			lines = append(lines, fmt.Sprintf("<b>Mode:</b> %s", strings.ToUpper(mode)))
		}
		if msg := strings.TrimSpace(event.Message); msg != "" {
			lines = append(lines, msg)
		}
		if v := strings.TrimSpace(event.Metadata["scan_watch"]); v != "" {
			lines = append(lines, fmt.Sprintf("<b>Scan/Watch:</b> %s", v))
		}
		if v := strings.TrimSpace(event.Metadata["starter_add_max"]); v != "" {
			lines = append(lines, fmt.Sprintf("<b>Ladder:</b> %s", v))
		}
		if v := strings.TrimSpace(event.Metadata["min_avail_reentry"]); v != "" {
			lines = append(lines, fmt.Sprintf("<b>Funds/Reentry:</b> %s", v))
		}
		title := strings.TrimSpace(event.Title)
		if title == "" {
			title = "SYSTEM STARTED"
		}
		return BuildEventHTML("🚦", title, lines...)
	}
	return ""
}

func renderTagged(tag string, event Event) string {
	var b strings.Builder
	fmt.Fprintf(&b, "[%s] %s\n", tag, strings.TrimSpace(event.Title))
	writeKV(&b, "Symbol", event.Symbol)
	writeKV(&b, "Message", event.Message)
	writeMetadata(&b, event.Metadata)
	return strings.TrimSpace(b.String())
}

func writeKV(b *strings.Builder, key, val string) {
	if strings.TrimSpace(val) == "" {
		return
	}
	fmt.Fprintf(b, "%s: %s\n", key, val)
}

func writeMetadata(b *strings.Builder, md map[string]string) {
	if len(md) == 0 {
		return
	}
	keys := make([]string, 0, len(md))
	for k := range md {
		if strings.TrimSpace(md[k]) != "" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(b, "%s: %s\n", k, md[k])
	}
}
