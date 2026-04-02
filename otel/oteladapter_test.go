package oteladapter

import (
	"context"
	"testing"

	"github.com/trycatchkamal/gauzer"
	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestOTelEmitter_SetsSpanAttributes(t *testing.T) {
	// Set up a span recorder so we can inspect emitted attributes.
	exporter := tracetest.NewInMemoryExporter()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "test-op")

	event := &gauzer.DiagnosticEvent{
		Field:      "Age",
		Constraint: "min:18",
		Value:      "5",
		ValueType:  "int",
		Message:    "Age must be at least 18",
	}

	emitter := New()
	emitter.Emit(ctx, event)
	span.End()

	spans := exporter.GetSpans()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}

	wantAttrs := map[string]string{
		"gauzer.field":      "Age",
		"gauzer.constraint": "min:18",
		"gauzer.value":      "5",
		"gauzer.value_type": "int",
		"gauzer.message":    "Age must be at least 18",
	}

	attrMap := make(map[string]string)
	for _, kv := range spans[0].Attributes {
		attrMap[string(kv.Key)] = kv.Value.AsString()
	}

	for k, want := range wantAttrs {
		got, ok := attrMap[k]
		if !ok {
			t.Errorf("missing span attribute %q", k)
			continue
		}
		if got != want {
			t.Errorf("attribute[%q] = %q; want %q", k, got, want)
		}
	}
}

func TestOTelEmitter_NoActiveSpan_NoopSafe(t *testing.T) {
	// Emitting with a context that has no active span should not panic.
	emitter := New()
	event := &gauzer.DiagnosticEvent{
		Field:      "Email",
		Constraint: "email",
		Value:      "bad",
		ValueType:  "string",
		Message:    "Email must be a valid email",
	}
	// Must not panic.
	emitter.Emit(context.Background(), event)
}

// noopSpan satisfies the attribute check in Emit without a real SDK.
func TestOTelEmitter_ImplementsEmitter(t *testing.T) {
	var _ gauzer.Emitter = OTelEmitter{}
	var _ gauzer.Emitter = New()
	_ = attribute.String // ensure import used
}
