// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"fmt"
	"iter"
	"slices"
)

// Point is a position in one frame: three ordered components, expressed in that
// frame's one linear unit.
//
// It carries no unit of its own, and that is not an omission. A frame declares
// exactly one linear unit
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)), so naming the
// frame a point is in names the unit it is in as well, and a point which
// carried a second one would be a point in two units.
//
// The order of the components is significant and is never sorted.
type Point [3]float64

// UndeclaredFrameError reports a frame id no registry file declares a frame of.
//
// It is an error rather than a diagnostic because it is a caller's mistake and
// not an author's: nothing in the model asked for this frame, a caller did.
type UndeclaredFrameError struct {
	// Frame is the id which was asked for.
	Frame ID
}

// Error implements the [error] interface.
func (e UndeclaredFrameError) Error() string {
	return fmt.Sprintf("no frame %s is declared", e.Frame)
}

// UnmeasuredFrameError reports a frame whose transform to its parent did not
// resolve to a transform claim, asked to transform something through it.
//
// The relationship between two frames is a measurement rather than a
// configuration constant, so a frame whose measurement is missing has no
// relationship to its parent at all — and answering with one anyway would mean
// inventing the fit nobody made.
type UnmeasuredFrameError struct {
	// Frame is the frame whose transform did not resolve.
	Frame ID

	// Parent is the frame the transform maps it into.
	Parent ID
}

// Error implements the [error] interface.
func (e UnmeasuredFrameError) Error() string {
	return fmt.Sprintf("the transform from %s to %s resolves to no transform claim", e.Frame, e.Parent)
}

// UnrelatedFramesError reports two frames whose parent chains never meet.
//
// Every frame of one model reaches one root, so this is a model with more than
// one root or with a chain which breaks before reaching it — each of them a
// load error the registry reports. It is an error here rather than a nil answer
// because "nowhere" is not a position.
type UnrelatedFramesError struct {
	// From and To are the two frames.
	From ID
	To   ID
}

// Error implements the [error] interface.
func (e UnrelatedFramesError) Error() string {
	return fmt.Sprintf("the frames %s and %s reach no common frame", e.From, e.To)
}

// FrameCycleError reports a parent chain which returns to a frame it already
// passed through.
//
// The cycle itself is a load error naming every frame in it, which the registry
// reports. This is what the walk does when a caller asks anyway: it stops at the
// frame it re-entered rather than following the chain forever.
type FrameCycleError struct {
	// Frame is the frame the walk re-entered.
	Frame ID
}

// Error implements the [error] interface.
func (e FrameCycleError) Error() string {
	return fmt.Sprintf("the parent chain returns to %s and reaches no root frame", e.Frame)
}

// UnknownUnitError reports a frame whose declared linear unit is not one the
// engine defines, asked to convert something into or out of it.
//
// Nothing is assumed in its place. A frame whose unit cannot be read is a frame
// whose coordinates have no magnitude, and converting them as though they were
// metres is the unlabelled conversion
// [0005](docs/decisions/0005-one-linear-unit-per-frame.md) exists to prevent.
type UnknownUnitError struct {
	// Frame is the frame which declared it.
	Frame ID

	// Unit is what it declared, which is empty where it declared none the
	// registry could read.
	Unit Unit
}

// Error implements the [error] interface.
func (e UnknownUnitError) Error() string {
	if e.Unit == "" {
		return fmt.Sprintf("the frame %s declares no linear unit", e.Frame)
	}
	return fmt.Sprintf("the frame %s declares %s, which is no linear unit", e.Frame, e.Unit)
}

// SingularTransformError reports a transform which maps a frame into its parent
// and cannot be run backwards.
//
// A rotation of zero determinant or a scale of zero collapses the frame onto a
// plane, a line or a point, and more than one position in the frame then has
// the same position in the parent. Coming back is not a computation which went
// wrong; it is a question with more than one answer.
type SingularTransformError struct {
	// Frame is the frame the transform is declared on.
	Frame ID

	// Parent is the frame it maps into.
	Parent ID
}

