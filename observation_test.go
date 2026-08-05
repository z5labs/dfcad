// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observationFixture is the root of one fixture: a registry, and the
// observation files judged against it.
func observationFixture(name string) string {
	return filepath.Join("testdata", "observations", name)
}

// loadObservations loads a fixture and renders the diagnostics of the pass the
// way the command line interface would.
//
// The registry's own diagnostics are asserted empty rather than rendered: every
// fixture here declares a registry which loads clean, so what the golden beside
// it holds is what the observation pass had to say and nothing else.
func loadObservations(t *testing.T, name string) (*ObservationLog, string) {
	t.Helper()

	root := observationFixture(name)

	registry, registryDiags := LoadRegistry(root)
	require.Empty(t, registryDiags, "the fixture registry loads clean")

	log, diags := LoadObservations(root, registry)

	return log, render(t, diags)
}

// render collects diagnostics into reporting order and renders them.
func render(t *testing.T, diags []Diagnostic) string {
	t.Helper()

	var collected Diagnostics
	collected.Add(diags...)

	var rendered strings.Builder
	require.NoError(t, collected.Render(&rendered, FileSources{}))

	return rendered.String()
}

// expectedObservationDiagnostics returns the rendering held beside the fixture,
// having first rewritten it from got when -update was passed.
func expectedObservationDiagnostics(t *testing.T, name, file, got string) string {
	t.Helper()

	path := filepath.Join(observationFixture(name), file)
	if *updateGolden {
		require.NoError(t, os.WriteFile(path, []byte(got), 0o644))
	}

	want, err := os.ReadFile(path)
	require.NoError(t, err)

	return string(want)
}

func TestLoadObservations(t *testing.T) {
	testCases := []struct {
		name    string
		fixture string
	}{
		{
			name:    "reports every malformed line of one file in one pass",
			fixture: "malformed",
		},
		{
			name:    "reports a last line nothing terminated",
			fixture: "unterminated",
		},
		{
			name:    "reports a byte order mark, a byte which is no encoding and a carriage return",
			fixture: "encoding",
		},
		{
			name:    "reports what no single line can say, across the files of one log",
			fixture: "log",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, rendered := loadObservations(t, testCase.fixture)

			assert.Equal(t, expectedObservationDiagnostics(t, testCase.fixture, "diagnostics.txt", rendered), rendered)
		})
	}
}

func TestLoadObservationsAcceptsASoundLog(t *testing.T) {
	log, rendered := loadObservations(t, "valid")

	assert.Empty(t, rendered, "a sound log has nothing to report")
	assert.Equal(t, 6, log.Len(), "five observations and one retirement")
}

