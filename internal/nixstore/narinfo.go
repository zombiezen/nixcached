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
	slashpath "path"
	"strconv"
)

// NARInfo is the parsed representation of a .narinfo file.
type NARInfo struct {
	// StorePath is the absolute path of this store object
	// (e.g. "/nix/store/s66mzxpvicwk07gjbjfw9izjfa797vsw-hello-2.12.1").
	// Nix requires this field to be set.
	StorePath string
	// URL is the path relative from the .narinfo file to download to .nar file.
	// Nix requires this field to be set.
	URL string
	// Compression is the algorithm used for the `.nar` file.
	// If empty, defaults to "bzip2".
	Compression CompressionType
	// FileHash is the hash of the compressed .nar file.
	FileHash Hash
	// FileSize is the size of the compressed .nar file in bytes.
	FileSize int64
	// NARHash is the hash of the decompressed .nar file.
	// Nix requires this field to be set.
	NARHash Hash
	// NARSize is the size of the decompressed .nar file in bytes.
	// Nix requires this field to be set.
	NARSize int64
	// References is the set of other store objects that this store object references.
	References []ObjectName
	// Deriver is the name of the store object that is the store derivation
	// of this store object.
	Deriver ObjectName
	// Sig is a set of signatures for this object.
	Sig []string
}

// Clone returns a deep copy of an info struct.
func (info *NARInfo) Clone() *NARInfo {
	info2 := new(NARInfo)
	*info2 = *info
	info.References = append([]ObjectName(nil), info.References...)
	info.Sig = append([]string(nil), info.Sig...)
	return info
}

// Directory returns the store directory of the store object.
func (info *NARInfo) Directory() Directory {
	return Directory(slashpath.Dir(info.StorePath))
}

// IsValid reports whether the NAR information fields are valid.
func (info *NARInfo) IsValid() bool {
	return info.validate() == nil
}

func (info *NARInfo) validate() error {
	if _, err := ParseObjectName(slashpath.Base(info.StorePath)); err != nil {
		return fmt.Errorf("store path: %v", err)
	}
	if info.URL == "" {
		return fmt.Errorf("url empty")
	}
	if !info.Compression.IsKnown() {
		return fmt.Errorf("unknown compression %q", info.Compression)
	}
	if info.FileSize < 0 {
		return fmt.Errorf("negative file size")
	}
	if info.NARHash.Type() == 0 {
		return fmt.Errorf("nar hash not set")
	}
	if info.NARSize == 0 {
		return fmt.Errorf("nar size not set")
	}
	if info.NARSize < 0 {
		return fmt.Errorf("negative nar size")
	}
	return nil
}

// MarshalText encodes the information as a .narinfo file.
func (info *NARInfo) MarshalText() ([]byte, error) {
	if err := info.validate(); err != nil {
		return nil, fmt.Errorf("marshal narinfo: %v", err)
	}

	var buf []byte
	buf = append(buf, "StorePath: "...)
	buf = append(buf, info.StorePath...)
	buf = append(buf, "\nURL: "...)
	buf = append(buf, info.URL...)
	buf = append(buf, "\nCompression: "...)
	compression := info.Compression
	if compression == "" {
		compression = Bzip2
	}
	buf = append(buf, compression...)
	if info.FileHash.Type() != 0 {
		buf = append(buf, "\nFileHash: "...)
		buf = append(buf, info.FileHash.Base32()...)
	}
	if info.FileSize != 0 {
		buf = append(buf, "\nFileSize: "...)
		buf = strconv.AppendInt(buf, info.FileSize, 10)
	}
	buf = append(buf, "\nNarHash: "...)
	buf = append(buf, info.NARHash.Base32()...)
	buf = append(buf, "\nNarSize: "...)
	buf = strconv.AppendInt(buf, info.NARSize, 10)
	if len(info.References) > 0 {
		buf = append(buf, "\nReferences:"...)
		for _, ref := range info.References {
			buf = append(buf, ' ')
			buf = append(buf, ref...)
		}
	}
	if info.Deriver != "" {
		buf = append(buf, "\nDeriver: "...)
		buf = append(buf, info.Deriver...)
	}
	for _, sig := range info.Sig {
		buf = append(buf, "\nSig: "...)
		buf = append(buf, sig...)
	}
	buf = append(buf, "\n"...)
	return buf, nil
}

// CompressionType is an enumeration of compression algorithms used in [NARInfo].
type CompressionType string

// Compression types.
const (
	Bzip2     CompressionType = "bzip2"
	XZ        CompressionType = "xz"
	Zstandard CompressionType = "zstd"
	Lzip      CompressionType = "lzip"
	LZ4       CompressionType = "lz4"
	Brotli    CompressionType = "br"
)

// IsKnown reports whether ct is one of the known compression types.
func (ct CompressionType) IsKnown() bool {
	switch ct {
	case "", Bzip2, XZ, Zstandard, Lzip, LZ4, Brotli:
		return true
	default:
		return false
	}
}
