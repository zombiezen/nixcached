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
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net/url"
	"os/exec"
	"sort"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/sys/unix"
	"zombiezen.com/go/nix"
)

var _ interface {
	Store
	BatchNARInfoStore
} = (*SSH)(nil)

// SSH queries a remote store by invoking the OpenSSH client program.
type SSH struct {
	// Host is a "hostname[:port]" string.
	// It must be set.
	Host string
	// Username is an optional username to use.
	Username string
	// Directory is the store directory.
	// If empty, then [nix.DefaultStoreDirectory] is used.
	Directory nix.StoreDirectory
	// SSHExecutable is the path to the OpenSSH client to use.
	// If empty, then "ssh" is searched on the user's PATH.
	SSHExecutable string
	// Executable is the path to the nix CLI on the remote that will be used.
	// If empty, then "nix" is searched on the user's PATH.
	Executable string
	// StoreExecutable is the path to the nix-store CLI on the remote that will be used.
	// If empty, then "nix-store" is searched on the user's PATH.
	StoreExecutable string
	// Log is used to write the standard error stream from any CLI invocations.
	// A nil Log will discard logs.
	Log io.Writer
}

func (s *SSH) sshExe() string {
	if s.Executable == "" {
		return "ssh"
	}
	return s.SSHExecutable
}

func (s *SSH) nixExe() string {
	if s.Executable == "" {
		return "nix"
	}
	return s.Executable
}

func (s *SSH) nixStoreExe() string {
	if s.StoreExecutable == "" {
		return "nix-store"
	}
	return s.StoreExecutable
}

func (s *SSH) remoteURL(path string) *url.URL {
	u := &url.URL{
		Scheme: "ssh",
		Host:   s.Host,
		Path:   path,
	}
	if s.Username != "" {
		u.User = url.User(s.Username)
	}
	return u
}

func (s *SSH) sshDestination() string {
	hasBrackets := strings.ContainsAny(s.Host, "[]")
	if !hasBrackets && s.Username == "" {
		return s.Host
	}

	var dest string
	if s.Username != "" {
		dest += s.Username + "@"
	}
	if hasBrackets {
		u := s.remoteURL("")
		dest += u.Hostname() // strip IPv6 square brackets
		if port := u.Port(); port != "" {
			dest += ":" + port
		}
	} else {
		dest += s.Host
	}
	return dest
}

func (s *SSH) dir() nix.StoreDirectory {
	if s.Directory == "" {
		return nix.DefaultStoreDirectory
	}
	return s.Directory
}

// CacheInfo returns a synthetic cache info with the store directory.
func (s *SSH) CacheInfo(ctx context.Context) (*nix.CacheInfo, error) {
	return &nix.CacheInfo{
		StoreDirectory: s.dir(),
	}, nil
}

// List lists the contents of the store directory.
func (s *SSH) List(ctx context.Context) ListIterator {
	entries, err := s.list(ctx)
	if err != nil {
		return errorIterator{err}
	}
	return &memoryListIterator{entries}
}

func (s *SSH) list(ctx context.Context) ([]fs.DirEntry, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	storeDir := s.dir()
	cmd := exec.CommandContext(ctx, s.sshExe(), "-s", "--", s.sshDestination(), "sftp")
	cmd.Cancel = func() error {
		return cmd.Process.Signal(unix.SIGTERM)
	}
	cmd.Stderr = s.Log
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("list %v: %v", s.remoteURL(string(storeDir)), err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("list %v: %v", s.remoteURL(string(storeDir)), err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("list %v: %v", s.remoteURL(string(storeDir)), err)
	}
	defer func() {
		cancel()
		cmd.Wait()
	}()

	client, err := sftp.NewClientPipe(stdout, stdin)
	if err != nil {
		return nil, fmt.Errorf("list %v: %v", s.remoteURL(string(storeDir)), err)
	}
	defer client.Close()
	infos, err := client.ReadDir(string(storeDir))
	if err != nil {
		return nil, fmt.Errorf("list %v: %v", s.remoteURL(string(storeDir)), err)
	}
	entries := make([]fs.DirEntry, len(infos))
	for i, info := range infos {
		entries[i] = fs.FileInfoToDirEntry(info)
	}
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	return entries, nil
}

// NARInfo invokes the Nix CLI to return information
// about the store object with the given digest.
func (s *SSH) NARInfo(ctx context.Context, storePathDigest string) (*nix.NARInfo, error) {
	infos, err := s.BatchNARInfo(ctx, []string{storePathDigest})
	if err != nil {
		return nil, err
	}
	if len(infos) > 1 {
		return nil, fmt.Errorf("read nar info for %s: multiple results", storePathDigest)
	}
	if len(infos) == 0 || infos[0].NARHash.IsZero() {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, ErrNotFound)
	}
	return infos[0], nil
}

