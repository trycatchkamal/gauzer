package gauzer

import (
	"net"
	"net/url"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Rule is implemented by every validation constraint.
// Validate must NOT use reflection inside simple rules; use strict type assertions instead.
// On success it returns nil (zero-alloc happy path).
// parent is the containing struct value — nil unless the rule requires cross-field access (e.g. eqfield).
type Rule interface {
	Validate(value any, parent any) *DiagnosticEvent
}

// IntMinRule validates that an int value is >= Min.
// Pattern: strict type assertion, strconv for conversion, string concatenation for messages.
type IntMinRule struct {
	Field string
	Min   int
}

func (r IntMinRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(int)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:int",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be an integer",
		}
	}
	if val < r.Min {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "min:" + strconv.Itoa(r.Min),
			Value:      strconv.Itoa(val),
			ValueType:  "int",
			Message:    r.Field + " must be at least " + strconv.Itoa(r.Min),
		}
	}
	return nil
}

// IntMaxRule validates that an int value is <= Max.
type IntMaxRule struct {
	Field string
	Max   int
}

func (r IntMaxRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(int)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:int",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be an integer",
		}
	}
	if val > r.Max {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "max:" + strconv.Itoa(r.Max),
			Value:      strconv.Itoa(val),
			ValueType:  "int",
			Message:    r.Field + " must be at most " + strconv.Itoa(r.Max),
		}
	}
	return nil
}

// RequiredRule validates that a string value is non-empty and not whitespace-only.
type RequiredRule struct {
	Field string
}

func (r RequiredRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "required",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " is required",
		}
	}
	if len(val) == 0 || (isASCIISpace(val[0]) && strings.TrimSpace(val) == "") {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "required",
			Value:      "",
			ValueType:  "string",
			Message:    r.Field + " is required",
		}
	}
	return nil
}

// EmailRule validates that a string value looks like an email address.
// Uses a minimal structural check (no reflection, no regex package at call-time).
type EmailRule struct {
	Field string
}

func (r EmailRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "email",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a valid email",
		}
	}

	if !isValidEmail(val) {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "email",
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be a valid email",
		}
	}
	return nil
}

// isValidEmail performs a structural email check without reflection or fmt.Sprintf.
// Single-pass: finds '@', checks local part non-empty, domain has dot and min length.
func isValidEmail(s string) bool {
	n := len(s)
	if n == 0 || n > 254 {
		return false
	}
	atIdx := -1
	dotAfterAt := false
	for i := 0; i < n; i++ {
		c := s[i]
		if c == '@' {
			if atIdx != -1 {
				return false // multiple '@'
			}
			atIdx = i
		} else if c == '.' && atIdx >= 0 {
			dotAfterAt = true
		}
	}
	// Need at least one char before '@', '@' present, domain >= 3 chars, dot in domain
	return atIdx >= 1 && n-atIdx > 3 && dotAfterAt
}

// truncate returns s truncated to maxLen characters (not bytes) if necessary.
// Used for PII/log-bloat protection in DiagnosticEvent.Value.
func truncate(s string, maxLen int) string {
	// Fast path: if byte length fits, no rune conversion needed.
	if len(s) <= maxLen {
		return s
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen])
}

// ----------------------------------------------------------------------------
// StringMinLengthRule
// ----------------------------------------------------------------------------

// StringMinLengthRule validates that a string's rune length is >= Min.
type StringMinLengthRule struct {
	Field string
	Min   int
}

func (r StringMinLengthRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:string",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a string",
		}
	}
	if len([]rune(val)) < r.Min {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "min:" + strconv.Itoa(r.Min),
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be at least " + strconv.Itoa(r.Min) + " characters",
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// StringMaxLengthRule
// ----------------------------------------------------------------------------

// StringMaxLengthRule validates that a string's rune length is <= Max.
type StringMaxLengthRule struct {
	Field string
	Max   int
}

func (r StringMaxLengthRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:string",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a string",
		}
	}
	if len([]rune(val)) > r.Max {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "max:" + strconv.Itoa(r.Max),
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be at most " + strconv.Itoa(r.Max) + " characters",
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// StringRequiredRule
// ----------------------------------------------------------------------------

// StringRequiredRule validates that a string is non-empty and not whitespace-only.
type StringRequiredRule struct {
	Field string
}

func (r StringRequiredRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "required",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " is required",
		}
	}
	// Empty check first; then whitespace-only check only when needed.
	// If the first byte is not whitespace the string can't be whitespace-only.
	if len(val) == 0 || (isASCIISpace(val[0]) && strings.TrimSpace(val) == "") {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "required",
			Value:      "",
			ValueType:  "string",
			Message:    r.Field + " is required",
		}
	}
	return nil
}

