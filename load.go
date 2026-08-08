// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package dfcad

import (
	"bytes"
	"io"
	"io/fs"
	"iter"
	"os"
	"path/filepath"
	"unicode/utf8"

	sexpr "github.com/z5labs/sexpr-go"
)

// Extension is the file extension a directory walk treats as an entity file.
//
// It is compared byte-wise, like everything else the format compares, so a
// file named Site.DFC is not an entity file. A path named explicitly is read
// whatever its extension; the extension decides only what a walk picks up.
const Extension = ".dfc"

// byteOrderMark is the UTF-8 encoding of U+FEFF.
var byteOrderMark = []byte{0xef, 0xbb, 0xbf}

// Load reads root into spanned trees, one file at a time.
//
// root may name a single file, which is read whatever its extension, or a
// directory, beneath which every file whose extension is [Extension] is read
// and everything else is ignored. A directory holding no entity file yields
// nothing and is not an error.
//
// Files arrive in a deterministic order — the lexical order of their paths,
// directory by directory — so walking the same tree twice yields the same
// files in the same sequence, and any output built from a walk diffs against
// the last one.
//
// A file that fails to load yields a nil file and its error, and the walk
// carries on to the next one. One unloadable file therefore does not hide what
// is wrong with the rest, which is what makes a single pass over a tree worth
// running. A caller that wants to stop at the first failure breaks out of the
// range.
//
// Loading is streaming: a file is opened, read, spanned and yielded before the
// next one is opened, so a tree costs the largest single file rather than the
// sum of all of them.
func Load(root string) iter.Seq2[*File, error] {
	return func(yield func(*File, error) bool) {
		for path, err := range Walk(root) {
			if err != nil {
				if !yield(nil, err) {
					return
				}
				continue
			}
			if !yield(LoadFile(path)) {
				return
			}
		}
	}
}

// Walk yields the path of every entity file under root, in the order [Load]
// reads them.
//
// root may name a single file, which is yielded whatever its extension, or a
// directory, beneath which every file whose extension is [Extension] is
// yielded and everything else is ignored. A directory holding no entity file
// yields nothing and is not an error.
//
// A path that cannot be reached at all — the root does not exist, a directory
// beneath it cannot be read — is yielded with the error that stopped it, and
// the walk carries on. The path is still reported alongside the error, so a
// caller collecting one result per file has something to key it on.
//
// It is separate from [Load] because reading a file is not the only thing a
// caller does with one. Formatting reads the bytes itself, so that it can
// compare them against what the canonical printer would write; it wants the
// same enumeration and not the same reading.
func Walk(root string) iter.Seq2[string, error] {
	return walkExtension(root, Extension)
}

// walkExtension is [Walk] over whichever extension is being enumerated.
//
// It is one function rather than one per format because "which files of this
// tree are mine" has one answer whatever the extension, and two copies of it
// would disagree the first time one of them learned something about a tree the
// other did not. [WalkObservations] is the other caller.
func walkExtension(root, extension string) iter.Seq2[string, error] {
	return func(yield func(string, error) bool) {
		info, err := os.Stat(root)
		if err != nil {
			yield(root, err)
			return
		}

		if !info.IsDir() {
			yield(root, nil)
			return
		}

		// The error WalkDir returns is only ever the one the callback gave it,
		// and the callback yields everything it has rather than returning it.
		_ = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				if !yield(path, err) {
					return fs.SkipAll
				}
				return nil
			}
			if entry.IsDir() || filepath.Ext(path) != extension {
				return nil
			}
			if !yield(path, nil) {
				return fs.SkipAll
			}
			return nil
		})
	}
}

// source is one file of a walked tree: the tree it parsed into, or the
// diagnostic which says why it did not.
//
// It exists so that reading a tree is written once and every pass which
// interprets one reads the same thing. A pass which walked and parsed for
// itself would be a second answer to "which files are in this model", and the
// two would disagree the first time one of them learned about a file the other
// did not.
type source struct {
	// path is the file the walk reached, which is the Path of every Position
	// read out of it.
	path string

	// file is the tree it parsed into, and is nil where reading it failed.
	file *File

	// content is the bytes the file held, and is nil where they could not be
	// read at all. It is what a digest of the tree is computed from
	// ([Graph.Digest]), so that the digest describes the bytes a graph was
	// actually built from rather than whatever is on disk by the time somebody
	// asks. A caller which does not need it drops it before keeping the source,
	// since a whole tree's bytes held beside a whole tree's parsed forms is
	// twice the memory for a value only one pass reads.
	content []byte

	// diag is why it failed, and is meaningful only where file is nil.
	diag Diagnostic
}

