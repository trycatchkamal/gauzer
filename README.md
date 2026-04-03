# Gauzer

Struct validation for Go that natively speaks `log/slog` and OpenTelemetry - so failures land in Datadog, CloudWatch, or Azure Monitor as queryable structured events, not as strings you have to parse.

```
go get github.com/trycatchkamal/gauzer
```

---

## The Problem

Most validation libraries return errors as flat strings. That works fine until you're on-call trying to answer *"how often are users failing the age check, and with what values?"* in a production system.

**What your SREs see today (go-playground/validator):**

```
# In your log aggregator, searching for validation errors:
message: "Key: 'User.Age' Error:Field validation for 'Age' failed on the 'gte' tag"
```

There is no `field`, no `constraint`, no `value` key. You write a regex parser, or you give up.

**What your SREs see with Gauzer:**

```go
if err := gauzer.ValidateStruct(ctx, req); err != nil {
    slog.Error("validation failed", "err", err)
    // done.
}
```

```json
{
  "time": "2024-01-15T10:30:00.000Z",
  "level": "ERROR",
  "msg": "validation failed",
  "err": {
    "field": "Age",
    "constraint": "gte:18",
    "value": "16",
    "type": "int"
  }
}
```

Every field is a first-class attribute. Your log aggregator can filter on `err.field = "Age"`, group by `err.constraint`, alert when `err.value` spikes - no parsing needed.

The `DiagnosticEvent` Gauzer returns implements both `error` and `slog.LogValuer`. Pass it to any structured logger and the nesting happens automatically.

---

## Zero-Friction Migration

If you're already using struct tags for validation, migration is a tag rename:

```go
// Before (go-playground/validator)
type CreateUserRequest struct {
    Username string `validate:"required,min=3,max=50"`
    Age      int    `validate:"gte=18,lte=120"`
    Email    string `validate:"required,email"`
    Role     string `validate:"oneof=admin|user|viewer"`
}

// After (Gauzer) - same constraints, different tag name
type CreateUserRequest struct {
    Username string `gauzer:"required,min=3,max=50"`
    Age      int    `gauzer:"gte=18,lte=120"`
    Email    string `gauzer:"required,email"`
    Role     string `gauzer:"oneof=admin|user|viewer"`
}
```

Call site:

```go
func (h *Handler) CreateUser(w http.ResponseWriter, r *http.Request) {
    var req CreateUserRequest
    json.NewDecoder(r.Body).Decode(&req)

    if err := gauzer.ValidateStruct(r.Context(), &req); err != nil {
        http.Error(w, err.Error(), http.StatusBadRequest)
        return
    }
    // ...
}
```

Gauzer parses and compiles your struct's rules exactly once on first use, then takes the zero-allocation hot path on every subsequent call.

---

## Performance

Benchmarks run on a 2-field struct (`Email string`, `Age int`) with all fields passing:

| Benchmark | ns/op | B/op | allocs/op |
|---|---|---|---|
| `ValidateStruct` (happy path) | ~61 | 0 | 0 |
| `IntMinRule` (happy path) | ~1.58 | 0 | 0 |

**How:** Struct tags are parsed and dispatched to concrete rule types at registration time. Rule dispatch on the hot path is a simple slice iteration with type-specific assertions. The ~61 ns/op includes strict safety guards (nil-pointer checks, unexported field skipping) to guarantee zero panics in production—a 3 ns trade-off we gladly pay for crash-proof reliability.

**On failure:** allocations are intentional. Building a `DiagnosticEvent` payload requires allocating strings. We accept that cost on the sad path to give you a safe, well-formed telemetry payload. The happy path stays at zero.

Run the benchmarks yourself:

```
go test -bench=. -benchmem ./...
```

---

## Vendor-Agnostic Telemetry

Gauzer routes validation failures through an `Emitter` interface:

```go
type Emitter interface {
    Emit(ctx context.Context, event *DiagnosticEvent)
}
```

The default backend is `log/slog` - zero configuration required. Add this to your main and you get structured JSON logs immediately:

