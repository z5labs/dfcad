// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"crypto/sha1"
	"fmt"
)

// GlobalID is IFC's identifier for one rooted object: a 128-bit value written
// as the 22 characters of IFC's own base64 alphabet.
//
// It is derived rather than authored, and it is derived from the node's [ID]
// and nothing else
// ([0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md)).
// There is no `GlobalId` field anywhere in the format, which is what stops a
// second identifier per node from being hand-maintained beside the one which
// identifies the thing, and drifting out of step with it.
//
// Deriving it is what makes an export stable: two exports of a node nothing
// happened to carry the same GlobalID, so a receiving system sees an update
// rather than a delete and a re-create. Everything which makes that true is
// recorded — the pinned URL in the registry, the two derivation steps below —
// so anyone holding the registry can recompute a value and check this package's
// arithmetic with any UUIDv5 implementation.
//
// A [SemanticNode] label is not part of it. Renaming a room changes what is
// displayed and nothing a downstream system holds, which is the same
// arrangement [ID] and [SemanticNode.Label] already have
// ([0002](docs/decisions/0002-immutable-id-mutable-label.md)).
type GlobalID string

// String returns the 22 characters, which is the form an IFC file carries.
func (g GlobalID) String() string { return string(g) }

// globalIDLength is the character count IFC fixes for the encoded form: one
// group of two characters and five of four.
const globalIDLength = 22

// globalIDAlphabet is IFC's base64 alphabet, which is not the one RFC 4648
// fixes: the digits come first, and the last two characters are an underscore
// and a dollar rather than a plus and a slash.
//
// Substituting the standard alphabet would produce 22 characters which look
// like a GlobalId, decode to the right 128 bits under the wrong reader, and be
// a different identifier to every IFC implementation there is.
const globalIDAlphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_$"

// uuidURLNamespace is the URL namespace RFC 4122 fixes, and the root of every
// GlobalID this package derives.
//
// It is a constant of the specification rather than a value this project chose,
// which is the point of using it: the project namespace is
// `UUIDv5(this, the pinned URL)`, so the only thing anybody has to record is a
// URL they already own, and the namespace UUID falls out of it rather than
// being a magic value pasted into a file.
//
// **Changing the URL pinned in the registry changes every GlobalID in the
// model**, and so does changing anything else on the path from an [ID] to the
// 22 characters — this constant, the [uuidV5] arithmetic, or the alphabet
// above. It is one line of a registry file and it is not a routine edit: every
// downstream system holding previously exported identifiers sees the whole
// model deleted and re-created, and every external record keyed on a GlobalID —
// a linked issue, a facility management record, an approved submission — is
// orphaned with nothing in the new export saying what it used to be. The old
// values cannot be recovered from the model, only recomputed from the old URL.
// Both are versioned parts of the export contract, and altering either is a
// re-identification of the whole model rather than a bug fix.
var uuidURLNamespace = uuid{
	0x6b, 0xa7, 0xb8, 0x11, 0x9d, 0xad, 0x11, 0xd1,
	0x80, 0xb4, 0x00, 0xc0, 0x4f, 0xd4, 0x30, 0xc8,
}

// DeriveGlobalID derives the GlobalID of id within the namespace pinned at url,
// per [0004](docs/decisions/0004-globalid-derives-from-a-pinned-namespace.md).
//
// The derivation is two steps and no state:
//
//  1. The project namespace UUID is `UUIDv5(RFC 4122 URL namespace, url)`.
//  2. The node UUID is `UUIDv5(that namespace, id)`, over the whole
//     `namespace:local` id as it is written, encoded as UTF-8.
//
// and the GlobalID is those 128 bits in IFC's 22-character encoding.
//
// It is a pure function of its two arguments. The same url and the same id
// yield the same characters in every run, every process and on every machine,
// because nothing here reads a clock, a random source, the environment or
// anything else which is not one of the two arguments — which is the whole
// reason the value is derived rather than minted and stored.
//
// url is not checked here. Which URL a model pins is a property of the model
// and is validated where it is read, and a derivation which refused an
// unexpected URL would be a derivation which could not reproduce a value
// exported by an older model — the one thing it exists to do.
//
// [Registry.GlobalID] is this with the url read from the model's project
// declaration, and is what a caller holding a loaded model wants.
func DeriveGlobalID(url string, id ID) GlobalID {
	return uuidV5(globalIDNamespace(url), string(id)).globalID()
}

