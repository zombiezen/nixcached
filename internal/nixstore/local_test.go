// Copyright 2024 Ross Light
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
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"zombiezen.com/go/nix"
)

func TestPathInfoFromCLI(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping due to -short")
	}

	ctx := context.Background()
	if _, err := os.Stat(string(nix.DefaultStoreDirectory)); os.IsNotExist(err) {
		t.Skipf("Nix not installed (%s does not exist). Skipping...", nix.DefaultStoreDirectory)
	} else if err != nil {
		t.Fatal(err)
	}

	const installable = "nixpkgs/d934204a0f8d9198e1e4515dd6fec76a139c87f0#legacyPackages.x86_64-linux.hello"
	l := &Local{Directory: nix.DefaultStoreDirectory}
	buildCmd := l.newCommand(ctx, []string{
		"build",
		"--out-link", filepath.Join(t.TempDir(), "result"),
		installable,
	})
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("nix build %s: %v", installable, err)
	}

	got, err := pathInfoFromCLI(ctx, l.newCommand, false, false, []string{installable})
	if err != nil {
		t.Fatal(err)
	}
	wantNARHash, err := nix.ParseHash("sha256-UdyaHNJXeDR1E1YTSQrL85gcxnIoje6kM/GVjrXjdhw=")
	if err != nil {
		t.Fatal(err)
	}
	const wantNARSize = 226560
	want := []*nix.NARInfo{{
		StorePath:   "/nix/store/63l345l7dgcfz789w1y93j1540czafqh-hello-2.12.1",
		Deriver:     "/nix/store/qb6j8v8z50shmrgsj2pk4fwrk2ff5jpn-hello-2.12.1.drv",
		Compression: nix.NoCompression,
		References: []nix.StorePath{
			"/nix/store/63l345l7dgcfz789w1y93j1540czafqh-hello-2.12.1",
			"/nix/store/cyrrf49i2hm1w7vn2j945ic3rrzgxbqs-glibc-2.38-44",
		},
		NARHash:  wantNARHash,
		NARSize:  wantNARSize,
		FileHash: wantNARHash,
		FileSize: wantNARSize,
	}}
	diff := cmp.Diff(
		want, got,
		cmpopts.IgnoreFields(nix.NARInfo{}, "URL", "System", "Sig", "CA"),
	)
	if diff != "" {
		t.Errorf("-want +got:\n%s", diff)
	}
}
