# 0021. An export is a build output keyed by its source digest

**Status:** Accepted

## Context

[0009](./0009-derived-values-are-never-written-back.md) decides that derived data is a build
output, written outside the authored tree, keyed by the digest of the source it was derived
from, and that "every derived value the engine reports says it is derived, and reports the
digest it was computed against". [0020](./0020-export-is-a-boundary-and-the-closed-set-is-what-crosses-it.md)
decides that an exporter may live in this repository and fixes what crosses the boundary. An
export is a derived artefact, so all three of 0009's clauses apply to it — and 0009 decides
none of the particulars. It does not say where the file goes, what occupies a format's
timestamp fields, or whether the key the engine already has is the right key for this.

The engine has worked those particulars out once already, for derived geometry. `Cache` sits
under `CacheDir(root)` — `<root>/.dfcad/cache`, beneath the `BuildDir` constant — holds one
entry per `Key`, hashes a `digestVersion` into every digest and a `cacheVersion` into every
entry, and discards anything it cannot verify rather than failing a run over a damaged
disposable. That machinery is right, and the temptation is to conclude that an export is just
another entry in it. Two of the three questions above answer differently for an export than
they do for a cache entry, which is why they are decided here rather than assumed.

**Writing an export into the model root is worse than untidy, and it is bad in two
incompatible ways at once.** `Walk` yields only files whose extension is `Extension` — `.dfc`
— so a `model.ifc` sitting beside the entity files is invisible to every check the engine
runs, while looking exactly as authoritative as the coordinates it may disagree with. It is
in the repository, it is in the diff, a reviewer approves it, and nothing will ever tell
anyone that it stopped matching. That is 0009's stale-by-default failure reproduced exactly,
with the added twist that the artefact is in a foreign format nobody proofreads. And the
digest covers only the files a walk reads, so an export inside the tree is not an input to its
own staleness detection: the thing that would notice it had gone stale cannot see it. Give the
artefact the extension that *would* make it visible and the failure inverts rather than
resolving — it becomes an input to the digest of the tree it was derived from, so writing it
changes the key it was written under and no export of that tree is ever current again.

**The existing `Key` is not the answer either.** It is `{Digest, Tolerance, Position}`: the
tree, plus the two named decisions a *geometry* derivation makes which are not in the tree —
the registry tolerance coincidence was judged against and the predicate corners were read
from. An export keyed by that would be asserting that its output depends on a coincidence
tolerance, which is either false or, if some future exporter makes it true, true for a reason
that has nothing to do with the two names being there. What an export actually depends on
beyond the tree is a different list: the target schema version, the unit it converted to per
[0005](./0005-one-linear-unit-per-frame.md), and whatever mapping data 0020 requires it to be
handed rather than to know.

**A clock reading in an exported file defeats the digest.** Most exchange formats have a
field defined as a creation time — IFC's `IfcOwnerHistory.CreationDate`, the STEP header's
`FILE_NAME` time stamp — and some of them make it mandatory. Filling one from `time.Now()`
costs one line and takes the whole of this record's value with it: two exports of an unchanged
tree stop being byte-identical, so there is nothing left to test, nothing to diff, and every
export looks to whatever compares them like a change to the model. The alternatives that were
live are to omit the field where the schema permits, to write a constant, to derive a time
from the source, or to accept the clock and give up on identity.

**Byte-identity needs to be checkable by something other than the exporter's own opinion.**
[0004](./0004-globalid-derives-from-a-pinned-namespace.md) already promises that "two exports
of an unchanged node produce byte-identical `GlobalId`s", which is a property of one field in
one line. The property worth having is over the whole file, and the obvious way to test it —
export twice in one process and assert the two byte slices are equal — is close to worthless.
It catches map iteration order, which Go randomises per run, and it is blind to everything
else: the clock, the hostname, the locale, the absolute path the model happens to sit at, a
library upgrade that reorders a serialiser, and above all a deliberate-looking change to the
emitted bytes that nobody reviewed. Two wrong exports agree with each other perfectly.

## Decision

**An export is a build output. It is written under `BuildDir` and never into the authored
tree.** The conventional location is `ExportDir(root)` — `<root>/.dfcad/export` — a sibling
of `CacheDir(root)`, laid out the same way: one file per key, beneath a directory named for
the digest, so a run against a new revision writes a new directory rather than replacing
anything. `BuildDir` is inside the root path and is not part of the model: it holds no file a
walk reads, contributes nothing to the tree digest, and is already in `.gitignore`. Deleting
the whole of it costs time and nothing else.

