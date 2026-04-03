package gauzer

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// mockEmitter records calls to Emit for assertion in tests.
type mockEmitter struct {
	calls atomic.Int32
	last  *DiagnosticEvent
}

func (m *mockEmitter) Emit(_ context.Context, event *DiagnosticEvent) {
	m.calls.Add(1)
	m.last = event
}

// resetMock wipes state between sub-tests.
func (m *mockEmitter) reset() {
	m.calls.Store(0)
	m.last = nil
}

// ----------------------------------------------------------------------------
// Rule unit tests
// ----------------------------------------------------------------------------

func TestIntMinRule_Happy(t *testing.T) {
	r := IntMinRule{Field: "Age", Min: 18}
	if got := r.Validate(18, nil); got != nil {
		t.Errorf("expected nil for value == min, got %+v", got)
	}
	if got := r.Validate(99, nil); got != nil {
		t.Errorf("expected nil for value > min, got %+v", got)
	}
}

func TestIntMinRule_Fail(t *testing.T) {
	r := IntMinRule{Field: "Age", Min: 18}
	got := r.Validate(5, nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Field != "Age" {
		t.Errorf("Field = %q; want %q", got.Field, "Age")
	}
	if got.Constraint != "min:18" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "min:18")
	}
	if got.Value != "5" {
		t.Errorf("Value = %q; want %q", got.Value, "5")
	}
	if got.ValueType != "int" {
		t.Errorf("ValueType = %q; want %q", got.ValueType, "int")
	}
}

func TestIntMinRule_WrongType(t *testing.T) {
	r := IntMinRule{Field: "Age", Min: 18}
	got := r.Validate("not an int", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for wrong type, got nil")
	}
	if got.Constraint != "type:int" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "type:int")
	}
}

func TestIntMaxRule_Happy(t *testing.T) {
	r := IntMaxRule{Field: "Score", Max: 100}
	if got := r.Validate(100, nil); got != nil {
		t.Errorf("expected nil for value == max, got %+v", got)
	}
	if got := r.Validate(0, nil); got != nil {
		t.Errorf("expected nil for value < max, got %+v", got)
	}
}

func TestIntMaxRule_Fail(t *testing.T) {
	r := IntMaxRule{Field: "Score", Max: 100}
	got := r.Validate(150, nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Constraint != "max:100" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "max:100")
	}
	if got.Value != "150" {
		t.Errorf("Value = %q; want %q", got.Value, "150")
	}
}

func TestRequiredRule_Happy(t *testing.T) {
	r := RequiredRule{Field: "Email"}
	if got := r.Validate("user@example.com", nil); got != nil {
		t.Errorf("expected nil for non-empty string, got %+v", got)
	}
}

func TestRequiredRule_Fail(t *testing.T) {
	r := RequiredRule{Field: "Email"}
	got := r.Validate("", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for empty string, got nil")
	}
	if got.Constraint != "required" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "required")
	}
}

func TestEmailRule_Happy(t *testing.T) {
	r := EmailRule{Field: "Email"}
	cases := []string{
		"user@example.com",
		"a@b.io",
		"first.last@sub.domain.org",
	}
	for _, c := range cases {
		if got := r.Validate(c, nil); got != nil {
			t.Errorf("Validate(%q) = %+v; want nil", c, got)
		}
	}
}

func TestEmailRule_Fail(t *testing.T) {
	r := EmailRule{Field: "Email"}
	cases := []string{
		"",
		"notanemail",
		"@nodomain",
		"no-at-sign",
		"two@@signs.com",
	}
	for _, c := range cases {
		if got := r.Validate(c, nil); got == nil {
			t.Errorf("Validate(%q) = nil; want DiagnosticEvent", c)
		}
	}
}

func TestEmailRule_Truncates(t *testing.T) {
	r := EmailRule{Field: "Email"}
	// Build a clearly invalid email (no dot in domain) longer than 64 chars.
	// "aaaa...@nodot" — no dot in domain so isValidEmail returns false.
	long := repeatStr("a", 60) + "@nodot"
	if len(long) <= 64 {
		t.Fatalf("test setup error: long should be > 64 chars, got %d", len(long))
	}
	got := r.Validate(long, nil)
	if got == nil {
		t.Fatalf("expected DiagnosticEvent for invalid email, got nil")
	}
	if len([]rune(got.Value)) > 64 {
		t.Errorf("Value length = %d; want <= 64", len([]rune(got.Value)))
	}
}

// ----------------------------------------------------------------------------
// ValidateStruct integration tests
// ----------------------------------------------------------------------------

type validUser struct {
	Email string `gauzer:"required,email"`
	Age   int    `gauzer:"gte=18"`
}

type invalidUserEmail struct {
	Email string `gauzer:"required,email"`
	Age   int    `gauzer:"gte=18"`
}

func TestValidateStruct_PassValid(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := validUser{Email: "alice@example.com", Age: 30}
	if err := ValidateStruct(context.Background(), u); err != nil {
		t.Errorf("expected nil error for valid struct, got %v", err)
	}
	if mock.calls.Load() != 0 {
		t.Errorf("Emit called %d times; want 0", mock.calls.Load())
	}
}

func TestValidateStruct_FailRequired(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := invalidUserEmail{Email: "", Age: 30}
	err := ValidateStruct(context.Background(), u)
	if err == nil {
		t.Fatal("expected error for empty email, got nil")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Emit called %d times; want 1", mock.calls.Load())
	}
	if mock.last == nil || mock.last.Constraint != "required" {
		t.Errorf("expected constraint 'required', got %v", mock.last)
	}
}

func TestValidateStruct_FailEmail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := validUser{Email: "notvalid", Age: 25}
	err := ValidateStruct(context.Background(), u)
	if err == nil {
		t.Fatal("expected error for invalid email, got nil")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Emit called %d times; want 1", mock.calls.Load())
	}
	if mock.last == nil || mock.last.Constraint != "email" {
		t.Errorf("expected constraint 'email', got %v", mock.last)
	}
}

