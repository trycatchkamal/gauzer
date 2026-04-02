package gauzer

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
)

// assertNotPanics runs f and fails the test if f panics.
func assertNotPanics(t *testing.T, name string, f func()) {
	t.Helper()
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("%s: expected no panic, but got: %v", name, r)
		}
	}()
	f()
}

// ── Structs used only by audit tests ─────────────────────────────────────────

// auditUnexportedField has an unexported field with a gauzer tag.
// Before fix: rv.Field(1).Interface() panics.
type auditUnexportedField struct {
	Name     string `gauzer:"required"`
	password string `gauzer:"required,min=8"` // unexported with gauzer tag
}

// auditBadRegex has a malformed regexp tag.
// Before fix: regexp.MustCompile panics on first ValidateStruct call.
type auditBadRegex struct {
	Code string `gauzer:"regexp=[a-z"` // missing closing bracket
}

// auditAnyStruct is used for nil-pointer and non-struct tests.
type auditAnyStruct struct {
	Age int `gauzer:"gte=0"`
}

// auditCrossFieldUnexported references an unexported sibling via nefield.
// Before fix: other.Interface() panics when OtherField is unexported.
type auditCrossFieldUnexported struct {
	Email        string `gauzer:"required,nefield=privateEmail"`
	privateEmail string // unexported, no gauzer tag, but targeted by nefield above
}

// ── Audit tests ───────────────────────────────────────────────────────────────

// TestPanic_UnexportedField: a gauzer tag on an unexported field must not crash.
// Bug: rv.Field(i).Interface() panics on unexported fields — no CanInterface() guard.
func TestPanic_UnexportedField(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	ctx := context.Background()
	assertNotPanics(t, "unexported field with gauzer tag", func() {
		_ = ValidateStruct(ctx, auditUnexportedField{Name: "Alice"})
	})
}

// TestError_InvalidRegex: a malformed regexp tag must return a descriptive error,
// not crash the process via regexp.MustCompile.
func TestError_InvalidRegex(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	ctx := context.Background()
	var err error
	assertNotPanics(t, "invalid regexp tag", func() {
		err = ValidateStruct(ctx, auditBadRegex{Code: "abc"})
	})
	if err == nil {
		t.Error("expected an error for invalid regexp tag, got nil")
		return
	}
	if !strings.Contains(err.Error(), "regexp") {
		t.Errorf("error message should mention 'regexp', got: %q", err.Error())
	}
}

// TestError_NilPointer: a nil pointer must return a clear error, not crash on
// rv.Elem() → rv.Type() when the pointer is nil.
func TestError_NilPointer(t *testing.T) {
	ctx := context.Background()
	var s *auditAnyStruct

	var err error
	assertNotPanics(t, "nil pointer input", func() {
		err = ValidateStruct(ctx, s)
	})
	if err == nil {
		t.Error("expected an error for nil pointer input, got nil")
	}
}

// TestError_NonStructInput: passing a raw string or int must return an error,
// not crash on rt.NumField() when the type is not a struct.
func TestError_NonStructInput(t *testing.T) {
	ctx := context.Background()

	assertNotPanics(t, "string input", func() {
		if err := ValidateStruct(ctx, "hello"); err == nil {
			t.Error("expected error for string input, got nil")
		}
	})

	assertNotPanics(t, "int input", func() {
		if err := ValidateStruct(ctx, 42); err == nil {
			t.Error("expected error for int input, got nil")
		}
	})
}

// TestPanic_UnexportedCrossField: nefield pointing at an unexported sibling field
// must return a validation error (fail), not panic on other.Interface().
func TestPanic_UnexportedCrossField(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	ctx := context.Background()
	var err error
	assertNotPanics(t, "nefield targeting unexported field", func() {
		err = ValidateStruct(ctx, auditCrossFieldUnexported{
			Email:        "test@example.com",
			privateEmail: "other@example.com",
		})
	})
	// The rule must degrade to fail() — returning a validation error, not nil.
	if err == nil {
		t.Error("expected a validation error when nefield targets unexported field, got nil")
	}
}

// ── Slog counting handler ─────────────────────────────────────────────────────

type countingHandler struct {
	mu    sync.Mutex
	count int
}

func (h *countingHandler) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (h *countingHandler) Handle(_ context.Context, _ slog.Record) error {
	h.mu.Lock()
	h.count++
	h.mu.Unlock()
	return nil
}
func (h *countingHandler) WithAttrs(_ []slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(_ string) slog.Handler      { return h }

// TestNoOp_DefaultEmitter: the DefaultSlogEmitter must be a no-op so that callers
// who log the returned error themselves are not double-logged.
// Before fix: DefaultSlogEmitter calls slog.ErrorContext, firing the handler once.
func TestNoOp_DefaultEmitter(t *testing.T) {
	handler := &countingHandler{}
	oldLogger := slog.Default()
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(oldLogger) })

	// Ensure the real default emitter (not a mock) is active.
	ResetEmitter()
	t.Cleanup(ResetEmitter)

	type failingAge struct {
		Age int `gauzer:"gte=100"`
	}
	_ = ValidateStruct(context.Background(), failingAge{Age: 5})

	handler.mu.Lock()
	got := handler.count
	handler.mu.Unlock()

	if got != 0 {
		t.Errorf("DefaultSlogEmitter must be a no-op: slog.Handle was called %d time(s), want 0", got)
	}
}