// BatchNARInfo invokes the Nix CLI to return information
// about the store objects with the given digests.
func (s *SSH) BatchNARInfo(ctx context.Context, storePathDigests []string) ([]*nix.NARInfo, error) {
	entries, err := s.list(ctx)
	if err != nil {
		return nil, fmt.Errorf("read nar info: %v", err)
	}

	outputs := make([]string, 0, len(storePathDigests))
	derivations := make([]string, 0, len(storePathDigests))
	storeDir := s.dir()
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
	infos, err := batchNARInfoFromCLI(ctx, s.newNixCommand, outputs, derivations)
	for _, info := range infos {
		s.setInfoURL(info)
	}
	return infos, err
}

func (s *SSH) setInfoURL(info *nix.NARInfo) {
	info.URL = s.remoteURL(string(info.StorePath)).String()
}

// Download dumps the store object as an uncompressed NAR archive.
func (s *SSH) Download(ctx context.Context, w io.Writer, u *url.URL) error {
	want := s.remoteURL("")
	if u.Scheme != want.Scheme || u.Opaque != "" || u.Host != want.Host || u.Fragment != "" || u.RawQuery != "" || u.Path == "" {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}
	storePath, sub, err := s.dir().ParsePath(u.Path)
	if err != nil || sub != "" {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}
	cmd := s.newNixStoreCommand(ctx, []string{
		"--dump", "--", string(storePath),
	})
	cmd.Stdout = w
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("download nar %v: %w", u, err)
	}
	return nil
}

func (s *SSH) newNixCommand(ctx context.Context, args []string) *exec.Cmd {
	return s.newCommand(ctx, s.nixExe(), args)
}

func (s *SSH) newNixStoreCommand(ctx context.Context, args []string) *exec.Cmd {
	return s.newCommand(ctx, s.nixStoreExe(), args)
}

func (s *SSH) newCommand(ctx context.Context, exe string, args []string) *exec.Cmd {
	commandArg := new(strings.Builder)
	commandArg.WriteString("NIX_STORE_DIR=")
	shellEscape(commandArg, string(s.dir()))
	commandArg.WriteString(" ")
	shellEscape(commandArg, exe)
	for _, arg := range args {
		commandArg.WriteString(" ")
		shellEscape(commandArg, arg)
	}

	cmd := exec.CommandContext(ctx, s.sshExe(), "--", s.sshDestination(), commandArg.String())
	cmd.Cancel = func() error {
		return cmd.Process.Signal(unix.SIGTERM)
	}
	cmd.Stderr = s.Log
	return cmd
}

// Upload uploads zero or more objects to the remote store.
func (s *SSH) Upload(ctx context.Context, objects []*ExportEntry) error {
	if len(objects) == 0 {
		return nil
	}
	// TODO(maybe): Pipe through gunzip.
	cmd := s.newNixStoreCommand(ctx, []string{"--import"})
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("upload to nix store %v: %v", s.remoteURL(""), err)
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("upload to nix store %v: %v", s.remoteURL(""), err)
	}

	w := bufio.NewWriter(stdin)
	var errors [4]error
	errors[0] = export(w, objects)
	errors[1] = w.Flush()
	errors[2] = stdin.Close()
	errors[3] = cmd.Wait()
	for _, err := range errors {
		if err != nil {
			return fmt.Errorf("upload to nix store %v: %v", s.remoteURL(""), err)
		}
	}
	return nil
}

type memoryListIterator struct {
	buf []fs.DirEntry
}

func (iter *memoryListIterator) NextDigest(ctx context.Context) (string, error) {
	for len(iter.buf) > 0 {
		p, err := nix.DefaultStoreDirectory.Object(iter.buf[0].Name())
		iter.buf = iter.buf[1:]
		if err == nil {
			return p.Digest(), nil
		}
	}
	return "", io.EOF
}

func (iter *memoryListIterator) Close() error {
	return nil
}

// shellEscape quotes s such that it can be used as a literal argument
// in a sh command line.
func shellEscape(sb *strings.Builder, s string) {
	if s == "" {
		sb.WriteString("''")
		return
	}

	safe := true
	newSize := 0
	const singleQuoteEscape = `'\''`
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			newSize += len(singleQuoteEscape)
		} else {
			newSize++
		}
		if !isShellSafe(s[i]) {
			safe = false
		}
	}
	if safe {
		sb.WriteString(s)
		return
	}
	sb.Grow(newSize + len("''"))
	sb.WriteByte('\'')
	for i := 0; i < len(s); i++ {
		if s[i] == '\'' {
			sb.WriteString(singleQuoteEscape)
		} else {
			sb.WriteByte(s[i])
		}
	}
	sb.WriteByte('\'')
}

func isShellSafe(b byte) bool {
	return b >= 'A' && b <= 'Z' ||
		b >= 'a' && b <= 'z' ||
		b >= '0' && b <= '9' ||
		b == '-' || b == '_' || b == '/' || b == '.'
}
