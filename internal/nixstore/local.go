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
	"io/fs"
	"net/url"
	"os"
	"os/exec"
	"sort"
	"strings"

	"golang.org/x/sys/unix"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
)

var _ interface {
	Store
	BatchNARInfoStore
} = (*Local)(nil)

const localDirListSize = 1000

// Local queries a local Nix store by invoking the Nix CLI.
type Local struct {
	// Directory is the store directory.
	// If empty, then [nix.DefaultStoreDirectory] is used.
	Directory nix.StoreDirectory
	// Executable is the path to the nix CLI to be used.
	// If empty, then "nix" is searched on the user's PATH.
	Executable string
	// Log is used to write the standard error stream from any CLI invocations.
	// A nil Log will discard logs.
	Log io.Writer
}

func (l *Local) exe() string {
	if l == nil || l.Executable == "" {
		return "nix"
	}
	return l.Executable
}

func (l *Local) dir() nix.StoreDirectory {
	if l == nil || l.Directory == "" {
		return nix.DefaultStoreDirectory
	}
	return l.Directory
}

// CacheInfo returns a synthetic cache info with the store directory.
func (l *Local) CacheInfo(ctx context.Context) (*nix.CacheInfo, error) {
	return &nix.CacheInfo{
		StoreDirectory: l.dir(),
		WantMassQuery:  true,
	}, nil
}

// List lists the contents of the store directory.
func (l *Local) List(ctx context.Context) ListIterator {
	dir, err := os.Open(string(l.dir()))
	if err != nil {
		return errorIterator{fmt.Errorf("list %s: %v", l.dir(), err)}
	}
	return &localListIterator{dir: dir}
}

// NARInfo invokes the Nix CLI to return information
// about the store object with the given digest.
func (l *Local) NARInfo(ctx context.Context, storePathDigest string) (*nix.NARInfo, error) {
	if err := validateDigest(storePathDigest); err != nil {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, err)
	}
	storePath, err := l.queryPathFromHashPart(ctx, storePathDigest)
	if err != nil {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, ErrNotFound)
	}
	infos, err := pathInfoFromCLI(ctx, l.newCommand, storePath.IsDerivation(), false, []string{string(storePath)})
	if err != nil {
		return nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}
	if len(infos) > 1 {
		return nil, fmt.Errorf("read nar info for %s: multiple results for %s", storePathDigest, storePath)
	}
	if len(infos) == 0 || infos[0].NARHash.IsZero() {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, ErrNotFound)
	}
	l.setInfoURL(infos[0])
	return infos[0], nil
}

// BatchNARInfo invokes the Nix CLI to return information
// about the store objects with the given digests.
func (l *Local) BatchNARInfo(ctx context.Context, storePathDigests []string) ([]*nix.NARInfo, error) {
	storeDir := l.dir()
	entries, err := os.ReadDir(string(storeDir))
	if err != nil {
		return nil, fmt.Errorf("read nar info: %v", err)
	}

	outputs := make([]string, 0, len(storePathDigests))
	derivations := make([]string, 0, len(storePathDigests))
	for _, digest := range storePathDigests {
		p, err := queryPathFromHashPart(storeDir, entries, true, digest)
		if err != nil {
			continue
		}
		if p.IsDerivation() {
			derivations = append(derivations, string(p))
		} else {
			outputs = append(outputs, string(p))
		}
	}
	infos, err := batchNARInfoFromCLI(ctx, l.newCommand, outputs, derivations)
	for _, info := range infos {
		l.setInfoURL(info)
	}
	return infos, err
}

func (l *Local) setInfoURL(info *nix.NARInfo) {
	info.URL = (&url.URL{
		Scheme: "file",
		Path:   string(info.StorePath),
	}).String()
}

func batchNARInfoFromCLI(ctx context.Context, f nixCommandFactory, outputs, derivations []string) ([]*nix.NARInfo, error) {
	result := make([]*nix.NARInfo, 0, len(outputs)+len(derivations))
	outputInfos, err := pathInfoFromCLI(ctx, f, false, false, outputs)
	if err != nil {
		return nil, fmt.Errorf("read nar info: %v", err)
	}
	for _, info := range outputInfos {
		if info.NARHash.IsZero() {
			continue
		}
		result = append(result, info)
	}
	derivationInfos, err := pathInfoFromCLI(ctx, f, true, false, derivations)
	if err != nil {
		return nil, fmt.Errorf("read nar info: %v", err)
	}
	for _, info := range derivationInfos {
		if info.NARHash.IsZero() {
			continue
		}
		result = append(result, info)
	}
	return result, nil
}

func (l *Local) queryPathFromHashPart(ctx context.Context, storePathDigest string) (nix.StorePath, error) {
	storeDir := l.dir()
	dir, err := os.Open(string(storeDir))
	if err != nil {
		return "", fmt.Errorf("query nix store path from hash part %q: %v", storePathDigest, err)
	}
	defer dir.Close()

	for {
		entries, err := dir.ReadDir(localDirListSize)
		if err != nil {
			if err == io.EOF {
				return "", fmt.Errorf("query nix store path from hash part %q: %w", storePathDigest, ErrNotFound)
			}
			return "", fmt.Errorf("query nix store path from hash part %q: %v", storePathDigest, err)
		}
		if p, err := queryPathFromHashPart(storeDir, entries, false, storePathDigest); err == nil {
			return p, nil
		}
	}
}