**A destination inside the authored tree is refused, before anything is written.** The check
is on the resolved absolute path, so a relative path that walks back in through `..` is
refused too, and it is a refusal rather than a warning: the artefact that must not exist is
not written and then complained about. A caller may name a destination outside the model root,
or ask for the bytes on stdout, and both are ordinary — the rule closes the authored tree, not
every path but one. `.dfcad` is the exception to the root's refusal because it is the one
place under the root that the authored tree does not include.

**An export is byte-identical for identical input.** The same tree and the same key produce
the same bytes on any machine, in any working directory, under any locale, at any time, on
any run. Nothing may reach an exported byte that is not either covered by the tree digest or
named in the key. That is the whole rule, and it is what makes every other property here
testable.

**Identity is tested by a golden fixture, not by an assertion.** This repository holds an
exported artefact of its fixture model under `testdata/`, regenerated with `go test . -update`
and compared byte for byte by an ordinary test and by CI, exactly as its other goldens are. A
difference is a failure. An in-process export-twice-and-compare assertion is not a substitute
and does not discharge this: it tests that the exporter agrees with itself, which is the one
thing a broken exporter also does. The golden file is what makes a change to the emitted bytes
arrive as a diff a person reads, which is the same argument 0009 makes for keeping the source
tree free of computed values, applied to the artefact instead of to the model.

**Clock-derived fields carry a constant, and no export reads the system clock.** Where a
target format defines a field as a creation or modification time:

- If the schema permits the field to be absent, it is omitted.
- If the schema requires it, it is written as the Unix epoch — `1970-01-01T00:00:00Z`, in
  whatever encoding the format demands.

There is no third case and no option to override it. The rejected alternatives are worth
naming: the commit time of the tree is unavailable from a working copy with uncommitted edits
and would make the export depend on git, which the digest does not cover; a time derived from
the digest is a fabricated value with a plausible shape, which is the worst of both. An
obviously wrong constant is better than a convincing lie. A receiving tool showing 1970
prompts someone to ask where the date comes from, and the answer — "nowhere; the provenance is
the digest" — is the conversation that should happen. A fabricated recent date invites nobody
to ask anything.

**The provenance the timestamp field pretends to carry is carried properly instead.** Per
0009, an export states that it is derived and states the digest it was derived from, through
whatever mechanism the target format has for it — a header description, a comment, a property
set. That does not weaken byte-identity: the digest is a function of the source, so identical
input yields an identical stamp.

**An export's key is its own type, and `Key` does not generalise.** A second key type is
declared, holding:

| Member | What it pins |
|--------|--------------|
| `Digest` | The digest of the source tree the export read — the same `Digest` type the cache uses. |
| `Format` | The target format and its schema version. Two exports of one tree to two schema versions are two artefacts. |
| `Unit` | The linear unit the export converted to. Per 0005 the boundary is the only place a conversion factor is applied, so which unit came out is a caller's decision and is not in the tree. |
| `Mapping` | A digest over the mapping data the export was handed, where that data is not itself part of the digested tree. 0020 requires an element's specific class to come from a declaration rather than an engine table; a declaration the digest does not cover is an input, and it enters the key as a digest rather than as a version string. |
| `Version` | The exporter's contract version, an integer, moved whenever the emitted bytes change for a reason that is not one of the inputs above — the role `cacheVersion` plays for the cache. |

`Tolerance` and `Position` are absent because an exporter writes authored structure and
authored geometry and reads no value judged against a coincidence tolerance. The general rule
behind that, which outlives this exporter: **every derived input an artefact reads is a member
of its key**, and if a later exporter genuinely needs a derived quantity, that dependency joins
this key and the move is recorded, rather than being tolerated as an unpinned input.

The two types stay separate for a reason stronger than tidiness. The value of a key is that it
is a closed record of every input, checkable by reading the declaration; a key generalised to
cover both artefacts is either a map or a struct with fields some callers fill and others
ignore, and both are keys whose meaning depends on who wrote them, which is the stale hit
0009's mechanism exists to make unrepresentable. Adding export fields to `Key` would also
change its entry derivation, invalidating every cached geometry entry in existence for a
reason that has nothing to do with geometry. What the two share is the `Digest` type, and
sharing a type is the whole of the sharing that is wanted.

## Consequences

An export cannot go stale unnoticed. It is keyed by everything it read, so an export whose key
does not match the tree in front of you is a miss and a re-export, never a stale hit — the
same property the cache has, extended to the artefact that actually leaves the building.

0004's stability promise acquires a test. "Two exports of an unchanged node produce
byte-identical `GlobalId`s" stops being a claim about a function and becomes a line in a
golden file that CI compares, along with everything else in the artefact.