func TestValidateStruct_FailAge(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := validUser{Email: "bob@example.com", Age: 10}
	err := ValidateStruct(context.Background(), u)
	if err == nil {
		t.Fatal("expected error for age < 18, got nil")
	}
	if mock.calls.Load() != 1 {
		t.Errorf("Emit called %d times; want 1", mock.calls.Load())
	}
	if mock.last == nil || mock.last.Field != "Age" {
		t.Errorf("expected Field 'Age', got %v", mock.last)
	}
}

func TestValidateStruct_StopsOnFirstError(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	// Both fields fail: empty email AND age < 18
	u := validUser{Email: "", Age: 5}
	err := ValidateStruct(context.Background(), u)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	// Must stop after the first failure
	if mock.calls.Load() != 1 {
		t.Errorf("Emit called %d times; want exactly 1 (stop-on-first)", mock.calls.Load())
	}
}

func TestValidateStruct_Pointer(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := &validUser{Email: "alice@example.com", Age: 21}
	if err := ValidateStruct(context.Background(), u); err != nil {
		t.Errorf("expected nil for valid pointer struct, got %v", err)
	}
}

func TestValidateStruct_EmitterNotCalled_OnSuccess(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	u := validUser{Email: "valid@test.org", Age: 18}
	_ = ValidateStruct(context.Background(), u)
	if mock.calls.Load() != 0 {
		t.Errorf("Emit should not be called on success, got %d calls", mock.calls.Load())
	}
}

// ----------------------------------------------------------------------------
// emitter tests
// ----------------------------------------------------------------------------

func TestSetEmitter_ResetEmitter(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	got := getEmitter()
	if got != mock {
		t.Error("getEmitter() did not return the emitter set by SetEmitter")
	}
	ResetEmitter()
	got = getEmitter()
	if _, ok := got.(DefaultSlogEmitter); !ok {
		t.Errorf("after ResetEmitter(), getEmitter() = %T; want DefaultSlogEmitter", got)
	}
}

// ----------------------------------------------------------------------------
// StringMinLengthRule tests
// ----------------------------------------------------------------------------

func TestStringMinLengthRule_Happy(t *testing.T) {
	r := StringMinLengthRule{Field: "Name", Min: 3}
	if got := r.Validate("abc", nil); got != nil {
		t.Errorf("expected nil for len==min, got %+v", got)
	}
	if got := r.Validate("abcdef", nil); got != nil {
		t.Errorf("expected nil for len>min, got %+v", got)
	}
}

func TestStringMinLengthRule_Fail(t *testing.T) {
	r := StringMinLengthRule{Field: "Name", Min: 3}
	got := r.Validate("ab", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Constraint != "min:3" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "min:3")
	}
	if got.Value != "ab" {
		t.Errorf("Value = %q; want %q", got.Value, "ab")
	}
	if got.ValueType != "string" {
		t.Errorf("ValueType = %q; want %q", got.ValueType, "string")
	}
}

// ----------------------------------------------------------------------------
// StringMaxLengthRule tests
// ----------------------------------------------------------------------------

func TestStringMaxLengthRule_Happy(t *testing.T) {
	r := StringMaxLengthRule{Field: "Name", Max: 10}
	if got := r.Validate("hello", nil); got != nil {
		t.Errorf("expected nil for len<max, got %+v", got)
	}
	if got := r.Validate("0123456789", nil); got != nil {
		t.Errorf("expected nil for len==max, got %+v", got)
	}
}

func TestStringMaxLengthRule_Fail(t *testing.T) {
	r := StringMaxLengthRule{Field: "Name", Max: 5}
	got := r.Validate("toolong", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Constraint != "max:5" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "max:5")
	}
}

// ----------------------------------------------------------------------------
// StringRequiredRule tests
// ----------------------------------------------------------------------------

func TestStringRequiredRule_Happy(t *testing.T) {
	r := StringRequiredRule{Field: "Name"}
	if got := r.Validate("hello", nil); got != nil {
		t.Errorf("expected nil for non-empty, got %+v", got)
	}
}

func TestStringRequiredRule_Fail_Empty(t *testing.T) {
	r := StringRequiredRule{Field: "Name"}
	got := r.Validate("", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for empty, got nil")
	}
	if got.Constraint != "required" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "required")
	}
}

func TestStringRequiredRule_Fail_Whitespace(t *testing.T) {
	r := StringRequiredRule{Field: "Name"}
	for _, ws := range []string{"   ", "\t", "\n", "  \t  "} {
		got := r.Validate(ws, nil)
		if got == nil {
			t.Errorf("expected DiagnosticEvent for whitespace-only %q, got nil", ws)
		}
	}
}

// ----------------------------------------------------------------------------
// OneOfRule tests
// ----------------------------------------------------------------------------

func TestOneOfRule_Happy(t *testing.T) {
	r := OneOfRule{Field: "Status", Allowed: []string{"dog", "cat"}}
	if got := r.Validate("dog", nil); got != nil {
		t.Errorf("expected nil for 'dog', got %+v", got)
	}
	if got := r.Validate("cat", nil); got != nil {
		t.Errorf("expected nil for 'cat', got %+v", got)
	}
}

func TestOneOfRule_Fail(t *testing.T) {
	r := OneOfRule{Field: "Animal", Allowed: []string{"dog", "cat"}}
	got := r.Validate("fish", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for 'fish', got nil")
	}
	if got.Field != "Animal" {
		t.Errorf("Field = %q; want %q", got.Field, "Animal")
	}
	if got.Value != "fish" {
		t.Errorf("Value = %q; want %q", got.Value, "fish")
	}
}

// ----------------------------------------------------------------------------
// RegexRule tests
// ----------------------------------------------------------------------------

func TestRegexRule_Happy(t *testing.T) {
	r, err := NewRegexRule("Code", `^\d{3}$`)
	if err != nil {
		t.Fatalf("NewRegexRule: %v", err)
	}
	if got := r.Validate("123", nil); got != nil {
		t.Errorf("expected nil for matching pattern, got %+v", got)
	}
}

