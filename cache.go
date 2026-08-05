// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
)

// BuildDir is the directory derived data is written to, relative to the root of
// the model it was derived from.
//
// Nothing in it is a source. Every byte beneath it can be recomputed from the
// entity files, deleting the whole of it costs time and nothing else, and no
// pass reads it for anything but speed
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)). It is
// gitignored for the same reason a compiled binary is: a build output committed
// beside its source is a second copy of the model which goes stale silently.
//
// It sits beside the entity files rather than inside them. A walk reads only
// files whose extension is [Extension], so nothing here is ever loaded as part
// of the model however deep it is nested.
const BuildDir = ".dfcad"

// CacheDir is where the derived-geometry cache of the model beneath root lives.
//
// It is a function rather than a constant because a cache belongs to the tree it
// was derived from. Two models sharing one cache directory would be two sets of
// entries under different digests in one place, which works and makes pruning
// either of them impossible to do without reading the other's.
func CacheDir(root string) string { return filepath.Join(root, BuildDir, "cache") }

// digestVersion is what the digest computation is, in the digest.
//
// It is hashed first, so a change to the rules below changes every key at once
// rather than leaving entries written under the old rules to be read under the
// new ones. A cache written by an older engine is a miss, never a wrong hit.
const digestVersion = "dfcad/tree-digest/1"

// cacheVersion is the version of the entry format [Cache] writes. An entry
// carrying any other version is discarded and recomputed, for the reason above.
const cacheVersion = 1

// Digest is a content digest over a source tree: the one value a derived
// artefact is keyed by.
//
// It is what makes the cache impossible to serve a stale answer from. A cached
// value is read only under the digest of the tree it was derived from, so a tree
// which changed anywhere has a different key and misses — there is no
// invalidation step to get wrong, because the key is the invalidation
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// # How it is computed
//
// SHA-256 over, in this order:
//
//  1. [digestVersion], length-prefixed.
//  2. For every entity file beneath the root, in the lexical path order [Walk]
//     yields them in: the file's path relative to the root, slash-separated,
//     length-prefixed; then the file's bytes, length-prefixed.
//
// Every field is prefixed with its length as eight bytes, big-endian, so no two
// different trees can serialise to the same byte sequence — without it a file
// named `a` holding `bc` and one named `ab` holding `c` would digest alike.
//
// Only files a walk reads are covered, which is the set the model is made of.
// Anything else in the directory — a build output, a README, an editor's swap
// file — is not an input to any derivation and changing it must not cost a
// recomputation.
//
// The digest is deterministic: the same tree digests to the same value on any
// machine, in any order the files happen to be created, whatever their
// timestamps or permissions. Nothing about the filesystem enters it, only the
// paths and the bytes.
//
// The zero Digest is the digest of nothing and reports [Digest.Known] false. It
// is what a model which is not on disk has, and nothing is cached under it.
type Digest struct {
	sum   [sha256.Size]byte
	known bool
}

// Known reports whether this is a digest of something.
//
// The zero Digest is not, and is the answer for a tree which could not be read
// and for a graph which was never on disk. A caller holding one knows only that
// nothing may be keyed by it.
func (d Digest) Known() bool { return d.known }

// String returns the digest as lower-case hex, or "unknown" for the zero one.
func (d Digest) String() string {
	if !d.known {
		return "unknown"
	}
	return hex.EncodeToString(d.sum[:])
}

// MarshalText writes the digest as lower-case hex, and the zero one as nothing.
func (d Digest) MarshalText() ([]byte, error) {
	if !d.known {
		return []byte{}, nil
	}
	return []byte(hex.EncodeToString(d.sum[:])), nil
}

// UnmarshalText reads a digest written as lower-case hex, and the zero one from
// an empty text.
func (d *Digest) UnmarshalText(text []byte) error {
	if len(text) == 0 {
		*d = Digest{}
		return nil
	}

	sum, err := hex.DecodeString(string(text))
	if err != nil {
		return DigestError{Err: err}
	}
	if len(sum) != sha256.Size {
		return DigestError{Err: fmt.Errorf("a digest is %d bytes, this one is %d", sha256.Size, len(sum))}
	}

	*d = Digest{known: true}
	copy(d.sum[:], sum)

	return nil
}

// DigestError is a source tree which could not be digested, and the file which
// stopped it.
//
// A derivation which gets one carries on without a cache rather than failing:
// the digest is what a cache is keyed by, and a run which cannot key one is a
// run which computes everything, which is slower and never wrong.
type DigestError struct {
	// Path is the file which could not be read, empty where the failure was not
	// about one file.
	Path string

	// Err is what stopped it.
	Err error
}

