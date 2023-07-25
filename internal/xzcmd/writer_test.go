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

package xzcmd

import (
	"bytes"
	"os/exec"
	"testing"
)

func TestWriter(t *testing.T) {
	exe, err := exec.LookPath("xz")
	if err != nil {
		t.Skip("xz unavailable:", err)
	}

	buf := new(bytes.Buffer)
	xzw := NewWriter(buf, &WriterOptions{
		Executable: exe,
	})
	const payload = "Hello, World!\n"
	if n, err := xzw.Write([]byte(payload)); n != len(payload) || err != nil {
		t.Errorf("xzw.Write([]byte(%q)) = %d, %v; want %d, <nil>", payload, n, err, len(payload))
	}
	if err := xzw.Close(); err != nil {
		t.Errorf("xzw.Close() = %v; want <nil>", err)
	}

	magic := []byte{0xfd, 0x37, 0x7a, 0x58, 0x5a, 0x00}
	if !bytes.HasPrefix(buf.Bytes(), magic) {
		header := buf.Bytes()
		if len(header) > len(magic) {
			header = header[:len(magic)]
		}
		t.Errorf("output starts with %#x; want %#x", header, magic)
	}
}
