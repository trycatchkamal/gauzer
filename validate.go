package gauzer

import (
	"context"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
)

// ruleCache uses a copy-on-write snapshot for lock-free reads on the hot path.
// Writes (first-time type registration) go through ruleCacheMu to build a new snapshot.
// After the snapshot is stored, all reads are a single atomic.Pointer.Load + map access.
type ruleCacheMap = map[reflect.Type][]fieldDef

var (
	ruleCacheMu       sync.Mutex
	ruleCacheSnapshot atomic.Pointer[ruleCacheMap]
)

// fieldDef captures which field index maps to which rules.
type fieldDef struct {
	index        int    // reflect field index
	rules        []Rule // rules parsed from the tag
	omitempty    bool   // skip validation when field is a zero-value
	hasParentDep bool   // true if any rule needs the parent struct (e.g. eqfield)
}

// ValidateStruct validates a struct using `gauzer` struct tags.
// Reflection is used ONLY during first-time setup (cached thereafter).
// The Emitter is pulled once and injected down into pure functions.
func ValidateStruct(ctx context.Context, obj any) error {
	emitter := getEmitter() // DI: pull once per call

	rv := reflect.ValueOf(obj)
	if rv.Kind() == reflect.Ptr {
		if rv.IsNil() {
			return fmt.Errorf("gauzer: pointer is nil")
		}
		rv = rv.Elem()
	}
	if rv.Kind() != reflect.Struct {
		return fmt.Errorf("gauzer: expected struct, got %s", rv.Kind())
	}
	rt := rv.Type()

	// Lock-free fast path: load the snapshot and look up the type.
	snap := ruleCacheSnapshot.Load()
	var defs []fieldDef
	var ok bool
	if snap != nil {
		defs, ok = (*snap)[rt]
	}

	if !ok {
		var err error
		defs, err = buildAndCacheFieldDefs(rt)
		if err != nil {
			return err
		}
	}

	// Compute parent lazily: only box the struct into an interface if an eqfield
	// rule is actually present (avoids any allocation on the zero-alloc hot path).
	var parent any
	for _, def := range defs {
		value := rv.Field(def.index).Interface()
		if def.omitempty && isZeroValue(value) {
			continue
		}
		var fieldParent any
		if def.hasParentDep {
			if parent == nil {
				parent = rv.Interface()
			}
			fieldParent = parent
		}
		if errEvent := validateField(ctx, emitter, value, def.rules, fieldParent); errEvent != nil {
			return errEvent
		}
	}
	return nil
}

// isZeroValue reports whether value is the zero-value for its type.
// Used by the omitempty flag to skip validation on unset fields.
func isZeroValue(value any) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string:
		return v == ""
	case int:
		return v == 0
	case int8:
		return v == 0
	case int16:
		return v == 0
	case int32:
		return v == 0
	case int64:
		return v == 0
	case uint:
		return v == 0
	case uint8:
		return v == 0
	case uint16:
		return v == 0
	case uint32:
		return v == 0
	case uint64:
		return v == 0
	case float32:
		return v == 0
	case float64:
		return v == 0
	case bool:
		return !v
	default:
		rv := reflect.ValueOf(value)
		switch rv.Kind() {
		case reflect.Ptr, reflect.Interface, reflect.Chan, reflect.Func:
			return rv.IsNil()
		case reflect.Slice, reflect.Map:
			return rv.IsNil() || rv.Len() == 0
		}
		return false
	}
}