func TestRegexRule_Fail(t *testing.T) {
	r, err := NewRegexRule("Code", `^\d{3}$`)
	if err != nil {
		t.Fatalf("NewRegexRule: %v", err)
	}
	got := r.Validate("abc", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for non-matching, got nil")
	}
	if got.Field != "Code" {
		t.Errorf("Field = %q; want %q", got.Field, "Code")
	}
}

// ----------------------------------------------------------------------------
// FloatMinRule tests
// ----------------------------------------------------------------------------

func TestFloatMinRule_Happy(t *testing.T) {
	r := FloatMinRule{Field: "Score", Min: 0.5}
	if got := r.Validate(0.5, nil); got != nil {
		t.Errorf("expected nil for value==min, got %+v", got)
	}
	if got := r.Validate(1.0, nil); got != nil {
		t.Errorf("expected nil for value>min, got %+v", got)
	}
}

func TestFloatMinRule_Fail(t *testing.T) {
	r := FloatMinRule{Field: "Score", Min: 1.0}
	got := r.Validate(0.5, nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.ValueType != "float64" {
		t.Errorf("ValueType = %q; want %q", got.ValueType, "float64")
	}
}

// ----------------------------------------------------------------------------
// FloatMaxRule tests
// ----------------------------------------------------------------------------

func TestFloatMaxRule_Happy(t *testing.T) {
	r := FloatMaxRule{Field: "Score", Max: 1.0}
	if got := r.Validate(1.0, nil); got != nil {
		t.Errorf("expected nil for value==max, got %+v", got)
	}
	if got := r.Validate(0.5, nil); got != nil {
		t.Errorf("expected nil for value<max, got %+v", got)
	}
}

func TestFloatMaxRule_Fail(t *testing.T) {
	r := FloatMaxRule{Field: "Score", Max: 1.0}
	got := r.Validate(2.0, nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.ValueType != "float64" {
		t.Errorf("ValueType = %q; want %q", got.ValueType, "float64")
	}
}

// ----------------------------------------------------------------------------
// UUIDRule tests
// ----------------------------------------------------------------------------

func TestUUIDRule_Happy(t *testing.T) {
	r := UUIDRule{Field: "ID"}
	valid := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"6ba7b810-9dad-11d1-80b4-00c04fd430c8",
	}
	for _, v := range valid {
		if got := r.Validate(v, nil); got != nil {
			t.Errorf("Validate(%q) = %+v; want nil", v, got)
		}
	}
}

func TestUUIDRule_Fail(t *testing.T) {
	r := UUIDRule{Field: "ID"}
	invalid := []string{
		"",
		"not-a-uuid",
		"550e8400-e29b-41d4-a716-44665544000",   // too short
		"550e8400-e29b-41d4-a716-4466554400000", // too long
		"550e8400e29b41d4a716446655440000",      // no hyphens
	}
	for _, v := range invalid {
		if got := r.Validate(v, nil); got == nil {
			t.Errorf("Validate(%q) = nil; want DiagnosticEvent", v)
		}
	}
}

// ----------------------------------------------------------------------------
// IPRule tests
// ----------------------------------------------------------------------------

func TestIPRule_Happy(t *testing.T) {
	r := IPRule{Field: "Address"}
	valid := []string{"192.168.1.1", "::1", "2001:db8::1"}
	for _, v := range valid {
		if got := r.Validate(v, nil); got != nil {
			t.Errorf("Validate(%q) = %+v; want nil", v, got)
		}
	}
}

func TestIPRule_Fail(t *testing.T) {
	r := IPRule{Field: "Address"}
	invalid := []string{"", "notanip", "999.999.999.999", "256.0.0.1"}
	for _, v := range invalid {
		if got := r.Validate(v, nil); got == nil {
			t.Errorf("Validate(%q) = nil; want DiagnosticEvent", v)
		}
	}
}

// ----------------------------------------------------------------------------
// RequiredRule whitespace test (existing rule update)
// ----------------------------------------------------------------------------

func TestRequiredRule_Fail_Whitespace(t *testing.T) {
	r := RequiredRule{Field: "Name"}
	for _, ws := range []string{"   ", "\t", "\n"} {
		got := r.Validate(ws, nil)
		if got == nil {
			t.Errorf("expected DiagnosticEvent for whitespace-only %q, got nil", ws)
		}
	}
}

// ----------------------------------------------------------------------------
// Type-aware tag integration tests (min/max on string vs int)
// ----------------------------------------------------------------------------

type productForm struct {
	Name   string  `gauzer:"min=3,max=50"`
	Price  float64 `gauzer:"min=0.01,max=9999.99"`
	Qty    int     `gauzer:"min=1,max=100"`
	Status string  `gauzer:"oneof=active|inactive|draft"`
	Code   string  `gauzer:"regexp=^[A-Z]{3}$"`
}

func TestTypeAwareTags_StringMin_Pass(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	p := productForm{Name: "Foo", Price: 1.99, Qty: 5, Status: "active", Code: "ABC"}
	if err := ValidateStruct(context.Background(), p); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestTypeAwareTags_StringMin_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	p := productForm{Name: "ab", Price: 1.99, Qty: 5, Status: "active", Code: "ABC"}
	err := ValidateStruct(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for short name, got nil")
	}
	if mock.last == nil || mock.last.Constraint != "min:3" {
		t.Errorf("expected constraint 'min:3', got %v", mock.last)
	}
}

func TestTypeAwareTags_FloatMin_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	p := productForm{Name: "Foo", Price: 0.0, Qty: 5, Status: "active", Code: "ABC"}
	err := ValidateStruct(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for price=0.0, got nil")
	}
	if mock.last == nil || mock.last.ValueType != "float64" {
		t.Errorf("expected float64 error, got %v", mock.last)
	}
}