func (e DigestError) Error() string {
	if e.Path == "" {
		return fmt.Sprintf("cannot digest a source tree: %v", e.Err)
	}
	return fmt.Sprintf("cannot digest %s: %v", e.Path, e.Err)
}

// Unwrap returns what stopped the digest.
func (e DigestError) Unwrap() error { return e.Err }

// DigestOf computes the digest of the source tree beneath root, exactly as
// [Digest] documents it.
//
// root is what [Load] takes: a single file, or a directory beneath which every
// file whose extension is [Extension] is read. A directory holding no entity
// file digests to the digest of an empty tree, which is a real digest and not
// the zero one — a model nobody has written yet is a model, and a derivation
// over it is legitimately cacheable.
//
// Reading is streaming, one file at a time, so digesting a tree costs the
// largest single file rather than the sum of them.
func DigestOf(root string) (Digest, error) {
	sum := sha256.New()
	digestField(sum, []byte(digestVersion))

	for path, err := range Walk(root) {
		if err != nil {
			return Digest{}, DigestError{Path: path, Err: err}
		}

		content, err := os.ReadFile(path)
		if err != nil {
			return Digest{}, DigestError{Path: path, Err: err}
		}

		digestField(sum, []byte(digestName(root, path)))
		digestField(sum, content)
	}

	digest := Digest{known: true}
	copy(digest.sum[:], sum.Sum(nil))

	return digest, nil
}

// digestField writes one length-prefixed field into a digest.
//
// The prefix is what makes the encoding unambiguous: two fields concatenated
// without their lengths are one field, and two different trees would then digest
// alike.
func digestField(sum hash.Hash, field []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(field)))
	sum.Write(length[:])
	sum.Write(field)
}

// digestName is the name a file is digested under: its path relative to the
// root, slash-separated so the digest of a tree is the same on any platform.
//
// A root which names one file has that file digested under its base name, which
// is what makes the digest of a file the same whether it was reached directly or
// as itself.
func digestName(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		rel = filepath.Base(path)
	}
	return filepath.ToSlash(rel)
}

// Key identifies one derived artefact: what it was derived from, and the two
// named decisions it was derived under.
//
// The digest pins the whole of the input the derivation read, and the tolerance
// and the position predicate pin the choices a caller made which are not in the
// tree. All three have to be in the key. A footprint judged against a tolerance
// of five millimetres is a different answer from one judged against fifty, and
// serving either for the other is the stale hit this whole mechanism exists to
// make unrepresentable.
type Key struct {
	// Digest is the digest of the source tree the artefact was derived from.
	Digest Digest

	// Tolerance is the name of the registry tolerance coincidence was judged
	// against.
	Tolerance string

	// Position is the name of the predicate the corners were read from.
	Position string
}

// String writes the key as it reads: the digest, then the two names.
func (k Key) String() string {
	return fmt.Sprintf("%s tolerance=%s position=%s", k.Digest, k.Tolerance, k.Position)
}

// cacheable reports whether anything may be stored under this key. A key with no
// digest pins nothing, so an entry written under it could be read back against
// any tree at all.
func (k Key) cacheable() bool { return k.Digest.Known() }

// entry is the file name an entry under this key is written to, beneath the
// directory named by its digest.
func (k Key) entry() string {
	sum := sha256.New()
	digestField(sum, []byte(k.Tolerance))
	digestField(sum, []byte(k.Position))
	return hex.EncodeToString(sum.Sum(nil)) + ".json"
}

// CacheStats is what a cache did: enough to tell a run which paid for its
// derivation from one which did not, and to tell a miss from an entry which was
// there and unusable.
//
// The last two are the ones worth watching. Discards are entries which were
// found, judged corrupt and thrown away, which is the cache working as intended
// exactly once and a broken build output directory if it keeps happening.
// Errors are writes which did not land, which cost time and nothing else.
type CacheStats struct {
	// Hits are lookups answered from the cache.
	Hits int

	// Misses are lookups with nothing written under the key.
	Misses int

	// Discards are lookups which found an entry and refused it: unreadable,
	// corrupt, written by another version, or written under another key.
	Discards int

	// Stores are entries written.
	Stores int

	// Errors are entries which could not be written, and digests which could
	// not be computed.
	Errors int
}

// CacheError is a cache operation which could not be carried out.
//
// Every one of them is survivable. A cache is advisory, so a store which fails
// costs a recomputation on the next run and nothing else, and a caller which
// ignores this error gets the right answer more slowly.
type CacheError struct {
	// Op is what was being done — "open", "store", "prune".
	Op string

	// Path is the file or directory it was being done to.
	Path string

	// Err is what stopped it.
	Err error
}

