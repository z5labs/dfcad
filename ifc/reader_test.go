// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// This file is a reader, and it shares nothing with the writer beside it.
//
// It exists because "the file is valid" asserted by the code which produced it
// is the writer agreeing with itself, and a writer which emits an unbalanced
// quote, an attribute too few or a reference to an instance it never wrote
// agrees with itself perfectly. So the checks in write_test.go go through this
// instead: an independent scanner over the bytes, which knows part 21's
// grammar and nothing at all about how those bytes came to be.
//
// Nothing here calls into encode.go or write.go, and it must stay that way. A
// reader which reused the writer's escaping, its real formatting or its
// attribute tables would stop being a second opinion the moment one of them
// was wrong.

// read parses an exchange file into its header entities and its data
// instances.
func read(source string) (*file, error) {
	scanner := &scanner{source: source}
	return scanner.file()
}

// SyntaxError is text this reader could not read as an exchange file.
//
// It is a type rather than a formatted message for the reason every error in
// this repository is one: a test which had to match message text would be a
// test of the wording. Nothing asserts on these today — the checks in
// write_test.go require that reading succeeds — and that is exactly why the
// fields are here rather than folded into a string, because the test which
// wants to say "the writer emits an unterminated literal" should be able to
// say it without one.
type SyntaxError struct {
	// Offset is where in the source the reader stopped, in bytes.
	Offset int

	// Want says what would have been legal there.
	Want string

	// Found is what was there instead: the text from Offset on, clipped, or
	// the token which was read.
	Found string
}

// Error implements the [error] interface.
func (e SyntaxError) Error() string {
	return fmt.Sprintf("expected %s at offset %d, found %q", e.Want, e.Offset, e.Found)
}

// DuplicateInstanceError is one instance number written on two instances.
//
// A part 21 file numbers each instance once and every reference resolves
// through that number, so a repeat is not a duplicate line — it is two
// entities a reader will silently take for one.
type DuplicateInstanceError struct {
	// Number is the instance number which was written twice.
	Number int
}

// Error implements the [error] interface.
func (e DuplicateInstanceError) Error() string {
	return fmt.Sprintf("expected each instance number to be written once, found #%d written twice", e.Number)
}

// file is one parsed exchange file.
type file struct {
	// header is the header entities, in the order they were written.
	header []simple

	// instances is every data instance by its number, and order is the
	// numbers in the order they were written.
	instances map[int]simple
	order     []int
}

// instance is the data instance numbered at, and whether the file holds one.
func (f *file) instance(at int) (simple, bool) {
	held, ok := f.instances[at]
	return held, ok
}

// simple is one entity instance: a keyword and its attributes.
type simple struct {
	number     int
	keyword    string
	attributes []item
}

// The forms an attribute takes. They are the part 21 grammar's, not this
// package's: a reader which folded two of them together would not notice a
// writer which confused them.
const (
	itemString    = "string"
	itemReal      = "real"
	itemInteger   = "integer"
	itemEnum      = "enumeration"
	itemReference = "reference"
	itemList      = "list"
	itemAbsent    = "absent"
	itemDerived   = "derived"

	// itemTyped is a typed parameter: a keyword naming a type, and the value
	// of that type in parentheses. It is what an attribute declared as a
	// select over the schema's measure types is written as, because the
	// characters on their own would not say which member they are.
	itemTyped = "typed"
)

// item is one attribute value.
type item struct {
	form string

	// text is the value as it was written, with a string's quotes removed and
	// its doubled quotes undoubled.
	text string

	// items are a list's elements.
	items []item

	// at is a reference's instance number.
	at int
}

// scanner walks the source once.
type scanner struct {
	source string
	at     int
}

