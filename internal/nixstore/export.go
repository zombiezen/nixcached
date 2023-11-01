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
	"encoding/binary"
	"fmt"
	"io"

	"zombiezen.com/go/nix"
)

type ExportEntry struct {
	StorePath  nix.StorePath
	NAR        io.Reader
	References []nix.StorePath
	Deriver    nix.StorePath
}

func export(w io.Writer, entries []*ExportEntry) error {
	var buf [8]byte
	for _, ent := range entries {
		binary.LittleEndian.PutUint64(buf[:], 1)
		if _, err := w.Write(buf[:]); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		if _, err := io.Copy(w, ent.NAR); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		copy(buf[:], "NIXE\x00\x00\x00\x00")
		if _, err := w.Write(buf[:]); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		if err := writeNARString(w, string(ent.StorePath)); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		binary.LittleEndian.PutUint64(buf[:], uint64(len(ent.References)))
		if _, err := w.Write(buf[:]); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		for _, ref := range ent.References {
			if err := writeNARString(w, string(ref)); err != nil {
				return fmt.Errorf("nix store export: %w", err)
			}
		}
		if err := writeNARString(w, string(ent.Deriver)); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
		binary.LittleEndian.PutUint64(buf[:], 0)
		if _, err := w.Write(buf[:]); err != nil {
			return fmt.Errorf("nix store export: %w", err)
		}
	}
	// Guaranteed that the buffer will be zero-filled.
	if _, err := w.Write(buf[:]); err != nil {
		return fmt.Errorf("nix store export: %w", err)
	}
	return nil
}

func writeNARString(w io.Writer, s string) error {
	const headerSize = 8
	const alignment = 8
	var buf [64]byte
	binary.LittleEndian.PutUint64(buf[:headerSize], uint64(len(s)))
	copy(buf[headerSize:], s)
	if len(s) <= len(buf)-headerSize {
		n := (len(s) + headerSize + (alignment - 1)) &^ (alignment - 1)
		_, err := w.Write(buf[:n])
		return err
	}

	if _, err := w.Write(buf[:]); err != nil {
		return err
	}
	s = s[len(buf)-headerSize:]
	if _, err := io.WriteString(w, s); err != nil {
		return err
	}
	padding := (^len(s) + 1) & (alignment - 1)
	if padding == 0 {
		return nil
	}
	for i := range buf[:padding] {
		buf[i] = 0
	}
	if _, err := w.Write(buf[:padding]); err != nil {
		return err
	}
	return nil
}
