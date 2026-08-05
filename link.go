// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// observedInChild is the child every entity form writes a link to an
// observation file with, per specification section 6.10.
//
// It is named here rather than in one of the two loaders because both of them
// read it: a corner and the room whose floor level was taken the same afternoon
// write the same relation, and it is one relation written on two families
// rather than two which happen to look alike.
const observedInChild = "observed-in"

// ObservationLink is one link from an entity to an observation file, per
// specification section 6.10.
//
// It is a path and not an id, which makes it the one reference of the entity
// format that names something outside the model's own id space. That is the
// point of it: an observation file holds thousands of records, they are bulk
// rather than vocabulary, and a link which named them one at a time would put
// the bulk back into the entity file it was moved out of.
//
// The link is to the file and never into it. Which record backs which claim is
// a claim's provenance, in the entity format, and is a different question with
// a different answer.
type ObservationLink struct {
	// Path is the path as it was written: relative to the model root, with
	// forward slashes, ending in [ObservationExtension].
	//
	// It is kept as written rather than as resolved so that what a diagnostic
	// quotes is what the author typed, and so that two clones of one repository
	// read the same model whatever they were checked out into.
	Path string

	// Span is where the path was written, which is what a diagnostic about the
	// link points at.
	Span Span
}

// cloneLinks copies a slice of links, so that what a caller is handed is not
// the entity's own.
func cloneLinks(links []ObservationLink) []ObservationLink {
	if len(links) == 0 {
		return nil
	}
	return slices.Clone(links)
}

// observedIn reads the `observed-in` children of one entity form.
//
// Every link is read whatever was wrong with the one before it, and a path
// written twice is held once: a link is a reference and not a count, so naming
// a file twice says exactly what naming it once says.
func (r *reader) observedIn(form *Node) []ObservationLink {
	var links []ObservationLink

	for _, child := range childForms(form, observedInChild) {
		arg, ok := argument(child, 0)
		if !ok {
			continue
		}

		written, ok := r.text(arg, "a string holding a path to an observation file")
		if !ok {
			continue
		}

		if slices.ContainsFunc(links, func(l ObservationLink) bool { return l.Path == written }) {
			continue
		}

		links = append(links, ObservationLink{Path: written, Span: arg.Span})
	}

	return links
}

// resolveObservationLinks checks the observation files a model's entities link
// to, without reading one of them.
//
// What it answers is the only question about a link which can be answered
// before somebody asks for the records: is the file there. A path which leaves
// the model root, one whose extension is not [ObservationExtension], and one
// which names nothing on disk are each a load error naming the entity which
// wrote it and the path it wrote — and each is a mistake whose diagnostic would
// otherwise arrive much later, from a query, as though the query were what was
// wrong.
//
// **Nothing here opens a file.** Existence is a stat, and a stat of a file
// holding a season of field work costs what a stat of an empty one costs, which
// is what keeps a whole-model load independent of how much surveying has been
// done ([Graph.Observations]).
func resolveObservationLinks(g *Graph) []Diagnostic {
	var diags []Diagnostic

	base := modelRoot(g.root)

	// Two entities routinely link to one file — a room and the corners of it
	// were levelled in the same afternoon — and a stat per link would ask the
	// same question of the filesystem once per entity rather than once per file.
	checked := make(map[string][]linkProblem)

	for entity := range g.entities() {
		for _, link := range entity.ObservedIn() {
			problems, seen := checked[link.Path]
			if !seen {
				problems = checkObservationLink(base, link)
				checked[link.Path] = problems
			}

			for _, problem := range problems {
				diags = append(diags, observationLinkDiagnostic(problem, entity.ID(), link, g.named(entity)))
			}
		}
	}

	return diags
}