// Error implements the [error] interface.
func (e SingularTransformError) Error() string {
	return fmt.Sprintf("the transform from %s to %s cannot be inverted", e.Frame, e.Parent)
}

// Frames is the frame graph of one model: the chain each declared frame reaches
// the root through, and the claim which measures each frame against its parent.
//
// A frame is both a registry entry and a node, and this is the second half of
// it. The registry says which frames exist, which unit each is in and which
// frame each is expressed relative to; what the relationship between two of them
// actually is, is a measurement — a fit, with a residual, produced by a method
// on a date — and lives in a claim like every other measurement in the model.
// Joining the two is what this pass does, and it is why a cross-frame answer can
// say how well the relationship it used is known.
//
// **A shape lives in exactly one frame and is transformed on demand.** Storing
// one shape in two frames is two sources of truth which drift the moment the
// georeference is re-fitted, so a shape assembled from parts in two frames is a
// load error rather than something this converts between.
//
// A Frames is read-only once resolved. The zero value holds nothing, which is
// what a model declaring no frame yields, and every method below works on it.
type Frames struct {
	// registry is where the frames themselves are read from. It is not written
	// to, and it is what every question here is answered against: which frames
	// exist, which unit each declares and which frame each is expressed relative
	// to are registry data rather than anything this pass decides.
	registry *Registry

	// measured is the claim which measures each frame's transform to its parent,
	// by the id of the frame it is declared on.
	//
	// It holds the claim rather than the [Transform] inside it, because the value
	// is the least interesting half: the source, the method, the date and the
	// accuracy of the fit are what make a cross-frame answer accountable, and a
	// map of bare transforms would have thrown all four away.
	//
	// A frame whose transform did not resolve to a transform claim is absent,
	// which is what makes it unmeasured rather than measured as the identity.
	measured map[ID]*Claim

	// root is the frame which declares no parent, which is the projected
	// coordinate reference system every other frame reaches through its parents.
	// Empty where the model declares none.
	root ID
}

// Root returns the frame every chain is rooted at, and whether the model
// declares one.
//
// It is the projected coordinate reference system the model is expressed
// relative to. Exactly one frame per model declares neither a parent nor a
// transform; a second is a load error the registry reports, and this is then the
// first of them in id order.
func (f *Frames) Root() (Frame, bool) {
	if f == nil || f.root == "" {
		return Frame{}, false
	}
	return f.registry.Frame(f.root)
}

// Chain iterates the frames from frame to the root, beginning with frame
// itself.
//
// It is what "expressed relative to" means followed all the way up, and it is
// what every cross-frame answer is composed along. A frame id nothing declares
// yields nothing.
//
// A chain which does not reach a root stops rather than running forever: at a
// parent nothing declares, and at a frame it has already passed through. Both
// are load errors the registry reports, and neither is a reason for a traversal
// to hang.
func (f *Frames) Chain(frame ID) iter.Seq[Frame] {
	return func(yield func(Frame) bool) {
		if f == nil {
			return
		}

		seen := make(map[ID]bool)
		for id := frame; id != ""; {
			declared, ok := f.registry.Frame(id)
			if !ok || seen[id] {
				return
			}
			seen[id] = true

			if !yield(declared) {
				return
			}

			id = declared.Parent
		}
	}
}

// Measurement returns the claim which measures frame's transform to its parent,
// and whether one resolved.
//
// This is the claim itself and not the transform inside it, because the whole
// point of the arrangement is what comes with it: the source it was fitted
// from, the method which produced it, the date it was produced on, and the
// accuracy it was produced to. That accuracy is a systematic term in every
// cross-frame budget the transform touches, and it exists here rather than
// nowhere because a georeference is a measurement rather than a setting.
//
// The root frame has none, which is what makes it the root.
func (f *Frames) Measurement(frame ID) (*Claim, bool) {
	if f == nil {
		return nil, false
	}
	claim, ok := f.measured[frame]
	return claim, ok
}

