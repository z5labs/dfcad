// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// LockName is the file a transaction creates in the model root to say that it
// holds it.
//
// It is not an entity file, so a walk of the model steps over it and a lock
// held while somebody reads the tree changes nothing about what they read.
const LockName = ".dfcad.lock"

// ErrLocked reports a model root another transaction already holds.
//
// The engine has no daemon and no journal, so concurrency is handled by
// refusing rather than by coordinating
// ([0015](docs/decisions/0015-the-cli-is-the-primary-write-path.md)). A caller
// which sees this has changed nothing and may simply try again.
var ErrLocked = errors.New("another transaction holds the model root")

// ErrFinished reports a transaction which has already committed.
//
// A transaction is one change. Committing it — whether it wrote, was refused or
// failed — ends it, and a caller which wants another change begins another
// transaction against the model as it now is.
var ErrFinished = errors.New("the transaction has finished")

// ErrOutsideModel reports a file which is not beneath the model root.
var ErrOutsideModel = errors.New("not beneath the model root")

// ErrNotAnEntityFile reports a file a walk of the model would not read back,
// because its extension is not [Extension].
var ErrNotAnEntityFile = errors.New("not an entity file")

// LockError reports a model root which could not be locked.
type LockError struct {
	// Path is the lock file, which is [LockName] in the model root.
	Path string

	// Err is what stopped it, and is [ErrLocked] where another transaction
	// holds the root.
	Err error
}

// Error implements the [error] interface.
func (e LockError) Error() string {
	return fmt.Sprintf("%s: %s", e.Path, cause(e.Err))
}

// Unwrap returns the underlying failure.
func (e LockError) Unwrap() error {
	return e.Err
}

// TargetError reports a file a transaction refuses to write.
//
// Both reasons are the same mistake seen from two sides: a file the model would
// not read back is a change which appears to have been made and was not. A path
// outside the root is never walked, and a path inside it whose extension is not
// [Extension] is stepped over.
type TargetError struct {
	// Path is the file, as it was asked for.
	Path string

	// Root is the model root it was judged against.
	Root string

	// Err is why it was refused: [ErrOutsideModel] or [ErrNotAnEntityFile].
	Err error
}

// Error implements the [error] interface.
//
// The root is carried as a field rather than printed, because the reasons name
// it themselves where it is the point.
func (e TargetError) Error() string {
	return fmt.Sprintf("%s: %v", e.Path, e.Err)
}

// Unwrap returns the underlying failure.
func (e TargetError) Unwrap() error {
	return e.Err
}

// UnknownFormError reports a form the transaction does not hold.
//
// A form is located by where it was written, so this is what a caller gets for
// a form built from nothing, read out of a different model, or already removed
// by an earlier mutation of the same transaction.
type UnknownFormError struct {
	// Span is where the form says it was written, which is the zero span for a
	// form which was never read from a file.
	Span Span
}

// Error implements the [error] interface.
func (e UnknownFormError) Error() string {
	if e.Span.Start.Path == "" {
		return "the transaction holds no such form"
	}
	return fmt.Sprintf("%s: the transaction holds no such form", e.Span.Start)
}

// RollbackError reports a change which failed part way through the renames and
// could not be completely undone.
//
// It is the one failure of a write which leaves the tree in a state nobody
// asked for, so it names the files rather than only saying that something went
// wrong: those files hold what the failed change wrote, and everything else was
// restored.
type RollbackError struct {
	// Err is the failure which began the rollback.
	Err error

	// Failed are the files which could not be put back, in the order they were
	// written.
	Failed []string
}

// Error implements the [error] interface.
func (e RollbackError) Error() string {
	return fmt.Sprintf("%v; and %s could not be restored", e.Err, strings.Join(e.Failed, ", "))
}

// Unwrap returns the failure which began the rollback.
func (e RollbackError) Unwrap() error {
	return e.Err
}

// Op is what a change did to one thing in the model.
type Op string

const (
	// OpCreated marks something the change wrote which the model did not hold.
	OpCreated Op = "created"

	// OpModified marks something the change rewrote in place.
	OpModified Op = "modified"

	// OpRetired marks something the change took out of the model.
	OpRetired Op = "retired"
)

