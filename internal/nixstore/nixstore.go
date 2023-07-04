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

// Package nixstore provides functions for manipulating Nix store paths.
package nixstore

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Directory is the location of a Nix store in the local filesystem.
type Directory string

// DefaultDirectory is the default Nix store directory.
const DefaultDirectory Directory = "/nix/store"

// DirectoryFromEnv returns the Nix store directory in use
// based on the NIX_STORE_DIR environment variable,
// falling back to [DefaultDirectory] if not set.
func DirectoryFromEnv() (Directory, error) {
	dir := os.Getenv("NIX_STORE_DIR")
	if dir == "" {
		return DefaultDirectory, nil
	}
	dir, err := filepath.Abs(dir)
	if err != nil {
		return "", fmt.Errorf("find nix store directory: %v", err)
	}
	return Directory(dir), nil
}

// StorePath returns the store path for the given store object name.
func (dir Directory) StorePath(name ObjectName) string {
	return filepath.Join(string(dir), string(name))
}

// ParseStorePath determines whether the given path could name a store object
// in the store directory, returning the object name if so.
func (dir Directory) ParseStorePath(storePath string) (_ ObjectName, ok bool) {
	storePath, err := filepath.Abs(storePath)
	if err != nil {
		return "", false
	}
	dirPrefix := string(dir)
	if !strings.HasSuffix(dirPrefix, string(filepath.Separator)) {
		dirPrefix += string(filepath.Separator)
	}
	if !strings.HasPrefix(storePath, dirPrefix) {
		return "", false
	}
	name, err := ParseObjectName(storePath[len(dirPrefix):])
	if err != nil {
		return "", false
	}
	return name, true
}

// ObjectName is the file name of a store object.
type ObjectName string

const (
	objectNameHashLength = 32
	maxObjectNameLength  = objectNameHashLength + 1 + 211
)

// ParseObjectName validates a string as the file name of a Nix store object,
// returning the error encountered if any.
func ParseObjectName(name string) (ObjectName, error) {
	if len(name) < objectNameHashLength+len("-")+1 {
		return "", fmt.Errorf("parse nix store object name: %q is too short", name)
	}
	if len(name) > maxObjectNameLength {
		return "", fmt.Errorf("parse nix store object name: %q is too long", name)
	}
	for i := 0; i < len(name); i++ {
		if !isNameChar(name[i]) {
			return "", fmt.Errorf("parse nix store object name: %q contains illegal character %q", name, name[i])
		}
	}
	for i := 0; i < objectNameHashLength; i++ {
		if !isBase32Char(name[i]) {
			return "", fmt.Errorf("parse nix store object name: %q contains illegal base-32 character %q", name, name[i])
		}
	}
	if name[objectNameHashLength] != '-' {
		return "", fmt.Errorf("parse nix store object name: %q does not separate hash with dash", name)
	}
	return ObjectName(name), nil
}

// IsDerivation reports whether the name ends in ".drv".
func (name ObjectName) IsDerivation() bool {
	return strings.HasSuffix(string(name), ".drv")
}

// Hash returns the hash part of the name.
func (name ObjectName) Hash() string {
	if len(name) < objectNameHashLength {
		return ""
	}
	return string(name[:objectNameHashLength])
}

// Name returns the part of the name after the hash.
func (name ObjectName) Name() string {
	if len(name) <= objectNameHashLength+len("-") {
		return ""
	}
	return string(name[objectNameHashLength+len("-"):])
}

func isNameChar(c byte) bool {
	return 'a' <= c && c <= 'z' ||
		'A' <= c && c <= 'Z' ||
		'0' <= c && c <= '9' ||
		c == '+' || c == '-' || c == '.' || c == '_' || c == '?' || c == '='
}
