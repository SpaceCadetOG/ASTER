package notify

import (
	"fmt"
	"sort"
	"strings"
)

func RenderEvent(event Event) string {
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