// Effect is one thing a change did to the model, as opposed to what it did to a
// file.
//
// The two are not the same question and a caller asks both. Which files changed
// is what a review, a commit and a formatting check are about; what was created,
// modified or retired is what the author asked for and what an agent driving the
// command reads back to know that it landed.
type Effect struct {
	// Op is what happened to it.
	Op Op `json:"op"`

	// Tag is the form it was written as — node, vertex, edge, loop, type — so
	// that an effect says which family it is about without the reader resolving
	// the id.
	Tag string `json:"tag"`

	// ID is the thing it was about, and is empty for a form which carries no id
	// or whose id could not be read. A form which names nothing is still an
	// effect: it changed the file.
	ID ID `json:"id,omitempty"`

	// Name is the plain symbol a registry entry is declared under — a type
	// name, a predicate name — and is empty for every form which names itself
	// with an id instead.
	//
	// It is a second field rather than a widening of ID because the two are not
	// the same thing: an id is namespaced, is never reissued and resolves to a
	// node, and a registry name is none of those. A caller which read either
	// into one field would resolve a type called `site` against the id
	// `site:S-101`.
	Name string `json:"name,omitempty"`
}

// FileStatus is what a commit did to one file.
type FileStatus string

const (
	// FileCreated marks a file the model did not hold before the change.
	FileCreated FileStatus = "created"

	// FileRewritten marks an existing file replaced by its new contents.
	FileRewritten FileStatus = "rewritten"

	// FileUnchanged marks a file a mutation touched whose canonical printing
	// turned out to be exactly what was already on disk. Nothing is written for
	// one: only the files actually affected are rewritten.
	FileUnchanged FileStatus = "unchanged"
)

// Change is what a commit did to one file.
type Change struct {
	// Path is the file, as the walk reached it or as the change named it.
	Path string `json:"path"`

	// Status is what happened to the file, or, on a dry run, what would have.
	Status FileStatus `json:"status"`

	// Effects are the things the change did to the model in this file, in the
	// order the mutations were applied.
	Effects []Effect `json:"effects,omitempty"`

	// Diff is the unified diff from what was on disk to what was written, and
	// is empty where the two are the same.
	Diff string `json:"diff,omitempty"`
}

// Commit is what one transaction did.
//
// It is the shape a write command reports on stdout, so its fields carry the
// names the machine output contract documents rather than being translated on
// the way out.
type Commit struct {
	// DryRun reports whether the change was validated and described without
	// being written.
	DryRun bool `json:"dryRun"`

	// Files is one entry per file a mutation touched, in the lexical order of
	// their paths — which is the order a walk of the model reaches them, so
	// that two runs over the same change report it the same way.
	Files []Change `json:"files"`
}

// Effects returns every effect of the change, file by file.
//
// It is here because "what did this command do to the model" is the question an
// author asks, and answering it from [Commit.Files] means a nested loop over a
// grouping the author did not care about.
func (c Commit) Effects() []Effect {
	var out []Effect
	for _, file := range c.Files {
		out = append(out, file.Effects...)
	}
	return out
}

// Changed returns the files the change wrote or, on a dry run, would have
// written.
//
// It is the count a report for a person wants: "would write nothing" is what a
// dry run of a change which does nothing should say, and reporting every dry run
// that way says the change was pointless rather than that it was not made.
// [Commit.Written] is the other question — which files this run actually put on
// disk — and the two differ only under [Tx.DryRun].
func (c Commit) Changed() []string {
	var out []string
	for _, file := range c.Files {
		if file.Status != FileUnchanged {
			out = append(out, file.Path)
		}
	}
	return out
}

// Written returns the files the change actually wrote.
//
// A dry run wrote none, whatever it would have written, so it returns nothing.
// A caller which read this as "the files this run touched" would otherwise
// report a dry run as having changed the tree, which is the one thing a dry run
// promises it did not do. What it would have written is [Commit.Files], with
// the status and the diff of each.
func (c Commit) Written() []string {
	if c.DryRun {
		return nil
	}
	return c.Changed()
}