// named is where the id of an entity was written, which is what a related
// location about that entity points at.
//
// It is the id rather than the form, for the reason [Nodes.named] gives: a span
// over the whole form quotes a dozen lines to point at one. The two families
// index their definitions separately, which is why this is a switch rather than
// a method on [Entity] — where a thing was named is the loader's bookkeeping and
// not part of what the thing is.
func (g *Graph) named(entity Entity) Span {
	switch found := entity.(type) {
	case *SemanticNode:
		return g.nodes.named(found)
	case *Vertex:
		return g.topology.namedAt(found.id, found.span)
	case *Edge:
		return g.topology.namedAt(found.id, found.span)
	case *Loop:
		return g.topology.namedAt(found.id, found.span)
	}
	return Span{}
}

// linkProblem is what is wrong with a link, as the sentence a diagnostic reads
// with.
type linkProblem struct {
	// message is that sentence, taking the path as written and then the id of
	// the entity which wrote it. Both are in every one of them: the path alone
	// leaves the reader to find which of a hundred nodes wrote it, and the
	// entity alone leaves them to guess which of its links is the one meant.
	message string

	// hint is what the diagnostic carries under it.
	hint string
}

// The three ways a link can be wrong before anything reads it.
var (
	linkOutsideModel = linkProblem{
		message: "expected a path beneath the model root, found %[1]q, which %[2]s links to",
		hint: "an observation file is part of the model and is read by a walk of it, so a link which leaves the " +
			"root names a file no clone of this repository is guaranteed to have; move the file inside the root " +
			"and link to it from there",
	}

	linkWrongExtension = linkProblem{
		message: "expected a path ending in " + ObservationExtension + ", found %[1]q, which %[2]s links to",
		hint: "observation files are " + ObservationExtension + " and are read by a walk of their own, which is " +
			"what keeps the two formats from picking each other up",
	}

	linkMissing = linkProblem{
		message: "expected the observation file %[2]s links to, found nothing at %[1]q",
		hint: "the records have not been collected yet, the file has been moved, or the path is relative to the " +
			"file it was written in rather than to the model root",
	}
)

// checkObservationLink is everything which can be said about one path without
// opening it.
//
// The three checks are asked in this order because each later one is only
// meaningful once the earlier ones hold: whether a file which is not part of
// the model is on disk says nothing worth reporting.
func checkObservationLink(base string, link ObservationLink) []linkProblem {
	clean := path.Clean(link.Path)

	switch {
	case link.Path == "", path.IsAbs(link.Path), clean == "..", strings.HasPrefix(clean, "../"):
		return []linkProblem{linkOutsideModel}
	case path.Ext(clean) != ObservationExtension:
		return []linkProblem{linkWrongExtension}
	}

	// Stat rather than open: what a load needs to know is that the file is
	// there, and opening it to find out would read the first block of a file
	// this whole arrangement exists to leave on disk.
	info, err := os.Stat(filepath.Join(base, filepath.FromSlash(clean)))
	switch {
	case err != nil, info.IsDir():
		return []linkProblem{linkMissing}
	}

	return nil
}

// observationLinkDiagnostic is one problem with one link, reported against the
// entity which wrote it.
//
// It names both. The path alone leaves the reader to find which of a hundred
// nodes wrote it, and the entity alone leaves them to guess which of its links
// is the one that is wrong.
func observationLinkDiagnostic(problem linkProblem, subject ID, link ObservationLink, named Span) Diagnostic {
	diagnostic := Diagnostic{
		Severity: SeverityError,
		Span:     link.Span,
		Message:  fmt.Sprintf(problem.message, link.Path, subject),
		Hint:     problem.hint,
	}

	if named != (Span{}) {
		diagnostic.Related = []RelatedLocation{{
			Span:    named,
			Message: fmt.Sprintf("%s is written here", subject),
		}}
	}

	return diagnostic
}

// modelRoot is the directory paths in a model are relative to.
//
// A load is asked for a directory or for a single file, and a link written in
// the second is still relative to the model rather than to the file: a path
// which meant one thing when a whole tree was loaded and another when one file
// of it was would make `dfcad check` and `dfcad check entities/site.dfc`
// disagree about whether the model is sound.
func modelRoot(root string) string {
	if root == "" {
		return "."
	}
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		return filepath.Dir(root)
	}
	return root
}