func (e CacheError) Error() string {
	return fmt.Sprintf("cannot %s the derived-geometry cache at %s: %v", e.Op, e.Path, e.Err)
}

// Unwrap returns what stopped the operation.
func (e CacheError) Unwrap() error { return e.Err }

// Cache is a store of derived geometry on disk, keyed by the digest of the
// source tree the geometry was derived from.
//
// It is a build output and never a source. Deleting it changes what a run
// reports by nothing at all — only how long the run takes — and every entry in
// it can be recomputed from the entity files
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
//
// It cannot serve a stale answer. An entry is written under the digest of the
// tree it was derived from and read back only under that same digest, so a tree
// which changed by one byte has a different key and misses. There is no
// invalidation pass, no timestamp comparison and no dependency list to get
// wrong.
//
// An entry which is unreadable, truncated, corrupt, written by another version
// of the engine, or written under a different key is discarded and recomputed.
// A cache is not a source of truth, so the only thing to do with a byte of it
// which does not verify is to throw it away — failing the run over a damaged
// build output would make a disposable artefact load-bearing.
//
// # What it holds and how it is bounded
//
// One entry per [Key]: one file, holding the derived geometry of the whole
// model, beneath a directory named for the digest. A run against a new revision
// of the tree writes a new directory rather than replacing anything, so a cache
// nobody prunes grows by one directory per distinct revision derived against.
//
// [Cache.Prune] is how that is bounded: it keeps one digest and removes every
// other. A build which derives against the tree it just read calls it with that
// tree's digest, which leaves the cache holding exactly one revision. Removing
// the whole directory is always safe as well, and is what a caller who wants no
// cache at all does.
//
// A nil *Cache is a working cache which holds nothing: every lookup misses and
// every store does nothing. That is what a derivation with no cache configured
// runs against, so nothing above has to branch on whether caching is on.
//
// A Cache is safe for concurrent use.
type Cache struct {
	// dir is the directory entries are written beneath.
	dir string

	// mu guards stats only. The entries themselves are guarded by the
	// filesystem: a store writes a temporary file and renames it over the
	// entry, so a reader sees either the whole of the old one or the whole of
	// the new one, and two processes storing the same key store the same bytes.
	mu    sync.Mutex
	stats CacheStats
}

// OpenCache opens the cache in dir, creating the directory where it does not
// exist.
//
// dir is a build output directory: [CacheDir] is where it conventionally sits
// for a given model, and anywhere outside the source tree is equally valid. It
// must not be a directory holding entity files — nothing here writes a file a
// walk would read, but a cache sharing a directory with sources is one rm away
// from taking them with it.
func OpenCache(dir string) (*Cache, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, CacheError{Op: "open", Path: dir, Err: err}
	}
	return &Cache{dir: dir}, nil
}

// Dir returns the directory the cache writes beneath.
func (c *Cache) Dir() string {
	if c == nil {
		return ""
	}
	return c.dir
}

// Stats returns what the cache has done so far.
func (c *Cache) Stats() CacheStats {
	if c == nil {
		return CacheStats{}
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	return c.stats
}

// record adds one event to the statistics.
func (c *Cache) record(count func(*CacheStats)) {
	if c == nil {
		return
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	count(&c.stats)
}

// Lookup returns what is stored under key, and whether anything usable was.
//
// An entry which is there and does not verify is discarded — removed from the
// cache and reported as a miss — rather than returned or raised. Corruption in a
// build output is a recomputation, never a failed run.
func (c *Cache) Lookup(key Key) (Footprints, bool) {
	if c == nil || !key.cacheable() {
		c.record(func(s *CacheStats) { s.Misses++ })
		return Footprints{}, false
	}

	path := filepath.Join(c.dir, key.Digest.String(), key.entry())

	content, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			c.record(func(s *CacheStats) { s.Misses++ })
			return Footprints{}, false
		}
		c.discard(path)
		return Footprints{}, false
	}

	prints, ok := decodeEntry(content, key)
	if !ok {
		c.discard(path)
		return Footprints{}, false
	}

	c.record(func(s *CacheStats) { s.Hits++ })

	return prints, true
}

// discard throws away an entry which did not verify and counts it.
//
// The removal is best-effort. An entry which cannot be removed is one which will
// be discarded again on the next run, which costs a recomputation and nothing
// else.
func (c *Cache) discard(path string) {
	_ = os.Remove(path)
	c.record(func(s *CacheStats) { s.Discards++ })
}