// staged is one file of the model as a transaction holds it: what is on disk,
// the tree the mutations are applied to, and what those mutations did.
type staged struct {
	// path is the file, as the walk reached it or as a mutation named it.
	path string

	// src is what is on disk, and is nil for a file the change creates.
	src []byte

	// existed reports whether the file was there when the transaction began,
	// which is what tells an empty file from an absent one.
	existed bool

	// file is the tree the mutations are applied to.
	file *File

	// touched reports whether any mutation reached this file. A file nothing
	// touched is never printed and never written, whatever state it is in.
	touched bool

	// effects are what the mutations did, in the order they were applied.
	effects []Effect
}

// Tx is one all-or-nothing change to a model.
//
// A transaction loads the whole model, holds every file of it as a tree in
// memory, applies mutations to those trees, and — at [Tx.Commit] — interprets
// the result as though it had already been written. A change which would
// produce a model that does not load is refused, with the diagnostics the load
// would have raised, and nothing reaches the disk
// ([0016](docs/decisions/0016-writes-are-all-or-nothing.md)).
//
// This is the mechanism every authoring command is built on rather than a
// command itself. What a command adds is which mutations to apply; everything
// after that — validating, printing canonically, writing atomically, rolling
// back, and reporting what changed — is the same for all of them, and is here so
// that it is the same for all of them.
//
// A transaction holds the model root for as long as it is open, so two of them
// cannot interleave. It is one change and one use: [Tx.Commit] ends it, whether
// it wrote, was refused or failed, and a caller wanting another change begins
// another transaction against the model as it now is.
//
// Nothing about a Tx is safe for concurrent use. The lock is between processes
// and between transactions, not between goroutines sharing one.
type Tx struct {
	// DryRun reports whether [Tx.Commit] performs every step of the change
	// except the writing.
	//
	// A dry run loads, mutates, validates and prints exactly as a real one
	// does, and is refused by exactly the same diagnostics; what it does not do
	// is rename anything into place. The [Commit] it returns carries the diff of
	// every file which would change, because a dry run which does not say what
	// would change has reported nothing.
	DryRun bool

	// root is the model root, which is the directory the transaction locked and
	// the directory every file it writes is beneath.
	root string

	// lock is the lock file this transaction created, and is empty once it has
	// been released.
	lock string

	// order is the cleaned path of every file the transaction holds, in the
	// order the walk reached them and then in the order mutations created them.
	order []string

	// files is those files, keyed by cleaned path.
	files map[string]*staged

	// graph is the model as the transaction found it.
	graph *Graph

	// finished reports whether Commit has run.
	finished bool
}

// Begin loads the model beneath root and locks it for one change.
//
// root names the model root, which is a directory. Every entity file beneath it
// is read and parsed once, and the result is interpreted exactly as [LoadGraph]
// would interpret it — because it is the same passes over the same trees.
//
// A tree which does not already load is refused. The diagnostics come back, no
// transaction does, and the lock is released. Writing into a model which is
// already broken would report the author's mistake and the pre-existing one
// together, leaving whoever reads the output to work out which of the two the
// command was responsible for; and a refusal which cannot be attributed is a
// refusal nobody can act on.
//
// The diagnostics come back in the order the passes found them, as
// [LoadGraph]'s do. Collecting them into a [Diagnostics] is what puts them in
// reporting order.
//
// The error is for the caller rather than for the author: the root is not there,
// is not a directory, or is held by another transaction — [ErrLocked], reached
// through [LockError]. A caller which gets one has changed nothing.
//
// The transaction which comes back must be finished, by [Tx.Commit] or by
// [Tx.Close], or the lock outlives the process which took it. Deferring
// [Tx.Close] is right in both cases: it does nothing to a transaction which has
// already committed.
func Begin(root string) (*Tx, []Diagnostic, error) {
	lock, err := acquire(root)
	if err != nil {
		return nil, nil, err
	}

	tx := &Tx{root: root, lock: lock, files: make(map[string]*staged)}

	diags := tx.read()
	if refused(diags) {
		_ = tx.Close()
		return nil, diags, nil
	}

	return tx, diags, nil
}

// acquire creates the lock file in root, which is what says the root is held.
//
// Exclusive creation is the whole of the mechanism: the file system decides
// which of two processes asking at once gets the file, and the loser is told
// the root is held rather than being left to discover it by overwriting
// somebody's change.
func acquire(root string) (string, error) {
	path := filepath.Join(root, LockName)

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o666)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return "", LockError{Path: path, Err: ErrLocked}
		}
		return "", LockError{Path: path, Err: err}
	}

	// The process which holds it is written into it so that a lock left behind
	// by something which was killed says who left it. Nothing reads it back:
	// the file existing is the lock, and a pid is for whoever has to decide
	// whether removing it is safe.
	if _, err := fmt.Fprintf(file, "%d\n", os.Getpid()); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", LockError{Path: path, Err: err}
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", LockError{Path: path, Err: err}
	}

	return path, nil
}