// file parses the whole exchange file.
func (s *scanner) file() (*file, error) {
	if err := s.expectKeyword("ISO-10303-21"); err != nil {
		return nil, err
	}
	if err := s.expect(';'); err != nil {
		return nil, err
	}

	parsed := &file{instances: make(map[int]simple)}

	if err := s.expectKeyword("HEADER"); err != nil {
		return nil, err
	}
	if err := s.expect(';'); err != nil {
		return nil, err
	}

	for {
		s.space()
		if s.peekKeyword("ENDSEC") {
			break
		}

		entity, err := s.simple()
		if err != nil {
			return nil, err
		}
		parsed.header = append(parsed.header, entity)
	}

	if err := s.endSection(); err != nil {
		return nil, err
	}

	if err := s.expectKeyword("DATA"); err != nil {
		return nil, err
	}
	if err := s.expect(';'); err != nil {
		return nil, err
	}

	for {
		s.space()
		if s.peekKeyword("ENDSEC") {
			break
		}

		if err := s.expect('#'); err != nil {
			return nil, err
		}

		number, err := s.number()
		if err != nil {
			return nil, err
		}
		if err := s.expect('='); err != nil {
			return nil, err
		}

		entity, err := s.simple()
		if err != nil {
			return nil, err
		}
		entity.number = number

		if _, held := parsed.instances[number]; held {
			return nil, DuplicateInstanceError{Number: number}
		}
		parsed.instances[number] = entity
		parsed.order = append(parsed.order, number)
	}

	if err := s.endSection(); err != nil {
		return nil, err
	}

	if err := s.expectKeyword("END-ISO-10303-21"); err != nil {
		return nil, err
	}
	if err := s.expect(';'); err != nil {
		return nil, err
	}

	s.space()
	if s.at != len(s.source) {
		return nil, SyntaxError{Offset: s.at, Want: "the end of the file", Found: s.rest()}
	}

	return parsed, nil
}

// endSection consumes an ENDSEC and its semicolon.
func (s *scanner) endSection() error {
	if err := s.expectKeyword("ENDSEC"); err != nil {
		return err
	}
	return s.expect(';')
}

// simple parses one entity instance and the semicolon after it.
func (s *scanner) simple() (simple, error) {
	keyword, err := s.keyword()
	if err != nil {
		return simple{}, err
	}

	attributes, err := s.arguments()
	if err != nil {
		return simple{}, err
	}

	if err := s.expect(';'); err != nil {
		return simple{}, err
	}

	return simple{keyword: keyword, attributes: attributes}, nil
}

// arguments parses a parenthesised, comma separated attribute list.
func (s *scanner) arguments() ([]item, error) {
	if err := s.expect('('); err != nil {
		return nil, err
	}

	var items []item

	s.space()
	if s.peek() == ')' {
		s.at++
		return items, nil
	}

	for {
		parsed, err := s.item()
		if err != nil {
			return nil, err
		}
		items = append(items, parsed)

		s.space()
		switch s.peek() {
		case ',':
			s.at++
		case ')':
			s.at++
			return items, nil
		default:
			return nil, SyntaxError{Offset: s.at, Want: "a comma or a closing parenthesis", Found: s.rest()}
		}
	}
}

// item parses one attribute value.
func (s *scanner) item() (item, error) {
	s.space()

	switch char := s.peek(); {
	case char == '$':
		s.at++
		return item{form: itemAbsent}, nil

	case char == '*':
		s.at++
		return item{form: itemDerived}, nil

	case char == '(':
		items, err := s.arguments()
		if err != nil {
			return item{}, err
		}
		return item{form: itemList, items: items}, nil

	case char == '#':
		s.at++
		at, err := s.number()
		if err != nil {
			return item{}, err
		}
		return item{form: itemReference, at: at}, nil

	case char == '\'':
		return s.string()

	case char == '.':
		return s.enumeration()

	case char == '-' || char == '+' || unicode.IsDigit(rune(char)):
		return s.numeric()

	case unicode.IsLetter(rune(char)):
		return s.typed()

	default:
		return item{}, SyntaxError{Offset: s.at, Want: "an attribute", Found: s.rest()}
	}
}

// typed parses a typed parameter: `IFCTEXT('a')`.
//
// The keyword goes in text and what it wraps goes in items, which keeps the
// two readable apart. A reader which threw the keyword away would report a
// text and a label as the same attribute, and those are the two an attribute
// written without its type is ambiguous between.
func (s *scanner) typed() (item, error) {
	keyword, err := s.keyword()
	if err != nil {
		return item{}, err
	}

	items, err := s.arguments()
	if err != nil {
		return item{}, err
	}

	if len(items) != 1 {
		return item{}, SyntaxError{Offset: s.at, Want: "one value inside a typed parameter", Found: keyword}
	}

	return item{form: itemTyped, text: keyword, items: items}, nil
}

// string parses a quoted string, undoubling the doubled quotes.
//
// The control directives are left as they were written. What this reader
// checks is that the literal is closed and that nothing in it could end it
// early; decoding `\X2\` back to text would be a second implementation of the
// encoding, which is the one thing a second opinion must not share.
func (s *scanner) string() (item, error) {
	if err := s.expect('\''); err != nil {
		return item{}, err
	}

	var written strings.Builder

	for s.at < len(s.source) {
		char := s.source[s.at]
		s.at++

		if char != '\'' {
			written.WriteByte(char)
			continue
		}

		if s.at < len(s.source) && s.source[s.at] == '\'' {
			written.WriteByte('\'')
			s.at++
			continue
		}

		return item{form: itemString, text: written.String()}, nil
	}

	return item{}, SyntaxError{Offset: s.at, Want: "a closing quote", Found: ""}
}

