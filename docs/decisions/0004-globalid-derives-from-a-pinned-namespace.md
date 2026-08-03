# 0004. GlobalId derives from a pinned namespace

**Status:** Accepted

## Context

IFC requires every rooted object to carry a `GlobalId`: a 128-bit identifier, presented as
22 characters in IFC's own base64 alphabet. Anything that ever exports to IFC has to
produce one for every semantic node, and the value has to be *stable* — the same node must
produce the same `GlobalId` on every export, or each export looks to the receiving system
like a model in which every object was deleted and a new one created.

Storing a `GlobalId` as an authored field is the obvious approach and the wrong one. It is
a second identifier for every node, hand-maintained, never read by a human, and free to
drift out of step with the id that actually identifies the thing. It also has to be minted
at authoring time, which means the authoring path needs a UUID generator and the review
diff carries 22 characters of noise per node that no reviewer can check.

Deriving it removes the field entirely — but only if the derivation is pinned. A
derivation that depends on anything unrecorded is worse than a stored field, because it
silently produces different values in different circumstances and nothing in the model
says why.

## Decision

**`GlobalId` is derived, never authored.** It is a function of the node's id
(see [0002](./0002-immutable-id-mutable-label.md)) and a project namespace UUID, and it is
recomputed on every export rather than stored.

**The derivation is recorded here in full:**

1. The project namespace UUID is `UUIDv5(RFC 4122 URL namespace, U)`, where `U` is a URL
   declared in the registry and the URL namespace is the constant
   `6ba7b811-9dad-11d1-80b4-00c04fd430c8` fixed by RFC 4122.
2. The node UUID is `UUIDv5(project namespace UUID, id)`, where `id` is the node's full
   `namespace:local` id, in its canonical form, encoded as UTF-8.
3. The `GlobalId` is that 128-bit value encoded in IFC's 22-character base64
   representation.

**The namespace is derived from a URL pinned in the registry, not a pasted magic UUID.**
The registry declares a URL the project controls — a repository URL, a domain the
organisation owns — and the namespace UUID falls out of it by step 1. Nobody types a UUID,
and the value can be recomputed from the recorded URL by anyone who wants to check it.

## Consequences

There is no `GlobalId` field to author, review, get wrong, or leave stale. The node id is
the only identifier a person handles.

Exports are stable and idempotent. Two exports of an unchanged node produce byte-identical
`GlobalId`s, so a receiving system sees an update rather than a delete-and-recreate, and
diffing two exports means something.

The value is reproducible outside the engine. Given the pinned URL and a node id, anyone
can compute the same `GlobalId` with a UUIDv5 implementation and check the engine's
arithmetic. There is no hidden state.

Because the derivation runs off the node id, [0002](./0002-immutable-id-mutable-label.md)
is load-bearing for it: an id that changed would change the `GlobalId`, which is precisely
the breakage the derivation exists to avoid.

Uniqueness of the `GlobalId` follows from uniqueness of the id within the model, so the
engine does not need a second uniqueness check — and a collision would be a UUIDv5 hash
collision, not a modelling mistake.

## Cost

The pinned URL becomes load-bearing configuration. It is a value nobody looks at, in a file
nobody edits, whose only symptom of being wrong is that a downstream system starts treating
every object as new.

The derivation is also a compatibility surface. It is UUIDv5, which is SHA-1 based; SHA-1
is unsuitable for anything adversarial. That is acceptable here — this is a name-to-name
mapping with no attacker in the model — but it means the choice is fixed by
interoperability rather than by cryptographic merit, and it cannot be swapped for something
modern without changing every value.

There is no way to override a single node's `GlobalId` to match a value some other system
already holds. A model that has to interoperate with an existing IFC dataset cannot adopt
its identifiers.

## What would reverse it

A requirement to round-trip `GlobalId`s that were minted elsewhere — importing an existing
IFC dataset and having to preserve its identifiers exactly — would force an authored,
stored field, at least as an optional override alongside the derivation.

**Changing the pinned URL changes every `GlobalId` in the model.** This is the consequence
worth stating plainly, because the change looks like editing one line of a registry file.
Every downstream system holding previously exported identifiers sees the entire model
deleted and re-created; any external record keyed on a `GlobalId` — a linked issue, a
facility management record, an approved submission — is orphaned, and nothing in the new
export says what it used to be. The old values cannot be recovered from the model, only
recomputed from the old URL, which is why the URL is pinned in a file rather than being
implicit anywhere.

The same holds for changing any other step of the derivation. It is a versioned part of the
export contract, and altering it is a re-identification of the whole model, not a bug fix.