func TestTypeAwareTags_OneOf_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	p := productForm{Name: "Foo", Price: 1.99, Qty: 5, Status: "unknown", Code: "ABC"}
	err := ValidateStruct(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for status='unknown', got nil")
	}
}

func TestTypeAwareTags_Regexp_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	p := productForm{Name: "Foo", Price: 1.99, Qty: 5, Status: "active", Code: "abc"}
	err := ValidateStruct(context.Background(), p)
	if err == nil {
		t.Fatal("expected error for code='abc' not matching ^[A-Z]{3}$, got nil")
	}
}

// UUID / IP integration test via struct tags
type resourceForm struct {
	ID      string `gauzer:"uuid"`
	Address string `gauzer:"ip"`
}

func TestUUIDAndIP_Pass(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	r := resourceForm{ID: "550e8400-e29b-41d4-a716-446655440000", Address: "192.168.1.1"}
	if err := ValidateStruct(context.Background(), r); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestUUIDAndIP_Fail_UUID(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	r := resourceForm{ID: "not-a-uuid", Address: "192.168.1.1"}
	err := ValidateStruct(context.Background(), r)
	if err == nil {
		t.Fatal("expected error for invalid UUID")
	}
}

// ----------------------------------------------------------------------------
// splitTagTokens parser tests
// ----------------------------------------------------------------------------

func TestSplitTagTokens_Simple(t *testing.T) {
	got := splitTagTokens("required,email")
	want := []string{"required", "email"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q; want %q", i, got[i], want[i])
		}
	}
}

func TestSplitTagTokens_WithValues(t *testing.T) {
	got := splitTagTokens("min=3,max=50")
	want := []string{"min=3", "max=50"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q; want %q", i, got[i], want[i])
		}
	}
}

func TestSplitTagTokens_RegexpWithComma(t *testing.T) {
	// Comma inside the regexp value must NOT split the token.
	got := splitTagTokens("required,min=5,regexp=^a,b$")
	want := []string{"required", "min=5", "regexp=^a,b$"}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q; want %q", i, got[i], want[i])
		}
	}
}

func TestSplitTagTokens_RegexpQuantifier(t *testing.T) {
	// {1,3} quantifier — comma is between a digit and a digit, not a tag name.
	got := splitTagTokens(`regexp=\d{1,3}`)
	want := []string{`regexp=\d{1,3}`}
	if len(got) != len(want) {
		t.Fatalf("got %v; want %v", got, want)
	}
	if got[0] != want[0] {
		t.Errorf("got %q; want %q", got[0], want[0])
	}
}

func TestSplitTagTokens_SingleToken(t *testing.T) {
	got := splitTagTokens("required")
	if len(got) != 1 || got[0] != "required" {
		t.Errorf("got %v; want [required]", got)
	}
}

// ----------------------------------------------------------------------------
// Integration: regexp tag with comma in pattern via struct tag
// ----------------------------------------------------------------------------

type csvFieldForm struct {
	Value string `gauzer:"required,regexp=^a,b$"`
}

func TestRegexpCommaInPattern_Pass(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := csvFieldForm{Value: "a,b"}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for value matching regexp, got %v", err)
	}
}

func TestRegexpCommaInPattern_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := csvFieldForm{Value: "ab"}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for value not matching regexp=^a,b$, got nil")
	}
}

// ----------------------------------------------------------------------------
// helpers
// ----------------------------------------------------------------------------

func repeatStr(s string, n int) string {
	result := ""
	for i := 0; i < n; i++ {
		result += s
	}
	return result
}

// ============================================================================
// omitempty tests
// ============================================================================

type omitemptyStringStruct struct {
	Name string `gauzer:"omitempty,min=3"`
}

type omitemptyIntStruct struct {
	Score int `gauzer:"omitempty,gte=10"`
}

type omitemptySliceStruct struct {
	Tags []string `gauzer:"omitempty,min=1"`
}

type omitemptyMultiStruct struct {
	Nick  string `gauzer:"omitempty,min=3,max=20"`
	Score int    `gauzer:"omitempty,gte=5"`
}

// TestOmitempty_String_ZeroValue skips rules when string is empty.
func TestOmitempty_String_ZeroValue(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyStringStruct{Name: ""} // zero value — should skip min=3
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for empty name with omitempty, got %v", err)
	}
	if mock.calls.Load() != 0 {
		t.Errorf("Emit called %d times; want 0", mock.calls.Load())
	}
}

// TestOmitempty_String_NonzeroValid passes when value is set and valid.
func TestOmitempty_String_NonzeroValid(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyStringStruct{Name: "alice"} // len=5 >= min=3 → pass
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for valid name, got %v", err)
	}
}

// TestOmitempty_String_NonzeroInvalid fails when value is set but violates the rule.
func TestOmitempty_String_NonzeroInvalid(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyStringStruct{Name: "ab"} // len=2 < min=3 → fail
	err := ValidateStruct(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for name 'ab' (len=2 < min=3), got nil")
	}
	if mock.last == nil || mock.last.Constraint != "min:3" {
		t.Errorf("expected constraint 'min:3', got %v", mock.last)
	}
}

// TestOmitempty_Int_ZeroValue skips rules when int is 0.
func TestOmitempty_Int_ZeroValue(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyIntStruct{Score: 0}
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for zero int with omitempty, got %v", err)
	}
}

// TestOmitempty_Int_NonzeroInvalid fails when int is nonzero but below threshold.
func TestOmitempty_Int_NonzeroInvalid(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyIntStruct{Score: 3} // 3 < gte=10 → fail
	err := ValidateStruct(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for score=3 with gte=10, got nil")
	}
	if mock.last == nil || mock.last.Field != "Score" {
		t.Errorf("expected Field 'Score', got %v", mock.last)
	}
}

// TestOmitempty_Slice_NilSkips skips rules when slice is nil.
func TestOmitempty_Slice_NilSkips(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptySliceStruct{Tags: nil}
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for nil slice with omitempty, got %v", err)
	}
}

