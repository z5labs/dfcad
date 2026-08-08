// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package main

import (
	"errors"
	"flag"
	"fmt"
	"io"

	"github.com/z5labs/dfcad"
)

// geometryFlagsHelp describes the two axes every geometric node has, which is
// the same pair for all three of the commands which write one.
//
// It is written once for the reason the global flags are: a frame which meant
// one thing for one command and something else for another is an axis nobody
// can rely on.
const geometryFlagsHelp = `Flags every command which writes a geometric node takes:

	--frame <id>         the coordinate frame it is expressed in; required, and
	                     a geometric node is always in exactly one
	--label "<text>"     its display text, which nothing resolves through
	--file <path>        write it here instead, overriding the routing rules; a
	                     path relative to the model root, ending in ` + dfcad.Extension + `

A geometric node declares no kind and no type, so the one criterion a routing
rule can match it on is the namespace of its id. A rule written with a kind or a
type never places one, which is what keeps the rules which file semantic nodes
from filing geometry as a side effect.
`

const addVertexUsage = `dfcad add-vertex — write a new corner.

Usage:

	dfcad add-vertex [flags] <id>

A vertex is a point which other things are written in terms of. It carries no
coordinate of its own: where it is, is a claim like any other, with the same
predicate validation, the same accuracy rules and the same resolution — which is
what makes two surveys of one corner two claims rather than a number somebody
overwrote.

The position is written in the same change as the vertex, because a vertex and
the first thing anybody knows about where it is are one statement. Leave
--predicate out for a corner which has been named and not yet surveyed, whose
position is then unknown rather than zero, and claim it later with
"dfcad add-claim".

Flags:

	--predicate <name>   the predicate its position is claimed under; the rest
	                     of the claim flags below are read only when it is given

` + geometryFlagsHelp + `
` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + claimFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const addEdgeUsage = `dfcad add-edge — write a connection between two corners.

Usage:

	dfcad add-edge [flags] <id>

An edge runs between two vertices named by id, and both are resolved before
anything reaches the disk: an id naming nothing, one naming something which is
not a vertex, and one vertex written at both ends are each a usage error naming
what was reached.

Naming the ends by id rather than by coordinate is what makes the shared-edge
case ordinary. Two rooms either side of a partition name one edge, so the second
of them is written by naming the vertices the first already has — one node with
one identity, which moves both rooms when it moves and cannot leave a sliver
between them.

Flags:

	--start <vertex-id>  the vertex it runs from; required
	--end <vertex-id>    the vertex it runs to; required
	--backed-by <id>     a semantic node which physically realises it; repeat
	                     for more than one

The order of the two ends is significant and is never sorted: an edge is
directed, and the region on the other side of it traverses it the other way.
Whether an edge is a physical boundary or a virtual one is computed from
--backed-by rather than written, so adding the wall later flips the answer with
no other edit.

` + geometryFlagsHelp + `
` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const addLoopUsage = `dfcad add-loop — write an ordered ring of edges.

Usage:

	dfcad add-loop [flags] <id>

A loop is what a semantic node references when it has an outline. The shape is
shared rather than copied, so two spaces either side of a partition reference
one edge and cannot drift apart.

Flags:

	--edge <edge-id>     an edge of the ring; repeat once per edge, in the order
	                     the loop is traversed

The order is the data. It is preserved exactly as written and is never sorted,
because it is the order in which the ring is walked. Every edge id is resolved
before anything is written; whether the ring closes is judged when the model the
change produces is loaded, against the tolerance the registry declares, and a
change which would produce a model that does not load is refused.

` + geometryFlagsHelp + `
` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + outputContractHelp + `
` + writeOutputHelp