// isASCIISpace reports whether b is an ASCII whitespace character.
func isASCIISpace(b byte) bool {
	return b == ' ' || b == '\t' || b == '\n' || b == '\r' || b == '\v' || b == '\f'
}

// ----------------------------------------------------------------------------
// OneOfRule
// ----------------------------------------------------------------------------

// OneOfRule validates that a string value is one of the allowed values.
type OneOfRule struct {
	Field   string
	Allowed []string
}

func (r OneOfRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "oneof",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be one of the allowed values",
		}
	}
	for _, a := range r.Allowed {
		if val == a {
			return nil
		}
	}
	allowed := strings.Join(r.Allowed, "|")
	return &DiagnosticEvent{
		Field:      r.Field,
		Constraint: "oneof:" + allowed,
		Value:      truncate(val, 64),
		ValueType:  "string",
		Message:    r.Field + " must be one of: " + allowed,
	}
}

// ----------------------------------------------------------------------------
// RegexRule
// ----------------------------------------------------------------------------

// RegexRule validates that a string matches the compiled regexp.
// The regexp is compiled at construction time (NewRegexRule), never during Validate.
type RegexRule struct {
	Field   string
	Pattern string
	re      *regexp.Regexp
}

// NewRegexRule compiles the pattern once and returns a ready-to-use RegexRule.
// Returns an error if the pattern is invalid instead of panicking.
func NewRegexRule(field, pattern string) (RegexRule, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return RegexRule{}, &tagParseError{field: field, token: "regexp=" + pattern}
	}
	return RegexRule{Field: field, Pattern: pattern, re: re}, nil
}

func (r RegexRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "regexp:" + r.Pattern,
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a string",
		}
	}
	if !r.re.MatchString(val) {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "regexp:" + r.Pattern,
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must match pattern " + r.Pattern,
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// FloatMinRule
// ----------------------------------------------------------------------------

// FloatMinRule validates that a float64 value is >= Min.
type FloatMinRule struct {
	Field string
	Min   float64
}

func (r FloatMinRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(float64)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:float64",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a float64",
		}
	}
	if val < r.Min {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "min:" + strconv.FormatFloat(r.Min, 'f', -1, 64),
			Value:      strconv.FormatFloat(val, 'f', -1, 64),
			ValueType:  "float64",
			Message:    r.Field + " must be at least " + strconv.FormatFloat(r.Min, 'f', -1, 64),
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// FloatMaxRule
// ----------------------------------------------------------------------------

// FloatMaxRule validates that a float64 value is <= Max.
type FloatMaxRule struct {
	Field string
	Max   float64
}

func (r FloatMaxRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(float64)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "type:float64",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a float64",
		}
	}
	if val > r.Max {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "max:" + strconv.FormatFloat(r.Max, 'f', -1, 64),
			Value:      strconv.FormatFloat(val, 'f', -1, 64),
			ValueType:  "float64",
			Message:    r.Field + " must be at most " + strconv.FormatFloat(r.Max, 'f', -1, 64),
		}
	}
	return nil
}

// ----------------------------------------------------------------------------
// UUIDRule
// ----------------------------------------------------------------------------

// UUIDRule validates that a string is a canonical UUID v4 format.
// Zero-alloc: checks length (36) and hyphen positions only — no regex.
// Format: xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx  (8-4-4-4-12 hex digits)
type UUIDRule struct {
	Field string
}

func (r UUIDRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || !isValidUUID(val) {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "uuid",
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must be a valid UUID",
		}
	}
	return nil
}

