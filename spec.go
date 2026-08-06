// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

// SpecVersion is the version of the entity format specification this engine
// implements, in the MAJOR.MINOR form SPEC.md section 10 defines.
//
// It is a fact about the engine rather than about any file it reads: models
// carry no version stamp, deliberately, so a consumer that needs to know which
// dialect of the format it is holding reads it from here — or, from the command
// line, out of the object `dfcad version` writes. That is the reader SPEC.md
// section 10 sends here, and it is why the constant is exported from the root
// package rather than kept unexported beside the loader.
//
// It moves under the rules SPEC.md section 10 states, and it moves independently
// of the version of the tool and of the version of the machine output contract.
// docs/versioning.md is the relationship between the three.
const SpecVersion = "1.2"