const scaffoldLoopUsage = `dfcad scaffold-loop — write a room's corners, walls and outline in one change.

Usage:

	dfcad scaffold-loop [flags]

Typing a coordinate list for a room is easy; hand-creating every vertex, every
edge and every shared endpoint by id is not. This takes the list and writes the
vertices, the edges between them and the closed loop they form, minting the ids
as it goes.

Flags:

	--corner "<x> <y> …"  one corner, in the shape the position predicate
	                      declares; repeat once per corner, in order, and name
	                      the first corner again at the end
	--namespace <name>    the declared id namespace the new nodes are minted in;
	                      required
	--predicate <name>    the predicate a corner's position is claimed under;
	                      required
	--tolerance <name>    the declared tolerance two corners are judged to be
	                      one point by, which is also what says the list closed;
	                      required
	--no-snap             write a new vertex at every corner, even where one is
	                      already there

Ids are minted as ` + "`<namespace>:<form>-<n>`" + ` — the namespace, the tag of the form
being written, and the lowest ordinal nothing in the model already holds. It is
a name and not a schema, and nothing is inferred back out of one.

The list is authored closed: its last corner names its first again, and a list
which does not return to where it started is refused with the gap and its size.
Closing one silently would leave this unable to tell an outline somebody
finished from one they stopped typing halfway through, and the wall it invented
would appear in no diagnostic anywhere.

A corner within the tolerance of a vertex the model already holds reuses that
vertex, and every reuse is reported with the distance it moved. That is the
whole reason this command exists: a second room which shares a wall with the
first shares its two corners and the edge between them, and a duplicate vertex a
millimetre away is exactly the sliver a shared topology exists to prevent. The
edge between two reused corners is reused too, so a partition is one node named
by both rooms rather than two which can drift apart.

--no-snap writes a duplicate anyway, and says so: every corner which would have
been reused is still reported, with the vertex it landed on and how far off it
was, so a run which chose the duplicate names what it duplicated.

Two corners of one list at the same point are refused rather than folded away:
either a coordinate was typed twice or the outline doubles back, and a ring
visits each of its corners once. That holds with --no-snap too, which says to
write a vertex where one already is rather than that a ring may visit a corner
twice.

--dry-run reports every node which would be created and every snap which would
happen, which is what makes it worth running first: the ids, the reuses and the
tolerance which decided them are the whole of what is being checked.

` + geometryFlagsHelp + `
` + globalFlagsHelp + `
` + writeFlagsHelp + `
` + claimFlagsHelp + `
--value is not read: a corner's value is the corner.

` + outputContractHelp + `
` + writeOutputHelp + `
It also carries "loop", the loop which was written, "vertices", the vertex each
corner is at in corner order, "created", the vertices which were minted,
"edges", the ring in traversal order, "reused", the edges of it the model
already held, "snaps", every corner which landed on an existing vertex, and
"tolerance", the declaration which decided them.
`

// ErrMissingCorners is a scaffold with no corner list.
var ErrMissingCorners = errors.New("expected the corners of the loop, found no --corner")

// geometryAxes are the flags every geometric node is written with, as they were
// given.
type geometryAxes struct {
	frame *string
	label *string
	file  *string
}

// geometryFlags defines the axes of a geometric node on a command's flag set.
func geometryFlags(flags *flag.FlagSet) geometryAxes {
	return geometryAxes{
		frame: flags.String("frame", "", ""),
		label: flags.String("label", "", ""),
		file:  flags.String("file", "", ""),
	}
}

// scaffoldResult is the object scaffold-loop writes to stdout.
//
// It is the write result with what the scaffold decided beside it, rather than a
// shape of its own, so that a caller reading .files and .dryRun reads them the
// same way whichever command wrote them.
type scaffoldResult struct {
	envelope
	dfcad.Commit

	// Loop is the id of the loop which was written.
	Loop string `json:"loop"`

	// Vertices is the vertex each corner is at, in corner order, with the
	// closing corner left out — it is the first corner written again.
	Vertices []string `json:"vertices"`

	// Created is the vertices which were minted, in the order they were. A
	// corner which reused one is not here and is in "snaps" instead.
	Created []string `json:"created"`

	// Edges is the ring, in traversal order.
	Edges []string `json:"edges"`

	// Reused is the edges of that ring which the model already held, in the
	// order the traversal reaches them.
	Reused []string `json:"reused"`

	// Snaps is every corner which landed on a vertex the model already held, in
	// corner order.
	Snaps []snapEntry `json:"snaps"`

	// Tolerance is the declaration coincidence and closure were judged against.
	// It travels with the answer because the answer depends on it.
	Tolerance toleranceEntry `json:"tolerance"`

	// Notices is what the change had to say about the model it produced, in the
	// order the engine reported them.
	Notices []noticeEntry `json:"notices"`
}