// Store writes prints under key, replacing whatever was there.
//
// The write is atomic: the entry is written to a temporary file in the same
// directory and renamed over the entry, so a run killed mid-write leaves either
// the previous entry or none, and never half of one.
//
// A key with no digest stores nothing and reports no error. Nothing pins an
// entry written under an unknown digest, so writing one would be the one way
// this cache could serve a stale answer.
func (c *Cache) Store(key Key, prints Footprints) error {
	if c == nil || !key.cacheable() {
		return nil
	}

	content, err := encodeEntry(key, prints)
	if err != nil {
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: c.dir, Err: err}
	}

	dir := filepath.Join(c.dir, key.Digest.String())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: dir, Err: err}
	}

	temp, err := os.CreateTemp(dir, ".entry-*")
	if err != nil {
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: dir, Err: err}
	}

	if _, err := temp.Write(content); err != nil {
		_ = temp.Close()
		_ = os.Remove(temp.Name())
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: temp.Name(), Err: err}
	}
	if err := temp.Close(); err != nil {
		_ = os.Remove(temp.Name())
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: temp.Name(), Err: err}
	}

	path := filepath.Join(dir, key.entry())
	if err := os.Rename(temp.Name(), path); err != nil {
		_ = os.Remove(temp.Name())
		c.record(func(s *CacheStats) { s.Errors++ })
		return CacheError{Op: "store", Path: path, Err: err}
	}

	c.record(func(s *CacheStats) { s.Stores++ })

	return nil
}

// Prune removes every generation of the cache but the one keep names, and
// reports how many it removed.
//
// This is how the cache is bounded. It holds one directory per source tree
// digest it has been derived against, so a long-lived checkout accumulates one
// per revision anybody derived against; a build which prunes with the digest it
// just used leaves exactly one, which is the working set.
//
// Pruning the zero [Digest] keeps nothing, which empties the cache without
// removing the directory itself. That is the same outcome as deleting the
// directory, said in a way a caller which does not own the directory can say it.
func (c *Cache) Prune(keep Digest) (int, error) {
	if c == nil {
		return 0, nil
	}

	entries, err := os.ReadDir(c.dir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return 0, nil
		}
		return 0, CacheError{Op: "prune", Path: c.dir, Err: err}
	}

	var removed int
	for _, entry := range entries {
		if entry.Name() == keep.String() {
			continue
		}

		path := filepath.Join(c.dir, entry.Name())
		if err := os.RemoveAll(path); err != nil {
			return removed, CacheError{Op: "prune", Path: path, Err: err}
		}
		removed++
	}

	return removed, nil
}

// cacheEntry is one entry as it is written: the key it was written under and the
// derived geometry under it.
//
// The key is written into the entry as well as into the path so that an entry
// read back can be checked against the key it was asked for. A file moved,
// copied or renamed by hand is then a discard rather than an answer about
// somebody else's tree.
type cacheEntry struct {
	Version    int               `json:"version"`
	Digest     Digest            `json:"digest"`
	Tolerance  string            `json:"tolerance"`
	Position   string            `json:"position"`
	Footprints []footprintRecord `json:"footprints"`
}

// footprintRecord is one footprint as it is written. A figure which could not be
// computed is absent rather than zero, which is the same distinction
// [Measurement] draws in memory.
type footprintRecord struct {
	Subject   ID            `json:"subject"`
	Frame     ID            `json:"frame,omitempty"`
	Unit      Unit          `json:"unit,omitempty"`
	Dimension int           `json:"dimension,omitempty"`
	Pieces    []pieceRecord `json:"pieces,omitempty"`
	Area      *float64      `json:"area,omitempty"`
	Perimeter *float64      `json:"perimeter,omitempty"`
	Centroid  *Point        `json:"centroid,omitempty"`
	Bounds    *Box          `json:"bounds,omitempty"`
	Within    []ID          `json:"within,omitempty"`
}

// pieceRecord is one connected part of a footprint as it is written.
type pieceRecord struct {
	Outer []Point   `json:"outer"`
	Holes [][]Point `json:"holes,omitempty"`
	Area  float64   `json:"area"`
}

// encodeEntry writes an entry: a hex checksum, a newline, then the JSON.
//
// The checksum is over the JSON and is what catches the damage JSON cannot catch
// itself — a truncated write which still parses, a byte flipped inside a number.
// A cache which trusted the parse would serve those as answers.
func encodeEntry(key Key, prints Footprints) ([]byte, error) {
	entry := cacheEntry{
		Version:   cacheVersion,
		Digest:    key.Digest,
		Tolerance: key.Tolerance,
		Position:  key.Position,
	}

	for _, id := range prints.order {
		print, ok := prints.byID[id]
		if !ok {
			continue
		}
		entry.Footprints = append(entry.Footprints, print.record())
	}

	payload, err := json.Marshal(entry)
	if err != nil {
		return nil, err
	}

	sum := sha256.Sum256(payload)

	// The buffer is grown by append rather than sized up front. Sizing it would
	// mean arithmetic over the length of a payload with no bound on it, which is
	// an overflow waiting to be reached by a large enough model.
	content := append([]byte(hex.EncodeToString(sum[:])), '\n')
	content = append(content, payload...)

	return content, nil
}

