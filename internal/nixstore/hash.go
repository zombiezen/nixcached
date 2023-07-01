// Copyright 2023 Ross Light
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//		 https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
//
// SPDX-License-Identifier: Apache-2.0

package nixstore

import (
	"bytes"
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"hash"
)

// base64Encoding is the Nix base64 alphabet.
var base64Encoding = base64.StdEncoding

// HashType is an enumeration of algorithms supported by [Hash].
type HashType int8

// Hash algorithms.
const (
	MD5 HashType = 1 + iota
	SHA1
	SHA256
	SHA512
)

// ParseHashType matches a string to its hash type,
// returning an error if the string does not name a hash type.
func ParseHashType(s string) (HashType, error) {
	allTypes := [...]HashType{MD5, SHA1, SHA256, SHA512}
	for _, typ := range allTypes {
		if s == typ.String() {
			return typ, nil
		}
	}
	return 0, fmt.Errorf("%q is not a hash type", s)
}

// Size returns the size of a hash produced by this type in bytes.
func (typ HashType) Size() int {
	switch typ {
	case 0:
		return 0
	case MD5:
		return md5.Size
	case SHA1:
		return sha1.Size
	case SHA256:
		return sha256.Size
	case SHA512:
		return sha512.Size
	default:
		panic("invalid hash type")
	}
}

// String returns the name of the hash algorithm.
func (typ HashType) String() string {
	switch typ {
	case MD5:
		return "md5"
	case SHA1:
		return "sha1"
	case SHA256:
		return "sha256"
	case SHA512:
		return "sha512"
	default:
		return fmt.Sprintf("HashType(%d)", int(typ))
	}
}

// Hash returns a new [hash.Hash] object for the algorithm.
func (typ HashType) Hash() hash.Hash {
	switch typ {
	case 0:
		return nil
	case MD5:
		return md5.New()
	case SHA1:
		return sha1.New()
	case SHA256:
		return sha256.New()
	case SHA512:
		return sha512.New()
	default:
		panic("invalid hash type")
	}
}

// A Hash is an output of a hash algorithm.
// The zero value is an empty hash with no type.
type Hash struct {
	typ  HashType
	hash [sha512.Size]byte
}

// ParseHash parses a hash
// in the format "<type>:<base16|base32|base64>" or "<type>-<base64>"
// (a [Subresource Integrity hash expression]).
// It is a wrapper around [Hash.UnmarshalText].
//
// [Subresource Integrity hash expression]: https://www.w3.org/TR/SRI/#the-integrity-attribute
func ParseHash(s string) (Hash, error) {
	var h Hash
	if err := h.UnmarshalText([]byte(s)); err != nil {
		return Hash{}, err
	}
	return h, nil
}

func SumHash(typ HashType, h hash.Hash) Hash {
	if h.Size() != typ.Size() {
		panic("hash size does not match hash type")
	}
	h2 := Hash{typ: typ}
	h.Sum(h2.hash[:0])
	return h2
}

// Type returns the hash's algorithm.
// It returns zero for a zero Hash.
func (h Hash) Type() HashType {
	return h.typ
}

// Append appends the raw bytes of the hash to dst
// and returns the resulting slice.
func (h Hash) Append(dst []byte) []byte {
	return append(dst, h.hash[:h.typ.Size()]...)
}

// String returns the result of [Hash.SRI]
// or "<nil>" if the hash is the zero Hash.
func (h Hash) String() string {
	if h.typ == 0 {
		return "<nil>"
	}
	return h.SRI()
}

// Base16 encodes the hash with base16 (i.e. hex)
// prefixed by the hash type separated by a colon.
func (h Hash) Base16() string {
	return string(h.encode(true, hex.EncodedLen, base16Encode))
}

// RawBase16 encodes the hash with base16 (i.e. hex).
func (h Hash) RawBase16() string {
	return string(h.encode(false, hex.EncodedLen, base16Encode))
}

func base16Encode(dst, src []byte) {
	hex.Encode(dst, src)
}