func TestObservationLog(t *testing.T) {
	log, _ := loadObservations(t, "valid")

	t.Run("reads every field of a record off its line", func(t *testing.T) {
		observation, ok := log.Observation("shot:2026-05-06-0001")
		require.True(t, ok)

		assert.Equal(t, ID("frame:site"), observation.Frame)
		assert.Equal(t, Point{412300.120, 5318220.455, 34.210}, observation.Coordinate)
		assert.Equal(t, ID("method:gnss-rtk"), observation.Method)
		assert.Equal(t, ID("fix:rtk-fixed"), observation.Fix)
		assert.Equal(t, 0.012, observation.HorizontalPrecision)
		assert.Equal(t, 0.021, observation.VerticalPrecision)
		assert.Equal(t, 2.0, observation.AntennaHeight)
		assert.Equal(t, ID("session:2026-05-06-am"), observation.Session)
		assert.Equal(t, 4, observation.Line())
	})

	t.Run("keeps the offset a timestamp was written in beside the instant", func(t *testing.T) {
		observation, ok := log.Observation("shot:2026-05-06-0005")
		require.True(t, ok)

		assert.True(t, observation.At.Equal(time.Date(2026, 5, 6, 11, 44, 51, 0, time.UTC)),
			"the instant is the same one 13:44:51+02:00 names")
		assert.Equal(t, "2026-05-06T13:44:51+02:00", observation.AtWritten,
			"the offset the author was working in is evidence and is not normalised away")
	})

	t.Run("resolves to every observation no retirement names", func(t *testing.T) {
		var current []ID
		for observation := range log.Current() {
			current = append(current, observation.ID)
		}

		assert.Equal(t, []ID{
			"shot:2026-05-06-0001",
			"shot:2026-05-06-0002",
			"shot:2026-05-06-0004",
			"shot:2026-05-06-0005",
		}, current)
	})

	t.Run("keeps a retired record readable where it was written", func(t *testing.T) {
		retired, ok := log.Observation("shot:2026-05-06-0003")
		require.True(t, ok, "a retirement removes nothing")

		assert.True(t, log.Retired(retired.ID))
		assert.Equal(t, 0.240, retired.HorizontalPrecision,
			"the record still says exactly what the instrument said it said")

		retirement, ok := log.RetirementOf(retired.ID)
		require.True(t, ok)
		assert.Equal(t, ID("retirement:2026-05-06-0001"), retirement.ID)
		assert.Equal(t, "float solution beside a fixed reshot of the same corner", retirement.Reason)
		assert.Greater(t, retirement.Line(), retired.Line(), "a retirement is the later record")
	})

	t.Run("groups the records of one occupation", func(t *testing.T) {
		assert.Equal(t, []ID{"session:2026-05-06-am", "session:2026-05-06-pm"}, log.Sessions())

		var afternoon []ID
		for observation := range log.Session("session:2026-05-06-pm") {
			afternoon = append(afternoon, observation.ID)
		}

		assert.Equal(t, []ID{"shot:2026-05-06-0005"}, afternoon)
	})
}

func TestParseObservationsReadsOneFileWithoutARegistry(t *testing.T) {
	path := filepath.Join(observationFixture("valid"), "2026-05-06-site-control.obs")

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	log, diags := ParseObservations(path, bytes.NewReader(src))

	assert.Empty(t, diags, "the lexis of a file is a question a registry does not answer")
	assert.Equal(t, 6, log.Len())
}

func TestParseObservationTime(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected time.Time
	}{
		{
			name:     "reads an instant written in UTC",
			written:  "2026-05-06T09:14:22Z",
			expected: time.Date(2026, 5, 6, 9, 14, 22, 0, time.UTC),
		},
		{
			name:     "reads an instant written at an offset",
			written:  "2026-05-06T11:14:22+02:00",
			expected: time.Date(2026, 5, 6, 9, 14, 22, 0, time.UTC),
		},
		{
			name:     "reads fractional seconds at whatever resolution was written",
			written:  "2026-05-06T09:14:22.480Z",
			expected: time.Date(2026, 5, 6, 9, 14, 22, 480_000_000, time.UTC),
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			at, err := ParseObservationTime(testCase.written)

			require.NoError(t, err)
			assert.True(t, at.Equal(testCase.expected), "expected %s, got %s", testCase.expected, at)
		})
	}
}

func TestParseObservationTimeRefusesAnAmbiguousInstant(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected TimestampProblem
	}{
		{
			name:     "refuses a local time in a zone nothing names",
			written:  "2026-05-06T09:14:22",
			expected: TimestampNoOffset,
		},
		{
			name:     "refuses a local time with fractional seconds and no offset",
			written:  "2026-05-06T09:14:22.480",
			expected: TimestampNoOffset,
		},
		{
			name:     "refuses the offset RFC 3339 spells nobody knows",
			written:  "2026-05-06T09:14:22-00:00",
			expected: TimestampUnknownOffset,
		},
		{
			name:     "refuses a zone abbreviation, which denotes two offsets in one year",
			written:  "2026-05-06T09:14:22 CEST",
			expected: TimestampNotRFC3339,
		},
		{
			name:     "refuses a date with no time at all",
			written:  "2026-05-06",
			expected: TimestampNotRFC3339,
		},
		{
			name:     "refuses a spelling which is not RFC 3339",
			written:  "06/05/2026 09:14:22",
			expected: TimestampNotRFC3339,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := ParseObservationTime(testCase.written)

			var malformed MalformedTimestampError
			require.True(t, errors.As(err, &malformed), "expected a MalformedTimestampError, got %T", err)
			assert.Equal(t, testCase.expected, malformed.Reason)
			assert.Equal(t, testCase.written, malformed.Written)
		})
	}
}