// decodeEntry reads an entry back, reporting whether it verified against the key
// it was asked for.
//
// Every way it can fail is one answer — no — because every one of them means the
// same thing to a caller: there is nothing usable here, compute it. Which way it
// failed is not actionable, since the remedy for a corrupt build output is to
// discard it whatever corrupted it.
func decodeEntry(content []byte, key Key) (Footprints, bool) {
	newline := slices.Index(content, '\n')
	if newline < 0 {
		return Footprints{}, false
	}

	want, err := hex.DecodeString(string(content[:newline]))
	if err != nil {
		return Footprints{}, false
	}

	payload := content[newline+1:]
	got := sha256.Sum256(payload)
	if !slices.Equal(want, got[:]) {
		return Footprints{}, false
	}

	var entry cacheEntry
	if err := json.Unmarshal(payload, &entry); err != nil {
		return Footprints{}, false
	}

	if entry.Version != cacheVersion {
		return Footprints{}, false
	}
	if entry.Digest != key.Digest || entry.Tolerance != key.Tolerance || entry.Position != key.Position {
		return Footprints{}, false
	}

	prints := Footprints{
		digest:    entry.Digest,
		tolerance: entry.Tolerance,
		position:  entry.Position,
		byID:      make(map[ID]Footprint, len(entry.Footprints)),
	}

	for _, record := range entry.Footprints {
		if record.Subject == "" {
			return Footprints{}, false
		}
		if _, taken := prints.byID[record.Subject]; taken {
			return Footprints{}, false
		}

		prints.order = append(prints.order, record.Subject)
		prints.byID[record.Subject] = footprintOf(record)
	}

	return prints, true
}

// Footprint is the derived geometry of one node: the plane figure it covers,
// how much area that encloses, where the area is centred, how far it reaches and
// which regions it lies inside.
//
// Everything here is derived and none of it is ever written back into an entity
// file ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
// [Footprint.Digest] is the digest of the source tree it was computed against,
// which is what lets a consumer check that a figure it was handed matches the
// model in front of it rather than an earlier one.
//
// Every figure is in the unit of the frame the node is declared in and nothing
// converts ([0005](docs/decisions/0005-one-linear-unit-per-frame.md)): a length
// in that unit, an area in it squared.
//
// A figure comes back with whether it could be computed, because "there is no
// answer" and "the answer is zero" are different states. The zero Footprint has
// none of them and every method below works on it.
type Footprint struct {
	// subject is the node the geometry was derived from.
	subject ID

	// digest is the source tree it was derived from.
	digest Digest

	// frame is the frame it is declared in and unit that frame's linear unit.
	frame ID
	unit  Unit

	// dimension is how many components the positions behind it were written
	// with, which is how many a point of it is printed with.
	dimension int

	// pieces are the plane figure it covers: one per connected part, each a
	// ring with the rings taken out of it.
	pieces []Piece

	// area is what the figure encloses, in unit squared.
	area    float64
	hasArea bool

	// perimeter is the total length of the loops bounding it.
	perimeter    float64
	hasPerimeter bool

	// centroid is where the area is centred.
	centroid    Point
	hasCentroid bool

	// bounds is the axis-aligned bounding box.
	bounds    Box
	hasBounds bool

	// within are the ids of the regions this one lies entirely inside, in id
	// order.
	within []ID
}

// Subject returns the id of the node the geometry was derived from.
func (f Footprint) Subject() ID { return f.subject }

// Digest returns the digest of the source tree it was derived from, which is the
// zero [Digest] where the tree could not be digested.
func (f Footprint) Digest() Digest { return f.digest }

// Frame returns the frame the node is declared in.
func (f Footprint) Frame() ID { return f.frame }

// Unit returns the linear unit of that frame, which every figure here is in.
func (f Footprint) Unit() Unit { return f.unit }

// Pieces returns the plane figure the node covers: one [Piece] per connected
// part, each a ring with the rings taken out of it.
//
// It is the same figure [Topology.RegionOf] reads, which is where it came from.
// A node covering nothing has none.
func (f Footprint) Pieces() []Piece { return slices.Clone(f.pieces) }