// readTree yields every entity file beneath root, in walk order, parsed.
//
// Parsing happens as each file is reached rather than up front, so a pass which
// reads a tree once holds one file at a time. A caller which reads the same
// tree with several passes collects this into a slice instead and hands the
// same trees to each of them, which is what [LoadGraph] does: parsing a model
// four times to ask four questions about it is four times the reading for one
// answer.
func readTree(root string) iter.Seq[source] {
	return func(yield func(source) bool) {
		for path, err := range Walk(root) {
			if err != nil {
				if !yield(source{path: path, diag: diagnose(path, err)}) {
					return
				}
				continue
			}

			// The bytes are read here rather than by LoadFile so that a caller
			// which digests the tree digests what this pass read, and not the
			// file as it stands whenever it gets round to asking.
			content, err := os.ReadFile(path)
			if err != nil {
				if !yield(source{path: path, diag: diagnose(path, err)}) {
					return
				}
				continue
			}

			file, err := parse(path, content)
			if err != nil {
				if !yield(source{path: path, content: content, diag: diagnose(path, err)}) {
					return
				}
				continue
			}

			if !yield(source{path: path, file: file, content: content}) {
				return
			}
		}
	}
}

// interpret feeds every parsed file of a tree to one pass, in walk order,
// reporting the files which could not be read as diagnostics of that pass.
//
// The report is here rather than in each pass so that a file which does not
// parse says so once per pass which needed it and always in the same words.
func interpret(r *reader, sources iter.Seq[source], file func(*File)) {
	for src := range sources {
		if src.file == nil {
			r.add(src.diag)
			continue
		}
		file(src.file)
	}
}

// LoadFile reads one file into a spanned tree, whatever its extension.
//
// This is the path a file named explicitly — on a command line, in a test —
// takes. [Load] is the same thing over a tree.
func LoadFile(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()

	return Parse(path, f)
}

// Parse reads S-expression source into a spanned tree, reporting positions
// against path.
//
// It is what [LoadFile] does once the file is open, exported for a caller
// holding source that did not come from a file it can name.
func Parse(path string, r io.Reader) (*File, error) {
	src, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return parse(path, src)
}

// parse is the whole of loading, once the bytes are in hand.
func parse(path string, src []byte) (*File, error) {
	lines := newLineIndex(path, src)

	if bytes.HasPrefix(src, byteOrderMark) {
		return nil, ByteOrderMarkError{Position: lines.at(0)}
	}
	if i := firstInvalidUTF8(src); i >= 0 {
		return nil, EncodingError{Position: lines.at(i), Byte: src[i]}
	}

	parsed, err := sexpr.Parse(bytes.NewReader(src))
	if err != nil {
		return nil, parseError(lines, err)
	}

	spans, err := newSpanner(lines, src)
	if err != nil {
		return nil, err
	}

	file := &File{Path: path, Comments: spans.comments(parsed.Comments)}
	for _, node := range parsed.Nodes {
		file.Nodes = append(file.Nodes, spans.node(node))
	}

	return file, nil
}

// firstInvalidUTF8 returns the index of the first byte of src which begins no
// valid encoding, or -1 when src is valid UTF-8 throughout.
//
// The tokenizer rejects most malformed input on its own, but not all of it: a
// byte inside a string literal or a comment is text to it. Checking the whole
// file up front means the answer does not depend on where in the file the byte
// happened to fall.
func firstInvalidUTF8(src []byte) int {
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRune(src[i:])
		if r == utf8.RuneError && size <= 1 {
			return i
		}
		i += size
	}
	return -1
}

// spanner gives every parsed node the extent of the source text it was written
// in.
//
// The parser records where a node starts and not where it ends, so the end has
// to come from somewhere. It comes from a second pass over the same bytes with
// sexpr's own tokenizer, whose tokens already say how long every lexeme is and
// which parenthesis closes which. Scanning for the end here instead would mean
// a hand-written lexer in this package, duplicating rules — string escapes,
// nested block comments, where a number ends — that would then have to be kept
// in step with the one the parser actually uses.
type spanner struct {
	lines lineIndex

	// toks holds one entry per token, in source order.
	toks []token

	// index maps a token's reported position to its place in toks, which is
	// how a parsed node — which carries only that position — finds its token.
	index map[sexpr.Pos]int

	// closes maps the index of a left parenthesis to the index of the right
	// parenthesis which closes it.
	closes map[int]int
}

// token is as much of a token as spanning needs: where its source text begins
// and where it ends. Which token it was has already been used, by the time one
// of these exists, to pair the parentheses.
type token struct {
	start int
	end   int
}

