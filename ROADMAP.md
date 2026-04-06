# Gauzer Roadmap

## v0.1.0 (Current)
*   Drop-in struct tag replacement.
*   Native `slog.LogValuer` implementation.
*   OTel Span attribute adapter.
*   Zero-alloc happy path (~61ns).
*   Native PII masking (`mask` tag).

## v0.2.0 (Planned)
*   **OTel Metrics Integration:** Adding `Int64Counter` support to the `gauzer/otel` adapter (e.g., `gauzer.validation.errors{rule="email"}`) to enable automated paging on validation spikes.
*   **Dependency Injection:** Adding Functional Options to `oteladapter.New()` (e.g., `WithMeter()`) to support strict enterprise OTel provider isolation.
*   **Nested `dive`:** Expanding `dive` support to validate nested struct slices (e.g., `[][]User`), not just primitive slices.