func queryPathFromHashPart(storeDir nix.StoreDirectory, entries []fs.DirEntry, sorted bool, storePathDigest string) (nix.StorePath, error) {
	if sorted {
		start := sort.Search(len(entries), func(i int) bool { return entries[i].Name() >= storePathDigest })
		end := start
		for end < len(entries) && strings.HasPrefix(entries[end].Name(), storePathDigest) {
			end++
		}
		entries = entries[start:end]
	}

	for _, ent := range entries {
		p, err := storeDir.Object(ent.Name())
		if err == nil && p.Digest() == storePathDigest {
			return p, nil
		}
	}
	return "", fmt.Errorf("query nix store path from hash part %q: %w", storePathDigest, ErrNotFound)
}

// Download dumps the store object as an uncompressed NAR archive.
func (l *Local) Download(ctx context.Context, w io.Writer, u *url.URL) error {
	if u.Scheme != "file" || u.Opaque != "" || !(u.Host == "" || u.Host == "localhost") || u.Fragment != "" || u.RawQuery != "" || u.Path == "" {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}
	storePath, sub, err := l.dir().ParsePath(u.Path)
	if err != nil || sub != "" {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}
	return nar.DumpPath(w, string(storePath))
}

// Query retrieves information about zero or more store objects.
// If a store path is known by Nix but is not present in the store,
// then a [NARInfo] that has StorePath populated will be in the resulting slice
// but [NARInfo.IsZero] will report true.
// If zero installables are given, then Query returns (nil, nil).
func (l *Local) Query(ctx context.Context, installables ...string) ([]*nix.NARInfo, error) {
	return pathInfoFromCLI(ctx, l.newCommand, false, false, installables)
}

// QueryRecursive retrieves information about
// the transitive closure of zero or more store objects
// as defined by their references.
// If a store path is known by Nix but is not present in the store,
// then a [NARInfo] that has StorePath populated will be in the resulting slice
// but [NARHash.IsZero] will report true.
// If zero installables are given, then QueryRecursive returns (nil, nil).
func (l *Local) QueryRecursive(ctx context.Context, installables ...string) ([]*nix.NARInfo, error) {
	return pathInfoFromCLI(ctx, l.newCommand, false, true, installables)
}

type nixCommandFactory func(ctx context.Context, args []string) *exec.Cmd

func (l *Local) newCommand(ctx context.Context, args []string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, l.exe(), args...)
	cmd.Env = append(os.Environ(), "NIX_STORE_DIR="+string(l.dir()))
	cmd.Cancel = func() error {
		return cmd.Process.Signal(unix.SIGTERM)
	}
	cmd.Stderr = l.Log
	return cmd
}

func pathInfoFromCLI(ctx context.Context, f nixCommandFactory, derivation bool, recursive bool, installables []string) ([]*nix.NARInfo, error) {
	if len(installables) == 0 {
		return nil, nil
	}

	args := []string{
		"--extra-experimental-features", "nix-command",
		"path-info", "--json",
	}
	if derivation {
		args = append(args, "--derivation")
	}
	if recursive {
		args = append(args, "--recursive")
	}
	args = append(args, "--")
	args = append(args, installables...)
	cmd := f(ctx, args)
	out := new(bytes.Buffer)
	cmd.Stdout = out
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
		CA         nix.ContentAddress
	}
	if err := json.Unmarshal(out.Bytes(), &parsedOutput); err != nil {
		return nil, fmt.Errorf("query nix store paths %s: parse output: %v", strings.Join(installables, " "), err)
	}
	result := make([]*nix.NARInfo, len(parsedOutput))
	for i := range parsedOutput {
		elem := &parsedOutput[i]
		result[i] = &nix.NARInfo{
			StorePath:   elem.Path,
			NARHash:     elem.NARHash,
			NARSize:     elem.NARSize,
			Compression: nix.NoCompression,
			FileHash:    elem.NARHash,
			FileSize:    elem.NARSize,
			Deriver:     elem.Deriver,
			References:  elem.References,
			Sig:         elem.Signatures,
			CA:          elem.CA,
		}
	}
	return result, nil
}

type errorIterator struct {
	err error
}

func (iter errorIterator) NextDigest(ctx context.Context) (string, error) {
	return "", iter.err
}

func (iter errorIterator) Close() error {
	return nil
}

type localListIterator struct {
	dir *os.File
	buf []fs.DirEntry
}

func (iter *localListIterator) NextDigest(ctx context.Context) (string, error) {
	for {
		if len(iter.buf) == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
			var err error
			iter.buf, err = iter.dir.ReadDir(localDirListSize)
			if err != nil {
				return "", err
			}
		}
		p, err := nix.DefaultStoreDirectory.Object(iter.buf[0].Name())
		iter.buf = iter.buf[1:]
		if err == nil {
			return p.Digest(), nil
		}
	}
}

func (iter *localListIterator) Close() error {
	return iter.dir.Close()
}
