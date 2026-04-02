package gauzer

import (
	"log/slog"
	"testing"
)

// TestDiagnosticEvent_Error verifies the error interface implementation.
func TestDiagnosticEvent_Error(t *testing.T) {
	e := DiagnosticEvent{
		Field:      "Age",
		Constraint: "min:18",
		Value:      "5",
		ValueType:  "int",
		Message:    "Age must be at least 18",
	}
	if got := e.Error(); got != e.Message {
		t.Errorf("Error() = %q; want %q", got, e.Message)
	}
}

// TestDiagnosticEvent_LogValue verifies the slog.LogValuer implementation.
func TestDiagnosticEvent_LogValue(t *testing.T) {
	e := DiagnosticEvent{
		Field:      "Email",
		Constraint: "email",
		Value:      "bad",
		ValueType:  "string",
		Message:    "Email must be a valid email",
	}
	lv := e.LogValue()
	if lv.Kind() != slog.KindGroup {
		t.Fatalf("LogValue() Kind = %v; want KindGroup", lv.Kind())
	}
	attrs := lv.Group()
	want := map[string]string{
		"field":      "Email",
		"constraint": "email",
		"value":      "bad",
		"type":       "string",
	}
	for _, attr := range attrs {
		expected, ok := want[attr.Key]
		if !ok {
			t.Errorf("unexpected attribute key %q", attr.Key)
			continue
		}
		if got := attr.Value.String(); got != expected {
			t.Errorf("attr[%q] = %q; want %q", attr.Key, got, expected)
		}
	}
}
