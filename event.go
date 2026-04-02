package gauzer

import "log/slog"

// DiagnosticEvent is the structured payload emitted on every validation failure.
// Value is always a stringified, truncated copy of the failing input — never `any` —
// to prevent PII leaks and log-bloat in cloud billing.
type DiagnosticEvent struct {
	Field      string
	Constraint string
	Value      string // stringified & truncated by the Rule, max 64 chars
	ValueType  string // e.g. "string", "int", "unknown"
	Message    string
}

// Error implements the standard error interface.
func (e DiagnosticEvent) Error() string {
	return e.Message
}

// LogValue implements slog.LogValuer so the event nests under "err" in JSON output.
func (e DiagnosticEvent) LogValue() slog.Value {
	return slog.GroupValue(
		slog.String("field", e.Field),
		slog.String("constraint", e.Constraint),
		slog.String("value", e.Value),
		slog.String("type", e.ValueType),
	)
}
