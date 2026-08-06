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
			return nil, fmt.Errorf("instance #%d is written twice", number)
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
		return nil, fmt.Errorf("trailing text at offset %d: %q", s.at, s.rest())
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
			return nil, fmt.Errorf("expected a comma or a closing parenthesis at offset %d: %q", s.at, s.rest())
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

	default:
		return item{}, fmt.Errorf("expected an attribute at offset %d: %q", s.at, s.rest())
	}
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

	return item{}, fmt.Errorf("unterminated string literal at offset %d", s.at)
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
		return item{}, fmt.Errorf("unterminated enumeration at offset %d", start)
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
			return item{}, fmt.Errorf("malformed integer %q at offset %d", written, start)
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
		return item{}, fmt.Errorf("malformed real %q at offset %d", written, start)
	}

	return item{form: itemReal, text: written}, nil
}

// keyword parses an entity keyword.
func (s *scanner) keyword() (string, error) {
	s.space()

	start := s.at
	for s.at < len(s.source) {
		char := s.source[s.at]
		if !(char == '_' || char == '-' || unicode.IsDigit(rune(char)) || unicode.IsLetter(rune(char))) {
			break
		}
		s.at++
	}

	if s.at == start {
		return "", fmt.Errorf("expected a keyword at offset %d: %q", s.at, s.rest())
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
		return 0, fmt.Errorf("expected an instance number at offset %d: %q", s.at, s.rest())
	}
	return strconv.Atoi(s.source[start:s.at])
}

// expectKeyword consumes the keyword given, and fails on anything else.
func (s *scanner) expectKeyword(want string) error {
	got, err := s.keyword()
	if err != nil {
		return err
	}
	if got != want {
		return fmt.Errorf("expected %s, found %s", want, got)
	}
	return nil
}

// expect consumes the character given, and fails on anything else.
func (s *scanner) expect(want byte) error {
	s.space()
	if s.at >= len(s.source) || s.source[s.at] != want {
		return fmt.Errorf("expected %q at offset %d: %q", want, s.at, s.rest())
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
