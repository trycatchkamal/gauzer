package gauzer

import (
	"context"
	"testing"
)

// passingUser is used for happy-path benchmarks — all fields valid.
type passingUser struct {
	Email string `gauzer:"required,email"`
	Age   int    `gauzer:"gte=18"`
}

// BenchmarkValidateStruct_HappyPath measures the hot path: a fully valid struct.
// Goal: < 50 ns/op, 0 B/op, 0 allocs/op (after the reflection setup is amortized).
// NOTE: The first call incurs reflect setup; the benchmark measures steady-state.
func BenchmarkValidateStruct_HappyPath(b *testing.B) {
	ResetEmitter()
	u := passingUser{Email: "bench@example.com", Age: 25}
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ValidateStruct(ctx, u)
	}
}

// BenchmarkIntMinRule_Happy benchmarks the rule itself (no reflection overhead).
func BenchmarkIntMinRule_Happy(b *testing.B) {
	r := IntMinRule{Field: "Age", Min: 18}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Validate(25, nil)
	}
}

// BenchmarkIntMinRule_Fail benchmarks the allocation path (failure case).
func BenchmarkIntMinRule_Fail(b *testing.B) {
	r := IntMinRule{Field: "Age", Min: 18}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Validate(5, nil)
	}
}

// BenchmarkEmailRule_Happy benchmarks a passing email rule.
func BenchmarkEmailRule_Happy(b *testing.B) {
	r := EmailRule{Field: "Email"}
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = r.Validate("bench@example.com", nil)
	}
}
