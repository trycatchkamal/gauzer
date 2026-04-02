package gauzer

import (
	"context" // retained: Emitter interface uses context.Context
	"sync/atomic"
)

// Emitter routes DiagnosticEvents to any telemetry backend.
type Emitter interface {
	Emit(ctx context.Context, event *DiagnosticEvent)
}

var globalEmitter atomic.Pointer[Emitter]

// SetEmitter replaces the active telemetry backend.
// Thread-safe; safe to call from multiple goroutines.
func SetEmitter(e Emitter) {
	globalEmitter.Store(&e)
}

// ResetEmitter restores the default SlogEmitter.
// Useful in tests to avoid cross-test pollution.
func ResetEmitter() {
	globalEmitter.Store(nil)
}

func getEmitter() Emitter {
	p := globalEmitter.Load()
	if p == nil {
		return DefaultSlogEmitter{}
	}
	return *p
}

// DefaultSlogEmitter is the zero-config fallback.
// It is intentionally a no-op: the *DiagnosticEvent returned by ValidateStruct
// implements slog.LogValuer, so callers log it themselves without double-logging.
type DefaultSlogEmitter struct{}

func (DefaultSlogEmitter) Emit(_ context.Context, _ *DiagnosticEvent) {
	// No-op. Log the returned error at the call site:
	//   slog.Error("validation failed", "err", err)
}