// Area returns what the figure encloses, in the square of [Footprint.Unit], and
// whether it could be computed.
func (f Footprint) Area() (float64, bool) { return f.area, f.hasArea }

// Perimeter returns the total length of the loops bounding the node, in
// [Footprint.Unit], and whether it could be computed.
func (f Footprint) Perimeter() (float64, bool) { return f.perimeter, f.hasPerimeter }

// Centroid returns where the area is centred, and whether it could be computed.
// It is the area centroid and not the mean of the corners.
func (f Footprint) Centroid() (Point, bool) { return f.centroid, f.hasCentroid }

// Bounds returns the axis-aligned bounding box, and whether it could be
// computed.
func (f Footprint) Bounds() (Box, bool) { return f.bounds, f.hasBounds }

// Within returns the ids of the regions this one lies entirely inside, in id
// order.
//
// It is derived membership and not the `within` written on a node: a courtyard
// is inside the floor plate around it because of where its corners are, whether
// or not anybody wrote the containment down. [Nodes.Within] is the authored
// answer, and the two disagreeing is a thing worth knowing rather than a bug in
// either.
//
// Membership is judged only between regions one operation could be run over —
// one frame, one tolerance, one plane. Two floor plates on different storeys are
// inside each other seen from above and are not inside each other, so neither
// is reported as within the other.
//
// A region coincident with another is not within it. Neither is inside the
// other, and reporting both as members of each other would make containment
// cyclic.
func (f Footprint) Within() []ID { return slices.Clone(f.within) }

// String writes the figures which were computed, with their units.
func (f Footprint) String() string {
	var parts []string

	if f.hasArea {
		parts = append(parts, fmt.Sprintf("area %s%s", decimal(f.area), squareSuffix(f.unit)))
	}
	if f.hasPerimeter {
		parts = append(parts, fmt.Sprintf("perimeter %s%s", decimal(f.perimeter), unitSuffix(f.unit)))
	}
	if f.hasCentroid {
		parts = append(parts, fmt.Sprintf("centroid %s", pointText(f.centroid, f.printed())))
	}
	if len(f.pieces) > 0 {
		parts = append(parts, plural(len(f.pieces), "piece"))

		var holes int
		for _, piece := range f.pieces {
			holes += len(piece.holes)
		}
		if holes > 0 {
			parts = append(parts, plural(holes, "hole"))
		}
	}
	if len(f.within) > 0 {
		names := make([]string, 0, len(f.within))
		for _, id := range f.within {
			names = append(names, string(id))
		}
		parts = append(parts, fmt.Sprintf("within %s", strings.Join(names, ", ")))
	}

	if len(parts) == 0 {
		return fmt.Sprintf("%s: nothing derived", f.subject)
	}

	return fmt.Sprintf("%s: %s", f.subject, strings.Join(parts, ", "))
}

// printed is how many components of a point to write, which is how many the
// positions were written with.
func (f Footprint) printed() int {
	if f.dimension < 1 || f.dimension > 3 {
		return 3
	}
	return f.dimension
}

// record is the footprint as it is written to the cache.
func (f Footprint) record() footprintRecord {
	out := footprintRecord{
		Subject:   f.subject,
		Frame:     f.frame,
		Unit:      f.unit,
		Dimension: f.dimension,
		Within:    f.within,
	}

	for _, piece := range f.pieces {
		out.Pieces = append(out.Pieces, pieceRecord{
			Outer: piece.outer,
			Holes: piece.holes,
			Area:  piece.area,
		})
	}

	if f.hasArea {
		area := f.area
		out.Area = &area
	}
	if f.hasPerimeter {
		perimeter := f.perimeter
		out.Perimeter = &perimeter
	}
	if f.hasCentroid {
		centroid := f.centroid
		out.Centroid = &centroid
	}
	if f.hasBounds {
		bounds := f.bounds
		out.Bounds = &bounds
	}

	return out
}

// footprintOf reads a footprint back out of what was written.
func footprintOf(record footprintRecord) Footprint {
	out := Footprint{
		subject:   record.Subject,
		frame:     record.Frame,
		unit:      record.Unit,
		dimension: record.Dimension,
		within:    record.Within,
	}

	for _, piece := range record.Pieces {
		out.pieces = append(out.pieces, Piece{
			outer: piece.Outer,
			holes: piece.Holes,
			area:  piece.Area,
		})
	}

	if record.Area != nil {
		out.area, out.hasArea = *record.Area, true
	}
	if record.Perimeter != nil {
		out.perimeter, out.hasPerimeter = *record.Perimeter, true
	}
	if record.Centroid != nil {
		out.centroid, out.hasCentroid = *record.Centroid, true
	}
	if record.Bounds != nil {
		out.bounds, out.hasBounds = *record.Bounds, true
	}

	return out
}

