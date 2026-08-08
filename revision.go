// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"archive/tar"
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

// A model is a set of text files under version control (specification section
// 1), so the previous revision of one is a question for git rather than for the
// loader. Everything in this file is that question and nothing else: git is
// asked what the merge base is, asked for the files at it, and asked which
// commit touched what. The checks in review.go never see any of it.

// ErrNotARepository reports a directory which is not inside a git working tree.
//
// It is what [NotARepositoryError] unwraps to, so that a caller which only
// wants to know which of the things went wrong writes [errors.Is] and one which
// wants to say where writes [errors.As].
var ErrNotARepository = errors.New("not inside a git working tree")

// ErrGitMissing reports that git is not on the path.
var ErrGitMissing = errors.New("git is not on the path")

// NotARepositoryError is a directory which is not inside a git working tree,
// and so has no previous revision to be compared with.
type NotARepositoryError struct {
	// Dir is the directory, as it was given.
	Dir string
}

// Error implements [error].
func (e NotARepositoryError) Error() string {
	return fmt.Sprintf("%s is %s: a review reads the revision before this one out of git", e.Dir, ErrNotARepository)
}

// Unwrap implements the interface [errors.Is] and [errors.As] walk.
func (e NotARepositoryError) Unwrap() error { return ErrNotARepository }

// OutsideRepositoryError is a directory the working tree does not hold.
type OutsideRepositoryError struct {
	// Dir is the directory, as it was given.
	Dir string

	// Root is the top level it was measured against.
	Root string
}

// Error implements [error].
func (e OutsideRepositoryError) Error() string {
	return fmt.Sprintf("%s is not beneath %s: the repository holds no history for it", e.Dir, e.Root)
}

// Unwrap implements the interface [errors.Is] and [errors.As] walk.
//
// It is [ErrOutsideModel] because it is the same mistake the engine reports
// under that name — a path named as part of something which does not contain it
// — seen from the repository rather than from the model root.
func (e OutsideRepositoryError) Unwrap() error { return ErrOutsideModel }

// ArchiveEntryError is an archive entry whose name is not a path beneath the
// directory it was being written into.
//
// It carries the name exactly as the archive wrote it, rather than the path
// joining it would have produced: what is being reported is what the input
// said, and the join is the thing which never happened.
type ArchiveEntryError struct {
	// Name is the entry name, as the archive wrote it.
	Name string

	// Dest is the directory it was being written into.
	Dest string
}

// Error implements [error].
func (e ArchiveEntryError) Error() string {
	return fmt.Sprintf("archive entry %q does not name a path beneath %s", e.Name, e.Dest)
}

// Unwrap implements the interface [errors.Is] and [errors.As] walk.
func (e ArchiveEntryError) Unwrap() error { return ErrOutsideModel }

// RepositoryError is something git was asked and could not answer.
type RepositoryError struct {
	// Dir is the directory the command ran in.
	Dir string

	// Args are the arguments git was given, without the program name.
	Args []string

	// Stderr is what git said, trimmed of its trailing line feed.
	Stderr string

	// Cause is what stopped the command, which is an [exec.ExitError] for a
	// command which ran and refused.
	Cause error
}

// Error implements [error].
func (e RepositoryError) Error() string {
	written := fmt.Sprintf("git %s in %s: %v", strings.Join(e.Args, " "), e.Dir, e.Cause)
	if e.Stderr != "" {
		written += ": " + e.Stderr
	}
	return written
}

// Unwrap implements the interface [errors.Is] and [errors.As] walk.
func (e RepositoryError) Unwrap() error { return e.Cause }

// UnknownRevisionError is a revision the repository does not hold.
type UnknownRevisionError struct {
	// Revision is what was asked for, as it was written.
	Revision string
}

// Error implements [error].
func (e UnknownRevisionError) Error() string {
	return fmt.Sprintf("unknown revision %s: this repository holds no commit under that name", e.Revision)
}

// NoMergeBaseError is two revisions with no common ancestor.
type NoMergeBaseError struct {
	// Head and Base are the two revisions, as they were written.
	Head, Base string
}

// Error implements [error].
func (e NoMergeBaseError) Error() string {
	return fmt.Sprintf("no merge base between %s and %s: the two share no commit", e.Head, e.Base)
}