// Transform returns the transform which maps a point in frame to a point in
// frame's parent, and whether one resolved.
//
// It is the value of [Frames.Measurement], and reading it without the claim
// around it is reading a number without the evidence for it. Where the answer
// is going to be reported to anybody, the claim is what to report from.
func (f *Frames) Transform(frame ID) (Transform, bool) {
	claim, ok := f.Measurement(frame)
	if !ok {
		return Transform{}, false
	}
	return claim.Value().Transform()
}

// TransformPoint maps a point expressed in one frame to the same position
// expressed in another.
//
// The point is in from's linear unit and the result is in to's, because a frame
// declares exactly one and a point in a frame is in that one
// ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)). Every conversion
// between them applies the pinned definitions in [Unit.Metres] and nothing else:
// a transform's `scale` is a scale rather than a unit conversion, so a fit which
// found no scale error contributes a factor of one however far apart the two
// units are.
//
// The route is up from's chain to the lowest frame both chains pass through and
// back down to's, which is what makes this work in both directions and between
// two frames neither of which contains the other. The transforms above the frame
// they meet at are not read, because the route does not pass through them.
//
// Everything which can stop it is an error naming what stopped it: a frame
// nothing declares, a parent chain which cycles, a frame whose transform did not
// resolve to a transform claim, two frames whose chains never meet, a frame
// whose linear unit is not one the engine defines, and a transform which cannot
// be run backwards. Each is a value a caller can inspect rather than a message
// to match on, and none of them is answered with a position anyway.
func (f *Frames) TransformPoint(point Point, from, to ID) (Point, error) {
	if f == nil {
		return Point{}, UndeclaredFrameError{Frame: from}
	}

	for _, id := range []ID{from, to} {
		if _, ok := f.registry.Frame(id); !ok {
			return Point{}, UndeclaredFrameError{Frame: id}
		}
	}

	if from == to {
		return point, nil
	}

	up, err := f.ancestry(from)
	if err != nil {
		return Point{}, err
	}

	down, err := f.ancestry(to)
	if err != nil {
		return Point{}, err
	}

	// The lowest frame both chains pass through. Walking to the root and back
	// would give the same answer wherever both chains reach one, and a different
	// one wherever a frame between them is unmeasured — reporting that a fit
	// nobody needed is missing.
	meet, arrival := -1, -1
	for i, id := range up {
		if j := slices.Index(down, id); j >= 0 {
			meet, arrival = i, j
			break
		}
	}
	if meet < 0 {
		return Point{}, UnrelatedFramesError{From: from, To: to}
	}

	for _, id := range up[:meet] {
		if point, err = f.toParent(point, id); err != nil {
			return Point{}, err
		}
	}

	for i := arrival - 1; i >= 0; i-- {
		if point, err = f.fromParent(point, down[i]); err != nil {
			return Point{}, err
		}
	}

	return point, nil
}

// ancestry is the ids of [Frames.Chain], which is what composing a route along
// two of them needs.
//
// It reports the two ways a chain fails to reach a root rather than stopping
// quietly at them, because a caller asking for a position gets a wrong answer
// from a route which silently ended early and no answer at all from an error.
func (f *Frames) ancestry(frame ID) ([]ID, error) {
	var chain []ID

	seen := make(map[ID]bool)
	for id := frame; id != ""; {
		if seen[id] {
			return nil, FrameCycleError{Frame: id}
		}
		seen[id] = true

		declared, ok := f.registry.Frame(id)
		if !ok {
			return nil, UndeclaredFrameError{Frame: id}
		}

		chain = append(chain, id)
		id = declared.Parent
	}

	return chain, nil
}

// toParent maps a point in one frame to a point in that frame's parent, which
// is specification section 6.6.3's
//
//	p_parent = t + s * R * convert(p, child unit -> parent unit)
//
// The conversion is the only one in the expression, and it is applied before the
// rotation rather than after it because the translation is in the parent's unit:
// adding metres to millimetres is the mistake this whole arrangement exists to
// make impossible.
func (f *Frames) toParent(point Point, frame ID) (Point, error) {
	transform, ratio, err := f.step(frame)
	if err != nil {
		return Point{}, err
	}

	converted := Point{point[0] * ratio, point[1] * ratio, point[2] * ratio}
	rotated := rotate(transform.Rotation, converted)

	return Point{
		transform.Translation[0] + transform.Scale*rotated[0],
		transform.Translation[1] + transform.Scale*rotated[1],
		transform.Translation[2] + transform.Scale*rotated[2],
	}, nil
}

