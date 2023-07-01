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
	"fmt"
	"strings"
)

const base32Chars = "0123456789abcdfghijklmnpqrsvwxyz"

func base32EncodedLen(n int) int {
	return (n*8 + 4) / 5
}

func base32Encode(dst, src []byte) {
	dst = dst[:0:len(dst)]
	for n := base32EncodedLen(len(src)) - 1; n >= 0; n-- {
		b := uint64(n) * 5
		i := int(b / 8)
		j := int(b % 8)
		c := src[i] >> j
		if i+1 < len(src) {
			c |= src[i+1] << (8 - j)
		}
		dst = append(dst, base32Chars[c&0x1f])
	}
}

func base32DecodedLen(n int) int {
	return n * 5 / 8
}

func base32Decode(dst, src []byte) (n int, err error) {
	maxDstSize := base32DecodedLen(len(src))
	for n := range src {
		c := src[len(src)-n-1]
		digit := strings.IndexByte(base32Chars, c)
		if digit == -1 {
			return 0, fmt.Errorf("illegal nix base32 character %q", c)
		}
		b := uint64(n) * 5
		i := int(b / 8)
		j := int(b % 8)
		dst[i] |= byte(digit) << j
		if i+1 < maxDstSize {
			dst[i+1] |= byte(digit) >> (8 - j)
		} else if digit>>(8-j) != 0 {
			return 0, fmt.Errorf("invalid base-32 hash %q", src)
		}
	}
	return maxDstSize, nil
}

func isBase32Char(c byte) bool {
	return '0' <= c && c <= '9' ||
		'a' <= c && c <= 'z' && c != 'e' && c != 'o' && c != 'u' && c != 't'
}