// TestOmitempty_Slice_EmptySkips skips rules when slice has zero length.
func TestOmitempty_Slice_EmptySkips(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptySliceStruct{Tags: []string{}}
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for empty slice with omitempty, got %v", err)
	}
}

// TestOmitempty_Slice_NonEmptyValidates validates a non-empty slice against its rules.
func TestOmitempty_Slice_NonEmptyValidates(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptySliceStruct{Tags: []string{"go"}} // len=1 >= min=1 → pass
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for non-empty valid slice, got %v", err)
	}
}

// TestOmitempty_Multi_BothZero skips both fields when both are zero.
func TestOmitempty_Multi_BothZero(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := omitemptyMultiStruct{Nick: "", Score: 0}
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil when both fields are zero with omitempty, got %v", err)
	}
}

// TestOmitempty_Multi_OneSetOneZero validates only the non-zero field.
func TestOmitempty_Multi_OneSetOneZero(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	// Score is set but invalid; Nick is zero so skipped.
	s := omitemptyMultiStruct{Nick: "", Score: 2}
	err := ValidateStruct(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for score=2 (gte=5), got nil")
	}
	if mock.last == nil || mock.last.Field != "Score" {
		t.Errorf("expected Field='Score', got %v", mock.last)
	}
}

// ============================================================================
// eqfield tests
// ============================================================================

type passwordForm struct {
	Password        string `gauzer:"min=8"`
	PasswordConfirm string `gauzer:"eqfield=Password"`
}

type intEqFieldForm struct {
	A int `gauzer:"min=1"`
	B int `gauzer:"eqfield=A"`
}

// TestEqfield_MatchingPasswords passes when both fields are equal.
func TestEqfield_MatchingPasswords(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := passwordForm{Password: "secret123", PasswordConfirm: "secret123"}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for matching passwords, got %v", err)
	}
}

// TestEqfield_MismatchedPasswords fails when fields differ.
func TestEqfield_MismatchedPasswords(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := passwordForm{Password: "secret123", PasswordConfirm: "different!"}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for mismatched passwords, got nil")
	}
	if mock.last == nil || mock.last.Field != "PasswordConfirm" {
		t.Errorf("expected Field='PasswordConfirm', got %v", mock.last)
	}
	if mock.last.Constraint != "eqfield=Password" {
		t.Errorf("expected Constraint='eqfield=Password', got %q", mock.last.Constraint)
	}
}

// TestEqfield_PasswordTooShort first fails the min=8 rule on Password before checking eqfield.
func TestEqfield_PasswordTooShort(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := passwordForm{Password: "short", PasswordConfirm: "short"}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for short password, got nil")
	}
	// Should fail on Password's min=8 first because fields are ordered.
	if mock.last == nil || mock.last.Field != "Password" {
		t.Errorf("expected Field='Password', got %v", mock.last)
	}
}

// TestEqfield_EmptyBothPasswords: both empty — min=8 fails first.
func TestEqfield_EmptyBothPasswords(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := passwordForm{Password: "", PasswordConfirm: ""}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for empty passwords, got nil")
	}
}

// TestEqfield_IntEqual passes when integer fields are equal.
func TestEqfield_IntEqual(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := intEqFieldForm{A: 5, B: 5}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for equal int fields, got %v", err)
	}
}

// TestEqfield_IntNotEqual fails when integer fields differ.
func TestEqfield_IntNotEqual(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := intEqFieldForm{A: 5, B: 3}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for unequal int fields, got nil")
	}
	if mock.last == nil || mock.last.Field != "B" {
		t.Errorf("expected Field='B', got %v", mock.last)
	}
}

// TestEqfield_PointerStruct works when struct is passed as a pointer.
func TestEqfield_PointerStruct(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := &passwordForm{Password: "supersecret", PasswordConfirm: "supersecret"}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for pointer struct with matching fields, got %v", err)
	}
}

// TestEqfield_DirectRule verifies EqFieldRule.Validate returns nil when parent matches.
func TestEqfield_DirectRule(t *testing.T) {
	r := EqFieldRule{Field: "PasswordConfirm", OtherField: "Password"}
	parent := passwordForm{Password: "abc123!!", PasswordConfirm: "abc123!!"}
	if got := r.Validate("abc123!!", parent); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

// TestEqfield_DirectRule_Mismatch verifies EqFieldRule.Validate fails on mismatch.
func TestEqfield_DirectRule_Mismatch(t *testing.T) {
	r := EqFieldRule{Field: "PasswordConfirm", OtherField: "Password"}
	parent := passwordForm{Password: "abc123!!", PasswordConfirm: "other"}
	got := r.Validate("other", parent)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Constraint != "eqfield=Password" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "eqfield=Password")
	}
}

// TestEqfield_NilParent returns an error when parent is nil.
func TestEqfield_NilParent(t *testing.T) {
	r := EqFieldRule{Field: "X", OtherField: "Y"}
	got := r.Validate("hello", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for nil parent, got nil")
	}
}

// ============================================================================
// Universal comparator tests
// ============================================================================

func TestGteRule_Int_Pass(t *testing.T) {
	r := GteRule{Field: "Age", Threshold: 18}
	if got := r.Validate(18, nil); got != nil {
		t.Errorf("expected nil for 18 >= 18, got %+v", got)
	}
	if got := r.Validate(25, nil); got != nil {
		t.Errorf("expected nil for 25 >= 18, got %+v", got)
	}
}

func TestGteRule_Int_Fail(t *testing.T) {
	r := GteRule{Field: "Age", Threshold: 18}
	got := r.Validate(17, nil)
	if got == nil {
		t.Fatal("expected error for 17 < 18")
	}
	if got.Constraint != "gte:18" {
		t.Errorf("Constraint = %q; want 'gte:18'", got.Constraint)
	}
}

func TestGteRule_Int64_Pass(t *testing.T) {
	r := GteRule{Field: "Count", Threshold: 100}
	if got := r.Validate(int64(200), nil); got != nil {
		t.Errorf("expected nil for int64(200) >= 100, got %+v", got)
	}
}

