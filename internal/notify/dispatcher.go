package notify

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"
)

type TelegramSender interface {
	Send(ctx context.Context, route Route, text string) error
}

type Dispatcher struct {
	policies map[string]Policy
	dedupe   *DedupeStore
	sender   TelegramSender
	accum    *Accumulator
}

func NewDispatcher(sender TelegramSender, accum *Accumulator) *Dispatcher {
	return &Dispatcher{
		policies: DefaultPolicies(),
		dedupe:   NewDedupeStore(),
		sender:   sender,
		accum:    accum,
	}
}

func (d *Dispatcher) Emit(ctx context.Context, event Event) DispatchResult {
	if d == nil {
		return DispatchResult{Sent: false, Reason: "dispatcher_nil"}
	}
	if event.OccurredAt.IsZero() {
		event.OccurredAt = time.Now().UTC()
	}
	policy, ok := d.policies[event.Key]
	if !ok {
		policy = Policy{
			Key:             event.Key,
			Class:           event.Class,
			Severity:        event.Severity,
			Route:           event.Route,
			Immediate:       false,
			DefaultEnabled:  false,
			DigestEligible:  true,
			DedupeWindowSec: envIntNotify("LIVE_TG_DEDUPE_DEFAULT_SEC", 120),
		}
	}
	if !isPolicyEnabled(policy) {
		if d.accum != nil && policy.DigestEligible {
			d.accum.Add(event)
		}
		return DispatchResult{Sent: false, Reason: "disabled"}
	}
	if d.accum != nil && policy.DigestEligible {
		d.accum.Add(event)
	}
	if !policy.Immediate {
		return DispatchResult{Sent: false, Reason: "digest_only", Route: policy.Route}
	}
	if d.sender == nil {
		return DispatchResult{Sent: false, Reason: "sender_nil", Route: policy.Route}
	}
	rendered := RenderEvent(event)
	if envBoolNotify("LIVE_TG_DEDUPE_ENABLE", true) && !d.dedupe.ShouldSend(event, rendered, policy) {
		return DispatchResult{Sent: false, Reason: "deduped", Route: policy.Route}
	}
	if err := d.sender.Send(ctx, policy.Route, rendered); err != nil {
		return DispatchResult{Sent: false, Reason: "send_failed", Route: policy.Route}
	}
	return DispatchResult{Sent: true, Reason: "sent", Route: policy.Route}
}

func isPolicyEnabled(policy Policy) bool {
	profile := strings.ToLower(strings.TrimSpace(os.Getenv("LIVE_TG_PROFILE")))
	if profile == "" {
		profile = "production"
	}
	switch policy.Class {
	case ClassDiagnostic:
		return envBoolNotify("LIVE_TG_DIAGNOSTICS_ENABLE", false)
	case ClassState:
		return envBoolNotify("LIVE_TG_STATE_TRANSITIONS_ENABLE", true)
	default:
		return policy.DefaultEnabled
	}
}

func envBoolNotify(key string, def bool) bool {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	switch strings.ToLower(v) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return def
	}
}

func envIntNotify(key string, def int) int {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		return def
	}
	out, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return out
}