// isValidUUID checks UUID format without allocations or regex.
// Accepts both upper and lower-case hex digits.
func isValidUUID(s string) bool {
	if len(s) != 36 {
		return false
	}
	// Hyphen positions: 8, 13, 18, 23
	if s[8] != '-' || s[13] != '-' || s[18] != '-' || s[23] != '-' {
		return false
	}
	for i := 0; i < 36; i++ {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return true
}

// ----------------------------------------------------------------------------
// IPRule
// ----------------------------------------------------------------------------

// IPRule validates that a string is a valid IPv4 or IPv6 address.
// Uses net.ParseIP — stdlib, no reflection.
type IPRule struct {
	Field string
}

func (r IPRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || net.ParseIP(val) == nil {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "ip",
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must be a valid IP address",
		}
	}
	return nil
}

// ============================================================================
// Universal Comparators (type-aware)
// ============================================================================

// toComparatorFloat converts value to float64 for ordered comparison.
// Strings use rune length; time.Time uses seconds since the time.
func toComparatorFloat(value any) (n float64, typ string, ok bool) {
	switch v := value.(type) {
	case int:
		return float64(v), "int", true
	case int8:
		return float64(v), "int8", true
	case int16:
		return float64(v), "int16", true
	case int32:
		return float64(v), "int32", true
	case int64:
		return float64(v), "int64", true
	case uint:
		return float64(v), "uint", true
	case uint8:
		return float64(v), "uint8", true
	case uint16:
		return float64(v), "uint16", true
	case uint32:
		return float64(v), "uint32", true
	case uint64:
		return float64(v), "uint64", true
	case float32:
		return float64(v), "float32", true
	case float64:
		return v, "float64", true
	case string:
		return float64(len([]rune(v))), "string", true
	case time.Time:
		return time.Since(v).Seconds(), "time.Time", true
	}
	return 0, "unknown", false
}

func comparatorErr(field, constraint, valueType string, threshold float64) *DiagnosticEvent {
	threshStr := strconv.FormatFloat(threshold, 'f', -1, 64)
	verb := map[string]string{
		"gte": ">=", "lte": "<=", "gt": ">", "lt": "<",
		"eq": "equal to", "ne": "not equal to",
	}[constraint]
	return &DiagnosticEvent{
		Field:      field,
		Constraint: constraint + ":" + threshStr,
		Value:      threshStr,
		ValueType:  valueType,
		Message:    field + " must be " + verb + " " + threshStr,
	}
}

// GteRule validates value >= Threshold (type-aware).
type GteRule struct {
	Field     string
	Threshold float64
}

func (r GteRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n < r.Threshold {
		return comparatorErr(r.Field, "gte", typ, r.Threshold)
	}
	return nil
}

// LteRule validates value <= Threshold (type-aware).
type LteRule struct {
	Field     string
	Threshold float64
}

func (r LteRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n > r.Threshold {
		return comparatorErr(r.Field, "lte", typ, r.Threshold)
	}
	return nil
}

// GtRule validates value > Threshold (type-aware).
type GtRule struct {
	Field     string
	Threshold float64
}

func (r GtRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n <= r.Threshold {
		return comparatorErr(r.Field, "gt", typ, r.Threshold)
	}
	return nil
}

// LtRule validates value < Threshold (type-aware).
type LtRule struct {
	Field     string
	Threshold float64
}

func (r LtRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n >= r.Threshold {
		return comparatorErr(r.Field, "lt", typ, r.Threshold)
	}
	return nil
}

// EqRule validates value == Threshold (type-aware; strings/slices compare length).
type EqRule struct {
	Field     string
	Threshold float64
}

func (r EqRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n != r.Threshold {
		return comparatorErr(r.Field, "eq", typ, r.Threshold)
	}
	return nil
}

// NeRule validates value != Threshold (type-aware).
type NeRule struct {
	Field     string
	Threshold float64
}

func (r NeRule) Validate(value any, _ any) *DiagnosticEvent {
	n, typ, ok := toComparatorFloat(value)
	if !ok || n == r.Threshold {
		return comparatorErr(r.Field, "ne", typ, r.Threshold)
	}
	return nil
}

// ============================================================================
// String Rules
// ============================================================================

// StringLenRule validates that a string has exactly Len runes.
type StringLenRule struct {
	Field string
	Len   int
}

