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
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net/url"
	slashpath "path"
	"strings"

	"github.com/dsnet/compress/brotli"
	"gocloud.dev/blob"
	"gocloud.dev/gcerrors"
	"zombiezen.com/go/log"
	"zombiezen.com/go/nix"
)

var _ Store = (*Bucket)(nil)

// A Bucket implements [Store] by reading from a [*blob.Bucket].
type Bucket struct {
	url    *url.URL
	bucket *blob.Bucket
}

// OpenBucket returns a new [Bucket] for the given URL.
// The caller is responsible for calling [Bucket.Close].
func OpenBucket(ctx context.Context, opener blob.BucketURLOpener, urlstr string) (*Bucket, error) {
	u, err := url.Parse(urlstr)
	if err != nil {
		return nil, fmt.Errorf("open nix store from bucket: %v", err)
	}
	if !u.IsAbs() {
		return nil, fmt.Errorf("open nix store from bucket %s: cannot use relative URLs", urlstr)
	}
	if u.Opaque != "" {
		return nil, fmt.Errorf("open nix store from bucket %s: cannot handle opaque URLs", urlstr)
	}
	b, err := opener.OpenBucketURL(ctx, u)
	if err != nil {
		return nil, fmt.Errorf("open nix store from bucket: %v", err)
	}
	u.Path = slashpath.Clean(u.Path)
	if u.Path == "/" {
		u.Path = ""
	}
	return &Bucket{
		url: &url.URL{
			Scheme: u.Scheme,
			Host:   u.Host,
			Path:   u.Path,
		},
		bucket: b,
	}, nil
}

// Close releases any resources associated with the bucket.
func (b *Bucket) Close() error {
	return b.bucket.Close()
}

// CacheInfo reads the nix-cache-info file in the bucket.
func (b *Bucket) CacheInfo(ctx context.Context) (*nix.CacheInfo, error) {
	data, err := b.bucket.ReadAll(ctx, nix.CacheInfoName)
	if gcerrors.Code(err) == gcerrors.NotFound {
		log.Debugf(ctx, "Bucket %s does not have a %s file", b.url, nix.CacheInfoName)
		return &nix.CacheInfo{
			StoreDirectory: nix.DefaultStoreDirectory,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s cache info: %v", b.url, err)
	}
	info := new(nix.CacheInfo)
	if err := info.UnmarshalText(data); err != nil {
		return nil, fmt.Errorf("read %s cache info: %v", b.url, err)
	}
	return info, nil
}

// List lists the ".narinfo" files at the root of the bucket.
func (b *Bucket) List(ctx context.Context) ListIterator {
	iterator := b.bucket.List(&blob.ListOptions{
		Delimiter: "/",
	})
	return &bucketListIterator{ctx: ctx, b: iterator}
}

// NARInfo reads the ".narinfo" file with the given digest at the root of the bucket.
func (b *Bucket) NARInfo(ctx context.Context, storePathDigest string) (*nix.NARInfo, error) {
	if err := validateDigest(storePathDigest); err != nil {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, err)
	}
	key := storePathDigest + nix.NARInfoExtension
	data, err := b.bucket.ReadAll(ctx, key)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return nil, fmt.Errorf("read nar info for %s: %w", storePathDigest, ErrNotFound)
	}
	if err != nil {
		return nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}
	info := new(nix.NARInfo)
	if err := info.UnmarshalText(data); err != nil {
		return nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}
	u, err := url.Parse(info.URL)
	if err != nil {
		return nil, fmt.Errorf("read nar info for %s: %v", storePathDigest, err)
	}
	info.URL = b.url.JoinPath(key).ResolveReference(u).String()
	return info, nil
}

// Download copies the named blob from the bucket.
func (b *Bucket) Download(ctx context.Context, w io.Writer, u *url.URL) error {
	key, ok := b.urlKey(u)
	if !ok {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}

	// TODO(soon): Some buckets can read Content-Encoding during read.
	attr, err := b.bucket.Attributes(ctx, key)
	if gcerrors.Code(err) == gcerrors.NotFound {
		return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("download nar %v: %v", u, err)
	}

	switch attr.ContentEncoding {
	case "":
		// Pass through.
		err := b.bucket.Download(ctx, key, w, nil)
		if gcerrors.Code(err) == gcerrors.NotFound {
			return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
	case "gzip":
		r, err := b.bucket.NewReader(ctx, key, nil)
		if gcerrors.Code(err) == gcerrors.NotFound {
			return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		defer r.Close()

		zr, err := gzip.NewReader(r)
		if err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		if _, err := io.Copy(w, zr); err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		return nil
	case "br":
		r, err := b.bucket.NewReader(ctx, key, nil)
		if gcerrors.Code(err) == gcerrors.NotFound {
			return fmt.Errorf("download nar %v: %w", u, ErrNotFound)
		}
		if err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		defer r.Close()

		br, err := brotli.NewReader(r, nil)
		if err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		if _, err := io.Copy(w, br); err != nil {
			return fmt.Errorf("download nar %v: %v", u, err)
		}
		return nil
	default:
		return fmt.Errorf("download nar %v: unknown Content-Encoding %q", u, attr.ContentEncoding)
	}

	return nil
}

func (b *Bucket) urlKey(u *url.URL) (key string, ok bool) {
	if u.Opaque != "" || u.Fragment != "" || u.RawQuery != "" {
		return "", false
	}
	if u.Scheme != b.url.Scheme || u.Host != b.url.Host {
		return "", false
	}
	prefix := b.url.Path + "/"
	return strings.CutPrefix(slashpath.Clean(u.Path), prefix)
}

type bucketListIterator struct {
	ctx context.Context
	b   *blob.ListIterator
}

func (iter *bucketListIterator) NextDigest(ctx context.Context) (string, error) {
	for {
		obj, err := iter.b.Next(iter.ctx)
		if err != nil {
			return "", err
		}
		if !obj.IsDir && isNARInfoPath(obj.Key) {
			return strings.TrimSuffix(obj.Key, nix.NARInfoExtension), nil
		}
	}
}

func (iter *bucketListIterator) Close() error {
	return nil
}
