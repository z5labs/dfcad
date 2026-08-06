// Copyright (c) 2026 Z5Labs and Contributors
//
// This software is released under the MIT License.
// https://opensource.org/licenses/MIT

package ifc

// GlobalID is IFC's identifier for one rooted object: 128 bits written as the
// 22 characters of IFC's own base64 alphabet.
//
// It is a string rather than the bits because that is the form an exchange
// file carries, and because deriving one is not this package's business. What
// a GlobalID means, and which of them two exports of the same thing share, is
// decided by whoever holds the model; see [EncodeGlobalID] for the half of
// that which is IFC's.
type GlobalID string

// String returns the 22 characters, which is the form an exchange file
// carries.
func (g GlobalID) String() string { return string(g) }

// Length is the character count IFC fixes for an encoded identifier: one group
// of two characters and five of four.
const Length = 22

// Alphabet is IFC's base64 alphabet, which is not the one RFC 4648 fixes: the
// digits come first, and the last two characters are an underscore and a
// dollar rather than a plus and a slash.
//
// Substituting the standard alphabet would produce 22 characters which look
// like a GlobalId, decode to the right 128 bits under the wrong reader, and be
// a different identifier to every IFC implementation there is.
//
// It is exported because it is the definition of the character set a GlobalId
// is written in, which is what anybody checking that a value is one has to
// hold — and holding a second copy of it is exactly the drift the constant
// exists to prevent.
const Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz_$"

// EncodeGlobalID writes 128 bits in IFC's 22-character form.
//
// The grouping is IFC's and is not a base64 of the whole number: the first
// byte is written as two characters, and the remaining fifteen as five groups
// of three bytes in four characters each. Two characters hold twelve bits
// where one byte needs eight, so the leading character is never above `3` — and
// encoding the 128 bits as one number instead would produce a different string
// for every value but the smallest.
//
// The bits are whatever the caller derived them from. This function is the
// encoding and nothing more: it reads no clock and no random source, so the
// same sixteen bytes are the same 22 characters in every run and in every
// process.
func EncodeGlobalID(bits [16]byte) GlobalID {
	encoded := make([]byte, 0, Length)

	encoded = appendBase64(encoded, uint32(bits[0]), 2)
	for i := 1; i < len(bits); i += 3 {
		group := uint32(bits[i])<<16 | uint32(bits[i+1])<<8 | uint32(bits[i+2])
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
		dst[at+i] = Alphabet[value%64]
		value /= 64
	}

	return dst
}
