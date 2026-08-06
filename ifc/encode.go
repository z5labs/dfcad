// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// value is one attribute of an entity instance, in the form part 21 writes it.
//
// It is an interface rather than a tagged struct because the encodings have
// nothing in common but their position: a string is quoted and escaped, a real
// carries a decimal point whether or not it needs one, an enumeration is
// written between dots, and absence is a dollar. Each is a few lines beside
// its own rules.
//
// Encoding may fail — a real which is not a number has no part 21 spelling —
// so this returns an error rather than writing something a reader would accept
// and misread.
type value interface {
	encode(dst []byte) ([]byte, error)
}

// absent is an optional attribute which was not given: `$`.
type absent struct{}

func (absent) encode(dst []byte) ([]byte, error) { return append(dst, '$'), nil }

// derived is an attribute a subtype redeclares as derived: `*`.
//
// It is not the same as absent and the two are not interchangeable: `$` says
// the value was not given, and `*` says it is computed by the schema and must
// not be given. IfcSIUnit's Dimensions is the one this package writes.
type derived struct{}

func (derived) encode(dst []byte) ([]byte, error) { return append(dst, '*'), nil }

// text is a string attribute.
type text string

func (t text) encode(dst []byte) ([]byte, error) { return appendString(dst, string(t)), nil }

// optionalText is a string attribute which is absent when it is empty.
//
// Every text attribute in this package's entities is optional, and the empty
// string is how a caller says it has nothing to write. Writing an empty string
// would be a name which is present and empty, which is a different statement
// and one no caller here means to make.
func optionalText(written string) value {
	if written == "" {
		return absent{}
	}
	return text(written)
}

// typedText is a string attribute whose type has to be named: `IFCTEXT('a')`.
//
// An attribute declared as a SELECT over the schema's measure types carries no
// type of its own, so the value has to say which member it is. A bare string
// where one of those is expected is a file readers reject, because there is
// nothing in it to say whether the characters are a label, a text or an
// identifier.
type typedText string

func (t typedText) encode(dst []byte) ([]byte, error) {
	dst = append(dst, "IFCTEXT("...)
	dst = appendString(dst, string(t))
	return append(dst, ')'), nil
}

// optionalTypedText is a [typedText] which is absent when it is empty.
func optionalTypedText(written string) value {
	if written == "" {
		return absent{}
	}
	return typedText(written)
}

// real is a floating point attribute.
type real float64

func (r real) encode(dst []byte) ([]byte, error) { return appendReal(dst, float64(r)) }

// integer is an integer attribute.
type integer int

func (i integer) encode(dst []byte) ([]byte, error) {
	return strconv.AppendInt(dst, int64(i), 10), nil
}

// enumeration is an attribute whose value is a member of a schema enumeration,
// written between dots.
type enumeration string

func (e enumeration) encode(dst []byte) ([]byte, error) {
	dst = append(dst, '.')
	dst = append(dst, string(e)...)
	return append(dst, '.'), nil
}

// optionalEnumeration is an enumeration attribute which is absent when it is
// empty.
func optionalEnumeration(member string) value {
	if member == "" {
		return absent{}
	}
	return enumeration(member)
}

// reference is a reference to another instance in the same file: `#12`.
type reference int

func (r reference) encode(dst []byte) ([]byte, error) {
	dst = append(dst, '#')
	return strconv.AppendInt(dst, int64(r), 10), nil
}

// list is an aggregate attribute: `(a,b,c)`.
//
// An empty list is written `()`, which is a list of nothing rather than an
// absent attribute — the two are different in the schema and a reader tells
// them apart.
type list []value

func (l list) encode(dst []byte) ([]byte, error) {
	dst = append(dst, '(')

	for i, element := range l {
		if i > 0 {
			dst = append(dst, ',')
		}

		var err error
		dst, err = element.encode(dst)
		if err != nil {
			return nil, err
		}
	}

	return append(dst, ')'), nil
}

// texts is a list of strings, which is what every header attribute taking one
// is.
func texts(written []string) list {
	out := make(list, 0, len(written))
	for _, one := range written {
		out = append(out, text(one))
	}
	return out
}

// appendReal writes value in the one canonical form this package uses.
//
// The rules, in the order they bite:
//
//   - A value which is not finite has no spelling. There is no NaN and no
//     infinity in part 21, and writing the nearest thing to one — a very large
//     number, a zero, the three letters — is a file which loads and lies.
//     [UnrepresentableRealError] says so instead.
//   - Negative zero is written as zero. The two are the same number and
//     different bytes, and a subtraction which happens to produce one of them
//     must not change the file.
//   - The digits are the shortest decimal which reads back as exactly this
//     float64, written without an exponent. Shortest-round-trip is what makes
//     the form canonical: two floats are the same bytes if and only if they
//     are the same number.
//   - A decimal point is always present, because part 21 distinguishes a real
//     from an integer by exactly that. `1` is an integer and `1.` is the real
//     one, and an attribute declared REAL which receives the first is a type
//     error in a reader which checks.
func appendReal(dst []byte, value float64) ([]byte, error) {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return nil, UnrepresentableRealError{Value: value}
	}

	if value == 0 {
		// Covers -0 as well, which compares equal to 0 and formats as "-0".
		return append(dst, '0', '.'), nil
	}

	written := strconv.FormatFloat(value, 'f', -1, 64)
	dst = append(dst, written...)

	if !strings.ContainsRune(written, '.') {
		dst = append(dst, '.')
	}

	return dst, nil
}

// appendString writes value as a part 21 string literal.
//
// Part 21 strings are single quoted and hold printable ASCII directly. A quote
// is doubled and a backslash is doubled, because the backslash introduces the
// control directives below and a literal one would otherwise start one.
//
// Anything else — a control character, an accented letter, a character outside
// the basic multilingual plane — is written with the `\X2\` directive, as
// UTF-16 code units in upper case hex, ended by `\X0\`. A run of them is
// written as one directive rather than one directive each, which is what the
// encoding is for and is what any other writer of this format produces.
func appendString(dst []byte, value string) []byte {
	dst = append(dst, '\'')

	for i := 0; i < len(value); {
		char := value[i]

		if char >= 0x20 && char <= 0x7e {
			switch char {
			case '\'':
				dst = append(dst, '\'', '\'')
			case '\\':
				dst = append(dst, '\\', '\\')
			default:
				dst = append(dst, char)
			}
			i++
			continue
		}

		// One directive covers everything up to the next character which can
		// be written directly.
		dst = append(dst, `\X2\`...)
		for i < len(value) {
			// A byte which is not valid UTF-8 decodes to the replacement
			// character, which is then what gets written. Refusing to write a
			// file over a string somebody's file system handed the caller
			// would be worse, and the replacement character is visible in the
			// output as exactly the thing it is.
			char, width := utf8.DecodeRuneInString(value[i:])
			if char >= 0x20 && char <= 0x7e {
				break
			}
			for _, unit := range utf16.Encode([]rune{char}) {
				dst = appendHex(dst, unit)
			}
			i += width
		}
		dst = append(dst, `\X0\`...)
	}

	return append(dst, '\'')
}

// appendHex writes one UTF-16 code unit as four upper case hex digits.
func appendHex(dst []byte, unit uint16) []byte {
	const digits = "0123456789ABCDEF"
	return append(dst,
		digits[unit>>12&0xf],
		digits[unit>>8&0xf],
		digits[unit>>4&0xf],
		digits[unit&0xf],
	)
}
