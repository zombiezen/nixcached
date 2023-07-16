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
	"bytes"
	"compress/bzip2"
	"context"
	"io"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gocloud.dev/blob"
	"gocloud.dev/blob/memblob"
	"zombiezen.com/go/log/testlog"
	"zombiezen.com/go/nix"
	"zombiezen.com/go/nix/nar"
	"zombiezen.com/go/nix/nixbase32"
)

func TestDump(t *testing.T) {
	const fileContent = "Hello, World!\n"
	expectedNAR, expectedNAROffset, err := regularFileNARData([]byte(fileContent))
	if err != nil {
		t.Fatal(err)
	}
	narHasher := nix.NewHasher(nix.SHA256)
	narHasher.Write(expectedNAR)
	expectedNARHash := narHasher.SumHash()
	var digestBits [20]byte
	nix.CompressHash(digestBits[:], expectedNARHash.Bytes(nil))
	objectName := nixbase32.EncodeToString(digestBits[:]) + "-hello.txt"

	t.Run("WithoutListing", func(t *testing.T) {
		ctx := testlog.WithTB(context.Background(), t)
		storeDir := nix.StoreDirectory(t.TempDir())
		storePath, err := storeDir.Object(objectName)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(string(storePath), []byte(fileContent), 0o666); err != nil {
			t.Fatal(err)
		}

		buf := new(bytes.Buffer)
		gotInfo, gotListing, gotMD5, err := dump(ctx, buf, storePath, false)
		if err != nil {
			t.Error(err)
		}

		if gotInfo.StorePath != storePath {
			t.Errorf("info.StorePath = %q; want %q", gotInfo.StorePath, storePath)
		}
		if !gotInfo.NARHash.Equal(expectedNARHash) {
			t.Errorf("info.NARHash = %q; want %q", gotInfo.NARHash, expectedNARHash)
		}
		if want := int64(len(expectedNAR)); gotInfo.NARSize != want {
			t.Errorf("info.NARSize = %d; want %d", gotInfo.NARSize, want)
		}
		fileSHA256Hasher := nix.NewHasher(nix.SHA256)
		fileSHA256Hasher.Write(buf.Bytes())
		if want := fileSHA256Hasher.SumHash(); !gotInfo.FileHash.Equal(want) {
			t.Errorf("info.FileHash = %v; want %v", gotInfo.FileHash, want)
		}
		if want := int64(buf.Len()); gotInfo.FileSize != want {
			t.Errorf("info.FileSize = %d; want %d", gotInfo.FileSize, want)
		}

		if gotInfo.Compression != nix.Bzip2 {
			t.Errorf("info.Compression = %q; want %q", gotInfo.Compression, nix.Bzip2)
		} else {
			br := bzip2.NewReader(bytes.NewReader(buf.Bytes()))
			uncompressed, err := io.ReadAll(br)
			if err != nil {
				t.Error(err)
			} else if diff := cmp.Diff(expectedNAR, uncompressed); diff != "" {
				t.Errorf("NAR data (-want +got):\n%s", diff)
			}
		}

		if gotListing != nil {
			t.Errorf("ls = %#v; want <nil>", gotListing)
		}

		fileMD5Hasher := nix.NewHasher(nix.MD5)
		fileMD5Hasher.Write(buf.Bytes())
		if want := fileMD5Hasher.SumHash(); !gotMD5.Equal(want) {
			t.Errorf("md5Hash = %v; want %v", gotMD5, want)
		}
	})

	t.Run("Listing", func(t *testing.T) {
		ctx := testlog.WithTB(context.Background(), t)
		storeDir := nix.StoreDirectory(t.TempDir())
		storePath, err := storeDir.Object(objectName)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(string(storePath), []byte(fileContent), 0o666); err != nil {
			t.Fatal(err)
		}

		_, got, _, err := dump(ctx, io.Discard, storePath, true)
		if err != nil {
			t.Error(err)
		}

		want := &nar.Listing{
			Root: nar.ListingNode{
				Header: nar.Header{
					Mode:          0o444,
					Size:          int64(len(fileContent)),
					ContentOffset: expectedNAROffset,
				},
			},
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("ls (-want +got):\n%s", diff)
		}
	})
}

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

func regularFileNARData(data []byte) (narData []byte, contentOffset int64, err error) {
	buf := new(bytes.Buffer)
	nw := nar.NewWriter(buf)
	err = nw.WriteHeader(&nar.Header{
		Size: int64(len(data)),
	})
	if err != nil {
		return nil, 0, err
	}
	contentOffset = nw.Offset()
	if _, err := nw.Write(data); err != nil {
		return nil, 0, err
	}
	if err := nw.Close(); err != nil {
		return nil, 0, err
	}
	return buf.Bytes(), contentOffset, nil
}