// fromParent maps a point in a frame's parent back into the frame, which is
// [Frames.toParent] run backwards:
//
//	p = convert(R^-1 * (p_parent - t) / s, parent unit -> child unit)
//
// The transform is written in one direction and walked in both, because a
// georeference measured from a building to a survey is the same measurement as
// one from the survey to the building. Writing the reverse down as a second
// claim would be a second source of truth for one fit, free to disagree with the
// first the moment either is re-measured.
func (f *Frames) fromParent(point Point, frame ID) (Point, error) {
	transform, ratio, err := f.step(frame)
	if err != nil {
		return Point{}, err
	}

	inverse, invertible := invert(transform.Rotation)
	if !invertible || transform.Scale == 0 {
		declared, _ := f.registry.Frame(frame)
		return Point{}, SingularTransformError{Frame: frame, Parent: declared.Parent}
	}

	offset := Point{
		point[0] - transform.Translation[0],
		point[1] - transform.Translation[1],
		point[2] - transform.Translation[2],
	}
	rotated := rotate(inverse, offset)

	divisor := transform.Scale * ratio
	return Point{rotated[0] / divisor, rotated[1] / divisor, rotated[2] / divisor}, nil
}

// step is what both directions of one hop need: the transform between a frame
// and its parent, and how many of the parent's linear unit one of the frame's
// is.
func (f *Frames) step(frame ID) (Transform, float64, error) {
	child, ok := f.registry.Frame(frame)
	if !ok {
		return Transform{}, 0, UndeclaredFrameError{Frame: frame}
	}

	parent, ok := f.registry.Frame(child.Parent)
	if !ok {
		return Transform{}, 0, UndeclaredFrameError{Frame: child.Parent}
	}

	transform, ok := f.Transform(frame)
	if !ok {
		return Transform{}, 0, UnmeasuredFrameError{Frame: frame, Parent: child.Parent}
	}

	from, ok := child.Unit.Metres()
	if !ok {
		return Transform{}, 0, UnknownUnitError{Frame: child.ID, Unit: child.Unit}
	}

	into, ok := parent.Unit.Metres()
	if !ok {
		return Transform{}, 0, UnknownUnitError{Frame: parent.ID, Unit: parent.Unit}
	}

	return transform, from / into, nil
}

// rotate multiplies a row-major 3x3 matrix by a point.
func rotate(matrix [9]float64, point Point) Point {
	return Point{
		matrix[0]*point[0] + matrix[1]*point[1] + matrix[2]*point[2],
		matrix[3]*point[0] + matrix[4]*point[1] + matrix[5]*point[2],
		matrix[6]*point[0] + matrix[7]*point[1] + matrix[8]*point[2],
	}
}

// invert returns the inverse of a row-major 3x3 matrix, and whether it has one.
//
// The inverse is computed rather than taken as the transpose, which would be the
// same answer and cheaper for a rotation and a wrong one for anything else. What
// the format calls a rotation is nine numbers produced by a fit, and a fit which
// came back very slightly not orthonormal would round-trip a point to somewhere
// it was not — quietly, and by a margin which looks like arithmetic.
func invert(matrix [9]float64) ([9]float64, bool) {
	cofactors := [9]float64{
		matrix[4]*matrix[8] - matrix[5]*matrix[7],
		matrix[2]*matrix[7] - matrix[1]*matrix[8],
		matrix[1]*matrix[5] - matrix[2]*matrix[4],
		matrix[5]*matrix[6] - matrix[3]*matrix[8],
		matrix[0]*matrix[8] - matrix[2]*matrix[6],
		matrix[2]*matrix[3] - matrix[0]*matrix[5],
		matrix[3]*matrix[7] - matrix[4]*matrix[6],
		matrix[1]*matrix[6] - matrix[0]*matrix[7],
		matrix[0]*matrix[4] - matrix[1]*matrix[3],
	}

	determinant := matrix[0]*cofactors[0] + matrix[1]*cofactors[3] + matrix[2]*cofactors[6]
	if determinant == 0 {
		return [9]float64{}, false
	}

	var inverse [9]float64
	for i, cofactor := range cofactors {
		inverse[i] = cofactor / determinant
	}

	return inverse, true
}

