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
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"golang.org/x/sys/unix"
	"zombiezen.com/go/nix"
)

// A Client queries and manipulates a local Nix store by invoking the nix CLI.
type Client struct {
	// Executable is the path to the nix CLI that the client will use.
	// If empty, then "nix" is searched on the user's PATH.
	Executable string
	// Log is used to write the standard error stream from any CLI invocations.
	// A nil Log will discard logs.
	Log io.Writer
}

func (c *Client) exe() string {
	if c == nil || c.Executable == "" {
		return "nix"
	}
	return c.Executable
}

// Query retrieves information about zero or more store objects.
// If a store path is known by Nix but is not present in the store,
// then a [NARInfo] that has StorePath populated will be in the resulting slice
// but [NARInfo.IsValid] will return false.
// If zero installables are given, then Query returns (nil, nil).
func (c *Client) Query(ctx context.Context, installables ...string) ([]*nix.NARInfo, error) {
	return c.query(ctx, false, installables)
}

// QueryRecursive retrieves information about
// the transitive closure of zero or more store objects
// as defined by their references.
// If a store path is known by Nix but is not present in the store,
// then a [NARInfo] that has StorePath populated will be in the resulting slice
// but [NARInfo.IsValid] will return false.
// If zero installables are given, then QueryRecursive returns (nil, nil).
func (c *Client) QueryRecursive(ctx context.Context, installables ...string) ([]*nix.NARInfo, error) {
	return c.query(ctx, true, installables)
}

func (c *Client) query(ctx context.Context, recursive bool, installables []string) ([]*nix.NARInfo, error) {
	if len(installables) == 0 {
		return nil, nil
	}

	args := []string{
		"--extra-experimental-features", "nix-command",
		"path-info", "--json",
	}
	if recursive {
		args = append(args, "--recursive")
	}
	args = append(args, "--")
	args = append(args, installables...)
	cmd := exec.CommandContext(ctx, c.exe(), args...)
	cmd.Cancel = func() error {
		return cmd.Process.Signal(unix.SIGTERM)
	}
	out := new(bytes.Buffer)
	cmd.Stdout = out
	cmd.Stderr = c.Log
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("query nix store paths %s: %v", strings.Join(installables, " "), err)
	}

	var parsedOutput []struct {
		Deriver    nix.StorePath
		NARHash    nix.Hash
		NARSize    int64
		Path       nix.StorePath
		References []nix.StorePath
		Signatures []*nix.Signature
	}
	if err := json.Unmarshal(out.Bytes(), &parsedOutput); err != nil {
		return nil, fmt.Errorf("query nix store paths %s: parse output: %v", strings.Join(installables, " "), err)
	}
	result := make([]*nix.NARInfo, len(parsedOutput))
	for i := range parsedOutput {
		elem := &parsedOutput[i]
		result[i] = &nix.NARInfo{
			StorePath:  elem.Path,
			NARHash:    elem.NARHash,
			NARSize:    elem.NARSize,
			Deriver:    elem.Deriver,
			References: elem.References,
			Sig:        elem.Signatures,
		}
	}
	return result, nil
}
