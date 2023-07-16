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

package main

import (
	"context"
	"testing"

	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
)

func TestEnsureCacheInfo(t *testing.T) {
	const wantStoreDir nix.StoreDirectory = "/foo"

	t.Run("EmptyBucket", func(t *testing.T) {

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err != nil {
			t.Error(err)
		}

		data, err := bucket.ReadAll(ctx, nix.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		info := new(nix.CacheInfo)
		if err := info.UnmarshalText(data); err != nil {
			t.Fatal(err)
		}
		if info.StoreDirectory != wantStoreDir {
			t.Errorf("StoreDirectory = %q; want %q", info.StoreDirectory, wantStoreDir)
		}
	})

	t.Run("InfoExists", func(t *testing.T) {
		const existingData = "StoreDir: /foo\nPriority: 40\n"

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()
		err := bucket.WriteAll(ctx, nix.CacheInfoName, []byte(existingData), &blob.WriterOptions{
			ContentType: nix.CacheInfoMIMEType,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err != nil {
			t.Error(err)
		}

		got, err := bucket.ReadAll(ctx, nix.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != existingData {
			t.Errorf("%s = %q; want %q", nix.CacheInfoName, got, existingData)
		}
	})

	t.Run("ConflictingDirs", func(t *testing.T) {
		const existingData = "StoreDir: /bar\nPriority: 40\n"

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()
		err := bucket.WriteAll(ctx, nix.CacheInfoName, []byte(existingData), &blob.WriterOptions{
			ContentType: nix.CacheInfoMIMEType,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err == nil {
			t.Error(err)
		} else {
			t.Log("ensureCacheInfo:", err)
		}

		got, err := bucket.ReadAll(ctx, nix.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != existingData {
			t.Errorf("%s = %q; want %q", nix.CacheInfoName, got, existingData)
		}
	})
}