// newSpanner tokenizes src and indexes the result.
func newSpanner(lines lineIndex, src []byte) (*spanner, error) {
	s := &spanner{
		lines:  lines,
		index:  make(map[sexpr.Pos]int),
		closes: make(map[int]int),
	}

	var open []int
	for tok, err := range sexpr.Tokenize(bytes.NewReader(src)) {
		if err != nil {
			return nil, parseError(lines, err)
		}

		start := lines.offsetOf(tok.Pos)
		end := start + len(tok.Value)
		if tok.Type == sexpr.TokenString {
			// A string token's value is the text between the quotes, escapes
			// as written. The quotes are the only delimiters any token type
			// leaves out of its value, so this is the one correction.
			end += len(`""`)
		}

		switch tok.Type {
		case sexpr.TokenLParen:
			open = append(open, len(s.toks))
		case sexpr.TokenRParen:
			if n := len(open); n > 0 {
				s.closes[open[n-1]] = len(s.toks)
				open = open[:n-1]
			}
		}

		s.index[tok.Pos] = len(s.toks)
		s.toks = append(s.toks, token{start: start, end: end})
	}

	return s, nil
}

// node spans one parsed datum and, recursively, everything written inside it.
func (s *spanner) node(datum sexpr.Node) *Node {
	out := &Node{Datum: datum}

	pos, ok := posOf(datum)
	if !ok {
		// The node interface is sealed by the parser, so this is unreachable
		// short of a new node type arriving there. Give it an empty span at
		// the start of the file rather than a wrong one.
		out.Span = Span{Start: s.lines.at(0), End: s.lines.at(0)}
		return out
	}

	i, ok := s.index[pos]
	if !ok {
		// Likewise unreachable: every datum began at some token. An empty span
		// at the reported position is at least honest about where it is.
		at := s.lines.position(pos)
		out.Span = Span{Start: at, End: at}
		return out
	}

	switch datum := datum.(type) {
	case sexpr.List:
		closing, ok := s.closes[i]
		if !ok {
			// Only possible for unbalanced input, which the parser rejected
			// before spanning began.
			closing = i
		}
		out.Span = Span{Start: s.lines.at(s.toks[i].start), End: s.lines.at(s.toks[closing].end)}

		for _, element := range datum.Elements {
			out.Children = append(out.Children, s.node(element))
		}
		if datum.Tail != nil {
			out.Children = append(out.Children, s.node(datum.Tail))
		}
		out.Comments = s.comments(datum.Comments)

	case sexpr.Quote:
		// A quote is its shorthand plus the datum it applies to, and the
		// shorthand is not always one byte, so the end comes from the datum.
		quoted := s.node(datum.Datum)
		out.Span = Span{Start: s.lines.at(s.toks[i].start), End: quoted.Span.End}
		out.Children = []*Node{quoted}

	default:
		out.Span = Span{Start: s.lines.at(s.toks[i].start), End: s.lines.at(s.toks[i].end)}
	}

	return out
}

// comments spans the comments a file or a list holds.
func (s *spanner) comments(cs []*sexpr.Comment) []*Comment {
	if len(cs) == 0 {
		return nil
	}

	out := make([]*Comment, 0, len(cs))
	for _, c := range cs {
		start := s.lines.position(c.Pos)

		// The comment's own token is where its length comes from. Falling back
		// to its text costs nothing and is right for every comment the parser
		// can produce, since a comment's text is its source verbatim.
		end := s.lines.at(start.Offset + len(c.Text))
		if i, ok := s.index[c.Pos]; ok {
			end = s.lines.at(s.toks[i].end)
		}

		out = append(out, &Comment{Text: c.Text, Span: Span{Start: start, End: end}})
	}

	return out
}

// posOf returns where the parser said a datum begins.
//
// The parser's node interface is sealed, so this switch is exhaustive over
// everything it can produce; ok reports whether it stayed that way.
func posOf(datum sexpr.Node) (sexpr.Pos, bool) {
	switch datum := datum.(type) {
	case sexpr.Bool:
		return datum.Pos, true
	case sexpr.Float:
		return datum.Pos, true
	case sexpr.Int:
		return datum.Pos, true
	case sexpr.List:
		return datum.Pos, true
	case sexpr.Nil:
		return datum.Pos, true
	case sexpr.Quote:
		return datum.Pos, true
	case sexpr.String:
		return datum.Pos, true
	case sexpr.Symbol:
		return datum.Pos, true
	}
	return sexpr.Pos{}, false
}
