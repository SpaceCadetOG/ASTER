package notify

import "time"

type Severity string

const (
	SeverityDebug    Severity = "debug"
	SeverityInfo     Severity = "info"
	SeverityNotice   Severity = "notice"
	SeverityWarning  Severity = "warning"
	SeverityCritical Severity = "critical"
)

type Class string

const (
	ClassCritical   Class = "critical"
	ClassLifecycle  Class = "lifecycle"
	ClassState      Class = "state_transition"
	ClassDigest     Class = "digest"
	ClassDiagnostic Class = "diagnostic"
)

type Route string

const (
	RouteCritical Route = "critical"
	RouteNormal   Route = "normal"
	RouteDigest   Route = "digest"
	RouteDebug    Route = "debug"
)

type Event struct {
	Key        string
	Title      string
	Class      Class
	Severity   Severity
	Route      Route
	Symbol     string
	PositionID string
	Message    string
	Metadata   map[string]string
	OccurredAt time.Time
}

type Policy struct {
	Key                 string
	Class               Class
	Severity            Severity
	Route               Route
	Immediate           bool
	DefaultEnabled      bool
	DigestEligible      bool
	StateTransitionOnly bool
	DedupeWindowSec     int
}

type DispatchResult struct {
	Sent      bool
	Reason    string
	Route     Route
	RenderKey string
}