// enumeration parses a dotted enumeration member.
func (s *scanner) enumeration() (item, error) {
	if err := s.expect('.'); err != nil {
		return item{}, err
	}

	start := s.at
	for s.at < len(s.source) && s.source[s.at] != '.' {
		s.at++
	}
	if s.at == len(s.source) {
		return item{}, SyntaxError{Offset: start, Want: "a closing dot", Found: s.source[start:]}
	}

	written := s.source[start:s.at]
	s.at++

	return item{form: itemEnum, text: written}, nil
}

// numeric parses an integer or a real, and tells them apart the way part 21
// does: by whether there is a decimal point.
func (s *scanner) numeric() (item, error) {
	start := s.at

	if s.peek() == '-' || s.peek() == '+' {
		s.at++
	}
	for s.at < len(s.source) && strings.ContainsRune("0123456789.eE+-", rune(s.source[s.at])) {
		s.at++
	}

	written := s.source[start:s.at]

	if !strings.ContainsAny(written, ".") {
		if _, err := strconv.ParseInt(written, 10, 64); err != nil {
			return item{}, SyntaxError{Offset: start, Want: "an integer", Found: written}
		}
		return item{form: itemInteger, text: written}, nil
	}

	// Go will not parse `1.`, which part 21 will, so the trailing point is
	// filled out before it is handed over. That the point is there at all is
	// what says the value is a real, and is checked above.
	readable := written
	if strings.HasSuffix(readable, ".") {
		readable += "0"
	}
	if _, err := strconv.ParseFloat(readable, 64); err != nil {
		return item{}, SyntaxError{Offset: start, Want: "a real", Found: written}
	}

	return item{form: itemReal, text: written}, nil
}

// keyword parses an entity keyword.
func (s *scanner) keyword() (string, error) {
	s.space()

	start := s.at
	for s.at < len(s.source) {
		char := s.source[s.at]
		if char != '_' && char != '-' && !unicode.IsDigit(rune(char)) && !unicode.IsLetter(rune(char)) {
			break
		}
		s.at++
	}

	if s.at == start {
		return "", SyntaxError{Offset: s.at, Want: "a keyword", Found: s.rest()}
	}

	return s.source[start:s.at], nil
}

// number parses a plain unsigned integer, which is what an instance number is.
func (s *scanner) number() (int, error) {
	start := s.at
	for s.at < len(s.source) && unicode.IsDigit(rune(s.source[s.at])) {
		s.at++
	}
	if s.at == start {
		return 0, SyntaxError{Offset: s.at, Want: "an instance number", Found: s.rest()}
	}
	written := s.source[start:s.at]

	number, err := strconv.Atoi(written)
	if err != nil {
		// A run of digits which will not fit in an int, which is the only way
		// this fails once the scan above has read one.
		return 0, SyntaxError{Offset: start, Want: "an instance number", Found: written}
	}

	return number, nil
}

// expectKeyword consumes the keyword given, and fails on anything else.
func (s *scanner) expectKeyword(want string) error {
	got, err := s.keyword()
	if err != nil {
		return err
	}
	if got != want {
		return SyntaxError{Offset: s.at - len(got), Want: want, Found: got}
	}
	return nil
}

// expect consumes the character given, and fails on anything else.
func (s *scanner) expect(want byte) error {
	s.space()
	if s.at >= len(s.source) || s.source[s.at] != want {
		return SyntaxError{Offset: s.at, Want: strconv.QuoteRune(rune(want)), Found: s.rest()}
	}
	s.at++
	return nil
}

// peek is the next character, or zero at the end of the source.
func (s *scanner) peek() byte {
	if s.at >= len(s.source) {
		return 0
	}
	return s.source[s.at]
}

// peekKeyword reports whether the keyword given is next, without consuming it.
func (s *scanner) peekKeyword(want string) bool {
	return strings.HasPrefix(s.source[s.at:], want)
}

// space skips whitespace.
func (s *scanner) space() {
	for s.at < len(s.source) && (s.source[s.at] == ' ' || s.source[s.at] == '\n' || s.source[s.at] == '\r' || s.source[s.at] == '\t') {
		s.at++
	}
}

// rest is what is left, clipped, for a message which quotes where it stopped.
func (s *scanner) rest() string {
	const clip = 40
	if len(s.source)-s.at > clip {
		return s.source[s.at : s.at+clip]
	}
	return s.source[s.at:]
}