func (r StringLenRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "len:" + strconv.Itoa(r.Len),
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a string",
		}
	}
	if len([]rune(val)) != r.Len {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "len:" + strconv.Itoa(r.Len),
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be exactly " + strconv.Itoa(r.Len) + " characters",
		}
	}
	return nil
}

// ContainsRule validates that a string contains Substr.
type ContainsRule struct {
	Field  string
	Substr string
}

func (r ContainsRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || !strings.Contains(val, r.Substr) {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "contains:" + r.Substr,
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must contain '" + r.Substr + "'",
		}
	}
	return nil
}

// ExcludesRule validates that a string does NOT contain Substr.
type ExcludesRule struct {
	Field  string
	Substr string
}

func (r ExcludesRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || strings.Contains(val, r.Substr) {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "excludes:" + r.Substr,
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must not contain '" + r.Substr + "'",
		}
	}
	return nil
}

// StartsWithRule validates that a string has the given prefix.
type StartsWithRule struct {
	Field  string
	Prefix string
}

func (r StartsWithRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || !strings.HasPrefix(val, r.Prefix) {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "startswith:" + r.Prefix,
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must start with '" + r.Prefix + "'",
		}
	}
	return nil
}

// EndsWithRule validates that a string has the given suffix.
type EndsWithRule struct {
	Field  string
	Suffix string
}

func (r EndsWithRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok || !strings.HasSuffix(val, r.Suffix) {
		v := "unknown"
		if ok {
			v = truncate(val, 64)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "endswith:" + r.Suffix,
			Value:      v,
			ValueType:  "string",
			Message:    r.Field + " must end with '" + r.Suffix + "'",
		}
	}
	return nil
}

// URLRule validates that a string is a fully-qualified URL (scheme + host required).
type URLRule struct {
	Field string
}

func (r URLRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "url",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a valid URL",
		}
	}
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "url",
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be a valid URL",
		}
	}
	return nil
}

// URIRule validates that a string is a valid URI (scheme required; host optional).
type URIRule struct {
	Field string
}

func (r URIRule) Validate(value any, _ any) *DiagnosticEvent {
	val, ok := value.(string)
	if !ok {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "uri",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a valid URI",
		}
	}
	u, err := url.Parse(val)
	if err != nil || u.Scheme == "" {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "uri",
			Value:      truncate(val, 64),
			ValueType:  "string",
			Message:    r.Field + " must be a valid URI",
		}
	}
	return nil
}

// ============================================================================
// Slice / Map Rules
// ============================================================================

// collectionLen returns the length of a slice, array, or map via reflection.
func collectionLen(value any) (int, bool) {
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array, reflect.Map:
		return rv.Len(), true
	}
	return 0, false
}

// CollectionMinLenRule validates that a collection has at least Min elements.
type CollectionMinLenRule struct {
	Field string
	Min   int
}

func (r CollectionMinLenRule) Validate(value any, _ any) *DiagnosticEvent {
	n, ok := collectionLen(value)
	if !ok || n < r.Min {
		got := "unknown"
		if ok {
			got = strconv.Itoa(n)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "min:" + strconv.Itoa(r.Min),
			Value:      got,
			ValueType:  "collection",
			Message:    r.Field + " must have at least " + strconv.Itoa(r.Min) + " elements",
		}
	}
	return nil
}

// CollectionMaxLenRule validates that a collection has at most Max elements.
type CollectionMaxLenRule struct {
	Field string
	Max   int
}

func (r CollectionMaxLenRule) Validate(value any, _ any) *DiagnosticEvent {
	n, ok := collectionLen(value)
	if !ok || n > r.Max {
		got := "unknown"
		if ok {
			got = strconv.Itoa(n)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "max:" + strconv.Itoa(r.Max),
			Value:      got,
			ValueType:  "collection",
			Message:    r.Field + " must have at most " + strconv.Itoa(r.Max) + " elements",
		}
	}
	return nil
}

// CollectionLenRule validates that a collection has exactly Len elements.
type CollectionLenRule struct {
	Field string
	Len   int
}