// buildAndCacheFieldDefs builds field defs for rt and stores them in the cache snapshot.
func buildAndCacheFieldDefs(rt reflect.Type) ([]fieldDef, error) {
	// Build outside the lock to avoid holding it during reflection.
	defs, err := buildFieldDefs(rt)
	if err != nil {
		return nil, err
	}

	ruleCacheMu.Lock()
	defer ruleCacheMu.Unlock()

	// Check again inside the lock in case another goroutine already did this.
	snap := ruleCacheSnapshot.Load()
	if snap != nil {
		if existing, ok := (*snap)[rt]; ok {
			return existing, nil
		}
	}

	// Copy-on-write: build a new map from the old snapshot plus the new entry.
	var oldSnap ruleCacheMap
	if snap != nil {
		oldSnap = *snap
	}
	newSnap := make(ruleCacheMap, len(oldSnap)+1)
	for k, v := range oldSnap {
		newSnap[k] = v
	}
	newSnap[rt] = defs
	ruleCacheSnapshot.Store(&newSnap)
	return defs, nil
}

// buildFieldDefs uses reflection to parse struct tags once and return fieldDef slice.
func buildFieldDefs(rt reflect.Type) ([]fieldDef, error) {
	var defs []fieldDef
	for i := 0; i < rt.NumField(); i++ {
		sf := rt.Field(i)
		if !sf.IsExported() {
			continue
		}
		tag := sf.Tag.Get("gauzer")
		if tag == "" || tag == "-" {
			continue
		}
		rules, omit, parentDep, err := parseTag(sf.Name, sf.Type, tag)
		if err != nil {
			return nil, err
		}
		if len(rules) > 0 || omit {
			defs = append(defs, fieldDef{
				index:        i,
				rules:        rules,
				omitempty:    omit,
				hasParentDep: parentDep,
			})
		}
	}
	return defs, nil
}

// parseTag converts a gauzer tag string into a slice of Rules.
// Returns the rules, an omitempty flag, a hasParentDep flag, and any error.
// Uses splitTagTokens to handle commas inside values (e.g. in regexp patterns).
func parseTag(field string, ft reflect.Type, tag string) (rules []Rule, omitempty bool, hasParentDep bool, err error) {
	parts := splitTagTokens(tag)

	// Locate the first "dive" separator.
	diveIdx := -1
	for i, p := range parts {
		if strings.TrimSpace(p) == "dive" {
			diveIdx = i
			break
		}
	}

	mainParts := parts
	var diveParts []string
	if diveIdx >= 0 {
		mainParts = parts[:diveIdx]
		diveParts = parts[diveIdx+1:]
	}

	ruleList := make([]Rule, 0, len(mainParts)+1)
	for _, part := range mainParts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if part == "omitempty" {
			omitempty = true
			continue
		}
		rule, rerr := buildRule(field, ft, part)
		if rerr != nil {
			err = rerr
			return
		}
		if rule != nil {
			if _, ok := rule.(EqFieldRule); ok {
				hasParentDep = true
			}
			if _, ok := rule.(NeFieldRule); ok {
				hasParentDep = true
			}
			ruleList = append(ruleList, rule)
		}
	}

	// Handle dive: build sub-rules from element type and append a DiveRule.
	if diveIdx >= 0 {
		var elemType reflect.Type
		if ft.Kind() == reflect.Slice || ft.Kind() == reflect.Array {
			elemType = ft.Elem()
		} else {
			elemType = ft
		}
		subRules := make([]Rule, 0, len(diveParts))
		for _, sp := range diveParts {
			sp = strings.TrimSpace(sp)
			if sp == "" {
				continue
			}
			subRule, rerr := buildRule(field, elemType, sp)
			if rerr != nil {
				err = rerr
				return
			}
			if subRule != nil {
				subRules = append(subRules, subRule)
			}
		}
		if len(subRules) > 0 {
			ruleList = append(ruleList, DiveRule{Field: field, SubRules: subRules})
		}
	}

	rules = ruleList
	return
}