// observationStore reads the observation files a model links to, once each, and
// only when something asks for them.
//
// This is the other half of what putting observations in their own files bought.
// Keeping them out of the entity files means a load does not carry them; reading
// them lazily means a command which never asks about survey records never pays
// for one, and a command which asks twice pays once. Retrieving a node is the
// case that matters: `dfcad get` on a corner reads four small entity files and
// nothing else, whatever the size of the log behind it.
//
// A store is held by the graph it was built for and lives exactly as long as
// that graph does, which is one command invocation. Nothing is cached between
// runs and nothing is written anywhere: what is on disk is the evidence, and a
// second copy of it with its own lifetime is a second thing to be stale.
type observationStore struct {
	// root is the model root the links are resolved against.
	root string

	// registry is what the records are validated against — the frames, the
	// methods and the namespaces they name. It is not written to.
	registry *Registry

	// read is how a file's bytes are obtained. It is a field so that a test can
	// count the reads and assert that a retrieval performed none; every load
	// leaves it as [os.ReadFile].
	read func(path string) ([]byte, error)

	// mu guards everything below it. A store is read through a *Graph, which is
	// otherwise read-only and so safe to share between goroutines, and lazy
	// reading is the one thing about a graph which is not: two goroutines asking
	// about one node must not read one file twice.
	mu sync.Mutex

	// content is the bytes of each file which has been read, keyed by the path
	// as written. It is what makes a second question about a file free, and it
	// is why "once read, never re-read" is a property of the store rather than
	// of whoever happens to call it.
	content map[string][]byte

	// logs is the log each set of links parsed into, keyed by the links which
	// produced it. An entity linking to three files has one log across all
	// three, because "earlier in the log" is a question about the whole of what
	// was read and not about whichever file a record landed in.
	logs map[string]*observationSet

	// reads counts how many times read was called, which is how many files were
	// read from disk. It is what the tests assert against: "did not read it" is
	// otherwise a claim about behaviour with nothing observable behind it.
	reads int
}

// observationSet is one link set, read.
type observationSet struct {
	log   *ObservationLog
	diags []Diagnostic
}

// newObservationStore returns the store a graph reads its observations through.
func newObservationStore(root string, registry *Registry) *observationStore {
	return &observationStore{
		root:     modelRoot(root),
		registry: registry,
		read:     os.ReadFile,
		content:  make(map[string][]byte),
		logs:     make(map[string]*observationSet),
	}
}

// log is the records of one set of links, read on first demand and held
// afterwards.
//
// subject is the entity which wrote the links, and is here for the diagnostics
// rather than for the reading: a file which is not there is reported in the
// words the load reports it in, naming the thing whose evidence is missing.
func (s *observationStore) log(subject ID, links []ObservationLink) (*ObservationLog, []Diagnostic) {
	if s == nil || len(links) == 0 {
		return newObservationLog(), nil
	}

	// The files of a log are read in the lexical order of their paths, which is
	// the order a walk reads them in and so the order which makes "earlier in
	// the log" one question with one answer however the links happened to be
	// written.
	ordered := slices.SortedFunc(slices.Values(links), func(a, b ObservationLink) int {
		return strings.Compare(path.Clean(a.Path), path.Clean(b.Path))
	})
	ordered = slices.CompactFunc(ordered, func(a, b ObservationLink) bool {
		return path.Clean(a.Path) == path.Clean(b.Path)
	})

	paths := make([]string, 0, len(ordered))
	for _, link := range ordered {
		paths = append(paths, path.Clean(link.Path))
	}
	key := strings.Join(paths, "\x00")

	s.mu.Lock()
	defer s.mu.Unlock()

	if set, read := s.logs[key]; read {
		return set.log, set.diags
	}

	var (
		log   = newObservationLog()
		diags []Diagnostic
	)

	for i, link := range ordered {
		// The same three checks the load made, made again because the answer
		// may have changed since: a file which was there when the model loaded
		// may not be there now. A link the checks refuse is not read, so a path
		// which leaves the model root cannot be opened by asking a question
		// about the entity which wrote it.
		if problems := checkObservationLink(s.root, link); len(problems) > 0 {
			for _, problem := range problems {
				diags = append(diags, observationLinkDiagnostic(problem, subject, link, Span{}))
			}
			continue
		}

		// The records are reported against the file as this run can reach it,
		// which is how every other pass reports the files it read: a diagnostic
		// whose path is relative to the model root cannot be opened to quote the
		// line it is about, and a diagnostic which cannot quote its line is half
		// a diagnostic.
		reachable := filepath.Join(s.root, filepath.FromSlash(paths[i]))

		src, err := s.bytes(paths[i])
		if err != nil {
			diags = append(diags, diagnose(reachable, err))
			continue
		}

		diags = append(diags, parseObservationFile(reachable, src, log)...)
	}

	diags = append(diags, ValidateObservations(log, s.registry)...)

	s.logs[key] = &observationSet{log: log, diags: diags}

	return log, diags
}