func TestGteRule_String_Length(t *testing.T) {
	r := GteRule{Field: "Name", Threshold: 3}
	if got := r.Validate("abc", nil); got != nil {
		t.Errorf("expected nil for len=3 >= 3, got %+v", got)
	}
	got := r.Validate("ab", nil)
	if got == nil {
		t.Fatal("expected error for len=2 < 3")
	}
	if got.ValueType != "string" {
		t.Errorf("ValueType = %q; want 'string'", got.ValueType)
	}
}

func TestLteRule_Float_Pass(t *testing.T) {
	r := LteRule{Field: "Score", Threshold: 100.0}
	if got := r.Validate(float64(99.9), nil); got != nil {
		t.Errorf("expected nil for 99.9 <= 100, got %+v", got)
	}
}

func TestGtRule_Pass(t *testing.T) {
	r := GtRule{Field: "Val", Threshold: 0}
	if got := r.Validate(1, nil); got != nil {
		t.Errorf("expected nil for 1 > 0, got %+v", got)
	}
	if got := r.Validate(0, nil); got == nil {
		t.Error("expected error for 0 > 0 = false")
	}
}

func TestLtRule_Pass(t *testing.T) {
	r := LtRule{Field: "Val", Threshold: 10}
	if got := r.Validate(9, nil); got != nil {
		t.Errorf("expected nil for 9 < 10, got %+v", got)
	}
	if got := r.Validate(10, nil); got == nil {
		t.Error("expected error for 10 < 10 = false")
	}
}

func TestEqRule_Int(t *testing.T) {
	r := EqRule{Field: "Count", Threshold: 5}
	if got := r.Validate(5, nil); got != nil {
		t.Errorf("expected nil for 5 == 5, got %+v", got)
	}
	if got := r.Validate(4, nil); got == nil {
		t.Error("expected error for 4 != 5")
	}
}

func TestNeRule_Int(t *testing.T) {
	r := NeRule{Field: "Status", Threshold: 0}
	if got := r.Validate(1, nil); got != nil {
		t.Errorf("expected nil for 1 != 0, got %+v", got)
	}
	if got := r.Validate(0, nil); got == nil {
		t.Error("expected error for 0 == 0")
	}
}

func TestGteRule_TimeTime(t *testing.T) {
	r := GteRule{Field: "CreatedAt", Threshold: 1} // at least 1 second ago
	old := time.Now().Add(-2 * time.Second)
	if got := r.Validate(old, nil); got != nil {
		t.Errorf("expected nil for time 2s ago >= 1s, got %+v", got)
	}
	recent := time.Now().Add(10 * time.Second) // in the future: Since < 0
	if got := r.Validate(recent, nil); got == nil {
		t.Error("expected error for future time with gte=1s")
	}
}

// ============================================================================
// String rules tests
// ============================================================================

func TestStringLenRule_Pass(t *testing.T) {
	r := StringLenRule{Field: "Code", Len: 5}
	if got := r.Validate("hello", nil); got != nil {
		t.Errorf("expected nil for len=5, got %+v", got)
	}
}

func TestStringLenRule_Fail(t *testing.T) {
	r := StringLenRule{Field: "Code", Len: 5}
	got := r.Validate("hi", nil)
	if got == nil {
		t.Fatal("expected error for len=2 != 5")
	}
	if got.Constraint != "len:5" {
		t.Errorf("Constraint = %q; want 'len:5'", got.Constraint)
	}
}

func TestContainsRule_Pass(t *testing.T) {
	r := ContainsRule{Field: "Desc", Substr: "go"}
	if got := r.Validate("golang", nil); got != nil {
		t.Errorf("expected nil for 'golang' contains 'go', got %+v", got)
	}
}

func TestContainsRule_Fail(t *testing.T) {
	r := ContainsRule{Field: "Desc", Substr: "rust"}
	got := r.Validate("golang", nil)
	if got == nil {
		t.Fatal("expected error for 'golang' not containing 'rust'")
	}
	if got.Constraint != "contains:rust" {
		t.Errorf("Constraint = %q; want 'contains:rust'", got.Constraint)
	}
}