// ResolveFrames joins the frames a registry declares to the claims which measure
// them: it resolves every frame's transform to the claim it names and indexes
// the chain each frame reaches the root through.
//
// It takes a loaded registry and loaded claims rather than a root, because it
// adds no reading of its own. [LoadRegistry] and [LoadClaims] each walk the tree
// once and each report what they found; a third walk here would read every file
// a third time and report the parse error in it a third time.
//
// It is a pass of its own because a frame's transform is a claim. Which frames
// exist and which parent each names is registry data and resolves first, and the
// claim which measures one may be written in any file of the tree — so the
// registry loader cannot resolve it and the claim loader, which does not know
// what a frame is beyond a form which carries claims, cannot judge it.
//
// What it reports is the one thing neither of those passes can say: a frame
// whose transform names a claim whose value is not a transform. The claim is a
// perfectly good claim and the reference reaches it; what it does not carry is a
// fit between two frames. The rest of what can be wrong with a frame is already
// reported where it is knowable — a parent nothing declares, a cycle in the
// chain, a second root and a unit which is no linear unit by [LoadRegistry], and
// a transform naming a claim nothing carries by [LoadClaims] — and repeating any
// of them here would report one mistake twice.
//
// A frame whose transform claim resolved but whose value could not be read is
// not reported here either, for the same reason: the claim loader has already
// said what was wrong with the value, and this pass would be saying it is
// therefore not a transform.
//
// Diagnostics come back in the order the pass found them, which is by frame id.
// Collecting them into a [Diagnostics] is what puts them in reporting order.
func ResolveFrames(registry *Registry, claims *Claims) (*Frames, []Diagnostic) {
	l := &frameResolver{
		claims: claims,
		frames: &Frames{registry: registry, measured: make(map[ID]*Claim)},
	}

	for frame := range registry.Frames() {
		l.resolve(frame)
	}

	return l.frames, l.diags
}

// frameResolver resolves one model's frames against its claims.
type frameResolver struct {
	reader

	// claims are the loaded claims of the model, which the transform references
	// are resolved against. They are not written to.
	claims *Claims

	// frames is the graph being built.
	frames *Frames
}

// resolve reads one declared frame: whether it is the root, and which claim
// measures it against its parent.
func (l *frameResolver) resolve(frame Frame) {
	if frame.Parent == "" && frame.Transform == "" {
		if l.frames.root == "" {
			l.frames.root = frame.ID
		}
		return
	}

	if frame.Transform == "" {
		return
	}

	claim, ok := l.claims.Claim(frame.Transform)
	if !ok {
		// A reference which reaches no claim, already reported by the pass which
		// read the claims.
		return
	}

	value := claim.Value()
	if value.Shape() == ShapeTransform {
		l.frames.measured[frame.ID] = claim
		return
	}

	if value.Shape() == "" {
		// A claim whose value could not be read at all, already reported as
		// whatever was written there instead.
		return
	}

	l.add(Diagnostic{
		Severity: SeverityError,
		Span:     frame.Span,
		Message: fmt.Sprintf(
			"expected a claim whose value is a transform, found %s, whose value is %s",
			frame.Transform, spellShape(value.Shape()),
		),
		Hint: "a frame's transform to its parent is a claim under a predicate declared (shape transform), which is what " +
			"carries the source, the method, the date and the accuracy of the fit which produced it",
		// The value rather than the whole claim: a claim spans as many lines as
		// it has children, and what is wrong with this one is the one line of it
		// which says what it measures.
		Related: []RelatedLocation{{Span: value.Span(), Message: "the value the claim it names carries is written here"}},
	})
}