// bytes is one file's content, read at most once for the life of the store.
//
// The bytes are held rather than dropped after parsing so that a second link
// set which shares this file is answered without going back to disk. That is
// the whole of "once read, a file is not re-read within a single command
// invocation", and it is here rather than at each call site because a rule
// enforced by whoever remembers it is not enforced.
func (s *observationStore) bytes(written string) ([]byte, error) {
	if src, read := s.content[written]; read {
		return src, nil
	}

	s.reads++

	src, err := s.read(filepath.Join(s.root, filepath.FromSlash(written)))
	if err != nil {
		// A path which could not be read is remembered as read, so that a whole
		// afternoon of failing stat calls is not what a model with a broken
		// link costs. The error itself is not cached, because it is reported
		// once per link set and each of those reports names a different set.
		s.content[written] = nil
		return nil, err
	}

	s.content[written] = src

	return src, nil
}

// Observations returns the records of the observation files entity links to,
// with whatever is wrong with them.
//
// **The files are read here and nowhere earlier.** Loading a model verifies that
// each linked file exists and stops ([resolveObservationLinks]); this is what
// opens one, and it happens because a caller asked a question the records are
// the answer to. An entity linking to nothing yields an empty log and no
// diagnostics, which is the ordinary case and is not a failure.
//
// Reading is done once per set of links for the life of the graph, so a command
// which asks about one corner four times reads the file behind it once. Nothing
// is cached beyond the graph: a second invocation reads the files again, because
// what is on disk may have changed and a cache which outlived the run would be
// answering about a model which is no longer there.
//
// The diagnostics are the observation format's own — a malformed line, a
// duplicate record identity, a retirement naming a record which is not there, a
// frame the registry does not declare — reported against the file they were
// found in. They are not part of what a load reports, and deliberately so: a
// model whose survey log has a malformed line still loads, and every command
// which does not read the records is entitled to run.
func (g *Graph) Observations(entity Entity) (*ObservationLog, []Diagnostic) {
	if g == nil || entity == nil {
		return newObservationLog(), nil
	}
	return g.observations.log(entity.ID(), entity.ObservedIn())
}

// ObservationFiles returns the paths of every observation file the model links
// to, in lexical order and with a file linked to twice named once.
//
// It is what a command reporting on a model as a whole reads — how much field
// work is behind this model, and which files hold it — and it answers that
// without opening one of them.
func (g *Graph) ObservationFiles() []string {
	if g == nil {
		return nil
	}

	var (
		paths []string
		seen  = make(map[string]bool)
	)

	for entity := range g.entities() {
		for _, link := range entity.ObservedIn() {
			clean := path.Clean(link.Path)
			if seen[clean] {
				continue
			}
			seen[clean] = true
			paths = append(paths, clean)
		}
	}

	slices.Sort(paths)

	return paths
}
