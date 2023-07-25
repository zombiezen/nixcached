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
	"fmt"
	"io"
	"os/exec"
)

type Writer struct {
	cmd   *exec.Cmd
	stdin io.WriteCloser
}

type WriterOptions struct {
	// Executable specifies the program to run.
	// Defaults to "xz".
	Executable string
}

func NewWriter(w io.Writer, opts *WriterOptions) *Writer {
	exe := "xz"
	if opts != nil && opts.Executable != "" {
		exe = opts.Executable
	}
	xzw := &Writer{cmd: exec.Command(exe, "--compress")}
	xzw.cmd.Stdout = w
	return xzw
}

func (xzw *Writer) init() error {
	if xzw.stdin != nil {
		return nil
	}
	var err error
	xzw.stdin, err = xzw.cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("xz: %v", err)
	}
	if err := xzw.cmd.Start(); err != nil {
		xzw.cmd.Stdin = nil
		xzw.stdin.Close()
		xzw.stdin = nil
		return fmt.Errorf("xz: %v", err)
	}
	return nil
}

func (xzw *Writer) Write(p []byte) (n int, err error) {
	if err := xzw.init(); err != nil {
		return 0, err
	}
	return xzw.stdin.Write(p)
}

func (xzw *Writer) Close() error {
	err1 := xzw.stdin.Close()
	err2 := xzw.cmd.Wait()
	if err1 != nil {
		return fmt.Errorf("xz: %v", err1)
	}
	if err2 != nil {
		return fmt.Errorf("xz: %v", err2)
	}
	return nil
}