// splitTagTokens splits a gauzer tag string on commas, but only at boundaries
// that are unambiguously the start of a new tag token.
//
// A comma is treated as a token separator only when the text that follows it
// consists of one or more lowercase ASCII letters immediately followed by '=',
// another ',', or the end of the string — the shape of every valid tag keyword.
//
// This means commas inside values (e.g. in regexp patterns like `^a,b$`) are
// preserved as part of the token value, because the text after such a comma
// does not match the tag-name pattern.
//
// Examples:
//
//	"required,min=5,regexp=^a,b$"  →  ["required", "min=5", "regexp=^a,b$"]
//	"min=3,max=50"                  →  ["min=3", "max=50"]
//	"oneof=active|draft"            →  ["oneof=active|draft"]
func splitTagTokens(tag string) []string {
	var tokens []string
	start := 0
	for i := 0; i < len(tag); i++ {
		if tag[i] == ',' && isTagNameStart(tag[i+1:]) {
			tokens = append(tokens, tag[start:i])
			start = i + 1
		}
	}
	tokens = append(tokens, tag[start:])
	return tokens
}

// isTagNameStart reports whether s begins with the pattern of a gauzer tag name:
// one or more lowercase ASCII letters immediately followed by '=', ',', or end of string.
func isTagNameStart(s string) bool {
	if len(s) == 0 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' {
			continue
		}
		return c == '=' || c == ','
	}
	// Reached end with only lowercase letters (e.g. "required", "email").
	return true
}

