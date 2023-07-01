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
	"encoding/hex"
	"testing"
)

type hashTest struct {
	typ    HashType
	base16 string
	base32 string
}

var hashTests = []hashTest{
	{MD5, "d41d8cd98f00b204e9800998ecf8427e", "3y8bwfr609h3lh9ch0izcqq7fl"},
	{MD5, "900150983cd24fb0d6963f7d28e17f72", "3jgzhjhz9zjvbb0kyj7jc500ch"},
	{SHA1, "a9993e364706816aba3e25717850c26c9cd0d89d", "kpcd173cq987hw957sx6m0868wv3x6d9"},
	{SHA1, "84983e441c3bd26ebaae4aa1f95129e5e54670f1", "y5q4drg5558zk8aamsx6xliv3i23x644"},
	{SHA256, "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad", "1b8m03r63zqhnjf7l5wnldhh7c134ap5vpj0850ymkq1iyzicy5s"},
	{SHA256, "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1", "1h86vccx9vgcyrkj3zv4b7j3r8rrc0z0r4r6q3jvhf06s9hnm394"},
	{SHA512, "ddaf35a193617abacc417349ae20413112e6fa4e89a97ea20a9eeee64b55d39a2192992a274fc1a836ba3c23a3feebbd454d4423643ce80e2a9ac94fa54ca49f", "2gs8k559z4rlahfx0y688s49m2vvszylcikrfinm30ly9rak69236nkam5ydvly1ai7xac99vxfc4ii84hawjbk876blyk1jfhkbbyx"},
	{SHA512, "8e959b75dae313da8cf4f72814fc143f8f7779c6eb9f7fa17299aeadb6889018501d289e4900f7e4331b99dec4b5433ac7d329eeb6dd26545e96e55b874be909", "04yjjw7bgjrcpjl4vfvdvi9sg3klhxmqkg9j6rkwkvh0jcy50fm064hi2vavblrfahpz7zbqrwpg3rz2ky18a7pyj6dl4z3v9srp5cf"},
}

func (test hashTest) base64(tb testing.TB) string {
	bits, err := hex.DecodeString(test.base16)
	if err != nil {
		tb.Fatal(err)
	}
	return base64Encoding.EncodeToString(bits)
}

func (test hashTest) sri(tb testing.TB) string {
	return test.typ.String() + "-" + test.base64(tb)
}

func TestParseHash(t *testing.T) {
	t.Run("Base16", func(t *testing.T) {
		for _, test := range hashTests {
			s := test.typ.String() + ":" + test.base16
			got, err := ParseHash(s)
			if err != nil {
				t.Errorf("ParseHash(%q) = %v, %v; want %s, <nil>", s, got, err, test.sri(t))
				continue
			}
			if got, want := got.Type(), test.typ; got != want {
				t.Errorf("ParseHash(%q).Type() = %v; want %v", s, got, want)
			}
			if got, want := got.RawBase16(), test.base16; got != want {
				t.Errorf("ParseHash(%q).RawBase16() = %q; want %q", s, got, want)
			}
			if got, want := got.SRI(), test.sri(t); got != want {
				t.Errorf("ParseHash(%q).SRI() = %q; want %q", s, got, want)
			}
		}
	})

	t.Run("Base32", func(t *testing.T) {
		for _, test := range hashTests {
			s := test.typ.String() + ":" + test.base32
			got, err := ParseHash(s)
			if err != nil {
				t.Errorf("ParseHash(%q) = %v, %v; want %s, <nil>", s, got, err, test.sri(t))
				continue
			}
			if got, want := got.Type(), test.typ; got != want {
				t.Errorf("ParseHash(%q).Type() = %v; want %v", s, got, want)
			}
			if got, want := got.RawBase16(), test.base16; got != want {
				t.Errorf("ParseHash(%q).RawBase16() = %q; want %q", s, got, want)
			}
			if got, want := got.SRI(), test.sri(t); got != want {
				t.Errorf("ParseHash(%q).SRI() = %q; want %q", s, got, want)
			}
		}
	})

	t.Run("Base64", func(t *testing.T) {
		for _, test := range hashTests {
			s := test.typ.String() + ":" + test.base64(t)
			got, err := ParseHash(s)
			if err != nil {
				t.Errorf("ParseHash(%q) = %v, %v; want %s, <nil>", s, got, err, test.sri(t))
				continue
			}
			if got, want := got.Type(), test.typ; got != want {
				t.Errorf("ParseHash(%q).Type() = %v; want %v", s, got, want)
			}
			if got, want := got.RawBase16(), test.base16; got != want {
				t.Errorf("ParseHash(%q).RawBase16() = %q; want %q", s, got, want)
			}
			if got, want := got.SRI(), test.sri(t); got != want {
				t.Errorf("ParseHash(%q).SRI() = %q; want %q", s, got, want)
			}
		}
	})

	t.Run("SRI", func(t *testing.T) {
		for _, test := range hashTests {
			s := test.sri(t)
			got, err := ParseHash(s)
			if err != nil {
				t.Errorf("ParseHash(%q) = %v, %v; want %s, <nil>", s, got, err, test.sri(t))
				continue
			}
			if got, want := got.Type(), test.typ; got != want {
				t.Errorf("ParseHash(%q).Type() = %v; want %v", s, got, want)
			}
			if got, want := got.RawBase16(), test.base16; got != want {
				t.Errorf("ParseHash(%q).RawBase16() = %q; want %q", s, got, want)
			}
			if got, want := got.SRI(), test.sri(t); got != want {
				t.Errorf("ParseHash(%q).SRI() = %q; want %q", s, got, want)
			}
		}
	})
}

func TestHashBase32(t *testing.T) {
	for _, test := range hashTests {
		s := test.typ.String() + ":" + test.base16
		h, err := ParseHash(s)
		if err != nil {
			t.Errorf("ParseHash(%q) = %v, %v; want %s, <nil>", s, h, err, test.sri(t))
			continue
		}
		if got, want := h.RawBase32(), test.base32; got != want {
			t.Errorf("ParseHash(%q).RawBase32() = %q; want %q", s, got, want)
		}
		if got, want := h.Base32(), test.typ.String()+":"+test.base32; got != want {
			t.Errorf("ParseHash(%q).Base32() = %q; want %q", s, got, want)
		}
	}
}
