// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrNotADirectory is a model root which is there but is not a directory.
var ErrNotADirectory = errors.New("not a directory")

// RootError is a model root which could not be read as one, and why.
//
// It is the caller's error rather than the author's: nothing about the model was
// read, so there is nothing a diagnostic could point at.
type RootError struct {
	// Path is the root, as it was given.
	Path string

	// Err is what stopped it being read: [ErrNotADirectory], or the file
	// system's own error.
	Err error
}

// Error implements [error].
func (e RootError) Error() string {
	return fmt.Sprintf("model root %s: %v", e.Path, e.Err)
}

// Unwrap returns what stopped the root being read, so that [errors.Is] and
// [errors.As] reach it.
func (e RootError) Unwrap() error {
	return e.Err
}

// Assumption is the model a batch would produce, interpreted without being
// written.
//
// It is what a read answers over when it is told to assume a batch
// ([0030](docs/decisions/0030-a-read-may-assume-a-batch.md)): not the tree on
// disk, but the tree which would be there had the batch been applied.
type Assumption struct {
	// Graph is the model the batch would produce. Its [Graph.Digest] is the
	// digest of the tree the batch would produce — what [DigestOf] would compute
	// were the batch written — so anything derived from it is keyed by that
	// tree and not by the one that was read.
	Graph *Graph

	// Applied is what each operation did, as [Tx.Apply] reports it.
	Applied []Applied

	// Base is the digest of the tree that was read, which is what tells a caller
	// which tree the batch was assumed over.
	Base Digest

	// Sources is what the diagnostics [Assume] returned beside it were raised
	// over: the printing of every file the batch touched, over the files on
	// disk it did not. A caller rendering those diagnostics quotes from it, so
	// that each one is shown the line of the tree the batch would produce
	// rather than the line at the same position on disk.
	//
	// It is set wherever the batch produced a tree, including a tree which
	// would not load — whose diagnostics are the ones that most need it — and
	// nil where it produced none, in which case every diagnostic is about the
	// tree on disk.
	Sources SourceMap
}

// Assume reads the model beneath root, applies batch to it in memory and
// interprets the model the batch would produce. It writes nothing and locks
// nothing.
//
// It is [Begin], [Tx.Apply] and a dry-run [Tx.Commit] without the lock and
// without the diff, and it is those calls rather than a copy of them: the tree
// is read once by the same read, the operations are applied by the same
// mutations, every touched file is printed and read back as a commit prints and
// reads it, and the result is interpreted by the same passes. A batch therefore
// means the same thing assumed as applied — it is refused by both or by
// neither, in the same words — and the model which comes back is the model a
// load would read once the batch had been written.
//
// No lock is taken, so it answers beside a transaction which holds the root,
// and nothing beneath the root is created, changed or removed.
//
// Refusals come back as [Begin], [Tx.Apply] and [Tx.Commit] return them:
//
//   - A tree which does not already load returns its diagnostics, a zero
//     Assumption and a nil error.
//   - An operation the model refuses returns an [OperationError] naming which
//     it was, beside the diagnostics the tree raised as it was read — which are
//     warnings, since it loaded.
//   - A result which would not load returns the diagnostics that load raised
//     and an Assumption carrying only the Sources they were raised over, with
//     a nil error.
//
// Otherwise the diagnostics are those of the model the batch would produce, the
// warnings a load of it would raise. Refusal is [Diagnostics.HasErrors] over
// them, as it is for a commit.
//
// The error is for the caller: root is not there, cannot be read, or is not a
// directory — a [RootError] — or an operation was refused.
func Assume(root string, batch Batch) (Assumption, []Diagnostic, error) {
	if err := readableDirectory(root); err != nil {
		return Assumption{}, nil, err
	}

	tx, diags := beginUnlocked(root)
	if refused(diags) {
		return Assumption{}, diags, nil
	}

	// The transaction holds no lock, so finishing it releases nothing; it is
	// finished so that nothing holding it by mistake could go on to commit it.
	defer func() { _ = tx.finish() }()

	applied, err := tx.Apply(batch)
	if err != nil {
		return Assumption{}, diags, err
	}

	_, graph, diags := tx.prepare()
	if refused(diags) {
		return Assumption{Sources: tx.Sources()}, diags, nil
	}

	base, _ := tx.graph.Digest()

	return Assumption{Graph: graph, Applied: applied, Base: base, Sources: tx.Sources()}, diags, nil
}

// readableDirectory reports why root cannot be read as a model root, or nil
// where it can.
//
// [Begin] learns this from taking the lock, which fails on a root that is not a
// directory; a read which takes none has to ask. Listing the directory is the
// question, because a directory whose entries cannot be listed is one whose
// model cannot be read, and saying so as a diagnostic about a file would point
// at a file nobody could find.
func readableDirectory(root string) error {
	info, err := os.Stat(root)
	if err != nil {
		return RootError{Path: root, Err: err}
	}
	if !info.IsDir() {
		return RootError{Path: root, Err: ErrNotADirectory}
	}

	dir, err := os.Open(root)
	if err != nil {
		return RootError{Path: root, Err: err}
	}
	defer func() { _ = dir.Close() }()

	if _, err := dir.Readdirnames(1); err != nil && !errors.Is(err, io.EOF) {
		return RootError{Path: root, Err: err}
	}

	return nil
}
