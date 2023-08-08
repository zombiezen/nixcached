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
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"golang.org/x/sync/errgroup"
	"zombiezen.com/go/nix"
)

// A Store represents a collection of Nix store objects.
type Store interface {
	// CacheInfo returns the store's settings.
	CacheInfo(ctx context.Context) (*nix.CacheInfo, error)
	// List returns an iterator over the store's object names.
	List(ctx context.Context) ListIterator
	// NARInfo returns the metadata about the store object with the given digest.
	// The returned URL should be absolute.
	// If there is no such object, then NARInfo returns an error
	// for which errors.Is(err, ErrNotFound) reports true.
	NARInfo(ctx context.Context, storePathDigest string) (*nix.NARInfo, error)
	// Download downloads the NAR file at the given URL,
	// which may be compressed depending on the NARInfo.
	// If there is no such resource, then NARInfo returns an error
	// for which errors.Is(err, ErrNotFound) reports true.
	Download(ctx context.Context, w io.Writer, u *url.URL) error
}

// BatchNARInfoStore is a store that can efficiently query for multiple metadata
// in a single request.
// If a digest is not found in the store,
// then it will not be present in the resulting list
// but BatchNARInfo will not return an error.
type BatchNARInfoStore interface {
	Store

	BatchNARInfo(ctx context.Context, storePathDigests []string) ([]*nix.NARInfo, error)
}

// BatchNARInfo retrieves zero or more store object metadata.
// If the store implements [BatchNARInfoStore], then the BatchNARInfo method will be used.
// Otherwise, the metadata will be fetched using many calls to [Store.NARInfo]
// with at most maxConcurrency called concurrently.
func BatchNARInfo(ctx context.Context, store Store, storePathDigests []string, maxConcurrency int) ([]*nix.NARInfo, error) {
	if maxConcurrency < 1 {
		return nil, errors.New("read nar info: non-positive concurrency")
	}
	if len(storePathDigests) == 0 {
		return nil, nil
	}
	if b, ok := store.(BatchNARInfoStore); ok {
		return b.BatchNARInfo(ctx, storePathDigests)
	}

	grp, grpCtx := errgroup.WithContext(ctx)
	grp.SetLimit(maxConcurrency)

	var mu sync.Mutex
	result := make([]*nix.NARInfo, 0, len(storePathDigests))
	for _, digest := range storePathDigests {
		digest := digest
		grp.Go(func() error {
			info, err := store.NARInfo(grpCtx, digest)
			if errors.Is(err, ErrNotFound) {
				return nil
			}
			if err != nil {
				return err
			}

			mu.Lock()
			result = append(result, info)
			mu.Unlock()
			return nil
		})
	}

	err := grp.Wait()
	return result, err
}

// ErrNotFound is the error returned by various [Store] methods
// when a store object does not exist.
var ErrNotFound = errors.New("nix store object not found")

func validateDigest(digest string) error {
	p, err := nix.DefaultStoreDirectory.Object(digest + "-x")
	if err != nil || p.Digest() != digest || p.Name() != "x" {
		return fmt.Errorf("invalid digest %q", err)
	}
	return nil
}

// ListIterator is the interface for listing store objects.
// The caller is responsible for calling Close.
type ListIterator interface {
	// NextDigest returns the next store object digest or an error.
	// At the end of the list, the error will be [io.EOF].
	NextDigest(ctx context.Context) (string, error)
	// Close releases any resources associated with the iterator.
	Close() error
}

func isNARInfoPath(path string) bool {
	path = strings.TrimPrefix(path, "/")
	digest, hasExt := strings.CutSuffix(path, nix.NARInfoExtension)
	if !hasExt {
		return false
	}
	p, err := nix.DefaultStoreDirectory.Object(digest + "-x")
	return err == nil && p.Digest() == digest && p.Name() == "x"
}
