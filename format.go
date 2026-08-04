// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"os"
	"path/filepath"
)

// Formatter rewrites entity files into the canonical form [Print] writes.
//
// It exists so that formatting stops being something anybody discusses. A tree
// that has been through a formatter has one spelling of every model, so a diff
// over it is about the change rather than about whitespace, and the command
// line interface writing a file and a person editing one by hand converge
// instead of developing competing house styles.
//
// Every file is treated on its own. A file that does not parse is reported and
// left exactly as it was, and the files after it are still formatted: a pass
// over a tree that stopped at the first unparseable file would be one nobody
// could run on the tree they were fixing.
//
// The zero value writes nothing, which makes a check the safe default: a
// caller that has not said it wants the tree changed does not change it.
type Formatter struct {
	// Rewrite reports whether a file that is not in canonical form is replaced
	// by it.
	//
	// The replacement is atomic. The complete new contents are written to a
	// temporary file in the same directory, flushed to durable storage, and
	// renamed over the target, so an interruption at any point leaves the
	// original file intact rather than a prefix of the new one.
	Rewrite bool

	// Diff reports whether a file that is not in canonical form carries a
	// unified diff of what would change.
	//
	// It is independent of Rewrite: the diff describes the same replacement
	// whether or not the replacement is performed.
	Diff bool
}

// Formatted is what formatting did to one file, or, where Err says a path
// could not be reached at all, what stopped it.
type Formatted struct {
	// Path is the file, exactly as the walk reached it. On a path that could
	// not be reached it is that path, which need not name a file: a directory
	// that cannot be read and a name that is not there are both reported here.
	Path string

	// Changed reports whether what is on disk differs from its canonical form.
	// A file that failed to be read or parsed reports false: nothing is known
	// about a file that could not be printed.
	Changed bool

	// Written reports whether the file was replaced by its canonical form.
	Written bool

	// Diff is the unified diff from what is on disk to its canonical form, or
	// empty when the file is already canonical or no diff was asked for.
	Diff string

	// Diagnostics are the problems found in the file's contents: it does not
	// parse, it is not UTF-8, it begins with a byte order mark. They are for
	// whoever wrote the file.
	Diagnostics []Diagnostic

	// Err is the failure that stopped the file being read or replaced. It is
	// for the caller: a file it has no permission to read is not a mistake
	// anybody made in the file.
	Err error
}

// Failed reports whether the file was neither confirmed canonical nor made so.
func (f Formatted) Failed() bool {
	return f.Err != nil || len(f.Diagnostics) > 0
}

// Format formats every entity file under each of paths.
//
// A path may name a single file, which is formatted whatever its extension, or
// a directory, beneath which every file whose extension is [Extension] is
// formatted. Several of each may be given, and a file reached twice — named
// twice, or named as well as walked into — is formatted once.
//
// Results come back one per file, in the order the paths were given and, under
// a directory, in the lexical order [Walk] yields. The order is therefore the
// same for every run over the same tree, which is what makes two runs' output
// worth diffing.
//
// A path the walk cannot reach at all comes back as a result too, carrying the
// error that stopped it. Its Path is then whatever could not be reached, which
// is a directory rather than a file when a directory is what could not be
// read, and it names something that does not exist when that is what was
// asked for. Leaving those out would mean a caller counting results could not
// tell a tree it formatted from a tree it mostly failed to open.
//
// Formatting is idempotent: a second pass over what a first one wrote reports
// every file unchanged, because canonical form is the fixed point the printer
// writes to.
func (f Formatter) Format(paths ...string) []Formatted {
	var out []Formatted

	seen := make(map[string]bool)
	for _, root := range paths {
		for path, err := range Walk(root) {
			if err != nil {
				out = append(out, Formatted{Path: path, Err: err})
				continue
			}

			// Two spellings of one file are one file. Formatting it twice
			// would be harmless — the second pass would find it canonical —
			// but reporting it twice is a result nobody can total up.
			clean := filepath.Clean(path)
			if seen[clean] {
				continue
			}
			seen[clean] = true

			out = append(out, f.format(path))
		}
	}

	return out
}

// format formats one file.
func (f Formatter) format(path string) Formatted {
	out := Formatted{Path: path}

	src, err := os.ReadFile(path)
	if err != nil {
		out.Err = err
		return out
	}

	file, err := parse(path, src)
	if err != nil {
		out.Diagnostics = []Diagnostic{diagnose(path, err)}
		return out
	}

	var want bytes.Buffer
	if err := Print(&want, file); err != nil {
		out.Err = err
		return out
	}

	if bytes.Equal(src, want.Bytes()) {
		return out
	}
	out.Changed = true

	if f.Diff {
		out.Diff = unified(path, src, want.Bytes())
	}

	if f.Rewrite {
		if err := replace(path, want.Bytes()); err != nil {
			out.Err = err
			return out
		}
		out.Written = true
	}

	return out
}

// replace writes src over path, atomically.
//
// The complete contents go to a temporary file in the same directory, which is
// flushed to durable storage and then renamed over the target. A rename within
// a directory is atomic, so an observer sees either the old file or the new
// one and never a prefix of the new one; an interruption at any point before
// the rename leaves the original untouched and at worst a temporary file
// behind.
func replace(path string, src []byte) error {
	name, err := stage(path, src)
	if err != nil {
		return err
	}

	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return WriteError{Path: path, Err: err}
	}

	return nil
}

// stage writes src to a temporary file beside path and returns its name,
// leaving path itself untouched.
//
// It is the whole of a replacement but the rename, which is what makes a change
// spanning several files all-or-nothing: every file's new contents are prepared
// and flushed before any of them is renamed into place, so the renames — the
// only steps which change what a reader sees — happen once nothing is left that
// can fail for an ordinary reason
// ([0016](docs/decisions/0016-writes-are-all-or-nothing.md)).
//
// The temporary file is created in the target's own directory rather than in
// the system temporary directory, because a rename across filesystems is not a
// rename at all — it is a copy, and a copy is exactly the partial write this
// avoids.
//
// That directory is created where it is not there, which is what lets a routing
// rule name a file in a directory the model does not hold yet: `entities/`
// exists because somebody wrote a file into it, and the first node routed to
// `entities/levels/level-1.dfc` is the one which has to make the directory. A
// change which is then rolled back leaves the directory behind, empty, which a
// walk of the model steps over and a load treats as nothing at all.
func stage(path string, src []byte) (string, error) {
	// Not 0o777: the umask is the only thing which would narrow that, and a
	// permissive one would leave a world-writable directory inside a model
	// somebody is version controlling. A directory holding entity files needs to
	// be readable and searchable by whoever reads them, and writable by nobody
	// else.
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", WriteError{Path: path, Err: err}
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return "", WriteError{Path: path, Err: err}
	}

	name := tmp.Name()
	if err := flush(tmp, src); err != nil {
		os.Remove(name)
		return "", WriteError{Path: path, Err: err}
	}

	// A temporary file is created private to its owner, and the file it
	// replaces was not. Carrying the mode over keeps a formatted file as
	// readable as the one it was written from.
	if info, err := os.Stat(path); err == nil {
		if err := os.Chmod(name, info.Mode().Perm()); err != nil {
			os.Remove(name)
			return "", WriteError{Path: path, Err: err}
		}
	}

	return name, nil
}

// flush writes src to f and forces it to durable storage before closing it.
//
// Without the sync, a rename can be recorded while the bytes it names are
// still in a write-back cache, which on a crash leaves the new name pointing
// at a file that was never written.
func flush(f *os.File, src []byte) error {
	if _, err := f.Write(src); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