// read walks the model root, holding every file it reaches as bytes and as a
// tree, and interprets what it read.
//
// The bytes are kept as well as the tree because two later steps need what is
// on disk rather than what it meant: the diff of a change, and the restoration
// of a file whose sibling failed to be written.
func (tx *Tx) read() []Diagnostic {
	var (
		parsed []source
		diags  []Diagnostic
	)

	// The graph this builds is as much a reading of the tree as [LoadGraph]'s, so
	// it is digested the same way and from the same bytes. A transaction's graph
	// which could not be digested would be one nothing derived from it could
	// ever be cached, for no reason but which function had read the files.
	digest := newTreeDigest(tx.root)

	for path, err := range Walk(tx.root) {
		if err != nil {
			digest.unreadable()
			diags = append(diags, diagnose(path, err))
			continue
		}

		src, err := os.ReadFile(path)
		if err != nil {
			digest.unreadable()
			diags = append(diags, diagnose(path, err))
			continue
		}

		digest.file(path, src)

		file, err := parse(path, src)
		if err != nil {
			diags = append(diags, diagnose(path, err))
			continue
		}

		clean := filepath.Clean(path)
		tx.order = append(tx.order, clean)
		tx.files[clean] = &staged{path: path, src: src, existed: true, file: file}

		parsed = append(parsed, source{path: path, file: file})
	}

	graph, diags := loadGraph(tx.root, parsed, diags, registeredChecks)
	graph.digest = digest.digest()
	tx.graph = graph

	return diags
}

// Root returns the model root the transaction holds.
func (tx *Tx) Root() string {
	if tx == nil {
		return ""
	}
	return tx.root
}

// Graph returns the model as the transaction found it.
//
// It is what a mutation is built from: which ids are taken, what a reference
// would reach, which types the registry declares. It does not change as
// mutations are applied — a transaction interprets its trees once, at
// [Tx.Commit], because interpreting the whole model after every mutation would
// make a batch of edits cost the model size times the batch size for an answer
// only the last one needs.
func (tx *Tx) Graph() *Graph {
	if tx == nil {
		return nil
	}
	return tx.graph
}

// Form returns the top-level form the model wrote id as, and whether the model
// holds one.
//
// It is how a mutation reaches what it is about. [Tx.Graph] says what the model
// means — which ids are taken, what a reference reaches, which type a node
// declares — and this says where that was written, which is what [Tx.Replace]
// and [Tx.Remove] take. An [Entity] carries its span and not its form, because
// nothing which only reads a model needs one.
//
// Only the forms written with an id as their first argument answer here, which
// is every entity and every claim which wrote an id of its own. A form the
// transaction removed no longer answers, and one it inserted does.
func (tx *Tx) Form(id ID) (*Node, bool) {
	if tx == nil || id == "" {
		return nil, false
	}

	// The scan is over the files the transaction already holds in memory rather
	// than over an index maintained beside them: an index would have to stay
	// true through every mutation, and being wrong about where a form is, is a
	// mutation applied to the wrong one.
	for _, key := range tx.order {
		for _, node := range tx.files[key].file.Nodes {
			if subjectID(node) == id {
				return node, true
			}
		}
	}

	return nil, false
}

// Insert adds form to the file at path.
//
// A relative path is resolved against the model root; an absolute one is taken
// as it is. Either way it must name a file beneath the root whose extension is
// [Extension], because a file the walk would not read back is a change which
// appears to have been made and was not. A path naming no file the model holds
// creates one.
//
// The form is written at the end of the file, which decides nothing: canonical
// form sorts the contents of a file, so where in the tree a form is inserted has
// no bearing on where it is printed.
func (tx *Tx) Insert(path string, form *Node) error {
	if tx.finished {
		return ErrFinished
	}
	if form == nil {
		return UnknownFormError{}
	}

	file, err := tx.file(path)
	if err != nil {
		return err
	}

	file.file.Nodes = append(file.file.Nodes, form)
	file.record(OpCreated, form)

	return nil
}

