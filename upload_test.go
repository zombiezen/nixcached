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
	"zombiezen.com/go/nixcached/internal/nixstore"
)

func TestEnsureCacheInfo(t *testing.T) {
	const wantStoreDir nixstore.Directory = "/foo"

	t.Run("EmptyBucket", func(t *testing.T) {

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err != nil {
			t.Error(err)
		}

		data, err := bucket.ReadAll(ctx, nixstore.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		cfg := new(nixstore.Configuration)
		if err := cfg.UnmarshalText(data); err != nil {
			t.Fatal(err)
		}
		if cfg.StoreDir != wantStoreDir {
			t.Errorf("StoreDir = %q; want %q", cfg.StoreDir, wantStoreDir)
		}
	})

	t.Run("InfoExists", func(t *testing.T) {
		const existingData = "StoreDir: /foo\nPriority: 40\n"

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()
		err := bucket.WriteAll(ctx, nixstore.CacheInfoName, []byte(existingData), &blob.WriterOptions{
			ContentType: nixstore.CacheInfoMIMEType,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err != nil {
			t.Error(err)
		}

		got, err := bucket.ReadAll(ctx, nixstore.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != existingData {
			t.Errorf("%s = %q; want %q", nixstore.CacheInfoName, got, existingData)
		}
	})

	t.Run("ConflictingDirs", func(t *testing.T) {
		const existingData = "StoreDir: /bar\nPriority: 40\n"

		ctx := testlog.WithTB(context.Background(), t)
		bucket := memblob.OpenBucket(nil)
		defer bucket.Close()
		err := bucket.WriteAll(ctx, nixstore.CacheInfoName, []byte(existingData), &blob.WriterOptions{
			ContentType: nixstore.CacheInfoMIMEType,
		})
		if err != nil {
			t.Fatal(err)
		}

		if err := ensureCacheInfo(ctx, bucket, wantStoreDir); err == nil {
			t.Error(err)
		} else {
			t.Log("ensureCacheInfo:", err)
		}

		got, err := bucket.ReadAll(ctx, nixstore.CacheInfoName)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != existingData {
			t.Errorf("%s = %q; want %q", nixstore.CacheInfoName, got, existingData)
		}
	})
}