func TestExcludesRule_Pass(t *testing.T) {
	r := ExcludesRule{Field: "Input", Substr: "<script>"}
	if got := r.Validate("Hello World", nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestExcludesRule_Fail(t *testing.T) {
	r := ExcludesRule{Field: "Input", Substr: "<script>"}
	got := r.Validate("<script>alert(1)</script>", nil)
	if got == nil {
		t.Fatal("expected error for string containing '<script>'")
	}
}

func TestStartsWithRule_Pass(t *testing.T) {
	r := StartsWithRule{Field: "Path", Prefix: "/api"}
	if got := r.Validate("/api/users", nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestStartsWithRule_Fail(t *testing.T) {
	r := StartsWithRule{Field: "Path", Prefix: "/api"}
	got := r.Validate("/admin", nil)
	if got == nil {
		t.Fatal("expected error for '/admin' not starting with '/api'")
	}
	if got.Constraint != "startswith:/api" {
		t.Errorf("Constraint = %q; want 'startswith:/api'", got.Constraint)
	}
}

func TestEndsWithRule_Pass(t *testing.T) {
	r := EndsWithRule{Field: "File", Suffix: ".go"}
	if got := r.Validate("main.go", nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestEndsWithRule_Fail(t *testing.T) {
	r := EndsWithRule{Field: "File", Suffix: ".go"}
	got := r.Validate("main.py", nil)
	if got == nil {
		t.Fatal("expected error for 'main.py' not ending with '.go'")
	}
}

func TestURLRule_Pass(t *testing.T) {
	r := URLRule{Field: "Website"}
	valid := []string{"https://example.com", "http://foo.bar/path?q=1"}
	for _, v := range valid {
		if got := r.Validate(v, nil); got != nil {
			t.Errorf("Validate(%q) = %+v; want nil", v, got)
		}
	}
}

func TestURLRule_Fail(t *testing.T) {
	r := URLRule{Field: "Website"}
	invalid := []string{"", "not-a-url", "ftp://", "/relative/path", "mailto:user@example.com"}
	for _, v := range invalid {
		if got := r.Validate(v, nil); got == nil {
			t.Errorf("Validate(%q) = nil; want DiagnosticEvent", v)
		}
	}
}

func TestURIRule_Pass(t *testing.T) {
	r := URIRule{Field: "Resource"}
	valid := []string{"https://example.com", "urn:isbn:0451450523", "mailto:user@example.com"}
	for _, v := range valid {
		if got := r.Validate(v, nil); got != nil {
			t.Errorf("Validate(%q) = %+v; want nil", v, got)
		}
	}
}

func TestURIRule_Fail(t *testing.T) {
	r := URIRule{Field: "Resource"}
	invalid := []string{"", "no-scheme", "//no-scheme.com"}
	for _, v := range invalid {
		if got := r.Validate(v, nil); got == nil {
			t.Errorf("Validate(%q) = nil; want DiagnosticEvent", v)
		}
	}
}

// ============================================================================
// Slice / Map rule tests
// ============================================================================

type sliceForm struct {
	Tags    []string       `gauzer:"min=1,max=5"`
	Counts  []int          `gauzer:"len=3"`
	Mapping map[string]int `gauzer:"min=1"`
}

func TestCollectionMin_Pass(t *testing.T) {
	r := CollectionMinLenRule{Field: "Tags", Min: 2}
	if got := r.Validate([]string{"a", "b"}, nil); got != nil {
		t.Errorf("expected nil, got %+v", got)
	}
}

func TestCollectionMin_Fail(t *testing.T) {
	r := CollectionMinLenRule{Field: "Tags", Min: 3}
	got := r.Validate([]string{"a", "b"}, nil)
	if got == nil {
		t.Fatal("expected error for len=2 < min=3")
	}
	if got.Constraint != "min:3" {
		t.Errorf("Constraint = %q; want 'min:3'", got.Constraint)
	}
	if got.ValueType != "collection" {
		t.Errorf("ValueType = %q; want 'collection'", got.ValueType)
	}
}

func TestCollectionMax_Fail(t *testing.T) {
	r := CollectionMaxLenRule{Field: "Tags", Max: 3}
	got := r.Validate([]string{"a", "b", "c", "d"}, nil)
	if got == nil {
		t.Fatal("expected error for len=4 > max=3")
	}
	if got.Constraint != "max:3" {
		t.Errorf("Constraint = %q; want 'max:3'", got.Constraint)
	}
}

func TestCollectionLen_Exact(t *testing.T) {
	r := CollectionLenRule{Field: "Items", Len: 2}
	if got := r.Validate([]int{1, 2}, nil); got != nil {
		t.Errorf("expected nil for exact len=2, got %+v", got)
	}
	got := r.Validate([]int{1}, nil)
	if got == nil {
		t.Fatal("expected error for len=1 != 2")
	}
}

func TestUniqueRule_Pass(t *testing.T) {
	r := UniqueRule{Field: "IDs"}
	if got := r.Validate([]string{"a", "b", "c"}, nil); got != nil {
		t.Errorf("expected nil for unique slice, got %+v", got)
	}
}

func TestUniqueRule_Fail(t *testing.T) {
	r := UniqueRule{Field: "IDs"}
	got := r.Validate([]string{"a", "b", "a"}, nil)
	if got == nil {
		t.Fatal("expected error for duplicate 'a'")
	}
	if got.Constraint != "unique" {
		t.Errorf("Constraint = %q; want 'unique'", got.Constraint)
	}
}

func TestUniqueRule_IntSlice_Fail(t *testing.T) {
	r := UniqueRule{Field: "Numbers"}
	got := r.Validate([]int{1, 2, 3, 2}, nil)
	if got == nil {
		t.Fatal("expected error for duplicate 2 in int slice")
	}
}

func TestCollectionMin_OnMap(t *testing.T) {
	r := CollectionMinLenRule{Field: "Config", Min: 2}
	m := map[string]int{"a": 1, "b": 2}
	if got := r.Validate(m, nil); got != nil {
		t.Errorf("expected nil for map len=2 >= min=2, got %+v", got)
	}
	m2 := map[string]int{"a": 1}
	if got := r.Validate(m2, nil); got == nil {
		t.Error("expected error for map len=1 < min=2")
	}
}

// Integration: min/max on slice via struct tag.
func TestSliceMinMax_ViaStructTag(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := sliceForm{
		Tags:    []string{"go", "rust", "python", "java", "c", "extra"},
		Counts:  []int{1, 2, 3},
		Mapping: map[string]int{"k": 1},
	}
	err := ValidateStruct(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for Tags len=6 > max=5")
	}
	if mock.last == nil || mock.last.Field != "Tags" {
		t.Errorf("expected Field='Tags', got %v", mock.last)
	}
}

// ============================================================================
// Dive rule tests
// ============================================================================

type diveStringForm struct {
	Names []string `gauzer:"dive,min=3"`
}

type diveIntForm struct {
	Scores []int `gauzer:"dive,gte=0"`
}

func TestDive_StringSlice_AllValid(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := diveStringForm{Names: []string{"alice", "bob", "charlie"}}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil, got %v", err)
	}
}

func TestDive_StringSlice_ElementFails(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := diveStringForm{Names: []string{"alice", "ab", "charlie"}} // "ab" len=2 < 3
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for 'ab' failing min=3")
	}
	ev, ok := err.(*DiagnosticEvent)
	if !ok {
		t.Fatalf("expected *DiagnosticEvent, got %T", err)
	}
	if ev.Field != "Names[1]" {
		t.Errorf("Field = %q; want 'Names[1]'", ev.Field)
	}
}

func TestDive_IntSlice_ElementFails(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := diveIntForm{Scores: []int{10, 5, -1, 7}} // -1 < gte=0
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for -1 failing gte=0")
	}
	ev, ok := err.(*DiagnosticEvent)
	if !ok {
		t.Fatalf("expected *DiagnosticEvent, got %T", err)
	}
	if ev.Field != "Scores[2]" {
		t.Errorf("Field = %q; want 'Scores[2]'", ev.Field)
	}
}

func TestDive_EmptySlice_Passes(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := diveStringForm{Names: []string{}}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for empty slice, got %v", err)
	}
}

func TestDive_Rule_Direct(t *testing.T) {
	subRule := StringMinLengthRule{Field: "Names", Min: 5}
	r := DiveRule{Field: "Names", SubRules: []Rule{subRule}}

	// First element fails: len("hi") = 2 < 5
	got := r.Validate([]string{"hello", "hi", "world"}, nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent, got nil")
	}
	if got.Field != "Names[1]" {
		t.Errorf("Field = %q; want 'Names[1]'", got.Field)
	}
}

// ============================================================================
// nefield tests
// ============================================================================

type neFieldTestStruct struct {
	FieldA string `gauzer:"nefield=FieldB"`
	FieldB string
}

// TestNeFieldRule_NotEqual: FieldA != FieldB → validation passes (nil).
func TestNeFieldRule_NotEqual(t *testing.T) {
	r := NeFieldRule{Field: "FieldA", OtherField: "FieldB"}
	type s struct{ FieldA, FieldB string }
	parent := s{FieldA: "hello", FieldB: "world"}
	if got := r.Validate("hello", parent); got != nil {
		t.Errorf("expected nil for different values, got %+v", got)
	}
}

// TestNeFieldRule_Equal: FieldA == FieldB → validation fails with correct DiagnosticEvent.
func TestNeFieldRule_Equal(t *testing.T) {
	r := NeFieldRule{Field: "FieldA", OtherField: "FieldB"}
	type s struct{ FieldA, FieldB string }
	parent := s{FieldA: "same", FieldB: "same"}
	got := r.Validate("same", parent)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for equal values, got nil")
	}
	if got.Constraint != "nefield:FieldB" {
		t.Errorf("Constraint = %q; want %q", got.Constraint, "nefield:FieldB")
	}
	if got.Message != "FieldA must not equal FieldB" {
		t.Errorf("Message = %q; want %q", got.Message, "FieldA must not equal FieldB")
	}
}

// TestNeFieldRule_DifferentTypes: int vs string → reflect.DeepEqual is false → passes.
func TestNeFieldRule_DifferentTypes(t *testing.T) {
	r := NeFieldRule{Field: "FieldB", OtherField: "FieldA"}
	type s struct {
		FieldA int
		FieldB string
	}
	parent := s{FieldA: 42, FieldB: "42"}
	if got := r.Validate("42", parent); got != nil {
		t.Errorf("expected nil for different types (int vs string), got %+v", got)
	}
}

// TestNeFieldRule_NilParent: nil parent → fails safely without panicking.
func TestNeFieldRule_NilParent(t *testing.T) {
	r := NeFieldRule{Field: "X", OtherField: "Y"}
	got := r.Validate("hello", nil)
	if got == nil {
		t.Fatal("expected DiagnosticEvent for nil parent, got nil")
	}
}

// TestNeField_Integration_Pass: different field values → ValidateStruct returns nil.
func TestNeField_Integration_Pass(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := neFieldTestStruct{FieldA: "foo", FieldB: "bar"}
	if err := ValidateStruct(context.Background(), f); err != nil {
		t.Errorf("expected nil for different fields, got %v", err)
	}
	if mock.calls.Load() != 0 {
		t.Errorf("Emit called %d times; want 0", mock.calls.Load())
	}
}

// TestNeField_Integration_Fail: equal field values → ValidateStruct returns error with correct event.
func TestNeField_Integration_Fail(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	f := neFieldTestStruct{FieldA: "same", FieldB: "same"}
	err := ValidateStruct(context.Background(), f)
	if err == nil {
		t.Fatal("expected error for equal fields, got nil")
	}
	if mock.last == nil || mock.last.Field != "FieldA" {
		t.Errorf("expected Field='FieldA', got %v", mock.last)
	}
	if mock.last == nil || mock.last.Constraint != "nefield:FieldB" {
		t.Errorf("expected Constraint='nefield:FieldB', got %v", mock.last)
	}
}

// ============================================================================
// mask tests
// ============================================================================

type maskedStringForm struct {
	Secret string `gauzer:"required,min=5,mask"`
}

// TestMaskRule ensures a failing masked field returns Value="***" instead of the raw value.
func TestMaskRule(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := maskedStringForm{Secret: "ab"} // len=2 < min=5 → fail; mask applied
	err := ValidateStruct(context.Background(), s)
	if err == nil {
		t.Fatal("expected error for secret 'ab' (len=2 < min=5), got nil")
	}
	ev, ok := err.(*DiagnosticEvent)
	if !ok {
		t.Fatalf("expected *DiagnosticEvent, got %T", err)
	}
	if ev.Value != "***" {
		t.Errorf("Value = %q; want %q (raw value must be masked)", ev.Value, "***")
	}
	if mock.last == nil || mock.last.Value != "***" {
		t.Errorf("emitted event Value = %q; want %q", mock.last.Value, "***")
	}
}

// TestMaskRule_Pass ensures a passing masked field emits nothing.
func TestMaskRule_Pass(t *testing.T) {
	mock := &mockEmitter{}
	SetEmitter(mock)
	defer ResetEmitter()

	s := maskedStringForm{Secret: "supersecret"} // len=11 >= min=5 → pass
	if err := ValidateStruct(context.Background(), s); err != nil {
		t.Errorf("expected nil for valid masked field, got %v", err)
	}
	if mock.calls.Load() != 0 {
		t.Errorf("Emit called %d times; want 0 on happy path", mock.calls.Load())
	}
}

// ensure strings import is used
var _ = strings.TrimSpace