// Base32 encodes the hash with base32
// prefixed by the hash type separated by a colon.
func (h Hash) Base32() string {
	return string(h.encode(true, base32EncodedLen, base32Encode))
}

// RawBase32 encodes the hash with base32.
func (h Hash) RawBase32() string {
	return string(h.encode(false, base32EncodedLen, base32Encode))
}

// Base64 encodes the hash with base64
// prefixed by the hash type separated by a colon.
func (h Hash) Base64() string {
	return string(h.encode(true, base64Encoding.EncodedLen, base64Encoding.Encode))
}

// RawBase64 encodes the hash with base64.
func (h Hash) RawBase64() string {
	return string(h.encode(false, base64Encoding.EncodedLen, base64Encoding.Encode))
}

// SRI returns the hash in the format of a [Subresource Integrity hash expression]
// (e.g. "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=").
//
// [Subresource Integrity hash expression]: https://www.w3.org/TR/SRI/#the-integrity-attribute
func (h Hash) SRI() string {
	b, _ := h.MarshalText()
	return string(b)
}

// MarshalText formats the hash as a [Subresource Integrity hash expression]
// (e.g. "sha256-47DEQpj8HBSa+/TImW+5JCeuQeRkm5NMpJWZG3hSuFU=").
// It returns an error if h is the zero Hash.
//
// [Subresource Integrity hash expression]: https://www.w3.org/TR/SRI/#the-integrity-attribute
func (h Hash) MarshalText() ([]byte, error) {
	if h.typ == 0 {
		return nil, fmt.Errorf("cannot marshal zero hash")
	}
	buf := h.encode(true, base64Encoding.EncodedLen, base64Encoding.Encode)
	buf[bytes.IndexByte(buf, ':')] = '-'
	return buf, nil
}

// UnmarshalText parses a hash
// in the format "<type>:<base16|base32|base64>" or "<type>-<base64>"
// (a [Subresource Integrity hash expression]).
//
// [Subresource Integrity hash expression]: https://www.w3.org/TR/SRI/#the-integrity-attribute
func (h *Hash) UnmarshalText(s []byte) error {
	sep := [1]byte{':'}
	prefix, rest, hasPrefix := bytes.Cut(s, sep[:])
	isSRI := false
	if !hasPrefix {
		sep[0] = '-'
		prefix, rest, isSRI = bytes.Cut(s, sep[:])
		if !isSRI {
			return fmt.Errorf("parse hash %q: missing prefix", s)
		}
	}
	var err error
	h.typ, err = ParseHashType(string(prefix))
	if err != nil {
		return fmt.Errorf("parse hash %q: %v", s, err)
	}
	switch {
	case isSRI && len(rest) != base64Encoding.EncodedLen(h.typ.Size()):
		return fmt.Errorf("parse hash %q: wrong length for SRI of type %v", s, h.typ)
	case len(rest) == hex.EncodedLen(h.typ.Size()):
		if _, err := hex.Decode(h.hash[:], rest); err != nil {
			return fmt.Errorf("parse hash %q: %v", s, err)
		}
	case len(rest) == base32EncodedLen(h.typ.Size()):
		if _, err := base32Decode(h.hash[:], rest); err != nil {
			return fmt.Errorf("parse hash %q: %v", s, err)
		}
	case len(rest) == base64Encoding.EncodedLen(h.typ.Size()):
		if _, err := base64Encoding.Decode(h.hash[:], rest); err != nil {
			return fmt.Errorf("parse hash %q: %v", s, err)
		}
	default:
		return fmt.Errorf("parse hash %q: wrong length for hash of type %v", s, h.typ)
	}
	return nil
}

func (h Hash) encode(includeType bool, encodedLen func(int) int, encode func(dst, src []byte)) []byte {
	if h.typ == 0 {
		return nil
	}
	hashLen := h.typ.Size()
	n := encodedLen(hashLen)
	if includeType {
		n += len(h.typ.String()) + 1
	}

	buf := make([]byte, 0, n)
	if includeType {
		buf = append(buf, h.typ.String()...)
		buf = append(buf, ':')
	}
	encode(buf[len(buf):n], h.hash[:hashLen])
	return buf[:n]
}