// buildRule maps a single tag token (e.g. "gte=18", "required") to a Rule.
// ft is the reflect.Type of the struct field, used to dispatch type-aware tags.
func buildRule(field string, ft reflect.Type, token string) (Rule, error) {
	kind := ft.Kind()

	if token == "required" {
		if kind == reflect.String {
			return StringRequiredRule{Field: field}, nil
		}
		return RequiredRule{Field: field}, nil
	}
	if token == "email" {
		return EmailRule{Field: field}, nil
	}
	if token == "uuid" {
		return UUIDRule{Field: field}, nil
	}
	if token == "ip" {
		return IPRule{Field: field}, nil
	}
	if token == "url" {
		return URLRule{Field: field}, nil
	}
	if token == "uri" {
		return URIRule{Field: field}, nil
	}
	if token == "unique" {
		return UniqueRule{Field: field}, nil
	}

	// Universal comparators — type-aware dispatch at parse time for common types;
	// GteRule/LteRule/etc. handle exotic types (time.Time, uint, …) at validate time.
	if strings.HasPrefix(token, "gte=") {
		f, err := strconv.ParseFloat(token[4:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		switch kind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return IntMinRule{Field: field, Min: int(f)}, nil
		case reflect.String:
			return StringMinLengthRule{Field: field, Min: int(f)}, nil
		case reflect.Float32, reflect.Float64:
			return FloatMinRule{Field: field, Min: f}, nil
		default:
			return GteRule{Field: field, Threshold: f}, nil
		}
	}
	if strings.HasPrefix(token, "lte=") {
		f, err := strconv.ParseFloat(token[4:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		switch kind {
		case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			return IntMaxRule{Field: field, Max: int(f)}, nil
		case reflect.String:
			return StringMaxLengthRule{Field: field, Max: int(f)}, nil
		case reflect.Float32, reflect.Float64:
			return FloatMaxRule{Field: field, Max: f}, nil
		default:
			return LteRule{Field: field, Threshold: f}, nil
		}
	}
	if strings.HasPrefix(token, "gt=") {
		f, err := strconv.ParseFloat(token[3:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		return GtRule{Field: field, Threshold: f}, nil
	}
	if strings.HasPrefix(token, "lt=") {
		f, err := strconv.ParseFloat(token[3:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		return LtRule{Field: field, Threshold: f}, nil
	}
	if strings.HasPrefix(token, "eq=") {
		f, err := strconv.ParseFloat(token[3:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		return EqRule{Field: field, Threshold: f}, nil
	}
	if strings.HasPrefix(token, "ne=") {
		f, err := strconv.ParseFloat(token[3:], 64)
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		return NeRule{Field: field, Threshold: f}, nil
	}

	// min= / max= — type-aware (string→length, slice/map→count, number→value)
	if strings.HasPrefix(token, "min=") {
		arg := token[4:]
		switch kind {
		case reflect.Slice, reflect.Array, reflect.Map:
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return CollectionMinLenRule{Field: field, Min: n}, nil
		case reflect.String:
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return StringMinLengthRule{Field: field, Min: n}, nil
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return FloatMinRule{Field: field, Min: f}, nil
		default: // int, int8, int16, int32, int64, uint…
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return IntMinRule{Field: field, Min: n}, nil
		}
	}
	if strings.HasPrefix(token, "max=") {
		arg := token[4:]
		switch kind {
		case reflect.Slice, reflect.Array, reflect.Map:
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return CollectionMaxLenRule{Field: field, Max: n}, nil
		case reflect.String:
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return StringMaxLengthRule{Field: field, Max: n}, nil
		case reflect.Float32, reflect.Float64:
			f, err := strconv.ParseFloat(arg, 64)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return FloatMaxRule{Field: field, Max: f}, nil
		default:
			n, err := strconv.Atoi(arg)
			if err != nil {
				return nil, &tagParseError{field: field, token: token}
			}
			return IntMaxRule{Field: field, Max: n}, nil
		}
	}

	// len= — exact length (string→rune count, slice/map→element count)
	if strings.HasPrefix(token, "len=") {
		n, err := strconv.Atoi(token[4:])
		if err != nil {
			return nil, &tagParseError{field: field, token: token}
		}
		switch kind {
		case reflect.Slice, reflect.Array, reflect.Map:
			return CollectionLenRule{Field: field, Len: n}, nil
		default:
			return StringLenRule{Field: field, Len: n}, nil
		}
	}

	// oneof=dog|cat
	if strings.HasPrefix(token, "oneof=") {
		options := strings.Split(token[6:], "|")
		return OneOfRule{Field: field, Allowed: options}, nil
	}

	// regexp=<pattern>
	if strings.HasPrefix(token, "regexp=") {
		pattern := token[7:]
		rule, rerr := NewRegexRule(field, pattern)
		if rerr != nil {
			return nil, rerr
		}
		return rule, nil
	}

	// contains= / excludes= / startswith= / endswith=
	if strings.HasPrefix(token, "contains=") {
		return ContainsRule{Field: field, Substr: token[9:]}, nil
	}
	if strings.HasPrefix(token, "excludes=") {
		return ExcludesRule{Field: field, Substr: token[9:]}, nil
	}
	if strings.HasPrefix(token, "startswith=") {
		return StartsWithRule{Field: field, Prefix: token[11:]}, nil
	}
	if strings.HasPrefix(token, "endswith=") {
		return EndsWithRule{Field: field, Suffix: token[9:]}, nil
	}

	// eqfield=OtherFieldName
	if strings.HasPrefix(token, "eqfield=") {
		return EqFieldRule{Field: field, OtherField: token[8:]}, nil
	}

	// nefield=OtherFieldName
	if strings.HasPrefix(token, "nefield=") {
		return NeFieldRule{Field: field, OtherField: token[8:]}, nil
	}

	// Unknown tag: silently skip to allow forward-compat
	return nil, nil
}

// validateField is a pure function; emitter is injected from the top level.
// parent is the containing struct value, non-nil only when a cross-field rule is present.
func validateField(ctx context.Context, emitter Emitter, value any, rules []Rule, parent any) *DiagnosticEvent {
	for _, rule := range rules {
		if errEvent := rule.Validate(value, parent); errEvent != nil {
			emitter.Emit(ctx, errEvent)
			return errEvent
		}
	}
	return nil
}

// tagParseError is returned when a tag value cannot be parsed.
type tagParseError struct {
	field string
	token string
}

func (e *tagParseError) Error() string {
	return "gauzer: invalid tag token '" + e.token + "' on field " + e.field
}
