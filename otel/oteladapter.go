// Package oteladapter provides a gauzer.Emitter implementation that forwards
// DiagnosticEvents to the active OpenTelemetry span as structured attributes.
package oteladapter

import (
	"context"

	"github.com/trycatchkamal/gauzer"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// OTelEmitter implements gauzer.Emitter by writing DiagnosticEvent fields
// as span attributes on the active span in the provided context.
// If there is no active span the event is silently dropped (no panic).
type OTelEmitter struct{}

// New returns a new OTelEmitter ready for use with gauzer.SetEmitter.
func New() gauzer.Emitter {
	return OTelEmitter{}
}

// Emit records the DiagnosticEvent on the span currently active in ctx.
// Attribute keys follow the "gauzer.*" namespace to avoid conflicts.
func (OTelEmitter) Emit(ctx context.Context, event *gauzer.DiagnosticEvent) {
	span := trace.SpanFromContext(ctx)
	if !span.IsRecording() {
		return
	}
	span.SetAttributes(
		attribute.String("gauzer.field", event.Field),
		attribute.String("gauzer.constraint", event.Constraint),
		attribute.String("gauzer.value", event.Value),
		attribute.String("gauzer.value_type", event.ValueType),
		attribute.String("gauzer.message", event.Message),
	)
}