// Replace swaps the form old for new, wherever old was written.
//
// old is a top-level form of a file the transaction holds — one reached through
// [Tx.Graph] and the spans its entities carry, or one an earlier [Tx.Insert] of
// the same transaction added. Anything else is an [UnknownFormError]: a
// transaction changes the model it loaded, and a form it never saw belongs to
// some other one.
func (tx *Tx) Replace(old, new *Node) error {
	if tx.finished {
		return ErrFinished
	}
	if new == nil {
		return UnknownFormError{}
	}

	file, at, err := tx.locate(old)
	if err != nil {
		return err
	}

	file.file.Nodes[at] = new
	file.record(OpModified, new)

	return nil
}

// Remove takes the form out of the file it was written in.
//
// Removing is not the same as retiring in the model's sense — a claim is
// deprecated by writing a rank on it, and stays in the file so that the record
// of what was once believed survives ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
// This is the mechanical operation the commands which do either are built from.
//
// A file whose every form is removed is written empty rather than deleted. An
// empty entity file is legal and contributes nothing, and deleting is a second
// kind of mutating step with its own failure mode, which a change spanning
// several files would then have to roll back too.
func (tx *Tx) Remove(form *Node) error {
	if tx.finished {
		return ErrFinished
	}

	file, at, err := tx.locate(form)
	if err != nil {
		return err
	}

	file.file.Nodes = slices.Delete(file.file.Nodes, at, at+1)
	file.record(OpRetired, form)

	return nil
}

// file returns the staged file at path, creating one where the model holds no
// such file.
func (tx *Tx) file(path string) (*staged, error) {
	full := path
	if !filepath.IsAbs(full) {
		full = filepath.Join(tx.root, full)
	}
	clean := filepath.Clean(full)

	if file, ok := tx.files[clean]; ok {
		return file, nil
	}

	if filepath.Ext(clean) != Extension {
		return nil, TargetError{Path: path, Root: tx.root, Err: ErrNotAnEntityFile}
	}

	if !beneath(tx.root, clean) {
		return nil, TargetError{Path: path, Root: tx.root, Err: ErrOutsideModel}
	}

	file := &staged{path: clean, file: &File{Path: clean}}
	tx.order = append(tx.order, clean)
	tx.files[clean] = file

	return file, nil
}

// beneath reports whether path is inside root.
//
// Both are made absolute before they are compared, because a model root given
// as a relative path and a file given as an absolute one are comparable only
// once they are spelled the same way. Comparing them as written would refuse a
// file which is in fact inside the root, and send whoever wrote the command
// looking for a mistake they did not make.
func beneath(root, path string) bool {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}

	absolutePath, err := filepath.Abs(path)
	if err != nil {
		return false
	}

	rel, err := filepath.Rel(absoluteRoot, absolutePath)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// locate finds the staged file holding form and where in it the form sits.
//
// Where the form says it was written is where to look first, which is what
// makes a form taken out of the loaded model a lookup rather than a search. A
// form an earlier mutation of the same transaction inserted carries the span of
// wherever it was built instead — which is nowhere, for one built in memory —
// so the fall-back is by identity across the files the transaction holds. A
// form neither finds is refused rather than written into a file chosen by
// guesswork.
func (tx *Tx) locate(form *Node) (*staged, int, error) {
	if form == nil {
		return nil, 0, UnknownFormError{}
	}

	if file, ok := tx.files[filepath.Clean(form.Span.Start.Path)]; ok {
		if at := slices.Index(file.file.Nodes, form); at >= 0 {
			return file, at, nil
		}
	}

	for _, key := range tx.order {
		file := tx.files[key]
		if at := slices.Index(file.file.Nodes, form); at >= 0 {
			return file, at, nil
		}
	}

	return nil, 0, UnknownFormError{Span: form.Span}
}

// record marks the file as touched and notes what the mutation did.
func (s *staged) record(op Op, form *Node) {
	s.touched = true

	tag, _ := formTag(form)

	effect := Effect{Op: op, Tag: tag, ID: subjectID(form)}
	if effect.ID == "" && registrySorts[tag] != "" {
		effect.Name = declaredName(form)
	}

	s.effects = append(s.effects, effect)
}

