package notify

func DefaultPolicies() map[string]Policy {
	return map[string]Policy{
		"LIVE_STARTED": {Key: "LIVE_STARTED", Class: ClassLifecycle, Severity: SeverityNotice, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: false, DedupeWindowSec: 600},
		"BOOT_RECONCILE_COMPLETE": {Key: "BOOT_RECONCILE_COMPLETE", Class: ClassLifecycle, Severity: SeverityNotice, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: false, DedupeWindowSec: 600},
		"ORDER_PLACED": {Key: "ORDER_PLACED", Class: ClassLifecycle, Severity: SeverityNotice, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 30},
		"POSITION_CLOSED": {Key: "POSITION_CLOSED", Class: ClassLifecycle, Severity: SeverityNotice, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 30},
		"ORDER_ERROR": {Key: "ORDER_ERROR", Class: ClassCritical, Severity: SeverityWarning, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 60},
		"KILL_SWITCH": {Key: "KILL_SWITCH", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 60},
		"MAINTENANCE_START": {Key: "MAINTENANCE_START", Class: ClassCritical, Severity: SeverityWarning, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 300},
		"MAINTENANCE_END": {Key: "MAINTENANCE_END", Class: ClassCritical, Severity: SeverityNotice, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 300},
		"MANUAL_STATE_CONFLICT": {Key: "MANUAL_STATE_CONFLICT", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 300},
		"FORCED_CLOSE_REQUESTED": {Key: "FORCED_CLOSE_REQUESTED", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 120},
		"EXECUTION_UNKNOWN": {Key: "EXECUTION_UNKNOWN", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 120},
		"RECONCILE_FAILED": {Key: "RECONCILE_FAILED", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 120},
		"PROTECTION_ATTACH_FAILED": {Key: "PROTECTION_ATTACH_FAILED", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 120},
		"DETECTED": {Key: "DETECTED", Class: ClassState, Severity: SeverityNotice, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 900},
		"AWAITING_OPERATOR": {Key: "AWAITING_OPERATOR", Class: ClassState, Severity: SeverityWarning, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 900},
		"ADOPTED": {Key: "ADOPTED", Class: ClassState, Severity: SeverityNotice, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 900},
		"ATTACHING_PROTECTION": {Key: "ATTACHING_PROTECTION", Class: ClassState, Severity: SeverityNotice, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 900},
		"PROTECTED": {Key: "PROTECTED", Class: ClassState, Severity: SeverityNotice, Route: RouteNormal, Immediate: true, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 900},
		"DEGRADED": {Key: "DEGRADED", Class: ClassCritical, Severity: SeverityCritical, Route: RouteCritical, Immediate: true, DefaultEnabled: true, DigestEligible: true, StateTransitionOnly: true, DedupeWindowSec: 300},
		"ADD_SUBMITTED": {Key: "ADD_SUBMITTED", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 300},
		"TRAIL_MOVE": {Key: "TRAIL_MOVE", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 300},
		"MOMENTUM_EXIT_SUBMITTED": {Key: "MOMENTUM_EXIT_SUBMITTED", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"PRE_FUNDING_EXIT_SUBMITTED": {Key: "PRE_FUNDING_EXIT_SUBMITTED", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"PRE_EOD_EXIT_SUBMITTED": {Key: "PRE_EOD_EXIT_SUBMITTED", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"PROFIT_SWEPT": {Key: "PROFIT_SWEPT", Class: ClassLifecycle, Severity: SeverityInfo, Route: RouteNormal, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"SHADOW_GATE_ACTIVE": {Key: "SHADOW_GATE_ACTIVE", Class: ClassDigest, Severity: SeverityInfo, Route: RouteDigest, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"RESERVE_LOCK_ACTIVE": {Key: "RESERVE_LOCK_ACTIVE", Class: ClassDigest, Severity: SeverityInfo, Route: RouteDigest, Immediate: false, DefaultEnabled: true, DigestEligible: true, DedupeWindowSec: 180},
		"TOP_CANDIDATE": {Key: "TOP_CANDIDATE", Class: ClassDiagnostic, Severity: SeverityDebug, Route: RouteDebug, Immediate: false, DefaultEnabled: false, DigestEligible: false, DedupeWindowSec: 300},
		"DRY_RUN_INTENT": {Key: "DRY_RUN_INTENT", Class: ClassDiagnostic, Severity: SeverityDebug, Route: RouteDebug, Immediate: false, DefaultEnabled: false, DigestEligible: false, DedupeWindowSec: 300},
		"SAFETY_SKIP": {Key: "SAFETY_SKIP", Class: ClassDiagnostic, Severity: SeverityDebug, Route: RouteDebug, Immediate: false, DefaultEnabled: false, DigestEligible: true, DedupeWindowSec: 300},
	}
}

