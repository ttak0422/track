package site

import (
	"crypto/sha1"
	"math/big"
	"strconv"
)

// publishNamespace is a fixed, arbitrary UUID namespace for the published-site id mapping. It must never
// change: PublishID derives every note's public slug from it, so altering it would shift all published
// URLs and break existing links/bookmarks.
var publishNamespace = [16]byte{
	0x9a, 0x7b, 0x3c, 0x1d, 0x5e, 0x2f, 0x46, 0x88,
	0xb1, 0xc2, 0xd3, 0xe4, 0xf5, 0x06, 0x17, 0x28,
}

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// PublishID maps an internal note id to a stable, opaque slug used as the note's filename and URL in the
// published static site. It is a UUIDv5 (deterministic: namespace + the decimal id), so the same note
// always yields the same slug across rebuilds — published URLs stay valid — while the slug reveals
// nothing about the id (the source files are timestamp-based, see note.NewID). The 128-bit value is
// base62-encoded to a fixed 22-character, URL/filename-safe string.
func PublishID(id int64) string {
	h := sha1.New()
	h.Write(publishNamespace[:])
	h.Write([]byte(strconv.FormatInt(id, 10)))
	sum := h.Sum(nil)[:16]
	// Set the RFC 4122 version (5) and variant (10) bits so the value is a well-formed UUIDv5.
	sum[6] = (sum[6] & 0x0f) | 0x50
	sum[8] = (sum[8] & 0x3f) | 0x80
	return base62Fixed(sum)
}

// base62Fixed encodes a 16-byte value as exactly 22 base62 digits (62^22 > 2^128 > 62^21), left-padded
// with the zero digit so every slug has the same length.
func base62Fixed(b []byte) string {
	n := new(big.Int).SetBytes(b)
	base := big.NewInt(62)
	rem := new(big.Int)
	buf := make([]byte, 22)
	for i := len(buf) - 1; i >= 0; i-- {
		n.DivMod(n, base, rem)
		buf[i] = base62Alphabet[rem.Int64()]
	}
	return string(buf)
}