func TestObservationNumberLexis(t *testing.T) {
	testCases := []struct {
		name     string
		written  string
		expected numberKind
	}{
		{name: "reads a fraction as a real", written: "34.210", expected: realNumber},
		{name: "reads an exponent as a real", written: "1.0e2", expected: realNumber},
		{name: "reads a bare exponent as a real", written: "12e-3", expected: realNumber},
		{name: "reads a signed fraction as a real", written: "-0.5", expected: realNumber},
		{name: "reads digits alone as a count", written: "100", expected: integerNumber},
		{name: "refuses a fraction with no digits after the point", written: "1.", expected: notANumber},
		{name: "refuses an exponent with no digits", written: "1.0e", expected: notANumber},
		{name: "refuses two signs", written: "--1.0", expected: notANumber},
		{name: "refuses a lexeme which begins like a number", written: "12abc", expected: notANumber},
		{name: "refuses an infinity", written: "Inf", expected: notANumber},
		{name: "refuses a hexadecimal float", written: "0x1p3", expected: notANumber},
		{name: "refuses the empty lexeme", written: "", expected: notANumber},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Equal(t, testCase.expected, classifyNumber(testCase.written))
		})
	}
}

// appendFixture reads one revision of the append-only fixture.
func appendFixture(t *testing.T, name string) ObservationSource {
	t.Helper()

	path := filepath.Join("testdata", "observations", "append", name)

	src, err := os.ReadFile(path)
	require.NoError(t, err)

	// Forward slashes because the path is printed into a golden which is
	// compared byte for byte.
	return ObservationSource{Path: filepath.ToSlash(path), Bytes: src}
}

func TestValidateAppendOnly(t *testing.T) {
	testCases := []struct {
		name    string
		head    string
		golden  string
		findsIt bool
	}{
		{
			name:   "reports the line an edit rewrote, and both revisions of it",
			head:   "edited.obs",
			golden: "edited.txt",
		},
		{
			name:   "reports the records a truncation deleted rather than retired",
			head:   "truncated.obs",
			golden: "truncated.txt",
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			diags := ValidateAppendOnly(appendFixture(t, "base.obs"), appendFixture(t, testCase.head))
			rendered := render(t, diags)

			assert.Equal(t, expectedObservationDiagnostics(t, "append", testCase.golden, rendered), rendered)
		})
	}
}

func TestValidateAppendOnlyAcceptsAnAppend(t *testing.T) {
	base := appendFixture(t, "base.obs")

	assert.Empty(t, ValidateAppendOnly(base, appendFixture(t, "appended.obs")),
		"bytes at the end are the only legal change, so they are not a finding")
	assert.Empty(t, ValidateAppendOnly(base, base),
		"a revision is an append of itself")
}

func TestValidateAppendOnlyReportsAFileWhichIsGone(t *testing.T) {
	base := appendFixture(t, "base.obs")

	diags := ValidateAppendOnly(base, ObservationSource{Path: base.Path})

	require.Len(t, diags, 1)
	assert.Equal(t, SeverityError, diags[0].Severity)
	assert.Equal(t, base.Path, diags[0].Span.Start.Path)
}