// DeriveGlobalIDNamespace returns the project namespace UUID derived from the
// pinned url, in the canonical 8-4-4-4-12 text a UUID is written in.
//
// It exists so the first half of the derivation can be checked without running
// the second: the value it returns is `uuid5(NAMESPACE_URL, url)` to any UUIDv5
// implementation, so a reader of the registry can reproduce it from the URL
// recorded there and see that this package started from the same 128 bits they
// did.
func DeriveGlobalIDNamespace(url string) string {
	return globalIDNamespace(url).String()
}

// GlobalID returns the GlobalID of id under the URL the model's project
// declaration pins, and whether the model pinned one.
//
// A model with no project declaration, and a model whose declaration does not
// pin a URL a GlobalID could derive from, are both load errors already — so the
// false here is what a caller holding the registry of a load which failed gets.
// It is the answer rather than a derivation from the empty URL, which would be
// 22 characters indistinguishable from any other GlobalID, stable across runs,
// and a stable identifier for a model nobody pinned. A registry which recorded
// a project whose `globalid-namespace` was not a URL holds the empty string for
// it, so absent and unusable are the same case here on purpose.
//
// Any [ID] derives, including a geometric node's. What IFC gives a GlobalId to
// is IFC's question, and answering it here would be this package holding an
// opinion about an exporter which does not exist yet
// ([0010](docs/decisions/0010-the-engine-carries-no-domain-vocabulary.md)).
func (r *Registry) GlobalID(id ID) (GlobalID, bool) {
	project, ok := r.Project()
	if !ok || project.GlobalIDNamespace == "" {
		return "", false
	}
	return DeriveGlobalID(project.GlobalIDNamespace, id), true
}

// globalIDNamespace is step one of the derivation: the project namespace UUID
// the pinned URL stands for.
func globalIDNamespace(url string) uuid { return uuidV5(uuidURLNamespace, url) }

// uuid is a 128-bit RFC 4122 identifier, in the byte order it is written in.
//
// It is unexported because it is an intermediate of the derivation rather than
// a thing this package deals in. What a caller holds is an [ID], which is the
// identifier a person handles, or a [GlobalID], which is the one IFC does.
type uuid [16]byte

// uuidV5 derives the name-based UUID of name within namespace, per RFC 4122
// section 4.3.
//
// SHA-1 is what version 5 is defined as and is therefore not a choice this
// package makes. It is unsuitable for anything adversarial and there is nothing
// adversarial here: this is a name-to-name mapping with no attacker in the
// model, and the algorithm is fixed by interoperability rather than by
// cryptographic merit. It cannot be swapped for something modern without
// changing every value ever exported.
func uuidV5(namespace uuid, name string) uuid {
	// Neither write is examined, and both are spelled the same way so that the
	// reason reads once: [hash.Hash] documents Write as never returning an
	// error, so there is nothing here a caller could be told.
	sum := sha1.New()
	sum.Write(namespace[:])
	sum.Write([]byte(name))

	var derived uuid
	copy(derived[:], sum.Sum(nil))

	// The version and variant bits are overwritten rather than hashed in, which
	// is what makes a version 5 UUID say that it is one.
	derived[6] = derived[6]&0x0f | 0x50
	derived[8] = derived[8]&0x3f | 0x80

	return derived
}

// globalID encodes the 128 bits in IFC's 22-character form.
//
// The grouping is IFC's and is not a base64 of the whole number: the first byte
// is written as two characters, and the remaining fifteen as five groups of
// three bytes in four characters each. Two characters hold twelve bits where
// one byte needs eight, so the leading character of a GlobalID is never above
// `3` — and encoding the 128 bits as one number instead would produce a
// different string for every value but the smallest.
func (u uuid) globalID() GlobalID {
	encoded := make([]byte, 0, globalIDLength)

	encoded = appendBase64(encoded, uint32(u[0]), 2)
	for i := 1; i < len(u); i += 3 {
		group := uint32(u[i])<<16 | uint32(u[i+1])<<8 | uint32(u[i+2])
		encoded = appendBase64(encoded, group, 4)
	}

	return GlobalID(encoded)
}

// appendBase64 writes value as exactly digits characters of IFC's alphabet,
// most significant first.
func appendBase64(dst []byte, value uint32, digits int) []byte {
	at := len(dst)
	dst = append(dst, make([]byte, digits)...)

	for i := digits - 1; i >= 0; i-- {
		dst[at+i] = globalIDAlphabet[value%64]
		value /= 64
	}

	return dst
}

// String writes the UUID in the canonical 8-4-4-4-12 text, which is the form
// anybody checking the derivation against another implementation has.
func (u uuid) String() string {
	return fmt.Sprintf("%x-%x-%x-%x-%x", u[0:4], u[4:6], u[6:8], u[8:10], u[10:16])
}