// Footprints is the derived geometry of a whole model, and the digest of the
// source tree every figure in it was computed against.
//
// It is derived as one artefact rather than one per node because membership is
// a question about the set: which regions a room is inside is not answerable
// from the room alone. Keying the set by the digest of the tree is what makes
// that sound — every footprint in one of these was derived from the same bytes.
//
// The zero Footprints holds nothing and every method below works on it.
type Footprints struct {
	// digest is the source tree every footprint was derived from.
	digest Digest

	// tolerance and position are the two named decisions they were derived
	// under.
	tolerance string
	position  string

	// order is the ids in the order the walk reached the nodes, and byID the
	// footprint each names.
	order []ID
	byID  map[ID]Footprint
}

// Digest returns the digest of the source tree every footprint was derived
// from, which is the zero [Digest] where the tree could not be digested.
func (f Footprints) Digest() Digest { return f.digest }

// Tolerance returns the name of the registry tolerance coincidence was judged
// against.
func (f Footprints) Tolerance() string { return f.tolerance }

// Position returns the name of the predicate the corners were read from.
func (f Footprints) Position() string { return f.position }

// Len reports how many nodes have a derived footprint.
func (f Footprints) Len() int { return len(f.order) }

// Of returns the footprint of one node, and whether it has one. A node which
// references no loop covers nothing and has none.
func (f Footprints) Of(id ID) (Footprint, bool) {
	print, ok := f.byID[id]
	if !ok {
		return Footprint{}, false
	}
	print.digest = f.digest
	return print, true
}

// All iterates the footprints in the order the walk reached their nodes, which
// is deterministic, so anything built from it diffs against the last run's.
func (f Footprints) All() iter.Seq[Footprint] {
	return func(yield func(Footprint) bool) {
		for _, id := range f.order {
			print, ok := f.byID[id]
			if !ok {
				continue
			}
			print.digest = f.digest
			if !yield(print) {
				return
			}
		}
	}
}

// Derivation is what a derived-geometry pass is computed against: the two named
// decisions it needs, and the cache it may read and write.
//
// Both names are names and never numbers. Which predicate carries a position is
// vocabulary the consuming repository owns
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)), and
// how close two corners have to be to be one corner is a tolerance the project
// wrote down ([0012](docs/decisions/0012-tolerances-are-registry-data.md)).
// Both are part of the cache key, because a footprint judged against a different
// tolerance is a different answer to a different question.
type Derivation struct {
	// Tolerance is the name of the registry tolerance coincidence and planarity
	// are judged against.
	Tolerance string

	// Position is the name of the predicate the corners are read from.
	Position string

	// Cache is where derived geometry is looked up and written back. A nil
	// Cache derives everything every time, which is slower and never different.
	Cache *Cache
}

// Digest returns the digest of the source tree this graph was read from.
//
// It is computed on first use and remembered, because a graph is a reading of a
// tree at one moment and re-digesting it later would answer about a different
// moment than the one in hand.
//
// A graph which was not read from disk — one interpreted from trees a write
// substituted, which is what [Tx.Commit] validates against — has no digest and
// reports the zero one with an error. Digesting the files on disk for it would
// key a derivation by bytes it was not derived from, which is precisely the too-
// narrow digest that turns this cache into a stale-answer machine
// ([0009](docs/decisions/0009-derived-values-are-never-written-back.md)).
func (g *Graph) Digest() (Digest, error) {
	if g == nil {
		return Digest{}, DigestError{Err: errors.New("there is no graph")}
	}

	if !g.onDisk {
		return Digest{}, DigestError{
			Path: g.root,
			Err:  errors.New("this graph was interpreted from trees which are not what is on disk"),
		}
	}

	g.digested.Do(func() {
		g.digest, g.digestErr = DigestOf(g.root)
	})

	return g.digest, g.digestErr
}