// ShallowHistoryError is a checkout whose history is cut off short of the merge
// base.
//
// It says what it needs rather than answering from what it has, which is the
// only safe answer: a shallow clone reports a merge base at the point its
// history was truncated, and a review against the wrong base reports every
// commit before the cut as though this change had made it.
type ShallowHistoryError struct {
	// Head and Base are the two revisions the review was asked to compare.
	Head, Base string

	// Boundary is the commit the history is truncated at, where the truncation
	// is what made the merge base wrong rather than missing.
	Boundary string
}

// Error implements [error].
func (e ShallowHistoryError) Error() string {
	written := fmt.Sprintf("the history of %s is shallow and does not reach its merge base with %s", e.Head, e.Base)
	if e.Boundary != "" {
		written += fmt.Sprintf(", which is cut off at %s", short(e.Boundary))
	}
	return written + ": fetch the full history — `git fetch --unshallow`, or `fetch-depth: 0` on actions/checkout"
}

// Repository is a git working tree a review reads two revisions out of.
//
// It is the git command line rather than a library. Reading the object database
// is a second implementation of a format this engine does not own, and the one
// thing a review must never do is disagree with git about what changed.
type Repository struct {
	// dir is the top level of the working tree, absolute.
	dir string

	// shallow is the commits the history is truncated at, which is empty for a
	// complete clone.
	shallow []string
}

// OpenRepository finds the working tree dir is inside.
//
// dir may be anywhere beneath the top level — the model root of a repository
// which holds more than a model is the ordinary case — and what comes back is
// rooted at the top level, because that is what every path git reports is
// relative to.
func OpenRepository(dir string) (*Repository, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}

	top, err := run(absolute, "rev-parse", "--show-toplevel")
	if err != nil {
		var refused RepositoryError
		if errors.As(err, &refused) && refused.Cause != ErrGitMissing {
			return nil, NotARepositoryError{Dir: dir}
		}
		return nil, err
	}

	repository := &Repository{dir: strings.TrimSpace(top)}

	// The truncation points are read from the file git keeps them in rather
	// than inferred from a walk: `--is-shallow-repository` answers whether the
	// file exists, and which commits are cut off is what says whether a merge
	// base can be trusted.
	gitDir, err := run(absolute, "rev-parse", "--absolute-git-dir")
	if err != nil {
		return nil, err
	}

	boundaries, err := os.ReadFile(filepath.Join(strings.TrimSpace(gitDir), "shallow"))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return nil, err
	}
	repository.shallow = append(repository.shallow, strings.Fields(string(boundaries))...)

	return repository, nil
}

// Dir is the top level of the working tree.
func (r *Repository) Dir() string { return r.dir }

// Shallow reports whether the clone's history is truncated.
func (r *Repository) Shallow() bool { return len(r.shallow) > 0 }

// Prefix is where path sits inside the repository, slash-separated, which is
// the spelling every path git reports uses.
//
// A path outside the working tree is [ErrOutsideModel] — the same answer the
// engine gives for a file outside the model root, because it is the same
// mistake: a review of a directory the repository does not hold has no history
// to read.
func (r *Repository) Prefix(dir string) (string, error) {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}

	relative, err := filepath.Rel(r.dir, absolute)
	if err != nil {
		return "", err
	}
	if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", OutsideRepositoryError{Dir: dir, Root: r.dir}
	}
	if relative == "." {
		return "", nil
	}

	return filepath.ToSlash(relative), nil
}

// Resolve is the full object name of a revision.
func (r *Repository) Resolve(revision string) (string, error) {
	name, err := run(r.dir, "rev-parse", "--verify", "--quiet", revision+"^{commit}")
	if err != nil {
		var refused RepositoryError
		if errors.As(err, &refused) && refused.Cause != ErrGitMissing {
			return "", UnknownRevisionError{Revision: revision}
		}
		return "", err
	}
	return strings.TrimSpace(name), nil
}

