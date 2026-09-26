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
| `ifc`               | The IFC4 writer: a file format library which imports nothing of this module. |
| `gml`               | The GML 3.2 writer, under the same rule. Named for the format, not for the use. |
| `.dagger`           | The pipeline: the root Dagger module CI calls. A Go module of its own, so nothing in it is part of the library. |

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
  surface justifies it, not in advance. `ifc` and `gml` are what a real boundary looks
  like: each is a
  different product with its own specification, its own closed vocabulary and its own
  fixtures, and the test of that is whether it could move to a repository of its own with
  no edit to its source. A package like them imports nothing of this module — enforced by a
  test rather than intended — and the arrow points one way, from the engine to it. Each is
  named for the format it writes rather than for what it is used for: a package named `gis`
  would accumulate whatever the next story needed, and one named for a format has an
  obvious edge.
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

## Continuous integration

CI and release are not hand-rolled in YAML. They call
[`z5labs/devex/daggerverse/z5labs`](https://github.com/z5labs/devex/tree/main/daggerverse/z5labs),
the Dagger module that implements the Z5Labs standard pipeline, through this repository's
own root Dagger module — `dagger.json` and `.dagger/`, the shape
[`z5labs/avroc`](https://github.com/z5labs/avroc) uses. dfcad ships `cmd/dfcad`, so it
builds an application off the module's Go chain rather than stopping at its checks.

A workflow step is a thin wrapper around one Dagger call against the root module:

```sh
dagger call ci                     # fmt, vet, golangci-lint, go test -race
dagger call version-scheme         # the version and publish rules, over literal cases
dagger call publish --publish-on=… # every platform's image; pushed only where the ref says
dagger call binary export --path=./dfcad
dagger call image export-image --name=dfcad:gate  # the same image, loaded into the local Docker
```

What follows from that:

- **The z5labs module owns the check stages and the image.** `Ci` is the Go chain's own
  `Ci`, and the images, their SBOMs, signatures and provenance are its `App` and `Publish`.
  Do not reimplement any of those as workflow steps or as functions in `.dagger/` — a step
  that duplicates a module stage is a second definition of the standard, and the two will
  drift.
- **The root module adds only what the standard leaves to its caller:** which version a
  build is, and whether it publishes (`.dagger/version.go`). A tag at `HEAD` is the version
  verbatim; anything else is `<short-sha>-<commit-time>`. A build publishes when a ref at
  `HEAD` matches `--publish-on`, which is why publishing is by ref and not by an `if:` on
  the job.
- **The z5labs module is pinned by commit in `dagger.json`,** and the CLI is installed at
  the `engineVersion` beside it rather than at a number typed into a workflow. An unpinned
  module is a pipeline that changes without a commit here; it broke every pull request
  twice, once when `GoApp` was replaced by the Go chain and once when the module moved to
  an engine the pinned CLI could not load. Moving the pin is a pull request of its own.
- **The source must be a git working tree.** The version, the publish decision and the
  stamp are all read from `HEAD`, so a checkout with `fetch-depth: 0` and the `.git`
  directory intact is required. The root module binds `.git` apart from the source, so the
  check stages keep their cache across commits.
- **Every function runs locally exactly as CI runs it.** `dagger call binary` is the stamped
  binary out of the same image `publish` pushes, so a change to the pipeline is testable
  without a push.
- **`.dagger/internal` and `.dagger/dagger.gen.go` are generated and committed.** Regenerate
  them with `dagger develop` after moving the pin or changing a function's signature.
- **Repo-specific verification stays in this repo,** but as its own job — the golden
  regeneration check (`go test . -update` and a clean `git diff -- testdata`), and
  anything that runs the `dfcad` binary against the fixture model. The z5labs module has
  no hook for project commands, and the standard is not the place to put them.
- **A GDAL read of the map export drops a `.gfs` beside the file it read.** A job — or a
  consumer's pipeline — which opens `model.gml` with `ogrinfo`, `ogr2ogr` or anything else
  on GDAL makes it infer the document's schema and cache that inference as `model.gfs` next
  to the document; every later read prefers the sidecar over the document. Two things follow.
  It is a build output and not a source, so `*.gfs` is gitignored and a workspace showing one
  is not dirty — a step asserting a clean tree must not read it as a change. And a stale one
  describes the document that produced it rather than the one beside it, so a job which
  re-exports and re-reads deletes it first. The `dfcad` side of this is unaffected: the
  sidecar is GDAL's, nothing in this repository writes or reads it, and it is not part of the
  artefact the digest keys.
- **A gap in the module is fixed in `z5labs/devex`,** not worked around here. If a
  story needs something the module does not expose — build-time `ldflags`, release
  assets attached to a GitHub release, an SBOM — the change belongs upstream and the
  story says so rather than growing a bespoke workflow beside the standard one.