// snapEntry is one corner which landed on a vertex the model already held.
type snapEntry struct {
	// Corner is the corner's place in the list, counted from one.
	Corner int `json:"corner"`

	// Vertex is the vertex it landed on.
	Vertex string `json:"vertex"`

	// Distance is how far it was from that vertex.
	Distance float64 `json:"distance"`

	// Unit is the unit the distance is in, which is the frame's.
	Unit string `json:"unit"`

	// Reused reports whether that vertex was used rather than a second one
	// being written at the same point. It is false exactly when snapping was
	// switched off, which is the case worth looking at.
	Reused bool `json:"reused"`
}

// toleranceEntry is the declared tolerance an answer was computed against.
type toleranceEntry struct {
	// Name is the tolerance's name in the registry.
	Name string `json:"name"`

	// Value is its magnitude.
	Value float64 `json:"value"`

	// Unit is the unit that magnitude is in.
	Unit string `json:"unit"`
}

// runAddVertex is the add-vertex command.
func runAddVertex(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	axes := geometryFlags(flags)
	predicate := flags.String("predicate", "", "")
	written := claimFlags(flags)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.VertexSpec{ID: id, Label: *axes.label, Frame: dfcad.ID(*axes.frame)}

	if *predicate != "" {
		position, err := written.spec(id, *predicate, tx.Graph().Registry())
		if err != nil {
			return usageError(cmd, err, stderr, false)
		}
		spec.Position = position
	}

	destination, err := spec.Destination(tx.Graph().Registry(), *axes.file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if err := tx.AddVertex(spec, destination.Path); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	reportRouted(cmd, globals, stderr, id, destination)

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runAddEdge is the add-edge command.
func runAddEdge(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	axes := geometryFlags(flags)
	start := flags.String("start", "", "")
	end := flags.String("end", "", "")

	backing := &repeated{}
	flags.Var(backing, "backed-by", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	// The ids on the command line are read before the model is, for the reason
	// `dfcad route` reads one there: nothing about the tree makes a malformed id
	// well formed, and reporting a load of the whole model before saying so
	// buries the one thing which is wrong.
	ends, err := identified([]string{*start, *end})
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	backed, err := identified(*backing)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.EdgeSpec{
		ID:       id,
		Label:    *axes.label,
		Frame:    dfcad.ID(*axes.frame),
		Start:    ends[0],
		End:      ends[1],
		BackedBy: backed,
	}

	destination, err := spec.Destination(tx.Graph().Registry(), *axes.file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if err := tx.AddEdge(spec, destination.Path); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	reportRouted(cmd, globals, stderr, id, destination)

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runAddLoop is the add-loop command.
func runAddLoop(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	axes := geometryFlags(flags)

	ring := &repeated{}
	flags.Var(ring, "edge", "")

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	id, exit, ok := subject(cmd, arguments, 1, stderr)
	if !ok {
		return exit
	}

	edges, err := identified(*ring)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec := dfcad.LoopSpec{ID: id, Label: *axes.label, Frame: dfcad.ID(*axes.frame), Edges: edges}

	destination, err := spec.Destination(tx.Graph().Registry(), *axes.file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	if err := tx.AddLoop(spec, destination.Path); err != nil {
		return usageError(cmd, err, stderr, false)
	}

	reportRouted(cmd, globals, stderr, id, destination)

	return commitChange(cmd, tx, globals, stdout, stderr)
}

// runScaffoldLoop is the scaffold-loop command.
func runScaffoldLoop(cmd command, args []string, _ io.Reader, stdout, stderr io.Writer) int {
	globals := &globals{}
	flags := newFlagSet(cmd, globals)

	axes := scaffoldFlags(flags)

	arguments, exit, done := parse(cmd, flags, globals, args, stderr)
	if done {
		return exit
	}

	if len(arguments) > 0 {
		return usageError(cmd, UnexpectedArgumentsError{Extra: arguments}, stderr, true)
	}

	if len(*axes.corners) == 0 {
		return usageError(cmd, ErrMissingCorners, stderr, true)
	}

	tx, exit, ok := begin(cmd, globals, stderr)
	if !ok {
		return exit
	}
	defer func() { _ = tx.Close() }()

	spec, err := axes.spec(tx.Graph().Registry())
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	built, notices, err := tx.Scaffold(spec, *axes.file)
	if err != nil {
		return usageError(cmd, err, stderr, false)
	}

	reportSnaps(cmd, built.Snaps, built.Tolerance, stderr)

	out, exit, ok := apply(cmd, tx, globals, stderr)
	if !ok {
		return exit
	}

	reportNotices(cmd, notices, stderr)

	return emitted(cmd, stdout, stderr, scaffoldResult{
		envelope:  newEnvelope(cmd.name),
		Commit:    out,
		Loop:      string(built.Loop),
		Vertices:  spelled(built.Vertices),
		Created:   spelled(built.Created),
		Edges:     spelled(built.Edges),
		Reused:    spelled(built.Reused),
		Snaps:     snapped(built.Snaps),
		Tolerance: declared(built.Tolerance),
		Notices:   noticed(notices),
	})
}

// scaffoldAxes are the flags a scaffold is described by, as they were given.
//
// They are a value of their own rather than eight variables threaded through a
// call because they are one thing: the room being laid out, its evidence and
// the two names — a predicate and a tolerance — the registry has to be asked
// about before any of it can be read.
type scaffoldAxes struct {
	geometryAxes
	claimAxes

	namespace *string
	predicate *string
	tolerance *string
	noSnap    *bool
	corners   *repeated
}

// scaffoldFlags defines the axes of a scaffold on the command's flag set.
func scaffoldFlags(flags *flag.FlagSet) scaffoldAxes {
	axes := scaffoldAxes{
		geometryAxes: geometryFlags(flags),
		claimAxes:    claimFlags(flags),
		namespace:    flags.String("namespace", "", ""),
		predicate:    flags.String("predicate", "", ""),
		tolerance:    flags.String("tolerance", "", ""),
		noSnap:       flags.Bool("no-snap", false, ""),
		corners:      &repeated{},
	}

	flags.Var(axes.corners, "corner", "")

	return axes
}

// spec is the scaffold the flags describe, read against what the registry
// declares about the position predicate.
//
// The registry is needed before a corner can be read at all: which shape a value
// takes and how many components it has are registry data, and reading `0 0 0` as
// anything is a question only the declaration answers. So a corner is parsed by
// exactly the code which parses a claim's value, and a corner of the wrong shape
// is refused in the same words.
func (axes scaffoldAxes) spec(registry *dfcad.Registry) (dfcad.ScaffoldSpec, error) {
	spec := dfcad.ScaffoldSpec{
		Namespace: *axes.namespace,
		Frame:     dfcad.ID(*axes.frame),
		Label:     *axes.label,
		Predicate: *axes.predicate,
		Tolerance: *axes.tolerance,
		Snap:      !*axes.noSnap,
	}

	provenance, err := axes.provenance(spec.Predicate)
	if err != nil {
		return spec, err
	}
	spec.Provenance = provenance

	declared, ok := registry.Predicate(spec.Predicate)
	if !ok {
		// Nothing can be read from a corner without the declaration — which
		// shape a value takes is what the declaration says — so none is read,
		// and the list is handed over unread. [dfcad.Tx.Scaffold] checks the
		// predicate before it looks at a corner, so the refusal is the engine's
		// words: a caller reads one sentence whether it came from here or from
		// a library call.
		return spec, nil
	}

	for _, written := range *axes.corners {
		value, err := dfcad.ParseValue(written, dfcad.Unit(*axes.unit), declared)
		if err != nil {
			return spec, err
		}
		spec.Corners = append(spec.Corners, dfcad.Corner{Position: value})
	}

	return spec, nil
}

// provenance is the evidence every claim a scaffold writes carries.
//
// It is the claim axes without the value, because a scaffold's values are its
// corners: one --value for a list of forty corners would have to mean one of
// them, and there is no reading of that which is not a guess.
func (axes claimAxes) provenance(predicate string) (dfcad.ClaimSpec, error) {
	return axes.written().Provenance(predicate)
}

// identified reads a list of ids written on a command line.
//
// An empty string comes back as the empty id rather than as a refusal, because a
// flag nobody wrote and a flag written empty are the same absence — which the
// spec being built reports against the axis it belongs to, naming the flag which
// is missing rather than the id which is malformed.
func identified(written []string) ([]dfcad.ID, error) {
	out := make([]dfcad.ID, 0, len(written))

	for _, one := range written {
		if one == "" {
			out = append(out, "")
			continue
		}

		id, err := dfcad.ParseID(one)
		if err != nil {
			return nil, err
		}

		out = append(out, id)
	}

	return out, nil
}

// spelled is a list of ids as the result object carries them.
//
// It is made rather than declared so that a scaffold which reused no edge
// carries an empty list rather than a null, and a caller indexing it needs no
// special case for the ordinary run.
func spelled(ids []dfcad.ID) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, string(id))
	}
	return out
}

// snapped is each snap as the result object carries it.
func snapped(snaps []dfcad.Snap) []snapEntry {
	out := make([]snapEntry, 0, len(snaps))

	for _, snap := range snaps {
		out = append(out, snapEntry{
			Corner:   snap.Corner,
			Vertex:   string(snap.Vertex),
			Distance: snap.Distance,
			Unit:     string(snap.Unit),
			Reused:   snap.Reused,
		})
	}

	return out
}

// declared is the tolerance an answer was computed against, as the result object
// carries it.
func declared(tolerance dfcad.Tolerance) toleranceEntry {
	return toleranceEntry{
		Name:  tolerance.Name,
		Value: tolerance.Value,
		Unit:  string(tolerance.Unit),
	}
}

// reportRouted says where a new node went, which every command writing one
// says the same way.
func reportRouted(cmd command, globals *globals, stderr io.Writer, id dfcad.ID, to dfcad.Destination) {
	if globals.Verbosity >= verbosityProgress {
		_, _ = fmt.Fprintf(stderr, "dfcad %s: %s -> %s\n", cmd.name, id, to.Path)
	}
}

// reportSnaps writes every corner which landed on an existing vertex to stderr.
//
// They go there on every run and in every format, as diagnostics and notices do.
// A reuse is not a rendering somebody opted into: it is the one thing about a
// scaffold which is surprising when it happens and worse when it does not, and a
// duplicate written with snapping switched off is a warning whether or not
// anybody asked to see the result.
func reportSnaps(cmd command, snaps []dfcad.Snap, tolerance dfcad.Tolerance, stderr io.Writer) {
	for _, snap := range snaps {
		if snap.Reused {
			_, _ = fmt.Fprintf(stderr,
				"dfcad %s: corner %d reuses %s, %g %s away, which is within the tolerance %s\n",
				cmd.name, snap.Corner, snap.Vertex, snap.Distance, snap.Unit, tolerance.Name,
			)
			continue
		}

		_, _ = fmt.Fprintf(stderr,
			"dfcad %s: warning: corner %d is %g %s from %s, within the tolerance %s, "+
				"and snapping is off: a second vertex is written at that point\n",
			cmd.name, snap.Corner, snap.Distance, snap.Unit, snap.Vertex, tolerance.Name,
		)
	}
}
