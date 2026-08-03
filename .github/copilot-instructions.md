# dfcad conventions

## Errors

Define a custom error type instead of wrapping with `fmt.Errorf` and a `%w` verb.

Custom types let tests assert on structure — `errors.As` plus field checks — rather than
matching substrings of a message. Message text is presentation; it should be free to change
without breaking a test.

```go
// Good
type UnexpectedTokenError struct {
	Want Kind
	Got  Kind
	Pos  Position
}

func (e UnexpectedTokenError) Error() string {
	return fmt.Sprintf("unexpected token at %s: want %s, got %s", e.Pos, e.Want, e.Got)
}

// In a test
var got UnexpectedTokenError
if !errors.As(err, &got) {
	t.Fatalf("expected UnexpectedTokenError, got %T", err)
}
if got.Want != KindLParen {
	t.Errorf("want %s, got %s", KindLParen, got.Want)
}
```

```go
// Avoid
return fmt.Errorf("unexpected token at %s: want %s, got %s", pos, want, got)
```

Guidelines:

- Carry the values that made the error — positions, names, offsets, the offending input —
  as exported fields, so callers and tests can inspect them.
- When an error wraps a lower-level cause, keep the cause in a field and implement
  `Unwrap() error` so `errors.Is`/`errors.As` still reach it.
- Sentinel values (`var ErrX = errors.New(...)`) are fine when there is nothing to carry
  and callers only need `errors.Is`.
- `fmt.Errorf` without `%w`, purely for a message, is still discouraged for the same
  reason: there is nothing to assert on.
- Assert with `errors.Is`/`errors.As` in tests. Do not compare `err.Error()` strings.

When reviewing a pull request, flag any new `fmt.Errorf`-based error as a defect and
suggest the custom type.