// Commit validates the change and writes it.
//
// Every file a mutation touched is printed in canonical form, and the model is
// then interpreted from those printings together with the files nothing
// touched — which is the model as it would be once written, read back exactly
// as a load would read it. A change which produces an error is refused: the
// diagnostics come back, the error is nil, and nothing at all was written. They
// are the diagnostics that load would have raised, because they came from the
// same passes over the text which would have been on disk.
//
// The diagnostics come back whether or not the change was refused. An error
// refuses it and a warning does not, and a warning about the model that was
// just written is exactly as worth reading as one about the model that was not;
// dropping it because the write succeeded would hide it from the only run that
// could have reported it. Refusal is [Diagnostics.HasErrors] over what comes
// back, which is the same test every other pass is judged by.
//
// A change which validates is written atomically. Every file's complete new
// contents go to a temporary file beside it and are flushed to durable storage
// before any of them is renamed into place, so an interruption before the
// renames leaves the tree untouched. Should a rename fail once others have
// succeeded, the files already renamed are put back and the failure is reported:
// a change spanning several files lands completely or not at all.
//
// Only the files a mutation touched are considered, and a touched file whose
// canonical printing is exactly what is already on disk is not written either.
// Everything else keeps the bytes it had, whether or not those bytes are
// canonical — a write command is not a formatter, and rewriting a file nobody
// asked about would put somebody else's reformatting in the author's diff.
//
// The commit which comes back describes what happened whether or not anything
// was written: which files changed, what the change did to the model, and the
// unified diff of each. With [Tx.DryRun] set it describes what would have
// happened and the disk is not touched.
//
// Commit ends the transaction and releases the model root, whether it wrote, was
// refused or failed. Calling it again returns [ErrFinished].
func (tx *Tx) Commit() (Commit, []Diagnostic, error) {
	if tx.finished {
		return Commit{}, nil, ErrFinished
	}
	// finish's error is a lock file which would not come off. It is dropped
	// here rather than returned, because by the time this runs the change has
	// either landed or been refused, and reporting a stuck lock as Commit's
	// error would say the change did not land when it did. The lock is still
	// visible: the next [Begin] against this root fails with [ErrLocked]
	// naming the file, which is where a caller can act on it.
	defer func() { _ = tx.finish() }()

	pending, diags := tx.prepare()
	if refused(diags) {
		return Commit{}, diags, nil
	}

	out := Commit{DryRun: tx.DryRun, Files: make([]Change, 0, len(pending))}
	for _, p := range pending {
		out.Files = append(out.Files, p.change())
	}

	if tx.DryRun {
		return out, diags, nil
	}

	if err := write(pending); err != nil {
		return Commit{}, diags, err
	}

	return out, diags, nil
}

// pending is one file on its way to disk: what is there now, what would be
// there, and how far the write got.
type pending struct {
	// staged is the file the change touched.
	*staged

	// src is its complete new contents, in canonical form.
	src []byte

	// tmp is the temporary file holding src, and is empty until it is staged.
	tmp string

	// renamed reports whether tmp has been renamed over path, which is what
	// says a rollback has something to put back.
	renamed bool
}

// changed reports whether the file's new contents differ from what is on disk.
func (p *pending) changed() bool {
	return !bytes.Equal(p.staged.src, p.src)
}

// change is how one file is reported.
func (p *pending) change() Change {
	out := Change{Path: p.path, Status: FileUnchanged, Effects: p.effects}
	if !p.changed() {
		return out
	}

	out.Status = FileRewritten
	if !p.existed {
		out.Status = FileCreated
	}

	// The bytes a file which did not exist is diffed against are none, which is
	// what a created file's diff is: every line added.
	out.Diff = unified(p.path, p.staged.src, p.src)

	return out
}

