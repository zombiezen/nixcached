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
	"fmt"
	"strconv"
)

const (
	// CacheInfoName is the name of the binary cache resource
	// that contains its [Configuration].
	CacheInfoName = "nix-cache-info"
	// CacheInfoMIMEType is the MIME content type for the nix-cache-info file.
	CacheInfoMIMEType = "text/x-nix-cache-info"
)

// Configuration holds various settings about a Nix store.
type Configuration struct {
	// StoreDir is the location of the store.
	// Defaults to [DefaultDirectory] if empty.
	StoreDir Directory
	// Priority is the priority of the store when used as a substituter.
	// Lower values mean higher priority.
	Priority int
	// WantMassQuery indicates whether this store (when used as a substituter)
	// can be queried efficiently for path validity.
	WantMassQuery bool
}

// MarshalText formats the configuration in the format of a nix-cache-info file.
func (cfg *Configuration) MarshalText() ([]byte, error) {
	storeDir := cfg.StoreDir
	if storeDir == "" {
		storeDir = DefaultDirectory
	}
	var buf []byte
	buf = append(buf, "StoreDir: "...)
	buf = append(buf, storeDir...)
	buf = append(buf, '\n')
	if cfg.Priority != 0 {
		buf = append(buf, "Priority: "...)
		buf = strconv.AppendInt(buf, int64(cfg.Priority), 10)
		buf = append(buf, '\n')
	}
	if cfg.WantMassQuery {
		buf = append(buf, "WantMassQuery: 1\n"...)
	}
	return buf, nil
}

// UnmarshalText parses the configuration from a nix-cache-info file.
func (cfg *Configuration) UnmarshalText(data []byte) error {
	*cfg = Configuration{}
	for lineIdx, line := range bytes.Split(data, []byte{'\n'}) {
		lineno := lineIdx + 1
		i := bytes.IndexByte(line, ':')
		if i == -1 {
			return fmt.Errorf("unmarshal %s: line %d: missing ':'", CacheInfoName, lineno)
		}
		val := bytes.TrimSpace(line[i+1:])
		switch string(line[:i]) {
		case "StoreDir":
			cfg.StoreDir = Directory(val)
		case "Priority":
			var err error
			cfg.Priority, err = strconv.Atoi(string(val))
			if err != nil {
				return fmt.Errorf("unmarshal %s: line %d: Priority: %v", CacheInfoName, lineno, err)
			}
		case "WantMassQuery":
			cfg.WantMassQuery = len(val) == 1 && val[0] == '1'
		}
	}
	return nil
}