// MergeBase is the commit head and base last had in common — the revision a
// change is a change against, rather than whatever base happens to point at
// now.
//
// It is the merge base and not base itself because those two differ the moment
// anything else lands on base, and a review against the tip would report every
// change made by everybody else as part of this one.
//
// A truncated history is refused rather than answered from. Git reports a merge
// base at the point a shallow clone was cut off, which is a commit the two
// revisions never shared, and the review which followed would attribute the
// whole of the branch's ancestry to this change.
func (r *Repository) MergeBase(head, base string) (string, error) {
	found, err := run(r.dir, "merge-base", head, base)
	if err != nil {
		if r.Shallow() {
			return "", ShallowHistoryError{Head: head, Base: base}
		}
		return "", NoMergeBaseError{Head: head, Base: base}
	}

	name := strings.TrimSpace(found)
	if slices.Contains(r.shallow, name) {
		return "", ShallowHistoryError{Head: head, Base: base, Boundary: name}
	}

	return name, nil
}

// Extract writes the tree of one revision into dest, which must already exist.
//
// It is `git archive` read through the standard library rather than a second
// worktree, because a worktree is a mutation of the repository — it writes an
// administrative file, it can be left behind by a run which is killed, and it
// refuses outright when one of the same name survives an earlier run. Reading
// an archive changes nothing and leaves nothing.
//
// Only regular files and directories are written. A model is text files, and a
// symbolic link or a device node in an archive is either not part of one or is
// something a review should not be creating on the machine it runs on.
func (r *Repository) Extract(revision, dest string) error {
	// The arguments are built once and used for both the invocation and
	// anything said about it, so that a flag added here cannot go missing from
	// the error which reports the command that failed.
	args := []string{"archive", "--format=tar", revision}

	command := exec.Command("git", append([]string{"-C", r.dir}, args...)...)

	var complaint bytes.Buffer
	command.Stderr = &complaint

	out, err := command.StdoutPipe()
	if err != nil {
		return err
	}
	if err := command.Start(); err != nil {
		return started(r.dir, args, complaint.String(), err)
	}

	unpacked := unpack(tar.NewReader(out), dest)

	// The archive is drained whatever the unpacking said, so that git is never
	// left writing into a pipe nothing reads and the wait below returns.
	_, _ = io.Copy(io.Discard, out)

	if err := command.Wait(); err != nil {
		return RepositoryError{
			Dir:    r.dir,
			Args:   args,
			Stderr: strings.TrimRight(complaint.String(), "\n"),
			Cause:  err,
		}
	}

	return unpacked
}

// unpack writes an archive's regular files and directories beneath dest.
func unpack(archive *tar.Reader, dest string) error {
	root, err := filepath.Abs(dest)
	if err != nil {
		return err
	}

	for {
		header, err := archive.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}

		// An entry which climbs out of the destination is refused rather than
		// cleaned into one which does not: an archive is input, and an input
		// which tried is not one to guess the intent of.
		//
		// The name is judged before it is joined to anything, and
		// [filepath.IsLocal] is the whole of the test: it refuses an absolute
		// path, a name which begins with a parent reference, and one which
		// walks out through a parent part way along. Judging the entry rather
		// than the path it produced is what makes this a statement about what
		// the archive said, which is the thing which cannot be trusted —
		// checking where the join landed is the same test written after the
		// fact, and one refactor away from being written after the write.
		name := filepath.FromSlash(header.Name)
		if !filepath.IsLocal(name) {
			return ArchiveEntryError{Name: header.Name, Dest: root}
		}

		target := filepath.Join(root, name)

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o700); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
				return err
			}
			if err := extractFile(target, archive); err != nil {
				return err
			}
		}
	}
}

// extractFile copies one archive entry into a new file.
//
// The file is closed exactly once, and which error comes back says which thing
// went wrong. A copy which failed is reported as itself and the close after it
// can say nothing useful; a copy which succeeded has only put the bytes into
// the kernel, so a full disk or a filesystem which failed on flush is reported
// by the close or by nothing at all.
func extractFile(target string, src io.Reader) error {
	file, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}

	if _, err := io.Copy(file, src); err != nil {
		_ = file.Close()
		return err
	}

	return file.Close()
}

// Log is which commit most recently changed each file over a range of history.
//
// "The commit which introduced the change" is that one: the newest commit in
// the range which touched the file the change is in. It is the file rather than
// the form inside it, because a form has no identity git knows about — the same
// node moves down the file as the ones above it grow — and a commit named from
// a line number would name the wrong one the first time anything was inserted.
type Log struct {
	// commits is the newest commit which touched each path, keyed by the path
	// relative to the top level, slash-separated.
	commits map[string]Revision
}