```go
slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
```

### OpenTelemetry

To write failures directly onto the active span as attributes:

```go
import "github.com/trycatchkamal/gauzer/otel"

gauzer.SetEmitter(oteladapter.New())
```

That's the entire integration. The OTel adapter writes attributes under the `gauzer.*` namespace (`gauzer.field`, `gauzer.constraint`, `gauzer.value`, `gauzer.value_type`, `gauzer.message`) on whatever span is active in the `context.Context` you pass to `ValidateStruct`. If there is no active span the event is silently dropped - no panics.

AWS CloudWatch and Azure Monitor adapters are on the roadmap. Because the interface is just `Emit(ctx, *DiagnosticEvent)`, you can write your own in about 10 lines.

The core `github.com/trycatchkamal/gauzer` module has **zero external dependencies**. The OTel adapter lives in its own `go.mod` at `github.com/trycatchkamal/gauzer/otel` and pulls in the OTel SDK only if you opt in.

---

## PII & Security

By default, Gauzer logs the failing value to help SREs debug edge cases. However, for fields containing PII (emails, SSNs, API keys), you can use the `mask` modifier.

The field will still be validated against your rules, but if it fails, the raw value will be replaced with `***` in your logs and OTel spans.

```go
type User struct {
    Email    string `gauzer:"required,email,mask"` // Failing value becomes "***"
    Password string `gauzer:"required,min=8,mask"` // Failing value becomes "***"
    Age      int    `gauzer:"gte=18"`              // Failing value remains "16"
}
```

---

## Supported Tags (v0.1.0)

### Presence

| Tag | Description |
|---|---|
| `required` | Non-zero value required. For strings, rejects empty and whitespace-only. |
| `omitempty` | Skip all rules for this field if it is the zero value. |

### Ordering & Comparison

| Tag | Description |
|---|---|
| `min=N` | Type-aware minimum: string length, slice/map element count, or numeric value. |
| `max=N` | Type-aware maximum. |
| `gte=N` | Value >= N. |
| `lte=N` | Value <= N. |
| `gt=N` | Value > N. |
| `lt=N` | Value < N. |
| `eq=N` | Value == N. |
| `ne=N` | Value != N. |
| `len=N` | Exact length (string rune count or collection element count). |

### Format

| Tag | Description |
|---|---|
| `email` | Valid email address (structural check, no network lookup). |
| `url` | Valid URL with scheme and host. |
| `uri` | Valid URI with scheme (host optional). |
| `uuid` | Canonical UUID format (8-4-4-4-12 hex). |
| `ip` | Valid IPv4 or IPv6 address. |
| `regexp=<pattern>` | String matches the given regular expression. Pattern is compiled once at startup. |

### String Content

| Tag | Description |
|---|---|
| `oneof=a\|b\|c` | Value must be one of the pipe-separated options. |
| `contains=<s>` | String contains substring. |
| `excludes=<s>` | String does not contain substring. |
| `startswith=<s>` | String has the given prefix. |
| `endswith=<s>` | String has the given suffix. |

### Collections

| Tag | Description |
|---|---|
| `unique` | Slice contains no duplicate elements. |
| `dive` | Apply the rules that follow to each element of the slice or array. Example: `gauzer:"min=1,dive,min=3"` - slice must have at least 1 element, and each element (string) must be at least 3 characters. |

### Cross-Field

| Tag | Description |
|---|---|
| `eqfield=OtherField` | Value must equal the named sibling field. |
| `nefield=OtherField` | Value must not equal the named sibling field. |

> **v0.2.0 roadmap:** `dive` into nested structs (currently works for slices of scalars and strings), and additional cross-field rules. If a tag token is unrecognized it is silently skipped, so future tags added in minor versions will not break existing code.

---

## Contributing

Bug reports and pull requests are welcome. For significant changes, open an issue first to discuss the approach.

```
go test ./...
go test -bench=. -benchmem ./...
```

## License

MIT. See [LICENSE](LICENSE).