The mapping 0020 closed acquires a review surface. Any change to what a `kind` emits, to how
a `within` edge is written, or to which unit comes out, shows up as a diff in the fixture
export. "Does this change what we emit" is answered by reading the diff rather than by reading
the exporter and hoping.

The source tree stays exactly the set of things somebody asserted, and `git status` after an
export is clean, because the artefact landed in an ignored build directory. Exporting is not
an operation that can dirty a working copy.

Consumers keying on a creation date get the epoch, and that is now a documented property
rather than a bug report. A workflow that needs a real date has to attach it outside the file,
where it is visibly a fact about the export run rather than a fact about the model.

`ExportDir` accumulates one directory per distinct key, exactly as the cache does, and needs
the same bounding — a prune that keeps the current key and removes the rest, and the standing
option of deleting the directory outright.

## Cost

**The epoch stamp is a lie of its own, and it is paid by everyone downstream.** Every
receiving system that sorts, filters or displays by creation date shows 1970; some BIM tools
present that badly and a few validators will complain about it. There is no way to say inside
the file's own vocabulary "this artefact was produced on Tuesday", and the honest answer — the
digest — is not a field those tools read. The predictable response in the field is a
post-processing step that stamps a real date after export, which reintroduces exactly the
nondeterminism this record removes, in a place where nothing here can see it and no golden
fixture covers it.

**The golden export is a maintenance obligation with a known failure mode.** Every intended
change to the emitted bytes needs a regeneration, and a regeneration is a diff. While the
fixture model stays small that diff is readable and the check does its job. Let the fixture
grow and the diff becomes unreadable, at which point the check degrades into running `-update`
until the build is green, which is the check having quietly stopped existing. Keeping the
fixture model bounded is now a permanent cost, and it competes with the wish to cover more of
the mapping.

**Byte-identity constrains every exporter this repository will ever have.** No map may be
ranged over into the output without sorting, no absolute path, hostname, locale or environment
variable may reach a byte, `time.Now()` is banned outright, and any concurrency inside an
exporter has to be order-preserving. That is a rule future authors inherit whether or not they
have read this record, and the golden test is the only thing that will tell them — after the
fact, as a failure they have to diagnose.

**Two key types will drift.** Both hold a digest and a version integer, both need an entry
derivation over their remaining fields, and the second one is going to be written by
copying the first. The two length-prefixing rules can disagree, and a disagreement there is
invisible until two distinct keys collide on one entry file.

**Refusing the model root refuses something people reasonably expect.** Running an export from
inside the model directory and letting the shell redirect it into a file there is the first
thing anyone types, and it will be refused. The refusal is correct and it will read as
obstinate at least once per new user.

**"Disposable" is a claim about recomputation cost, and export is not free.** A cold `.dfcad`
means re-exporting everything, and for a large model that is not the trivial cost the word
disposable suggests. This record inherits 0009's cost rather than avoiding it.

## What would reverse it

A target format that both mandates a real timestamp and validates it — a signed exchange, a
submission schema that rejects the epoch — would break the constant. The response is not
`time.Now()`; it is to make the timestamp an explicit input supplied by the caller, which puts
it in the key and keeps byte-identity per key while recording the value instead of sampling
it. That reversal is cheap precisely because the key already exists, and it is the shape any
future "the export needs to know X" takes.

Evidence that byte-identity is unattainable for a format — a serialiser whose own canonical
output is nondeterministic and cannot be pinned — would reverse the golden fixture rather than
the key. The replacement is a semantic comparison of two exports, which is weaker, much more
code, and blind to exactly the class of change the golden file exists to surface. It should
not be reached for until the byte comparison has actually been shown to be impossible.

The location is the cheap half to reverse. Exports are disposable, so moving `ExportDir`
elsewhere costs a delete and a re-run and no data at all; what it costs is measured in other
people's build scripts, which is real and is not this repository's to enumerate.

**The key is the expensive half, and its failure is silent.** A key that turns out to be too
narrow has not been failing loudly — it has been serving byte-identical exports for inputs
that differed, and those files have left this repository. Per 0020 they are in a receiving BIM
tool, an approved submission or a facility management database, in places nothing here can
list. Widening the key afterwards is a `Version` move: correct, loud, and it re-exports
everything, which is fine. The part with no remedy is the interval before anyone notices,
because the mechanism that would have noticed is the one that was wrong. That is the argument
for putting an input in the key when it is *arguably* an input, and the reason `Mapping` is a
digest rather than a version string.
