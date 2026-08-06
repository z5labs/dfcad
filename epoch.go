// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"time"
)

// DerivationEpoch is the instant an artefact derived from the tree named by of
// carries, wherever the format it is written in demands a creation or a
// modification time.
//
// It is the single derivation of that instant. Every command whose product is a
// file reads the value from here and no exporter reads a clock, because a clock
// reading in an exported file defeats everything the artefact is keyed by: two
// exports of an unchanged tree stop being byte-identical, so there is nothing
// left to diff, nothing to cache and nothing a golden fixture can hold
// ([0021](docs/decisions/0021-an-export-is-a-build-output-keyed-by-its-source-digest.md)).
//
// # It is a function of the source, and the function is constant
//
// The epoch is defined as a function of the tree the artefact was derived from,
// which is what the digest names, and that is why the digest is the argument:
// nothing else may reach the value, and a call site reads as the tree deciding
// it rather than the machine the export happened to run on.
//
// The function itself is constant — every tree, including the one nothing could
// be read from, gets 1970-01-01T00:00:00Z. 0021 settled that against the two
// live alternatives. A commit time is not available to a library which reads
// files rather than a repository, and is wrong the moment a working copy has an
// uncommitted edit. A time folded out of the digest would be stable, and is
// worse: it is a fabricated value with the shape of a real one, so every tool
// downstream displays a date somebody will eventually rely on. An obviously
// wrong constant is better than a convincing lie. A receiving tool showing 1970
// prompts the question, and the answer — the provenance is the digest, carried
// through whatever mechanism the target format has for it — is the conversation
// which should happen.
//
// So the argument does not change the answer, and it is not decoration: it is
// the rule the signature states. The day a format both mandates a real
// timestamp and validates it, the response 0021 names is not a clock but an
// explicit input which enters the artefact's key, and the value moves here
// rather than into an exporter.
//
// # Where the digest is unknown
//
// A tree a file of which could not be read has the zero [Digest], and a graph
// which was never on disk has one too. Both derive an epoch, the same one:
// producing an artefact may be refused for such a tree and that refusal is a
// diagnostic, but nothing about a timestamp is entitled to fail or to panic on
// the way to reporting it.
//
// Note that the published image's org.opencontainers.image.created annotation
// is not this value. It is the commit's committer time, stamped by the release
// pipeline, which is a fact about a build rather than about a model — see
// docs/publishing.md.
func DerivationEpoch(of Digest) Epoch {
	// of is unread on purpose. See the doc comment above: the parameter states
	// that the instant is a function of the source and of nothing else, and
	// 0021 fixes that function at a constant.
	return Epoch{}
}

// Epoch is the instant an artefact carries where its target format demands a
// creation or a modification time, together with the encodings such formats
// demand it in.
//
// The renderings live here rather than in each exporter for the same reason the
// derivation does. A format's time field has a form the format decides —
// seconds since 1970 in one, a zoneless ISO 8601 string in the next, a prefixed
// digit run in a third — and an exporter writing its own is one `time.Format`
// call away from writing a clock reading instead. Adding a format to the ones
// below is a method here.
//
// The zero Epoch is the epoch: an Epoch nobody derived renders as the one which
// was derived, rather than as year one.
type Epoch struct {
	// unix is the instant, in seconds since 1970-01-01T00:00:00Z.
	//
	// It is a field rather than a constant the renderings are written around
	// because the instant is a value — 0021 names the one change which would
	// move it — and because the zero value of the field is the value every
	// artefact carries today.
	unix int64
}

// Time is the instant, in UTC.
//
// It is the rendering of last resort: a format whose encoding is not one of the
// ones below takes this and formats it, in the exporter, under a layout which
// is that format's business. Any exporter doing so is a candidate for a method
// here instead.
func (e Epoch) Time() time.Time { return time.Unix(e.unix, 0).UTC() }

// Seconds is the instant as seconds since 1970-01-01T00:00:00Z.
//
// It is what a format defining its time field as an integer count takes —
// IFC2x3's IfcTimeStamp, and the modification time of an entry in an archive
// format which stores POSIX times.
func (e Epoch) Seconds() int64 { return e.unix }

// ISO8601 is the instant as an ISO 8601 date and time in UTC, with the zone
// written as Z: 1970-01-01T00:00:00Z.
//
// It is what a format defining its time field as an RFC 3339 string takes —
// IFC4's IfcDateTime, the created field of a container image manifest, and the
// date metadata of the XML-derived formats.
func (e Epoch) ISO8601() string { return e.Time().Format(time.RFC3339) }

// STEP is the instant as the time stamp in the FILE_NAME header entity of an
// ISO 10303-21 exchange file: 1970-01-01T00:00:00.
//
// The field carries no zone designator, which is a property of the field rather
// than of the instant — what is written is the UTC instant, spelt the way part
// 21 exchange files spell one.
func (e Epoch) STEP() string { return e.Time().Format("2006-01-02T15:04:05") }

// PDF is the instant as a PDF date string, the form ISO 32000-1 section 7.9.4
// defines: D:19700101000000Z.
//
// It is what the CreationDate and ModDate entries of a document information
// dictionary take, which is the field a PDF writer fills from the clock unless
// it is told not to.
func (e Epoch) PDF() string {
	at := e.Time()
	return fmt.Sprintf(
		"D:%04d%02d%02d%02d%02d%02dZ",
		at.Year(), int(at.Month()), at.Day(), at.Hour(), at.Minute(), at.Second(),
	)
}

// String is the instant as [Epoch.ISO8601] renders it.
func (e Epoch) String() string { return e.ISO8601() }