// Derive computes the derived geometry of every node of the model which covers
// area: the plane figure, its area, its perimeter, its centroid, its bounding
// box, and which regions it lies inside.
//
// The cache is advisory in the strongest sense: deleting it changes what this
// returns by nothing at all, and changes only how long it takes. That holds
// because of two rules. The first is the key — a cached set is read back only
// under the digest of the tree it was derived from, so a changed tree misses.
// The second is that **only a clean derivation is cached**: a model which
// reported a diagnostic is stored nowhere and recomputed every run, so the
// diagnostics a run reports never depend on what a build output directory
// happens to hold.
//
// A node which references no loop is not derived and is absent from the result.
// A circuit group and a warranty cover no area, and a footprint of zero for them
// would be an answer to a question nobody asked.
//
// A node whose geometry could not be read — a ring which does not close, corners
// which are not in one plane, a tolerance the registry does not declare in the
// unit of the frame — is a diagnostic and no footprint, exactly as it is from
// [Topology.RegionOf], which is where the figure comes from.
//
// Membership is derived rather than read. Which regions a node lies inside is
// computed from where its corners are, by [Region.Containment], between every
// pair of regions one operation could be run over. A pair which could not be —
// two frames, two tolerances, two planes — is not a membership question and is
// passed over silently, because a refusal is about an operation somebody asked
// for and nobody asked for this one.
func (g *Graph) Derive(against Derivation) (Footprints, []Diagnostic) {
	if g == nil {
		return Footprints{}, nil
	}

	digest, err := g.Digest()
	if err != nil {
		against.Cache.record(func(s *CacheStats) { s.Errors++ })
	}

	key := Key{Digest: digest, Tolerance: against.Tolerance, Position: against.Position}

	if cached, ok := against.Cache.Lookup(key); ok {
		return cached, nil
	}

	prints, diags := g.derive(digest, against)

	if len(diags) == 0 {
		// A derivation which reported nothing is the only one worth keeping.
		// Storing one which reported something would make the diagnostics of a
		// run depend on whether an earlier run had populated a cache, which is
		// the one way a disposable artefact could change an answer.
		_ = against.Cache.Store(key, prints)
	}

	return prints, diags
}

// derive is [Graph.Derive] with the cache already consulted and missed.
func (g *Graph) derive(digest Digest, against Derivation) (Footprints, []Diagnostic) {
	prints := Footprints{
		digest:    digest,
		tolerance: against.Tolerance,
		position:  against.Position,
		byID:      make(map[ID]Footprint),
	}

	var diags []Diagnostic

	// shape is one node's derived figure, kept so that membership can be
	// computed between every pair of them once all of them are read.
	type shape struct {
		id     ID
		region Region
	}
	var shapes []shape

	for node := range g.nodes.All() {
		if node.id == "" {
			continue
		}
		if _, taken := prints.byID[node.id]; taken {
			// Two nodes holding one id is a diagnostic the load already
			// reported, and the id names the first of them everywhere else.
			continue
		}

		survey := positionSurvey(g, against.Tolerance, against.Position, g.boundaries.Vertices(node))

		region, regionDiags := g.topology.RegionOf(node, g.boundaries, survey)
		if len(regionDiags) > 0 {
			diags = append(diags, regionDiags...)
			continue
		}
		if !region.ready || region.Empty() {
			continue
		}

		// The measurement is a second pass over the same rings rather than a
		// second answer about them: both are computed by the same measurer from
		// the same survey, and each reads half of what a footprint is — the
		// figure from one, the figures measured off it from the other. Paying
		// for the assembly twice is exactly the cost this cache exists to stop
		// a caller paying on every run.
		measurement, measureDiags := g.topology.MeasureRegion(node, g.boundaries, survey)
		if len(measureDiags) > 0 {
			diags = append(diags, measureDiags...)
			continue
		}

		// The digest is not set here. It belongs to the set rather than to any
		// one footprint, and [Footprints.Of] and [Footprints.All] stamp it on
		// the way out — which is what keeps a footprint computed now and one
		// read back out of the cache identical rather than nearly so.
		print := Footprint{
			subject:   node.id,
			frame:     region.frame,
			unit:      region.unit,
			dimension: region.dimension,
			pieces:    region.pieces,
		}
		print.area, print.hasArea = measurement.Area()
		print.perimeter, print.hasPerimeter = measurement.Length()
		print.centroid, print.hasCentroid = measurement.Centroid()
		print.bounds, print.hasBounds = measurement.Bounds()

		prints.order = append(prints.order, node.id)
		prints.byID[node.id] = print
		shapes = append(shapes, shape{id: node.id, region: region})
	}

	for i, one := range shapes {
		var within []ID

		for j, other := range shapes {
			if i == j || one.region.frame != other.region.frame {
				continue
			}

			// The refusals are discarded rather than reported. Two regions which
			// are not operands of one operation are not members of each other,
			// and saying so once per pair would bury the diagnostics about the
			// model in diagnostics about questions nobody asked.
			state, refused := other.region.Containment(one.region)
			if len(refused) > 0 || state != ContainmentInside {
				continue
			}

			within = append(within, other.id)
		}

		slices.Sort(within)

		print := prints.byID[one.id]
		print.within = within
		prints.byID[one.id] = print
	}

	return prints, diags
}