// TestAppendOnlyFindsEveryModifiedLine is the property the goldens cannot
// state: a golden says what one edit reports, and this says that *no* edit goes
// unreported.
//
// Every line of a real log is modified in turn, one at a time, and each has to
// come back as a finding at that line. A check which happened to notice the
// edits somebody thought to write a fixture for, and not the rest, would pass
// every golden in this package and let the next quiet correction through.
func TestAppendOnlyFindsEveryModifiedLine(t *testing.T) {
	base := appendFixture(t, "base.obs")
	lines := bytes.SplitAfter(base.Bytes, []byte("\n"))

	// SplitAfter leaves an empty final element for a file ending in a newline.
	lines = slices.DeleteFunc(lines, func(line []byte) bool { return len(line) == 0 })
	require.NotEmpty(t, lines)

	for line := range lines {
		edited := slices.Clone(lines)
		edited[line] = append([]byte("x"), edited[line]...)

		head := ObservationSource{Path: base.Path, Bytes: bytes.Join(edited, nil)}

		diags := ValidateAppendOnly(base, head)

		require.Len(t, diags, 1, "line %d", line+1)
		assert.Equal(t, line+1, diags[0].Span.Start.Line)
		require.Len(t, diags[0].Related, 1, "a modified line names both revisions of itself")
		assert.Equal(t, line+1, diags[0].Related[0].Span.Start.Line)
	}
}

// TestObservationsCoverEveryReportedCase walks the table of section 7 of the
// observation specification the same way the corpus test walks the entity
// forms: every case the specification says is reported has to be reported by a
// fixture in this package, so a case nobody wrote a fixture for is a failure
// rather than an omission nobody notices.
func TestObservationsCoverEveryReportedCase(t *testing.T) {
	testCases := []struct {
		name     string
		reported string
	}{
		{name: "a form tag nothing declares", reported: "expected a record form"},
		{name: "the wrong number of fields", reported: "fields obs takes"},
		{name: "an id which is not one", reported: "as an id, found"},
		{name: "a timestamp which is not one", reported: "expected a timestamp"},
		{name: "a timestamp with no offset", reported: "a timestamp states its offset from UTC"},
		{name: "a timestamp whose offset is unknown", reported: "an offset nobody knows"},
		{name: "a count where a real belongs", reported: "found the count"},
		{name: "a lexeme which is not a number", reported: "which is not a number"},
		{name: "a negative magnitude", reported: "to be zero or more"},
		{name: "an unterminated quoted string", reported: "expected a closing quote"},
		{name: "an escape the format does not know", reported: "expected an escape"},
		{name: "an empty reason", reported: "found an empty one"},
		{name: "a quoted string where one does not belong", reported: "found a quoted string"},
		{name: "a reason nobody quoted", reported: "expected a quoted reason"},
		{name: "a carriage return", reported: "found a carriage return"},
		{name: "an unterminated last line", reported: "found the end of the file"},
		{name: "a byte order mark", reported: "byte order mark"},
		{name: "a byte which begins no encoding", reported: "invalid UTF-8"},
		{name: "a duplicate record identity", reported: "which is already written"},
		{name: "a retirement naming a record nothing holds", reported: "which this log does not hold"},
		{name: "a retirement naming itself", reported: "which is this record"},
		{name: "a retirement of a record written later", reported: "which is written later"},
		{name: "a record retired twice", reported: "which is already retired"},
		{name: "a frame no registry file declares", reported: "expected a declared frame"},
		{name: "a namespace no registry file declares", reported: "expected a declared namespace"},
		{name: "a retirement earlier than the record it retires", reported: "no earlier than the record it retires"},
		{name: "a line an edit rewrote", reported: "found it modified"},
		{name: "lines a truncation removed", reported: "of them removed"},
	}

	var reports strings.Builder
	for _, fixture := range []string{"malformed", "unterminated", "encoding", "log"} {
		_, rendered := loadObservations(t, fixture)
		reports.WriteString(rendered)
	}
	for _, revision := range []string{"edited.obs", "truncated.obs"} {
		reports.WriteString(render(t, ValidateAppendOnly(appendFixture(t, "base.obs"), appendFixture(t, revision))))
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			assert.Contains(t, reports.String(), testCase.reported,
				"no fixture in this package reports %s", testCase.name)
		})
	}
}