func (r CollectionLenRule) Validate(value any, _ any) *DiagnosticEvent {
	n, ok := collectionLen(value)
	if !ok || n != r.Len {
		got := "unknown"
		if ok {
			got = strconv.Itoa(n)
		}
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "len:" + strconv.Itoa(r.Len),
			Value:      got,
			ValueType:  "collection",
			Message:    r.Field + " must have exactly " + strconv.Itoa(r.Len) + " elements",
		}
	}
	return nil
}

// UniqueRule validates that a slice contains no duplicate values.
type UniqueRule struct {
	Field string
}

func (r UniqueRule) Validate(value any, _ any) *DiagnosticEvent {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "unique",
			Value:      "unknown",
			ValueType:  "unknown",
			Message:    r.Field + " must be a slice",
		}
	}
	seen := make(map[any]struct{}, rv.Len())
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		if _, exists := seen[elem]; exists {
			return &DiagnosticEvent{
				Field:      r.Field,
				Constraint: "unique",
				Value:      r.Field,
				ValueType:  "slice",
				Message:    r.Field + " must contain unique values",
			}
		}
		seen[elem] = struct{}{}
	}
	return nil
}

// ============================================================================
// DiveRule — applies sub-rules to each slice element.
// ============================================================================

// DiveRule iterates over a slice and runs SubRules against every element.
// On first failure it returns a DiagnosticEvent with Field set to "Name[i]".
type DiveRule struct {
	Field    string
	SubRules []Rule
}

func (r DiveRule) Validate(value any, _ any) *DiagnosticEvent {
	rv := reflect.ValueOf(value)
	if rv.Kind() != reflect.Slice && rv.Kind() != reflect.Array {
		return nil
	}
	for i := 0; i < rv.Len(); i++ {
		elem := rv.Index(i).Interface()
		for _, sub := range r.SubRules {
			if ev := sub.Validate(elem, nil); ev != nil {
				ev.Field = r.Field + "[" + strconv.Itoa(i) + "]"
				return ev
			}
		}
	}
	return nil
}

// ============================================================================
// EqFieldRule — cross-field equality (reflection used intentionally here only).
// ============================================================================

// EqFieldRule validates that this field's value equals the named sibling field.
// parent must be the containing struct value passed from ValidateStruct.
type EqFieldRule struct {
	Field      string
	OtherField string
}

// ============================================================================
// NeFieldRule — cross-field inequality (reflection used intentionally here only).
// ============================================================================

// NeFieldRule validates that this field's value does NOT equal the named sibling field.
// parent must be the containing struct value passed from ValidateStruct.
type NeFieldRule struct {
	Field      string
	OtherField string
}

func (r NeFieldRule) Validate(value any, parent any) *DiagnosticEvent {
	fail := func() *DiagnosticEvent {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "nefield:" + r.OtherField,
			Value:      "match",
			ValueType:  "any",
			Message:    r.Field + " must not equal " + r.OtherField,
		}
	}
	if parent == nil {
		return fail()
	}
	pv := reflect.ValueOf(parent)
	if pv.Kind() == reflect.Ptr {
		pv = pv.Elem()
	}
	if pv.Kind() != reflect.Struct {
		return fail()
	}
	other := pv.FieldByName(r.OtherField)
	if !other.IsValid() || !other.CanInterface() {
		return fail()
	}
	if reflect.DeepEqual(value, other.Interface()) {
		return fail()
	}
	return nil
}

func (r EqFieldRule) Validate(value any, parent any) *DiagnosticEvent {
	fail := func() *DiagnosticEvent {
		return &DiagnosticEvent{
			Field:      r.Field,
			Constraint: "eqfield=" + r.OtherField,
			Value:      "mismatch",
			ValueType:  "any",
			Message:    r.Field + " must equal " + r.OtherField,
		}
	}
	if parent == nil {
		return fail()
	}
	pv := reflect.ValueOf(parent)
	if pv.Kind() == reflect.Ptr {
		pv = pv.Elem()
	}
	if pv.Kind() != reflect.Struct {
		return fail()
	}
	other := pv.FieldByName(r.OtherField)
	if !other.IsValid() || !other.CanInterface() {
		return fail()
	}
	if !reflect.DeepEqual(value, other.Interface()) {
		return fail()
	}
	return nil
}
