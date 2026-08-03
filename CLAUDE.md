# dfcad conventions

These are the implementation conventions every story in this repository follows. They are
adapted from the sibling Z5Labs Go repositories — [`z5labs/sexpr-go`](https://github.com/z5labs/sexpr-go)
in particular, which dfcad's entity format is built on. Mirror those patterns rather than
inventing new ones.

## License header

Every `.go` file — implementation, test and example alike — starts with this header,
followed by a blank line:

```go
// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT
```

## Package layout

The engine is a library first and a command second.

| Path                | Contents                                                              |
|---------------------|-----------------------------------------------------------------------|
| `.` (`package dfcad`) | The engine's public API. `doc.go` holds the package doc comment only. |
| `cmd/dfcad`         | The command line interface. `package main`, and nothing reusable.      |

Rules that hold as the tree grows:

- **The root package is the API.** A caller does `go get github.com/z5labs/dfcad` and gets
  something useful without reaching into a subdirectory.
- **`cmd/dfcad` holds no logic worth testing on its own.** Anything a test would want to
  exercise belongs in a package the library exposes; the command wires it to `os.Args`,
  the writers and an exit code. `main` itself is one line — `os.Exit(run(...))` — so that
  `run(args []string, stdout, stderr io.Writer) int` is drivable from a test without a
  subprocess.
- **Layers get their own package only once they have a boundary.** Format, model, query
  and authoring are layers of one engine, not four products. Split when the exported
  surface justifies it, not in advance.
- **`internal/` is for what must not be imported from outside.** Prefer an unexported
  identifier in an existing package over a new internal one.
- **Domain vocabulary never lands here.** Kinds and geometry forms are a closed set
  compiled in; types, predicates, frames, id namespaces and tolerances arrive as registry
  data from the consuming repository. A change that adds a domain concept to the engine
  belongs in the data repository instead.
- **Tests live beside their implementation** as `*_test.go` in the same package. Runnable
  examples go in `example_test.go`, which is the only file in `package dfcad_test` — they
  are user-facing documentation, so they must compile against the exported API exactly as
  a caller would write it.

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

## Diagnostics

An `error` is for a caller. A *diagnostic* is for whoever wrote the file — a human author
or an LLM one — and the two are not interchangeable.

Anything reporting a problem in user-authored input produces diagnostics:

- Carry a position or a span, not just a message. A diagnostic that cannot say where is a
  bug in the reporting, not a terse diagnostic.
- Say what was expected and what was found. "Invalid entity" is not actionable; "expected a
  unit after the value, found `)`" is.
- **Collect, do not stop at the first.** One pass over the input reports every independent
  problem it finds. Bailing out on the first turns fixing a file into a guessing loop.
- Ordering is deterministic — by file, then by position — so output diffs mean something.
- Every diagnostic has both a human rendering (`file:line:col`, the offending source line,
  a caret or underline) and a machine-readable form carrying the same fields. Neither is
  derived by parsing the other.

The command line interface keeps the two streams apart: structured results on stdout as a
single JSON object, diagnostics and anything else human facing on stderr. Exit codes
distinguish success, check failure, load failure and usage error. A caller must be able to
pipe stdout into `jq` without filtering prose out of it first.

## Testing

Tests are table-driven, with subtests named as behavioural phrases — what the code does,
not which function is under test.

```go
func TestResolve(t *testing.T) {
	testCases := []struct {
		name     string
		claims   []Claim
		expected Value
	}{
		{
			name:     "prefers the more accurate claim",
			claims:   []Claim{/* ... */},
			expected: Value{/* ... */},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := Resolve(testCase.claims)

			require.NoError(t, err)
			assert.Equal(t, testCase.expected, got)
		})
	}
}
```

- `github.com/stretchr/testify` is the assertion library. `require` when the test cannot
  meaningfully continue, `assert` when it can and more failures are informative.
- Name the table `testCases` and the loop variable `testCase`. Consistency here is what
  makes the tests skimmable across packages.
- A case that exercises a different *shape* of behaviour — a different signature, a
  different set of assertions — gets its own function rather than an extra flag threaded
  through the table. Tables describe variations on one behaviour, not a switch.
- Errors are asserted with `errors.Is`/`errors.As` and a field check, per the section
  above. Never on message text.
- Round-tripping is tested as a property, not only against expected literals: parse then
  print then parse must give back the same values. A test asserting an exact output string
  can pass while that output no longer reads back.

## Verification

Before opening a pull request, all of these must pass:

```sh
go build ./...
go vet ./...
go test -race ./...
gofmt -l .
```