// The separators the log is read back with.
//
// They are the ASCII record and unit separators rather than anything a person
// would type, because every field being read is free text: a commit subject may
// hold a tab, a newline is what separates the file names, and an author may be
// called anything at all.
const (
	logRecord = "\x1e"
	logField  = "\x1f"
)

// Log reads which commit last changed each file between two revisions.
//
// The range is exclusive of from and inclusive of to, which is the range a
// change is: the commits this branch added, and not the ones it was branched
// from. A merge inside the range contributes nothing of its own, because the
// commits it merged are in the range too and each says what it changed.
func (r *Repository) Log(from, to string) (*Log, error) {
	format := logRecord + "%H" + logField + "%an" + logField + "%aI" + logField + "%s"

	out, err := run(r.dir, "log", "--format="+format, "--name-only", "--no-renames", from+".."+to)
	if err != nil {
		return nil, err
	}

	log := &Log{commits: make(map[string]Revision)}

	// The commits arrive newest first, so the first one to claim a path is the
	// one which most recently changed it and every later claim is history.
	for _, record := range strings.Split(out, logRecord) {
		lines := strings.Split(strings.Trim(record, "\n"), "\n")
		if len(lines) == 0 || lines[0] == "" {
			continue
		}

		fields := strings.Split(lines[0], logField)
		if len(fields) < 4 {
			continue
		}

		commit := Revision{SHA: fields[0], Author: fields[1], Summary: fields[3]}
		if when, err := time.Parse(time.RFC3339, fields[2]); err == nil {
			commit.Date = when
		}

		for _, name := range lines[1:] {
			if name == "" {
				continue
			}
			if _, claimed := log.commits[name]; !claimed {
				log.commits[name] = commit
			}
		}
	}

	return log, nil
}

// Len is how many files the range touched.
func (l *Log) Len() int {
	if l == nil {
		return 0
	}
	return len(l.commits)
}

// Within is the log as seen by a model loaded at dir, whose place in the
// repository is prefix.
//
// The indirection is what lets one log attribute findings from both revisions.
// The merge base is read out of a directory somewhere else entirely, and the
// spans in it name that directory; prefix is what puts a path back where the
// repository holds it, so a file which was removed by the change is attributed
// to the commit which removed it rather than to nothing at all.
func (l *Log) Within(dir, prefix string) History {
	absolute, err := filepath.Abs(dir)
	if err != nil {
		absolute = dir
	}
	return rooted{log: l, dir: absolute, prefix: prefix}
}

// rooted is a [History] which resolves a span's path against the tree it was
// loaded from before looking it up.
type rooted struct {
	log    *Log
	dir    string
	prefix string
}

// Introduced implements [History].
func (r rooted) Introduced(name string) (Revision, bool) {
	if r.log == nil || name == "" {
		return Revision{}, false
	}

	absolute, err := filepath.Abs(name)
	if err != nil {
		return Revision{}, false
	}

	relative, err := filepath.Rel(r.dir, absolute)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Revision{}, false
	}

	commit, ok := r.log.commits[path.Join(r.prefix, filepath.ToSlash(relative))]

	return commit, ok
}

// run is one git command, with its output trimmed of nothing and its complaint
// kept.
func run(dir string, args ...string) (string, error) {
	command := exec.Command("git", append([]string{"-C", dir}, args...)...)

	var out, complaint bytes.Buffer
	command.Stdout = &out
	command.Stderr = &complaint

	if err := command.Run(); err != nil {
		return "", started(dir, args, complaint.String(), err)
	}

	return out.String(), nil
}

// started is the error for a command which would not run or which refused,
// telling the two apart so that a machine with no git installed is not reported
// as a repository which said no.
func started(dir string, args []string, complaint string, err error) error {
	if errors.Is(err, exec.ErrNotFound) {
		err = ErrGitMissing
	}
	return RepositoryError{
		Dir:    dir,
		Args:   args,
		Stderr: strings.TrimRight(complaint, "\n"),
		Cause:  err,
	}
}

// short is the abbreviated spelling of an object name.
func short(name string) string {
	if len(name) <= 12 {
		return name
	}
	return name[:12]
}