// prepare prints every touched file and interprets the model those printings
// would make, together with the files nothing touched.
//
// Printing and then reading back is what makes the refusal say what a load
// would say. A form built in memory carries no span, so diagnostics about it
// would have nowhere to point; printed and re-read, it has the position it
// would have had on disk, and the author is sent to the line they would have
// been sent to had the file been written and loaded.
func (tx *Tx) prepare() ([]*pending, []Diagnostic) {
	var (
		out    []*pending
		parsed []source
		diags  []Diagnostic
	)

	// Lexically, which is the order a walk reaches files in, so that a change
	// is reported and written the same way on every run.
	for _, key := range slices.Sorted(slices.Values(tx.order)) {
		file := tx.files[key]

		if !file.touched {
			parsed = append(parsed, source{path: file.path, file: file.file})
			continue
		}

		var printed bytes.Buffer
		if err := Print(&printed, file.file); err != nil {
			diags = append(diags, diagnose(file.path, err))
			continue
		}

		// The tree is read back rather than reused so that what is validated is
		// the text which would be on disk. A printer which lost something would
		// otherwise be validated on what it was given rather than on what it
		// wrote, which is the one thing this step exists to catch.
		read, err := parse(file.path, printed.Bytes())
		if err != nil {
			diags = append(diags, diagnose(file.path, err))
			continue
		}

		out = append(out, &pending{staged: file, src: printed.Bytes()})
		parsed = append(parsed, source{path: file.path, file: read})
	}

	_, diags = loadGraph(tx.root, parsed, diags, registeredChecks)

	return out, diags
}

// write puts every prepared file on disk, all of them or none.
//
// The two phases are the whole of the guarantee. Nothing visible changes in the
// first, so a failure there is a change which never began; the second is
// nothing but renames, each of which is atomic, so a failure there has an exact
// set of files to put back.
func write(files []*pending) error {
	for _, file := range files {
		if !file.changed() {
			continue
		}

		tmp, err := stage(file.path, file.src)
		if err != nil {
			discard(files)
			return err
		}
		file.tmp = tmp
	}

	for _, file := range files {
		if file.tmp == "" {
			continue
		}

		if err := os.Rename(file.tmp, file.path); err != nil {
			return rollback(files, WriteError{Path: file.path, Err: err})
		}
		file.renamed = true
	}

	return nil
}

// discard removes the temporary files of a change which never reached its
// renames.
func discard(files []*pending) {
	for _, file := range files {
		if file.tmp != "" {
			_ = os.Remove(file.tmp)
		}
	}
}

// rollback puts back every file a failed change had already renamed, and
// reports the failure which began it.
//
// A file which existed is restored by the same atomic replacement which
// overwrote it, so the rollback is no more interruptible than the write was. A
// file the change created is removed, because putting back what was there means
// putting back nothing.
func rollback(files []*pending, failure error) error {
	var failed []string

	for _, file := range files {
		switch {
		case !file.renamed:
			if file.tmp != "" {
				_ = os.Remove(file.tmp)
			}

		case file.existed:
			if err := replace(file.path, file.staged.src); err != nil {
				failed = append(failed, file.path)
			}

		default:
			if err := os.Remove(file.path); err != nil {
				failed = append(failed, file.path)
			}
		}
	}

	if len(failed) > 0 {
		return RollbackError{Err: failure, Failed: failed}
	}

	return failure
}

// Close ends the transaction without writing anything, releasing the model
// root.
//
// It does nothing to a transaction [Tx.Commit] has already ended, which is what
// makes deferring it right in every case: a caller defers Close and then
// commits, and the model root is released exactly once either way.
func (tx *Tx) Close() error {
	if tx == nil {
		return nil
	}
	return tx.finish()
}

// finish releases the model root and marks the transaction spent.
func (tx *Tx) finish() error {
	tx.finished = true

	if tx.lock == "" {
		return nil
	}

	// The name is cleared before the file is removed, so that a failure to
	// remove it is reported once rather than retried by every later call — and
	// so that a lock this transaction no longer believes it holds is never
	// removed out from under the transaction which took it next.
	lock := tx.lock
	tx.lock = ""

	if err := os.Remove(lock); err != nil {
		return LockError{Path: lock, Err: err}
	}

	return nil
}

// refused reports whether any diagnostic is an error, which is what makes a
// tree unloadable and a change refused.
//
// A warning is not a refusal. It marks input the engine accepts, and a write
// path which refused one would make a model somebody is part way through
// writing unwritable by the tool that is supposed to be writing it.
func refused(diags []Diagnostic) bool {
	for _, diagnostic := range diags {
		if diagnostic.Severity == SeverityError {
			return true
		}
	}
	return false
}
